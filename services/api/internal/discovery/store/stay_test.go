package store_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/tesserix/dwellm8/services/api/internal/discovery/store"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy/isolationtest"
)

// HomeStay against PostgreSQL (#233): the nightly price agreed at booking,
// the exclusion constraint as the whole double-booking story, the
// verification gate before occupancy, and holds that cannot be confirmed
// after they lapse.

func stayFixture(t *testing.T) (*store.Stay, *store.Prospects, tenancy.PlatformPool, string) {
	t.Helper()
	pool, plat := pools(t)
	stay := store.NewStay(pool, plat)
	u := unit(t, plat, "ST")

	id, err := stay.CreateListing(owner(), store.StayDraft{
		PropertyID: isolationtest.PropertyGranted, UnitID: u,
		Headline: "Poolside studio", Locality: "Indiranagar",
		City: fmt.Sprintf("Stayville-%d", time.Now().UnixNano()), StateCode: "KA",
		NightlyMinor: 3_500_00, CleaningMinor: 800_00, MaxGuests: 3, MinNights: 2, MaxNights: 14,
	})
	if err != nil {
		t.Fatalf("creating the stay listing: %v", err)
	}
	if err := stay.MoveListing(owner(), id, "live"); err != nil {
		t.Fatalf("publishing: %v", err)
	}
	return stay, store.NewProspects(plat), plat, id
}

func verifiedGuest(t *testing.T, prospects *store.Prospects) store.Prospect {
	t.Helper()
	_, hash := token(t)
	p, err := prospects.Ensure(context.Background(), hash)
	if err != nil {
		t.Fatalf("guest: %v", err)
	}
	if err := prospects.Verify(context.Background(), hash, "exo-"+p.ID[:8], "XXXXXX2222"); err != nil {
		t.Fatalf("verifying: %v", err)
	}
	return p
}

func TestStayBookingStory(t *testing.T) {
	stay, prospects, plat, listing := stayFixture(t)
	in := time.Now().AddDate(0, 0, 30).Truncate(24 * time.Hour)
	out := in.AddDate(0, 0, 4) // four nights

	// Occupancy needs a verified phone — browsing does not.
	_, hash := token(t)
	stranger, err := prospects.Ensure(context.Background(), hash)
	if err != nil {
		t.Fatalf("stranger: %v", err)
	}
	if _, err := stay.Hold(context.Background(), listing, stranger.ID, in, out, 2); !errors.Is(err, store.ErrNotVerified) {
		t.Fatalf("unverified hold = %v, want ErrNotVerified", err)
	}

	// The listing's own rules are enforced before any dates are taken.
	guest := verifiedGuest(t, prospects)
	if _, err := stay.Hold(context.Background(), listing, guest.ID, in, in.AddDate(0, 0, 1), 2); !errors.Is(err, store.ErrStayRules) {
		t.Fatalf("one night against min_nights=2 = %v", err)
	}
	if _, err := stay.Hold(context.Background(), listing, guest.ID, in, out, 9); !errors.Is(err, store.ErrStayRules) {
		t.Fatalf("nine guests against max_guests=3 = %v", err)
	}

	// The hold: four nights at the agreed rate plus the clean.
	b, err := stay.Hold(context.Background(), listing, guest.ID, in, out, 2)
	if err != nil {
		t.Fatalf("holding: %v", err)
	}
	if b.TotalMinor != 4*3_500_00+800_00 {
		t.Fatalf("total = %d, want 1480000 (4 nights + cleaning)", b.TotalMinor)
	}

	// Overlapping nights lose to the exclusion constraint — a second guest
	// wanting nights two and three is told so by name.
	rival := verifiedGuest(t, prospects)
	if _, err := stay.Hold(context.Background(), listing, rival.ID,
		in.AddDate(0, 0, 1), in.AddDate(0, 0, 3), 1); !errors.Is(err, store.ErrDatesTaken) {
		t.Fatalf("overlapping hold = %v, want ErrDatesTaken", err)
	}

	// Host confirms, and the stay completes after checkout.
	if err := stay.Confirm(owner(), b.ID); err != nil {
		t.Fatalf("confirming: %v", err)
	}
	if err := stay.Complete(owner(), b.ID); err != nil {
		t.Fatalf("completing: %v", err)
	}

	// Completed nights stay booked history; the same dates are free again only
	// through cancellation, which the next test walks.
	_ = plat
}

