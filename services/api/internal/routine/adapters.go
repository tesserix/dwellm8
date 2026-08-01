package routine

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	leaseservice "github.com/tesserix/dwellm8/services/api/internal/lease/service"
	maintenancedomain "github.com/tesserix/dwellm8/services/api/internal/maintenance/domain"
	maintenanceservice "github.com/tesserix/dwellm8/services/api/internal/maintenance/service"
	moneyservice "github.com/tesserix/dwellm8/services/api/internal/money/service"
	"github.com/tesserix/dwellm8/services/api/internal/platform/automation"
	"github.com/tesserix/dwellm8/services/api/internal/platform/effective"
	"github.com/tesserix/dwellm8/services/api/internal/platform/events"
	tdsstore "github.com/tesserix/dwellm8/services/api/internal/platform/statutory/tds/store"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
)

// The adapters, and why they exist rather than the catalogue naming the services
// directly: the catalogue is the part worth testing, and a test that had to
// construct a lease service would need a database to assert an arithmetic
// decision about a ladder. Each of these is a shape change and nothing else.

// FromLeases adapts the lease module.
type FromLeases struct{ Leases *leaseservice.Leases }

// Live lists the tenancies that are running.
func (f FromLeases) Live(ctx context.Context, limit int) ([]Tenancy, error) {
	live, err := f.Leases.Live(ctx, limit)
	if err != nil {
		return nil, err
	}
	out := make([]Tenancy, 0, len(live))
	for _, l := range live {
		out = append(out, Tenancy{
			ID: l.ID, PropertyID: l.Property, UnitID: l.Unit, StartedOn: l.Term.From()})
	}
	return out, nil
}

// Expiring lists tenancies ending within `within` days.
func (f FromLeases) Expiring(ctx context.Context, within int) ([]ExpiringTenancy, error) {
	rows, err := f.Leases.Expiring(ctx, within)
	if err != nil {
		return nil, err
	}
	out := make([]ExpiringTenancy, 0, len(rows))
	for _, e := range rows {
		out = append(out, ExpiringTenancy{
			LeaseID: e.LeaseID, PropertyID: e.PropertyID, UnitID: e.UnitID,
			EndsOn: e.EndsOn, DaysRemaining: e.DaysRemaining,
			InsideNoticeWindow: e.InsideNoticeWindow,
		})
	}
	return out, nil
}

// PartiesOf returns the tenant and the owner as at a date.
func (f FromLeases) PartiesOf(ctx context.Context, leaseID string, on effective.Date) (string, string, error) {
	return f.Leases.PartiesOf(ctx, leaseID, on)
}

// FromMoney adapts the money module.
type FromMoney struct{ Statements *moneyservice.Statements }

// OutstandingMinor is what the tenant still owes: the receivable less any advance
// held against it, which is what Position already derives.
func (f FromMoney) OutstandingMinor(ctx context.Context, leaseID, partyID string) (int64, error) {
	s, err := f.Statements.Position(ctx, leaseID, partyID)
	if err != nil {
		return 0, err
	}
	owed := int64(s.Due)
	if owed < 0 {
		return 0, nil // in credit, which is not arrears
	}
	return owed, nil
}

// LastChargedOn is the day the most recent charge was raised.
func (f FromMoney) LastChargedOn(ctx context.Context, leaseID string) (effective.Date, error) {
	t, err := f.Statements.BilledThrough(ctx, leaseID)
	if err != nil || t.IsZero() {
		return effective.Date{}, err
	}
	return effective.DateOf(t, t.Location()), nil
}

// FromChecklists adapts the maintenance module. ADR-0032.
type FromChecklists struct {
	Checklists *maintenanceservice.Checklists
}

// Start fires a process and returns the checklist's id.
//
// An already-open process comes back rather than erroring, which is the service's
// own behaviour and exactly what an automation wants: firing a move-out that a
// manager already started is not a failure, it is the automation finding the work
// underway.
func (f FromChecklists) Start(ctx context.Context, process, propertyKind, propertyID, unitID, leaseID string,
	anchor effective.Date) (string, error) {

	c, err := f.Checklists.Start(ctx,
		maintenancedomain.Process(process), maintenancedomain.PropertyKind(propertyKind),
		maintenancedomain.Subject{PropertyID: propertyID, UnitID: unitID, LeaseID: leaseID},
		anchor, "")
	if err != nil {
		return "", err
	}
	return c.ID, nil
}

// FromCertificates adapts the statutory certificate store.
type FromCertificates struct{ Certificates *tdsstore.Certificates }

// ExpiringCertificates lists lower-deduction certificates lapsing soon.
func (f FromCertificates) ExpiringCertificates(ctx context.Context, on effective.Date, within int) ([]ExpiringCertificate, error) {
	rows, err := f.Certificates.Expiring(ctx, on, within)
	if err != nil {
		return nil, err
	}
	out := make([]ExpiringCertificate, 0, len(rows))
	for _, c := range rows {
		out = append(out, ExpiringCertificate{
			PartyID: c.PartyID, CertificateNumber: c.CertificateNumber,
			Section: string(c.Section), ValidTo: c.ValidTo, DaysRemaining: c.DaysRemaining,
		})
	}
	return out, nil
}

// Outbox publishes what an automation decided. ADR-0002 §3.
//
// The event and nothing else: whether anybody is told is the notify module's
// question (dwellm8#126), and an automation that reached for a channel here would
// be the one place in the product where a message is sent outside the outbox.
type Outbox struct{ Pool tenancy.Pool }

// Publish appends one event.
//
// Its own transaction, unlike every other Append in the codebase, and the reason
// is worth stating: an automation's "state change" happened in another module's
// transaction that has already committed, so there is nothing here to commit
// alongside. The run log is what makes the pair recoverable — a published event
// with no run row is repeated on the next pass, and a run row with no event is the
// crash window ADR-0033 names.
func (o Outbox) Publish(ctx context.Context, typ string, subject automation.Subject, data map[string]any) error {
	tenant, ok := tenancy.From(ctx)
	if !ok {
		return tenancy.ErrNoTenant
	}
	env, err := events.New(typ, tenant.String(),
		events.Subject{Kind: string(subject.Kind), ID: subject.ID},
		events.Actor{Kind: events.ActorSystem}, data)
	if err != nil {
		return err
	}
	return tenancy.Scoped(ctx, o.Pool, func(ctx context.Context, tx pgx.Tx) error {
		return events.Append(ctx, tx, env)
	})
}

// Today is the ordinary clock, in the zone the product's dates are in.
//
// India, explicitly: a run at 23:30 UTC is already tomorrow in Bengaluru, and an
// automation that computed "today" in UTC would date a reminder to the day before
// the one the manager reading it is living in.
func Today() effective.Date {
	loc, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		loc = time.FixedZone("IST", 5*3600+1800)
	}
	return effective.DateOf(time.Now().In(loc), loc)
}

// Ensure the adapters satisfy the catalogue's interfaces at compile time rather
// than at the first run of a CronJob nobody is watching.
var (
	_ Tenancies  = FromLeases{}
	_ Money      = FromMoney{}
	_ Checklists = FromChecklists{}
	_ Compliance = FromCertificates{}
	_ Events     = Outbox{}
	_ Clock      = Today
)
