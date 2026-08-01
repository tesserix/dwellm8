// Package routine is the prebuilt automation catalogue. ADR-0033 §5, dwellm8#200.
//
// It is a seam rather than a module, the shape internal/e2e established for the
// billing run: coordination above the modules, expressed as narrow interfaces the
// modules satisfy, so the lease module does not acquire a dependency on money to
// have its arrears chased.
//
// Every automation here is the same shape and the shape is the point. It finds the
// subjects it cares about, and for each one it calls Propose — never a module
// service directly. Proposing is what lets the engine decide between acting,
// asking for approval and skipping something it has already done, and an
// automation that reached past it would be one where the ceiling did not apply.
//
// # What these do, and what they do not
//
// Two of them propose a reminder. A reminder here is an event on the outbox and a
// row in the run log; nothing is delivered, because the notify module is unbuilt
// and delivery is dwellm8#126. An automation that pretended to send a message
// would be the worst option available — the run log would say a tenant was chased
// and no tenant would have been.
package routine

import (
	"context"
	"fmt"

	"github.com/tesserix/dwellm8/services/api/internal/platform/automation"
	"github.com/tesserix/dwellm8/services/api/internal/platform/effective"
)

// Clock is today, injected so a run is reproducible in a test. Everything here is
// dated, and a catalogue that read time.Now() directly could only be tested by
// waiting.
type Clock func() effective.Date

// Deps is what the catalogue needs from the modules. Interfaces rather than the
// services themselves, so this package can be exercised without a database and so
// the dependency is a list somebody reads rather than an import graph.
type Deps struct {
	Now        Clock
	Tenancies  Tenancies
	Money      Money
	Checklists Checklists
	Compliance Compliance
	Events     Events
}

// Tenancies is the lease module's slice: what is running, what is running out,
// and who is on it.
type Tenancies interface {
	Live(ctx context.Context, limit int) ([]Tenancy, error)
	Expiring(ctx context.Context, within int) ([]ExpiringTenancy, error)
	PartiesOf(ctx context.Context, leaseID string, on effective.Date) (tenant, owner string, err error)
}

// Tenancy is a live tenancy, as an automation needs it.
type Tenancy struct {
	ID         string
	PropertyID string
	UnitID     string
	StartedOn  effective.Date
}

// ExpiringTenancy is a tenancy running out.
type ExpiringTenancy struct {
	LeaseID            string
	PropertyID         string
	UnitID             string
	EndsOn             effective.Date
	DaysRemaining      int
	InsideNoticeWindow bool
}

// Money is the money module's slice: what a tenancy owes, and since when.
type Money interface {
	OutstandingMinor(ctx context.Context, leaseID, partyID string) (int64, error)
	// LastChargedOn is the day the most recent charge was raised on this tenancy,
	// or zero when none has been. See the note on the ladder's rungs for why this
	// is the ageing signal rather than the oldest unpaid charge.
	LastChargedOn(ctx context.Context, leaseID string) (effective.Date, error)
}

// Checklists is the maintenance module's slice: fire a process. ADR-0032.
type Checklists interface {
	Start(ctx context.Context, process, propertyKind, propertyID, unitID, leaseID string,
		anchor effective.Date) (string, error)
}

// Compliance is the statutory slice: what lapses soon.
type Compliance interface {
	ExpiringCertificates(ctx context.Context, on effective.Date, within int) ([]ExpiringCertificate, error)
}

// ExpiringCertificate is a lower-deduction certificate running out. A landlord
// whose certificate lapses is deducted at the full rate from the next payment,
// and finds out on the payout rather than in time to renew it.
type ExpiringCertificate struct {
	PartyID           string
	CertificateNumber string
	Section           string
	ValidTo           effective.Date
	DaysRemaining     int
}

// Events publishes what an automation decided, for anything that later delivers
// it. The seam is deliberately this narrow: the catalogue says what happened and
// never how somebody is told.
type Events interface {
	Publish(ctx context.Context, typ string, subject automation.Subject, data map[string]any) error
}

// Catalogue returns the prebuilt automations, wired to the modules.
//
// Order is the order the settings screen shows them and the order a scheduled run
// works through them: the money first, because an arrears reminder that goes out
// after a renewal reminder reads as an afterthought.
func Catalogue(d Deps) automation.Catalogue {
	return automation.Catalogue{
		arrearsLadder(d),
		leaseExpiryReminder(d),
		renewalKickoff(d),
		inspectionScheduling(d),
		complianceRenewal(d),
		moveInChecklist(d),
		moveOutChecklist(d),
		ownerOnboardingChecklist(d),
	}
}

