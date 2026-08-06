package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/tesserix/dwellm8/services/api/internal/discovery/store"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy/isolationtest"
)

// Five years of address history (#259). The gaps are the point: a hole in the
// history is reported so the manager asks about it, and an overlap — a family
// home kept while renting — is ordinary rather than a defect.

func month(back int) time.Time {
	now := time.Now()
	return time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, -back, 0)
}

func indianAddress(kind string, from, to int) store.Address {
	a := store.Address{
		Kind: kind, Line1: "12 MG Road", Locality: "Indiranagar", City: "Bengaluru",
		StateCode: "KA", Pin: "560038", Country: "IN", From: month(from),
	}
	if to >= 0 {
		t := month(to)
		a.To = &t
	}
	return a
}

func addressFixture(t *testing.T) (*store.Applicants, string) {
	t.Helper()
	packs, _, application := packFixture(t)
	if _, err := packs.SaveProfile(owner(), application, store.Profile{FullName: "Meera Menon"}); err != nil {
		t.Fatalf("saving the pack: %v", err)
	}
	return packs, application
}

func TestSixtyUnbrokenMonthsIsCompleteHistory(t *testing.T) {
	packs, application := addressFixture(t)

	err := packs.SaveAddresses(owner(), application, []store.Address{
		indianAddress("rented", 24, -1),
		indianAddress("family", 62, 24),
	})
	if err != nil {
		t.Fatalf("saving the history: %v", err)
	}

	history, err := packs.Addresses(owner(), application)
	if err != nil {
		t.Fatalf("reading the history: %v", err)
	}
	if !history.Complete {
		t.Fatalf("history = %+v; want complete with no gaps", history.Gaps)
	}
}

func TestAHoleInTheHistoryIsNamedRatherThanAccepted(t *testing.T) {
	packs, application := addressFixture(t)

	// Present since 20 months ago, and before that up to 28 months ago: the
	// eight months between are the hole the manager has to ask about.
	err := packs.SaveAddresses(owner(), application, []store.Address{
		indianAddress("rented", 20, -1),
		indianAddress("rented", 62, 28),
	})
	if err != nil {
		t.Fatalf("saving the history: %v", err)
	}

	history, err := packs.Addresses(owner(), application)
	if err != nil {
		t.Fatalf("reading the history: %v", err)
	}
	if history.Complete {
		t.Fatal("an eight-month hole read as a complete history")
	}
	if len(history.Gaps) != 1 {
		t.Fatalf("gaps = %+v; want the one hole", history.Gaps)
	}
	// The month they moved out is a month they lived there, so the hole opens
	// the month after.
	if got, want := history.Gaps[0].From, month(27); !got.Equal(want) {
		t.Fatalf("the gap starts %s, want %s", got.Format("2006-01"), want.Format("2006-01"))
	}
}

func TestAFamilyHomeKeptWhileRentingIsNotAGap(t *testing.T) {
	packs, application := addressFixture(t)

	err := packs.SaveAddresses(owner(), application, []store.Address{
		indianAddress("rented", 30, -1),
		indianAddress("family", 62, -1),
	})
	if err != nil {
		t.Fatalf("saving overlapping addresses: %v", err)
	}

	history, err := packs.Addresses(owner(), application)
	if err != nil {
		t.Fatalf("reading: %v", err)
	}
	if !history.Complete || len(history.Gaps) != 0 {
		t.Fatalf("overlap read as a gap: %+v", history.Gaps)
	}
}

func TestAnAddressThatEndsBeforeItStartsIsRefused(t *testing.T) {
	packs, application := addressFixture(t)

	bad := indianAddress("rented", 10, 20)
	if err := packs.SaveAddresses(owner(), application, []store.Address{bad}); !errors.Is(err, store.ErrAddressWindow) {
		t.Fatalf("a backwards address gave %v, want ErrAddressWindow", err)
	}
}

func TestAnAddressAbroadNeedsNoStateOrPin(t *testing.T) {
	packs, application := addressFixture(t)

	err := packs.SaveAddresses(owner(), application, []store.Address{
		indianAddress("rented", 12, -1),
		{Kind: "rented", Line1: "44 Jurong West", City: "Singapore", Country: "SG", From: month(62),
			To: func() *time.Time { m := month(12); return &m }()},
	})
	if err != nil {
		t.Fatalf("an address abroad was refused: %v", err)
	}

	history, err := packs.Addresses(owner(), application)
	if err != nil || len(history.Addresses) != 2 {
		t.Fatalf("history = %d rows, %v; want both", len(history.Addresses), err)
	}
}

func TestSavingTheHistoryReplacesTheWholeSet(t *testing.T) {
	packs, application := addressFixture(t)

	if err := packs.SaveAddresses(owner(), application, []store.Address{
		indianAddress("rented", 12, -1), indianAddress("family", 40, 12),
	}); err != nil {
		t.Fatalf("saving: %v", err)
	}
	if err := packs.SaveAddresses(owner(), application, []store.Address{
		indianAddress("owned", 12, -1),
	}); err != nil {
		t.Fatalf("replacing: %v", err)
	}

	history, err := packs.Addresses(owner(), application)
	if err != nil || len(history.Addresses) != 1 || history.Addresses[0].Kind != "owned" {
		t.Fatalf("history = %+v, %v; want only the replacement", history.Addresses, err)
	}
}

func TestAHistoryTheFirmDoesNotActForIsNotThere(t *testing.T) {
	packs, application := addressFixture(t)

	if err := packs.SaveAddresses(owner(), application, []store.Address{
		indianAddress("rented", 12, -1),
	}); err != nil {
		t.Fatalf("saving: %v", err)
	}
	outsider := tenancy.With(context.Background(), isolationtest.OrgOutsider)
	if _, err := packs.Addresses(outsider, application); !errors.Is(err, store.ErrNoProfile) {
		t.Fatalf("another owner's manager read the history: %v", err)
	}
}
