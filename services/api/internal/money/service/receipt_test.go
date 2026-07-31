package service_test

import (
	"strings"
	"testing"
	"time"

	"github.com/tesserix/dwellm8/services/api/internal/money/service"
)

// The receipt reference. Issue #51.
//
// No database, because it is a pure derivation — and it has to stay one: the
// number a tenant quotes on a phone call in March must be the number the same
// payment produces in September, from any replica, with no row to look it up in.

func TestAReceiptNumberIsStableAndReproducible(t *testing.T) {
	const payment = "3f2a9c11-7b4e-4d21-9f60-1a2b3c4d5e6f"
	at := time.Date(2026, time.February, 5, 18, 30, 0, 0, time.UTC)

	got := service.ReceiptNumber(payment, at)
	if want := "DW-20260205-3F2A9C117B4E"; got != want {
		t.Fatalf("receipt number %q, want %q", got, want)
	}
	if again := service.ReceiptNumber(payment, at); again != got {
		t.Fatalf("the same payment produced %q and then %q — a reference that changes proves nothing",
			got, again)
	}
}

// Two payments get different references. The distinction is carried by the first
// six bytes of the uuid, which is where a random v4 differs; two ids that agree
// on all six would collide, and the doc comment says so rather than this test
// pretending otherwise.
func TestTwoPaymentsGetDifferentReceiptNumbers(t *testing.T) {
	at := time.Date(2026, time.February, 5, 0, 0, 0, 0, time.UTC)
	a := service.ReceiptNumber("3f2a9c11-7b4e-4d21-9f60-1a2b3c4d5e6f", at)
	b := service.ReceiptNumber("90b1e7d4-2c55-4d21-9f60-1a2b3c4d5e6f", at)
	if a == b {
		t.Fatalf("two payments share the receipt reference %q", a)
	}
}

// The date on the receipt is the day the money was received, in the tenant's own
// calendar terms. A payment made late on the 5th and captured minutes later
// still belongs to the 5th.
func TestTheReceiptCarriesTheDayItWasReceived(t *testing.T) {
	at := time.Date(2026, time.December, 31, 23, 55, 0, 0, time.UTC)
	if got := service.ReceiptNumber("aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee", at); !strings.Contains(got, "20261231") {
		t.Fatalf("receipt number %q does not carry the date it was received", got)
	}
}
