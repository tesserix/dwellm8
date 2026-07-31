package domain_test

import (
	"math/rand/v2"
	"testing"
	"time"

	"github.com/tesserix/dwellm8/services/api/internal/money/domain"
)

// The property the whole ledger rests on: whatever the amounts, an entry balances
// and its reversal nets it to nothing.
//
// Table-driven tests check the cases somebody thought of. This checks the ones
// nobody did — the rounding edge in a split payment, the optional GST line at
// zero, an amount at the representable ceiling.

const cases = 2000

// Bounded at half the ADR-0007 ceiling so that a template summing two of these
// cannot overflow it: the generator's job is to find defects in the rules, not to
// rediscover that the ceiling is enforced. templates_test covers that directly.
const ceiling = domain.MaxSafeMinor / 2

func amounts(r *rand.Rand) domain.Minor {
	// Weighted towards small values, where the interesting boundaries are, but
	// reaching the ceiling often enough to exercise it.
	switch r.IntN(10) {
	case 0:
		return domain.Minor(r.Int64N(int64(ceiling)) + 1)
	case 1:
		return ceiling
	default:
		return domain.Minor(r.Int64N(5_000_000) + 1)
	}
}

func randomEntry(r *rand.Rand) (domain.Entry, error) {
	place := domain.Place{Property: propertyID, Unit: unitID}
	src := domain.Source{
		Kind: "property-test", ID: "x", IdempotencyKey: "x",
		OccurredOn: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	a, b := amounts(r), amounts(r)

	switch r.IntN(8) {
	case 0:
		return domain.Invoice(a, domain.Minor(r.IntN(2))*(b/10), place, tenantID, ownerID, src)
	case 1:
		return domain.LateFee(a, place, tenantID, ownerID, src)
	case 2:
		return domain.Payment(a, b, place, tenantID, src)
	case 3:
		return domain.PaymentWithTDS(a+b, a, place, tenantID, src)
	case 4:
		return domain.Settlement(a, place, src)
	case 5:
		return domain.DepositCollection(a, place, tenantID, src)
	case 6:
		return domain.Payout(a, place, ownerID, src)
	default:
		return domain.WriteOff(a, place, tenantID, src)
	}
}

func TestAnyEntryTheTemplatesProduceBalances(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	built := 0

	for range cases {
		e, err := randomEntry(r)
		// Every combination the generator produces is a legal one, so a refusal is
		// a defect in the rules rather than in the input. An earlier version of
		// this test skipped refusals, which made a template that could no longer
		// build an entry at all look like a pass.
		if err != nil {
			t.Fatalf("the templates refused a legal combination: %v", err)
		}
		built++
		if err := e.Validate(); err != nil {
			t.Fatalf("%s: a template produced an entry that does not validate: %v", e.Kind, err)
		}
		debits, credits := e.Totals()
		if debits != credits {
			t.Fatalf("%s: debits %s, credits %s", e.Kind, debits, credits)
		}
	}
	if built != cases {
		t.Fatalf("built %d entries from %d cases", built, cases)
	}
}

func TestAnyEntryAndItsReversalNetToZero(t *testing.T) {
	r := rand.New(rand.NewPCG(3, 4))

	for range cases {
		e, err := randomEntry(r)
		if err != nil {
			t.Fatalf("the templates refused a legal combination: %v", err)
		}
		reversal, err := domain.Reverse(e, "00000000-0000-4000-8000-000000000001",
			domain.ReasonWrongAmount, "reversal")
		if err != nil {
			t.Fatalf("%s: reversing: %v", e.Kind, err)
		}

		net := map[string]domain.Minor{}
		for _, p := range append(append([]domain.Posting{}, e.Postings...), reversal.Postings...) {
			net[p.Account] += p.Signed()
		}
		for account, amount := range net {
			if amount != 0 {
				t.Fatalf("%s: %s nets to %s after its reversal, want 0", e.Kind, account, amount)
			}
		}
	}
}
