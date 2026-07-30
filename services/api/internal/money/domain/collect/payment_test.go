package collect

import (
	"errors"
	"testing"
	"time"

	"github.com/tesserix/dwellm8/services/api/internal/money/domain"
)

// ADR-0011. The properties asserted here are the two the issue names: a retried
// request collects once, and a webhook delivered five times, twice out of order,
// lands the payment in one correct terminal state.

func sample() Payment {
	return Payment{
		ID: "pay-1", TenantID: "org-1", Property: "prop-1", Unit: "unit-1",
		PayerKind: domain.Tenant, PayerID: "tenant-1",
		Amount: 2_750_000, Method: MethodUPICollect,
		Provider: "razorpay", IdempotencyKey: "collect-2026-08-unit-1",
		Status: StatusCreated,
	}
}

// The issue's failure scenario, in full: the same webhook five times, two of
// them out of order, and the payment must reach the correct terminal state once
// with no regression.
func TestFiveDeliveriesTwiceOutOfOrderLandInOneCorrectState(t *testing.T) {
	p := sample()

	// What the provider actually did, in the order it happened.
	confirmed := []Status{StatusAttempted, StatusCaptured, StatusSettled}
	for _, s := range confirmed {
		if s == StatusCaptured {
			p.EntryID = "entry-1" // capture is what posts; §5
		}
		if err := p.ApplyConfirmed(s, time.Now()); err != nil {
			t.Fatalf("applying %s: %v", s, err)
		}
	}
	if p.Status != StatusSettled {
		t.Fatalf("payment is %s, want settled", p.Status)
	}

	// Now the deliveries, as a provider actually sends them: duplicates, and two
	// arriving after the state they describe has been overtaken.
	deliveries := []Status{
		StatusCaptured,  // late duplicate of a state already passed
		StatusSettled,   // says what is already true
		StatusCaptured,  // again
		StatusSettled,   // again
		StatusAttempted, // very late, from the start of the sequence
	}
	parked, ignored := 0, 0
	for i, claimed := range deliveries {
		d := Delivery{
			Provider: "razorpay", EventID: "evt-" + claimed.String(),
			SignatureVerified: true, ProviderPaymentID: "pay_x", Claimed: claimed,
		}
		switch got := Decide(d, &p); got.Disposition {
		case Park:
			parked++
			if got.Reason != ParkStaleTransition {
				t.Errorf("delivery %d parked for %q, want stale_transition", i, got.Reason)
			}
		case Ignore:
			ignored++
		case Confirm:
			t.Errorf("delivery %d asked for confirmation of %s against a settled payment", i, claimed)
		}
	}
	if parked != 3 || ignored != 2 {
		t.Errorf("parked %d and ignored %d, want 3 parked and 2 ignored", parked, ignored)
	}
	if p.Status != StatusSettled {
		t.Errorf("the payment regressed to %s", p.Status)
	}
}

// The rule the whole design rests on: no webhook, however well-formed, produces
// a status. Decide's only affirmative answer is "go and ask the provider".
func TestNoDeliveryEverProducesAStatus(t *testing.T) {
	p := sample()
	before := p.Status

	for _, claimed := range Statuses() {
		d := Delivery{Provider: "razorpay", EventID: "e", SignatureVerified: true,
			ProviderPaymentID: "pay_x", Claimed: claimed}
		dec := Decide(d, &p)
		if dec.Disposition == Confirm && dec.Confirm != claimed {
			t.Errorf("%s: confirmation target is %s", claimed, dec.Confirm)
		}
		if p.Status != before {
			t.Fatalf("Decide moved the payment from %s to %s", before, p.Status)
		}
	}
}

func TestAnUnsignedDeliveryIsParkedWithoutBeingRead(t *testing.T) {
	p := sample()
	// Unsigned, and claiming something that would otherwise be a valid
	// transition. Signature is checked before anything else is believed.
	d := Delivery{Provider: "razorpay", EventID: "e", SignatureVerified: false,
		ProviderPaymentID: "pay_x", Claimed: StatusAttempted}
	got := Decide(d, &p)
	if got.Disposition != Park || got.Reason != ParkSignatureInvalid {
		t.Errorf("unsigned delivery got %+v", got)
	}
	// And with no payment at all it is still the signature that is reported,
	// not the missing payment — an attacker learns nothing about what exists.
	if got := Decide(d, nil); got.Reason != ParkSignatureInvalid {
		t.Errorf("unsigned delivery for an unknown payment reported %q", got.Reason)
	}
}

func TestAWebhookForAnUnknownPaymentIsParkedNotDropped(t *testing.T) {
	d := Delivery{Provider: "razorpay", EventID: "e", SignatureVerified: true,
		ProviderPaymentID: "pay_never_seen", Claimed: StatusCaptured}
	got := Decide(d, nil)
	if got.Disposition != Park || got.Reason != ParkUnknownPayment {
		t.Errorf("unknown payment got %+v, want park/unknown_payment", got)
	}
}

func TestAnUnknownClaimIsParkedAsUnsupported(t *testing.T) {
	p := sample()
	for _, claimed := range []Status{"", "refunded", "disputed"} {
		got := Decide(Delivery{Provider: "razorpay", EventID: "e",
			SignatureVerified: true, ProviderPaymentID: "x", Claimed: claimed}, &p)
		if got.Disposition != Park || got.Reason != ParkUnsupportedEvent {
			t.Errorf("claim %q got %+v", claimed, got)
		}
	}
}