// arrearsLadder chases a tenancy that owes money, once per rung.
//
// A ladder rather than a daily message, and the rungs are parameters: an
// organisation that chases on day 3, 7 and 14 and one that chases on day 5 and 30
// are the same automation with different numbers, and the difference between them
// is a settings row rather than a release.
//
// The idempotency key is the rung, not the day. That is what stops a tenancy in
// arrears for two months receiving sixty reminders, and it is why the key carries
// the period as well: a tenancy that clears its arrears and falls behind again
// next month starts the ladder over.
func arrearsLadder(d Deps) automation.Definition {
	return automation.Definition{
		Key:              "arrears_ladder",
		Name:             "Arrears follow-up",
		Purpose:          "Chases a tenancy that has fallen behind, once at each step of the ladder.",
		Trigger:          automation.TriggerSchedule,
		EnabledByDefault: true,
		Params: []automation.Param{
			{Name: "first_reminder_after", Purpose: "Days overdue before the first reminder",
				Unit: "days", Default: 3, Min: 1, Max: 30},
			{Name: "second_reminder_after", Purpose: "Days overdue before the second",
				Unit: "days", Default: 7, Min: 2, Max: 60},
			{Name: "final_reminder_after", Purpose: "Days overdue before the final reminder",
				Unit: "days", Default: 14, Min: 3, Max: 120},
			{Name: "minimum_arrears_minor", Purpose: "Below this, nobody is chased",
				Unit: "paise", Default: 100_00, Min: 0, Max: 100_000_00},
		},
		Act: func(ctx context.Context, r *automation.Run) error {
			today := d.Now()
			tenancies, err := d.Tenancies.Live(ctx, 0)
			if err != nil {
				return fmt.Errorf("listing live tenancies: %w", err)
			}
			rungs := []struct {
				name string
				days int
			}{
				{"first", r.Days("first_reminder_after")},
				{"second", r.Days("second_reminder_after")},
				{"final", r.Days("final_reminder_after")},
			}
			floor := r.Param("minimum_arrears_minor")

			for _, t := range tenancies {
				tenant, _, err := d.Tenancies.PartiesOf(ctx, t.ID, today)
				if err != nil {
					// A tenancy with nobody on it as of today is a tenancy starting
					// next month. Not an error, and not something to chase.
					continue
				}
				owed, err := d.Money.OutstandingMinor(ctx, t.ID, tenant)
				if err != nil {
					return fmt.Errorf("reading what %s owes: %w", t.ID, err)
				}
				if owed < floor {
					continue
				}
				chargedOn, err := d.Money.LastChargedOn(ctx, t.ID)
				if err != nil {
					return fmt.Errorf("reading when %s was last charged: %w", t.ID, err)
				}
				if chargedOn.Zero() {
					continue // owes money against no charge — not something a ladder fixes
				}
				overdue := daysBetween(chargedOn, today)

				// The highest rung this tenancy has reached. Only that one is
				// proposed: climbing two rungs in one pass would send two messages
				// to somebody who missed a single run.
				reached := -1
				for i, rung := range rungs {
					if overdue >= rung.days {
						reached = i
					}
				}
				if reached < 0 {
					continue
				}
				rung := rungs[reached]

				subject := automation.Subject{Kind: automation.SubjectLease, ID: t.ID}
				period := today.String()[:7] // the month the ladder is about
				_, err = r.Propose(ctx, automation.Proposal{
					Subject: subject,
					Action:  "arrears.reminder." + rung.name,
					Detail: fmt.Sprintf("%d paise outstanding, %d days since the last charge, at the %s reminder",
						owed, overdue, rung.name),
					Key: fmt.Sprintf("%s:%s:%s", t.ID, period, rung.name),
					Do: func(ctx context.Context) error {
						return d.Events.Publish(ctx, "notify.arrears.reminded", subject, caused(r, map[string]any{
							"lease_id": t.ID, "party_id": tenant,
							"outstanding_minor": owed, "step": rung.name,
						}))
					},
				})
				if err != nil {
					return err
				}
			}
			return nil
		},
	}
}

