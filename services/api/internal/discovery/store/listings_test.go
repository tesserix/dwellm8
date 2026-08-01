package store_test

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tesserix/dwellm8/services/api/internal/discovery/domain"
	"github.com/tesserix/dwellm8/services/api/internal/discovery/store"
	"github.com/tesserix/dwellm8/services/api/internal/platform/events"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy/isolationtest"
)

// The funnel against PostgreSQL: a listing goes live and becomes visible to a
// stranger, a prospect verifies and enquires, the owner responds, the unit is
// let and the advertisement dies. And the refusals the schema owns: one live
// advert per unit, no enquiry from an unverified prospect, no contact bridge
// over an unanswered enquiry.

func pools(t *testing.T) (tenancy.Pool, tenancy.PlatformPool) {
	t.Helper()
	dsn, plat := os.Getenv("TEST_DATABASE_URL"), os.Getenv("TEST_PLATFORM_DATABASE_URL")
	if dsn == "" || plat == "" {
		t.Skip("TEST_DATABASE_URL and TEST_PLATFORM_DATABASE_URL are not set")
	}
	req, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connecting: %v", err)
	}
	t.Cleanup(req.Close)
	p, err := pgxpool.New(context.Background(), plat)
	if err != nil {
		t.Fatalf("connecting as platform: %v", err)
	}
	t.Cleanup(p.Close)
	return req, tenancy.NewPlatformPool(p)
}

// seq makes unit codes unique within a run: a nanosecond residue cycles every
// millisecond, and two fixtures a millisecond apart collided on it once.
var seq atomic.Int64

// unit seeds a fresh unit of OrgOwner's, because the one-live-advert index is
// real and a shared unit would fail tests for the wrong reason.
func unit(t *testing.T, plat tenancy.PlatformPool, code string) string {
	code = fmt.Sprintf("%s-%d", code, seq.Add(1))
	t.Helper()
	isolationtest.SeedPropertyTree(t, plat)
	var id string
	err := tenancy.Platform(context.Background(), plat, "seeding a listing unit",
		func(ctx context.Context, tx pgx.Tx) error {
			return tx.QueryRow(ctx, `
				INSERT INTO units (id, tenant_id, property_id, unit_kind, code, carpet_area_sqft)
				VALUES (gen_random_uuid(), $1, $2, 'flat', $3, 950)
				RETURNING id`,
				isolationtest.OrgOwner.String(), isolationtest.PropertyGranted, code).Scan(&id)
		})
	if err != nil {
		t.Fatalf("seeding a unit: %v", err)
	}
	return id
}

func draft(unitID, city string) domain.Draft {
	return domain.Draft{
		PropertyID: isolationtest.PropertyGranted, UnitID: unitID,
		Headline: "2BHK with covered parking", Locality: "Indiranagar", City: city, StateCode: "KA",
		Bedrooms: 2,
		Costs: domain.Costs{
			RentMinor: 32_000_00, MaintenanceMinor: 3_500_00, ParkingMinor: 1_000_00,
			DepositMinor: 96_000_00, OneTimeMinor: 5_000_00, Confirmed: true,
		},
	}
}

func owner() context.Context {
	return tenancy.With(context.Background(), isolationtest.OrgOwner)
}

func token(t *testing.T) (raw string, hash []byte) {
	t.Helper()
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("random token: %v", err)
	}
	raw = fmt.Sprintf("%x", b)
	sum := sha256.Sum256([]byte(raw))
	return raw, sum[:]
}