// The money leg (#233→#234): a payment attaches to the guest's own held
// booking exactly once, and the received fact confirms it — even after the
// hold clock lapsed, because money that moved beats a timer.
func TestStayPaymentConfirms(t *testing.T) {
	stay, prospects, plat, listing := stayFixture(t)
	in := time.Now().AddDate(0, 0, 90).Truncate(24 * time.Hour)
	out := in.AddDate(0, 0, 2)

	guest := verifiedGuest(t, prospects)
	b, err := stay.Hold(context.Background(), listing, guest.ID, in, out, 2)
	if err != nil {
		t.Fatalf("holding: %v", err)
	}

	// Only the guest's own held booking takes a payment, and only once.
	paymentID := uuid.NewString()
	other := verifiedGuest(t, prospects)
	if err := stay.AttachPayment(context.Background(), other.ID, b.ID, paymentID); !errors.Is(err, store.ErrNoStay) {
		t.Fatalf("another guest attached a payment: %v", err)
	}
	if err := stay.AttachPayment(context.Background(), guest.ID, b.ID, paymentID); err != nil {
		t.Fatalf("attaching: %v", err)
	}
	if err := stay.AttachPayment(context.Background(), guest.ID, b.ID, uuid.NewString()); !errors.Is(err, store.ErrNoStay) {
		t.Fatalf("a second payment attached over the first: %v", err)
	}

	// The hold lapses; the payment arrives anyway. Money wins.
	if err := tenancy.Platform(context.Background(), plat, "test: lapsing a paid hold",
		func(ctx context.Context, tx pgx.Tx) error {
			_, err := tx.Exec(ctx,
				`UPDATE stay_bookings SET hold_expires_at = now() - interval '1 minute' WHERE id = $1`, b.ID)
			return err
		}); err != nil {
		t.Fatalf("lapsing: %v", err)
	}
	confirmed, err := stay.ConfirmByPayment(context.Background(), paymentID)
	if err != nil || confirmed != b.ID {
		t.Fatalf("ConfirmByPayment = %q, %v; want %s", confirmed, err, b.ID)
	}
	// Idempotent: the same fact redelivered confirms nothing twice.
	again, err := stay.ConfirmByPayment(context.Background(), paymentID)
	if err != nil || again != "" {
		t.Fatalf("redelivery = %q, %v; want quiet", again, err)
	}
	// And an unknown payment is the consumer's ordinary silence.
	if id, err := stay.ConfirmByPayment(context.Background(), uuid.NewString()); err != nil || id != "" {
		t.Fatalf("unknown payment = %q, %v; want quiet", id, err)
	}
}

// The sweep releases lapsed unpaid holds and nothing else — a paid hold is
// the money consumer's to confirm, never the timer's to kill.
func TestSweepReleasesOnlyUnpaidLapsedHolds(t *testing.T) {
	stay, prospects, plat, listing := stayFixture(t)
	in := time.Now().AddDate(0, 0, 120).Truncate(24 * time.Hour)

	lapse := func(id string) {
		if err := tenancy.Platform(context.Background(), plat, "test: lapsing",
			func(ctx context.Context, tx pgx.Tx) error {
				_, err := tx.Exec(ctx,
					`UPDATE stay_bookings SET hold_expires_at = now() - interval '1 minute' WHERE id = $1`, id)
				return err
			}); err != nil {
			t.Fatalf("lapsing: %v", err)
		}
	}

	unpaid, err := stay.Hold(context.Background(), listing,
		verifiedGuest(t, prospects).ID, in, in.AddDate(0, 0, 2), 1)
	if err != nil {
		t.Fatalf("unpaid hold: %v", err)
	}
	paidGuest := verifiedGuest(t, prospects)
	paid, err := stay.Hold(context.Background(), listing, paidGuest.ID,
		in.AddDate(0, 0, 5), in.AddDate(0, 0, 7), 1)
	if err != nil {
		t.Fatalf("paid hold: %v", err)
	}
	if err := stay.AttachPayment(context.Background(), paidGuest.ID, paid.ID, uuid.NewString()); err != nil {
		t.Fatalf("attaching: %v", err)
	}
	lapse(unpaid.ID)
	lapse(paid.ID)

	if _, err := stay.SweepExpiredHolds(context.Background()); err != nil {
		t.Fatalf("sweeping: %v", err)
	}

	// The unpaid one released its nights; the paid one still holds them.
	if _, err := stay.Hold(context.Background(), listing,
		verifiedGuest(t, prospects).ID, in, in.AddDate(0, 0, 2), 1); err != nil {
		t.Fatalf("rebooking swept nights: %v", err)
	}
	if _, err := stay.Hold(context.Background(), listing,
		verifiedGuest(t, prospects).ID, in.AddDate(0, 0, 5), in.AddDate(0, 0, 7), 1); !errors.Is(err, store.ErrDatesTaken) {
		t.Fatalf("a paid hold was swept: %v", err)
	}
}