// The rungs are measured from the most recent charge rather than from the oldest
// unpaid one, and the difference is worth stating because it is a simplification.
//
// True ageing needs an allocation of payments to charges, which is the derived
// bucket ADR-0012 §7 argues for and nothing computes yet. What the ledger can
// answer today is "what is outstanding" and "when was the last charge raised", and
// their conjunction — still owing, N days after the last invoice — is a sound and
// conservative rung: it never chases somebody earlier than a true ageing would,
// because the most recent charge is never older than the oldest unpaid one.
//
// The cost is a tenancy that has been behind for months but was invoiced again
// yesterday drops back to the first rung. That is the safe direction, and the fix
// is an ageing reader (dwellm8#180) rather than a different ladder.

// leaseExpiryReminder tells an owner a tenancy is running out, while there is
// still time to serve notice.
//
// The window is the notice period rather than a fixed number of days, because a
// tenancy with ninety days' notice and one with thirty need the reminder at
// different times and an owner who is told too late has lost the option.
func leaseExpiryReminder(d Deps) automation.Definition {
	return automation.Definition{
		Key:              "lease_expiry_reminder",
		Name:             "Lease expiry reminder",
		Purpose:          "Tells the owner a tenancy is ending while there is still time to act.",
		Trigger:          automation.TriggerSchedule,
		EnabledByDefault: true,
		Params: []automation.Param{
			{Name: "remind_within", Purpose: "How far ahead to look",
				Unit: "days", Default: 60, Min: 7, Max: 180},
		},
		Act: func(ctx context.Context, r *automation.Run) error {
			expiring, err := d.Tenancies.Expiring(ctx, r.Days("remind_within"))
			if err != nil {
				return fmt.Errorf("listing expiring tenancies: %w", err)
			}
			for _, e := range expiring {
				subject := automation.Subject{Kind: automation.SubjectLease, ID: e.LeaseID}
				detail := fmt.Sprintf("ends %s, %d days away", e.EndsOn, e.DaysRemaining)
				if e.InsideNoticeWindow {
					detail += " — inside the notice window"
				}
				if _, err := r.Propose(ctx, automation.Proposal{
					Subject: subject,
					Action:  "lease.expiry.reminded",
					Detail:  detail,
					// Keyed on the end date rather than on today, so a tenancy whose
					// term is extended gets a fresh reminder and one that is not gets
					// exactly one.
					Key: fmt.Sprintf("%s:%s", e.LeaseID, e.EndsOn),
					Do: func(ctx context.Context) error {
						return d.Events.Publish(ctx, "notify.expiry.reminded", subject, caused(r, map[string]any{
							"lease_id": e.LeaseID, "ends_on": e.EndsOn.String(),
							"days_remaining": e.DaysRemaining,
							"inside_notice":  e.InsideNoticeWindow,
						}))
					},
				}); err != nil {
					return err
				}
			}
			return nil
		},
	}
}

// renewalKickoff fires ADR-0032's renewal checklist before a term ends.
//
// Separate from the reminder above and deliberately later: a reminder is a message
// and a checklist is work with owners and due dates, and firing the second at the
// moment of the first would put a renewal on somebody's list two months before
// anybody has decided whether to renew.
func renewalKickoff(d Deps) automation.Definition {
	return automation.Definition{
		Key:              "renewal_kickoff",
		Name:             "Renewal checklist",
		Purpose:          "Starts the renewal process for a tenancy approaching its end.",
		Trigger:          automation.TriggerSchedule,
		EnabledByDefault: true,
		Params: []automation.Param{
			{Name: "start_within", Purpose: "How close to the end the process starts",
				Unit: "days", Default: 45, Min: 14, Max: 120},
		},
		Act: func(ctx context.Context, r *automation.Run) error {
			expiring, err := d.Tenancies.Expiring(ctx, r.Days("start_within"))
			if err != nil {
				return fmt.Errorf("listing expiring tenancies: %w", err)
			}
			for _, e := range expiring {
				subject := automation.Subject{Kind: automation.SubjectLease, ID: e.LeaseID}
				if _, err := r.Propose(ctx, automation.Proposal{
					Subject: subject,
					Action:  "checklist.tenancy_renewal",
					Detail:  fmt.Sprintf("term ends %s", e.EndsOn),
					Key:     fmt.Sprintf("%s:%s:renewal", e.LeaseID, e.EndsOn),
					Do: func(ctx context.Context) error {
						// Anchored on the end of the term, so every step's offset is
						// measured from the date the work is actually about.
						_, err := d.Checklists.Start(ctx, "tenancy_renewal", "",
							e.PropertyID, e.UnitID, e.LeaseID, e.EndsOn)
						return err
					},
				}); err != nil {
					return err
				}
			}
			return nil
		},
	}
}