func TestListingLifecycleAndPublicVisibility(t *testing.T) {
	pool, plat := pools(t)
	listings, public := store.NewListings(pool), store.NewPublic(pool)
	city := fmt.Sprintf("Testville-%d", time.Now().UnixNano())
	u := unit(t, plat, fmt.Sprintf("L-%d", time.Now().UnixNano()%1_000_000))

	id, err := listings.Create(owner(), draft(u, city))
	if err != nil {
		t.Fatalf("creating the draft: %v", err)
	}

	// A draft is invisible to a stranger.
	if _, err := public.Detail(context.Background(), id); !errors.Is(err, store.ErrNoListing) {
		t.Fatalf("a stranger sees a draft: %v", err)
	}

	actor := events.Actor{Kind: events.ActorSystem}
	if err := listings.Move(owner(), id, domain.StateLive, actor); err != nil {
		t.Fatalf("publishing: %v", err)
	}

	// Live: searchable, with the true cost stated (#133, #134).
	cards, err := public.Search(context.Background(), store.Query{City: city})
	if err != nil || len(cards) != 1 {
		t.Fatalf("search = %d cards, %v; want the one live listing", len(cards), err)
	}
	c := cards[0]
	if c.TotalMonthlyMinor != 36_500_00 {
		t.Fatalf("monthly total = %d, want 3650000 (rent+maintenance+parking)", c.TotalMonthlyMinor)
	}
	if c.TotalOneTimeMinor != 101_000_00 {
		t.Fatalf("one-time total = %d, want 10100000 (deposit+one-time)", c.TotalOneTimeMinor)
	}

	// The search must not answer with the unit or the address — a stranger gets
	// a locality (#133's edge case). The card type carries no unit id at all;
	// what this asserts is that the detail page agrees.
	if d, err := public.Detail(context.Background(), id); err != nil || d.Locality != "Indiranagar" {
		t.Fatalf("detail = %+v, %v", d, err)
	}

	// A second advert for the same flat is the thing a prospect screenshots.
	id2, err := listings.Create(owner(), draft(u, city))
	if err != nil {
		t.Fatalf("second draft: %v", err)
	}
	if err := listings.Move(owner(), id2, domain.StateLive, actor); !errors.Is(err, store.ErrAlreadyAdvertised) {
		t.Fatalf("second publish = %v, want ErrAlreadyAdvertised", err)
	}

	// Paused keeps the publication but leaves the market.
	if err := listings.Move(owner(), id, domain.StatePaused, actor); err != nil {
		t.Fatalf("pausing: %v", err)
	}
	if _, err := public.Detail(context.Background(), id); !errors.Is(err, store.ErrNoListing) {
		t.Fatalf("a stranger sees a paused listing")
	}
	if err := listings.Move(owner(), id, domain.StateLive, actor); err != nil {
		t.Fatalf("resuming: %v", err)
	}

	// The unit is let: the advertisement dies with no manual step (#135).
	closed, err := store.NewLetMarker(plat).MarkLetByUnit(context.Background(),
		isolationtest.OrgOwner.String(), u)
	if err != nil || closed != 1 {
		t.Fatalf("MarkLetByUnit = %d, %v; want 1 closed", closed, err)
	}
	if _, err := public.Detail(context.Background(), id); !errors.Is(err, store.ErrNoListing) {
		t.Fatalf("a stranger sees a let listing")
	}
	got, err := listings.Get(owner(), id)
	if err != nil || got.State != domain.StateLet {
		t.Fatalf("owner's view after let = %s, %v; want let", got.State, err)
	}
}

// Tenant isolation on the owner side of the funnel: another organisation sees
// nothing, whatever the state.
func TestListingIsolation(t *testing.T) {
	pool, plat := pools(t)
	listings := store.NewListings(pool)
	city := fmt.Sprintf("Isoville-%d", time.Now().UnixNano())
	u := unit(t, plat, fmt.Sprintf("I-%d", time.Now().UnixNano()%1_000_000))

	id, err := listings.Create(owner(), draft(u, city))
	if err != nil {
		t.Fatalf("creating: %v", err)
	}

	other := tenancy.With(context.Background(), isolationtest.OrgFirm)
	if _, err := listings.Get(other, id); !errors.Is(err, store.ErrNoListing) {
		t.Fatalf("another organisation reads the draft: %v", err)
	}
	if err := listings.Move(other, id, domain.StateLive, events.Actor{Kind: events.ActorSystem}); !errors.Is(err, store.ErrNoListing) {
		t.Fatalf("another organisation publishes it: %v", err)
	}

	// Once live it is public — that is the point — but the owner view with its
	// draft costs stays the owner's.
	if err := listings.Move(owner(), id, domain.StateLive, events.Actor{Kind: events.ActorSystem}); err != nil {
		t.Fatalf("publishing: %v", err)
	}
	got, err := listings.List(other, "")
	if err != nil {
		t.Fatalf("listing as the firm: %v", err)
	}
	for _, l := range got {
		if l.ID == id {
			t.Fatalf("the firm's own list contains the owner's listing")
		}
	}
}

