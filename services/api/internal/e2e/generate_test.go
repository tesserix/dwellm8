package e2e_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/tesserix/dwellm8/services/api/internal/e2e"
	leasedomain "github.com/tesserix/dwellm8/services/api/internal/lease/domain"
	leaseservice "github.com/tesserix/dwellm8/services/api/internal/lease/service"
	moneyservice "github.com/tesserix/dwellm8/services/api/internal/money/service"
	"github.com/tesserix/dwellm8/services/api/internal/platform/effective"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
)

// The run's own behaviour, without a database: what it does when one tenancy in
// the middle of the list cannot be billed.

// orgs is the enumeration a run starts from.
type orgs struct{ ids []tenancy.ID }

func (o *orgs) Active(context.Context) ([]tenancy.ID, error) { return o.ids, nil }

// tenancies answers per organisation, so the run's loop is observable: it
// records which organisation each call was scoped to.
type tenancies struct {
	leases []leasedomain.Lease
	seen   []tenancy.ID
}

func (t *tenancies) Billable(ctx context.Context, _ int) ([]leasedomain.Lease, error) {
	id, ok := tenancy.From(ctx)
	if !ok {
		return nil, errors.New("the run did not scope this call to an organisation")
	}
	t.seen = append(t.seen, id)
	return t.leases, nil
}

type charges struct{ failFor string }

func (c *charges) Billable(_ context.Context, l leasedomain.Lease, _ effective.Date) ([]leaseservice.Charge, error) {
	if l.ID == c.failFor {
		return nil, errors.New("this tenancy has no recorded owner")
	}
	return []leaseservice.Charge{{LeaseID: l.ID, FullAmountMinor: 100, Days: 1, InPeriod: 1}}, nil
}

type biller struct{ billed int }

func (b *biller) Bill(_ context.Context, cs []leaseservice.Charge) (moneyservice.Run, error) {
	b.billed += len(cs)
	return moneyservice.Run{Raised: len(cs), TotalMinor: 100}, nil
}

// One bad tenancy does not cost the rest their month.
func TestOneUnbillableTenancyDoesNotStopTheRun(t *testing.T) {
	list := &tenancies{leases: []leasedomain.Lease{{ID: "a"}, {ID: "broken"}, {ID: "c"}}}
	b := &biller{}
	run := e2e.NewBillingRun(&orgs{ids: []tenancy.ID{"org-1", "org-2"}}, list, &charges{failFor: "broken"}, b,
		slog.New(slog.NewTextHandler(io.Discard, nil)))

	out, err := run.Run(context.Background(), effective.Day(2029, 9, 30), 0)
	if err != nil {
		t.Fatalf("the run stopped: %v", err)
	}
	// Two organisations, each with the three tenancies the fixture returns.
	if out.Organisations != 2 || out.Tenancies != 6 {
		t.Errorf("the run covered %d organisations and %d tenancies, want 2 and 6",
			out.Organisations, out.Tenancies)
	}
	if out.Raised != 4 || out.Failed != 2 {
		t.Errorf("the run produced %+v, want four raised and two failed — a month's rent must not "+
			"go unbilled because of one bad row", out)
	}
	if b.billed != 4 {
		t.Errorf("the biller saw %d charges", b.billed)
	}

	// Every read happened inside an organisation's session, and each was its own.
	// A run that read across the boundary would be a run where one wrong join is
	// an invoice in somebody else's ledger.
	if len(list.seen) != 2 || list.seen[0] == list.seen[1] {
		t.Errorf("the run scoped its reads to %v, want one session per organisation", list.seen)
	}
}