// Date-filtered search answers only with listings genuinely free for the
// asked nights, within their own rules.
func TestSearchByDates(t *testing.T) {
	stay, prospects, _, listing := stayFixture(t)
	l, err := stay.Live(context.Background(), listing)
	if err != nil {
		t.Fatalf("reading the listing: %v", err)
	}
	city := l.City
	in := time.Now().AddDate(0, 0, 150).Truncate(24 * time.Hour)
	out := in.AddDate(0, 0, 3)
	day := func(t2 time.Time) time.Time { return t2 }

	free, err := stay.Search(context.Background(), store.StaySearch{
		City: city, CheckIn: day(in), CheckOut: day(out), Guests: 2})
	if err != nil || len(free) != 1 {
		t.Fatalf("free search = %d, %v; want the listing", len(free), err)
	}
	// Too many guests, or a stay shorter than min_nights: filtered by the
	// listing's own rules.
	if got, _ := stay.Search(context.Background(), store.StaySearch{
		City: city, CheckIn: in, CheckOut: out, Guests: 9}); len(got) != 0 {
		t.Fatalf("nine guests matched a max_guests=3 listing")
	}
	if got, _ := stay.Search(context.Background(), store.StaySearch{
		City: city, CheckIn: in, CheckOut: in.AddDate(0, 0, 1)}); len(got) != 0 {
		t.Fatalf("one night matched a min_nights=2 listing")
	}

	// Book the middle night: the span no longer fits, adjacent nights do.
	g := verifiedGuest(t, prospects)
	if _, err := stay.Hold(context.Background(), listing, g.ID,
		in.AddDate(0, 0, 1), in.AddDate(0, 0, 3), 1); err != nil {
		t.Fatalf("blocking hold: %v", err)
	}
	if got, _ := stay.Search(context.Background(), store.StaySearch{
		City: city, CheckIn: in, CheckOut: out}); len(got) != 0 {
		t.Fatalf("a blocked span still matched")
	}
	if got, _ := stay.Search(context.Background(), store.StaySearch{
		City: city, CheckIn: in.AddDate(0, 0, 5), CheckOut: in.AddDate(0, 0, 8)}); len(got) != 1 {
		t.Fatalf("free later nights did not match")
	}
}

func TestStayCancelReleasesAndExpiredHoldRefusesConfirm(t *testing.T) {
	stay, prospects, plat, listing := stayFixture(t)
	in := time.Now().AddDate(0, 0, 60).Truncate(24 * time.Hour)
	out := in.AddDate(0, 0, 3)

	guest := verifiedGuest(t, prospects)
	b, err := stay.Hold(context.Background(), listing, guest.ID, in, out, 2)
	if err != nil {
		t.Fatalf("holding: %v", err)
	}

	// A guest cancelling releases the nights for the next person, and another
	// prospect cannot cancel somebody else's stay.
	other := verifiedGuest(t, prospects)
	if err := stay.CancelByGuest(context.Background(), other.ID, b.ID); !errors.Is(err, store.ErrNoStay) {
		t.Fatalf("cancelling another guest's stay = %v, want refusal", err)
	}
	if err := stay.CancelByGuest(context.Background(), guest.ID, b.ID); err != nil {
		t.Fatalf("cancelling: %v", err)
	}
	b2, err := stay.Hold(context.Background(), listing, other.ID, in, out, 1)
	if err != nil {
		t.Fatalf("rebooking released nights: %v", err)
	}

	// A hold that lapsed cannot be confirmed — fifteen minutes of indecision
	// must not become a booking at leisure.
	if err := tenancy.Platform(context.Background(), plat, "test: lapsing a hold",
		func(ctx context.Context, tx pgx.Tx) error {
			_, err := tx.Exec(ctx,
				`UPDATE stay_bookings SET hold_expires_at = now() - interval '1 minute' WHERE id = $1`,
				b2.ID)
			return err
		}); err != nil {
		t.Fatalf("lapsing: %v", err)
	}
	if err := stay.Confirm(owner(), b2.ID); !errors.Is(err, store.ErrHoldExpired) {
		t.Fatalf("confirming a lapsed hold = %v, want ErrHoldExpired", err)
	}
}