func TestTerminalStatesAbsorb(t *testing.T) {
	for _, terminal := range []Status{StatusSettled, StatusFailed, StatusExpired, StatusCancelled} {
		if !terminal.IsTerminal() {
			t.Errorf("%s is not terminal", terminal)
		}
		for _, to := range Statuses() {
			if to == terminal {
				continue
			}
			if CanTransition(terminal, to) {
				t.Errorf("%s can still become %s", terminal, to)
			}
		}
		p := sample()
		p.Status = terminal
		if err := p.ApplyConfirmed(StatusCaptured, time.Now()); !errors.Is(err, ErrStaleTransition) {
			t.Errorf("a %s payment accepted capture: %v", terminal, err)
		}
	}
	for _, live := range []Status{StatusCreated, StatusAttempted, StatusAuthorised, StatusCaptured} {
		if live.IsTerminal() {
			t.Errorf("%s is terminal, so nothing can follow it", live)
		}
	}
}

// Re-applying the state a payment is already in is a permitted no-op, and it is
// what removes every delivery counter from the design.
func TestReapplyingTheCurrentStateIsANoOp(t *testing.T) {
	p := sample()
	p.Status = StatusCaptured
	p.EntryID = "entry-1"
	p.CapturedAt = time.Unix(1_700_000_000, 0)

	if err := p.ApplyConfirmed(StatusCaptured, time.Unix(1_800_000_000, 0)); err != nil {
		t.Fatalf("reapplying captured: %v", err)
	}
	if !p.CapturedAt.Equal(time.Unix(1_700_000_000, 0)) {
		t.Errorf("the timestamp moved to %v — the second delivery rewrote when it happened", p.CapturedAt)
	}
}

func TestCaptureWithoutALedgerEntryIsRefused(t *testing.T) {
	p := sample()
	p.Status = StatusCaptured // captured and posted nothing
	if err := p.Validate(); err == nil {
		t.Error("a captured payment with no entry validated")
	}
	p.EntryID = "entry-1"
	if err := p.Validate(); err != nil {
		t.Errorf("a captured payment with an entry was refused: %v", err)
	}
	// Before capture there is nothing to post, so no entry is correct.
	p.Status, p.EntryID = StatusAttempted, ""
	if err := p.Validate(); err != nil {
		t.Errorf("an attempted payment with no entry was refused: %v", err)
	}
}

func TestValidateRefusesWhatTheSchemaWould(t *testing.T) {
	cases := map[string]func(*Payment){
		"no organisation":                      func(p *Payment) { p.TenantID = "" },
		"no property":                          func(p *Payment) { p.Property = "" },
		"no payer":                             func(p *Payment) { p.PayerID = "" },
		"payer is not a party":                 func(p *Payment) { p.PayerKind = domain.Platform },
		"zero amount":                          func(p *Payment) { p.Amount = 0 },
		"negative amount":                      func(p *Payment) { p.Amount = -1 },
		"unrepresentable":                      func(p *Payment) { p.Amount = domain.MaxSafeMinor + 1 },
		"unknown method":                       func(p *Payment) { p.Method = "crypto" },
		"unknown status":                       func(p *Payment) { p.Status = "pending" },
		"no idempotency key":                   func(p *Payment) { p.IdempotencyKey = "" },
		"no provider":                          func(p *Payment) { p.Provider = "" },
		"offline method on an online provider": func(p *Payment) { p.Method = MethodOfflineCash },
	}
	for name, break_ := range cases {
		p := sample()
		break_(&p)
		if err := p.Validate(); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if err := sample().Validate(); err != nil {
		t.Errorf("the sample payment was refused: %v", err)
	}
}

func TestOfflineMethodsAreMarkedAndOnlineOnesAreNot(t *testing.T) {
	offline := map[Method]bool{
		MethodOfflineCash: true, MethodOfflineCheque: true, MethodOfflineTransfer: true,
	}
	for _, m := range Methods() {
		if m.IsOffline() != offline[m] {
			t.Errorf("%s: IsOffline is %v", m, m.IsOffline())
		}
	}
	if len(Methods()) != 8 {
		t.Errorf("the method vocabulary has %d entries", len(Methods()))
	}
}

// The state machine, stated as a table so a change to it is visible in a diff
// rather than inferred from which test broke.
func TestTheTransitionTableIsWhatTheADRSays(t *testing.T) {
	allowed := map[Status][]Status{
		StatusCreated:    {StatusAttempted, StatusFailed, StatusExpired, StatusCancelled},
		StatusAttempted:  {StatusAuthorised, StatusCaptured, StatusFailed, StatusExpired},
		StatusAuthorised: {StatusCaptured, StatusFailed, StatusCancelled},
		StatusCaptured:   {StatusSettled},
	}
	for _, from := range Statuses() {
		want := map[Status]bool{from: true} // the no-op is always permitted
		for _, to := range allowed[from] {
			want[to] = true
		}
		for _, to := range Statuses() {
			if got := CanTransition(from, to); got != want[to] {
				t.Errorf("CanTransition(%s, %s) = %v, want %v", from, to, got, want[to])
			}
		}
	}
	// An unknown status is not a state, so nothing transitions to or from it.
	if CanTransition("pending", "pending") || CanTransition(StatusCreated, "pending") {
		t.Error("an unknown status behaves like a state")
	}
}