// inspectionScheduling raises a routine inspection on a tenancy that has not had
// one for long enough, with the notice a tenant is owed built into the anchor.
//
// The notice is the part that matters. An inspection scheduled for tomorrow is an
// inspection a tenant can refuse, so the checklist is anchored far enough ahead
// that the notice step falls due before the visit rather than after it.
func inspectionScheduling(d Deps) automation.Definition {
	return automation.Definition{
		Key:              "inspection_scheduling",
		Name:             "Routine inspections",
		Purpose:          "Schedules a periodic inspection with the notice the tenant is owed.",
		Trigger:          automation.TriggerSchedule,
		EnabledByDefault: true,
		Params: []automation.Param{
			{Name: "every", Purpose: "How often a tenancy is inspected",
				Unit: "days", Default: 180, Min: 30, Max: 730},
			{Name: "notice_days", Purpose: "Notice given before the visit",
				Unit: "days", Default: 14, Min: 1, Max: 90},
		},
		Act: func(ctx context.Context, r *automation.Run) error {
			today := d.Now()
			every, notice := r.Days("every"), r.Days("notice_days")
			tenancies, err := d.Tenancies.Live(ctx, 0)
			if err != nil {
				return fmt.Errorf("listing live tenancies: %w", err)
			}

			for _, t := range tenancies {
				// Which cycle this tenancy is in, counted from its own start rather
				// than from the calendar: inspecting every tenancy in the portfolio
				// on the same fortnight is a week nobody can staff.
				elapsed := daysBetween(t.StartedOn, today)
				if elapsed < every {
					continue
				}
				cycle := elapsed / every
				subject := automation.Subject{Kind: automation.SubjectLease, ID: t.ID}
				visit := today.AddDays(notice)

				if _, err := r.Propose(ctx, automation.Proposal{
					Subject: subject,
					Action:  "inspection.scheduled",
					Detail:  fmt.Sprintf("cycle %d, visit on or after %s", cycle, visit),
					Key:     fmt.Sprintf("%s:inspection:%d", t.ID, cycle),
					Do: func(ctx context.Context) error {
						return d.Events.Publish(ctx, "notify.inspection.scheduled", subject, caused(r, map[string]any{
							"lease_id": t.ID, "unit_id": t.UnitID,
							"visit_on_or_after": visit.String(), "notice_days": notice,
						}))
					},
				}); err != nil {
					return err
				}
			}
			return nil
		},
	}
}

// complianceRenewal raises a lower-deduction certificate that is running out.
//
// This is the compliance item the product actually holds today. A lapsed section
// 197 certificate means the next payout deducts at the full rate, which the
// landlord discovers from the payout rather than in time to renew — and the
// renewal has to be applied for weeks in advance, so a reminder on the day it
// lapses is worth nothing.
//
// The property-level register — fire NOC, lift certificate, society dues — has no
// table yet. When it lands, it is a second Compliance reader and a second block
// here rather than a different design.
func complianceRenewal(d Deps) automation.Definition {
	return automation.Definition{
		Key:              "compliance_renewal",
		Name:             "Compliance renewal",
		Purpose:          "Raises a statutory certificate that is about to lapse, while it can still be renewed.",
		Trigger:          automation.TriggerSchedule,
		EnabledByDefault: true,
		Params: []automation.Param{
			{Name: "remind_within", Purpose: "How far ahead to look",
				Unit: "days", Default: 45, Min: 7, Max: 180},
		},
		Act: func(ctx context.Context, r *automation.Run) error {
			today := d.Now()
			expiring, err := d.Compliance.ExpiringCertificates(ctx, today, r.Days("remind_within"))
			if err != nil {
				return fmt.Errorf("listing expiring certificates: %w", err)
			}
			for _, c := range expiring {
				subject := automation.Subject{Kind: automation.SubjectOrganisation, ID: c.PartyID}
				if _, err := r.Propose(ctx, automation.Proposal{
					Subject: subject,
					Action:  "compliance.certificate_expiring",
					Detail: fmt.Sprintf("%s certificate %s lapses %s, %d days away",
						c.Section, c.CertificateNumber, c.ValidTo, c.DaysRemaining),
					Key: fmt.Sprintf("%s:%s", c.CertificateNumber, c.ValidTo),
					Do: func(ctx context.Context) error {
						return d.Events.Publish(ctx, "notify.compliance.reminded", subject, caused(r, map[string]any{
							"party_id": c.PartyID, "certificate": c.CertificateNumber,
							"section": c.Section, "valid_to": c.ValidTo.String(),
							"days_remaining": c.DaysRemaining,
						}))
					},
				}); err != nil {
					return err
				}
			}
			return nil
		},
	}
}