func TestProspectFunnel(t *testing.T) {
	pool, plat := pools(t)
	listings := store.NewListings(pool)
	prospects := store.NewProspects(plat)
	enquiries := store.NewEnquiries(pool, plat)
	city := fmt.Sprintf("Funnelton-%d", time.Now().UnixNano())
	u := unit(t, plat, fmt.Sprintf("F-%d", time.Now().UnixNano()%1_000_000))

	id, err := listings.Create(owner(), draft(u, city))
	if err != nil {
		t.Fatalf("creating: %v", err)
	}
	if err := listings.Move(owner(), id, domain.StateLive, events.Actor{Kind: events.ActorSystem}); err != nil {
		t.Fatalf("publishing: %v", err)
	}

	_, hash := token(t)
	p, err := prospects.Ensure(context.Background(), hash)
	if err != nil {
		t.Fatalf("ensuring the prospect: %v", err)
	}
	if p.Verified {
		t.Fatalf("a fresh prospect is verified")
	}

	// Browsing and shortlisting are anonymous; making contact is not.
	if _, err := enquiries.Create(context.Background(), id, p.ID, "enquiry", "Is it available?", nil); !errors.Is(err, store.ErrNotVerified) {
		t.Fatalf("an unverified prospect enquired: %v", err)
	}

	if err := prospects.ShortlistAdd(context.Background(), p.ID, id); err != nil {
		t.Fatalf("shortlisting: %v", err)
	}
	saved, err := prospects.Shortlist(context.Background(), p.ID)
	if err != nil || len(saved) != 1 || saved[0].ListingID != id {
		t.Fatalf("shortlist = %+v, %v", saved, err)
	}

	if err := prospects.Verify(context.Background(), hash, "exo-ref-1", "XXXXXX4321"); err != nil {
		t.Fatalf("verifying: %v", err)
	}

	e, err := enquiries.Create(context.Background(), id, p.ID, "enquiry", "Is it available?", nil)
	if err != nil || e.State != "new" {
		t.Fatalf("enquiry = %+v, %v", e, err)
	}
	// The same person tapping twice is one enquiry.
	again, err := enquiries.Create(context.Background(), id, p.ID, "enquiry", "hello again", nil)
	if err != nil || again.ID != e.ID {
		t.Fatalf("duplicate enquiry = %s, %v; want %s back", again.ID, err, e.ID)
	}

	// The pipeline, on the owner's side.
	mine, err := enquiries.ForOwner(owner(), "new")
	if err != nil {
		t.Fatalf("owner pipeline: %v", err)
	}
	var found bool
	for _, m := range mine {
		if m.ID == e.ID {
			found = true
			if m.Headline == "" {
				t.Fatalf("the pipeline row carries no headline")
			}
		}
	}
	if !found {
		t.Fatalf("the enquiry is not in the owner's pipeline")
	}

	// No bridge over an unanswered enquiry — the trigger's rule.
	if _, err := enquiries.OpenBridge(owner(), store.Bridge{
		EnquiryID: e.ID, Provider: "exotel", ProviderRef: "br-1",
		ProxyMasked: "XXXXXX9000", ExpiresAt: time.Now().Add(48 * time.Hour),
	}); err == nil {
		t.Fatalf("a bridge opened over an unanswered enquiry")
	}

	if err := enquiries.Move(owner(), e.ID, "owner_responded", events.Actor{Kind: events.ActorSystem}); err != nil {
		t.Fatalf("responding: %v", err)
	}
	if _, err := enquiries.OpenBridge(owner(), store.Bridge{
		EnquiryID: e.ID, Provider: "exotel", ProviderRef: "br-1",
		ProxyMasked: "XXXXXX9000", ExpiresAt: time.Now().Add(48 * time.Hour),
	}); err != nil {
		t.Fatalf("opening the bridge after response: %v", err)
	}

	// The prospect's own timeline, and the masked contact for the owner's view.
	timeline, err := enquiries.ForProspect(context.Background(), p.ID)
	if err != nil || len(timeline) == 0 {
		t.Fatalf("prospect timeline = %+v, %v", timeline, err)
	}
	masked, err := prospects.MaskedContacts(context.Background(), []string{p.ID})
	if err != nil || masked[p.ID] != "XXXXXX4321" {
		t.Fatalf("masked contacts = %+v, %v", masked, err)
	}
}

// An enquiry on a listing that has just been paused or let is answered
// honestly (#137's edge case).
func TestEnquiryOnAnUnavailableListing(t *testing.T) {
	pool, plat := pools(t)
	listings := store.NewListings(pool)
	prospects := store.NewProspects(plat)
	enquiries := store.NewEnquiries(pool, plat)
	city := fmt.Sprintf("Gonesburg-%d", time.Now().UnixNano())
	u := unit(t, plat, fmt.Sprintf("G-%d", time.Now().UnixNano()%1_000_000))

	id, err := listings.Create(owner(), draft(u, city))
	if err != nil {
		t.Fatalf("creating: %v", err)
	}
	if err := listings.Move(owner(), id, domain.StateLive, events.Actor{Kind: events.ActorSystem}); err != nil {
		t.Fatalf("publishing: %v", err)
	}
	if err := listings.Move(owner(), id, domain.StatePaused, events.Actor{Kind: events.ActorSystem}); err != nil {
		t.Fatalf("pausing: %v", err)
	}

	_, hash := token(t)
	p, err := prospects.Ensure(context.Background(), hash)
	if err != nil {
		t.Fatalf("prospect: %v", err)
	}
	if err := prospects.Verify(context.Background(), hash, "exo-ref-2", "XXXXXX1111"); err != nil {
		t.Fatalf("verifying: %v", err)
	}
	if _, err := enquiries.Create(context.Background(), id, p.ID, "enquiry", "still there?", nil); !errors.Is(err, store.ErrListingNotLive) {
		t.Fatalf("enquiry on a paused listing = %v, want ErrListingNotLive", err)
	}
}