// The three event-triggered ones. A tenancy going live is a move-in, a notice
// being served is a move-out, and an organisation being created is an onboarding —
// each fires the ADR-0032 checklist that matches, once.
//
// Event-triggered rather than scheduled because the trigger is a fact rather than
// a date: a scheduled version would have to ask "which tenancies went live since
// the last run", which is the outbox's job and is already done.

func moveInChecklist(d Deps) automation.Definition {
	return checklistOn(d, automation.Definition{
		Key:              "move_in_checklist",
		Name:             "Move-in checklist",
		Purpose:          "Starts the move-in process when a tenancy goes live.",
		On:               "lease.tenancy.started",
		EnabledByDefault: true,
	}, "move_in")
}

func moveOutChecklist(d Deps) automation.Definition {
	return checklistOn(d, automation.Definition{
		Key:              "move_out_checklist",
		Name:             "Move-out checklist",
		Purpose:          "Starts the move-out process when notice is served, so the exit steps exist before the tenancy ends.",
		On:               "lease.notice.served",
		EnabledByDefault: true,
	}, "move_out")
}

func ownerOnboardingChecklist(d Deps) automation.Definition {
	return checklistOn(d, automation.Definition{
		Key:              "owner_onboarding_checklist",
		Name:             "Owner onboarding",
		Purpose:          "Starts the onboarding process when an organisation is created.",
		On:               "identity.organisation.created",
		EnabledByDefault: true,
	}, "owner_onboarding")
}

// checklistOn is the shared body of the three: read the subject out of the event
// and fire the process, once per subject.
func checklistOn(d Deps, def automation.Definition, process string) automation.Definition {
	def.Trigger = automation.TriggerEvent
	def.Act = func(ctx context.Context, r *automation.Run) error {
		e := r.Event
		propertyID, unitID := e.Data["property_id"], e.Data["unit_id"]
		leaseID := e.Data["lease_id"]
		if leaseID == "" && e.Subject.Kind == automation.SubjectLease {
			leaseID = e.Subject.ID
		}
		if propertyID == "" {
			// Nothing to anchor a checklist to. Recorded rather than dropped,
			// because "the automation is on and nothing happened" is the report
			// somebody will otherwise have to reconstruct from the event stream.
			_, err := r.Propose(ctx, automation.Proposal{
				Subject: e.Subject,
				Action:  "checklist." + process,
				Detail:  "the event named no property, so there was nothing to start a process against",
				Key:     e.Subject.ID + ":" + process,
			})
			return err
		}

		anchor := d.Now()
		if on, ok := e.Data["anchor_on"]; ok {
			if parsed, err := effective.ParseDate(on); err == nil {
				anchor = parsed
			}
		}

		_, err := r.Propose(ctx, automation.Proposal{
			Subject: e.Subject,
			Action:  "checklist." + process,
			Detail:  "started from " + e.Type,
			Key:     e.Subject.ID + ":" + process,
			Do: func(ctx context.Context) error {
				_, err := d.Checklists.Start(ctx, process, e.Data["property_kind"],
					propertyID, unitID, leaseID, anchor)
				return err
			},
		})
		return err
	}
	return def
}

// caused stamps the automation onto an event's payload.
//
// The activity feed (dwellm8#196) is the outbox read back, so every event these
// automations publish already appears on the record. What it cannot know without
// this is *which* automation caused it, and "a reminder was sent" is a materially
// worse line than "the arrears follow-up sent a reminder".
//
// The run log answers the same question in more detail and answers it for the
// times an automation decided *not* to act, which publish nothing. The two are
// complementary rather than duplicates: one is the record's story, the other is
// the automation's.
func caused(r *automation.Run, data map[string]any) map[string]any {
	out := make(map[string]any, len(data)+1)
	for k, v := range data {
		out[k] = v
	}
	out["automation"] = r.Definition.Key.String()
	return out
}

func daysBetween(from, to effective.Date) int {
	if from.Zero() || to.Zero() {
		return 0
	}
	return int(to.Time().Sub(from.Time()).Hours() / 24)
}
