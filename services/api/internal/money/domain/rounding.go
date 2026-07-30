package domain

import (
	"fmt"
	"math/bits"
	"sort"
)

// Rounding, rates and allocation. ADR-0007 §2, §3 and §4.
//
// Every fraction in this product comes from a division: a month's rent split
// across days, a percentage fee, a deduction at source. There is exactly one
// division primitive below, it rounds once, and everything else calls it — so
// "which way does this round" has one answer for the whole platform rather than
// one per caller.
//
// No float appears here or anywhere under internal/money. The guard in
// internal/platform/arch fails the build on one, and CI plants a float to prove
// the guard still fires.

// mulDivRound computes amount × num ÷ den, rounded half away from zero, with no
// intermediate loss.
//
// The multiplication is carried in 128 bits, so 2,750,000 paise × 17 is exact
// before anything is divided. Rounding half *away from zero* rather than half up
// is the difference that matters for corrections: it makes the rule symmetric,
// so round(-x) is always -round(x) and a reversing entry reverses to the paisa.
// Half up would round -0.5 to 0 and +0.5 to 1, and an entry reversed at that
// boundary would leave a stranded paisa in the ledger forever.
func mulDivRound(amount Minor, num, den int64) (Minor, error) {
	if den <= 0 {
		return 0, fmt.Errorf("money: division by %d", den)
	}
	if num < 0 {
		return 0, fmt.Errorf("money: negative multiplier %d — the caller's sign convention, not this one", num)
	}
	if err := amount.Valid(); err != nil {
		return 0, err
	}

	negative := amount < 0
	magnitude := uint64(amount)
	if negative {
		// Unsigned negation is exact for every int64, math.MinInt64 included,
		// which -amount is not.
		magnitude = -magnitude
	}

	hi, lo := bits.Mul64(magnitude, uint64(num))
	if hi >= uint64(den) {
		return 0, fmt.Errorf("money: %d × %d ÷ %d does not fit in an int64", int64(amount), num, den)
	}
	quotient, remainder := bits.Div64(hi, lo, uint64(den))

	// Round away from zero when the remainder is at least half the divisor.
	// Written as a subtraction rather than remainder×2 so it cannot overflow
	// for a divisor near the top of the range.
	if remainder >= uint64(den)-remainder {
		quotient++
	}

	out := Minor(quotient)
	if negative {
		out = -out
	}
	return out, out.Valid()
}

// Round is the platform's rounding rule, exposed for the one case that needs it
// directly: turning a quantity already expressed in some finer unit into paise.
//
// It is not a general "tidy this number" helper. Rounding is permitted once, at
// the posting boundary — ADR-0007 §2 — and every posting amount reaching the
// ledger has been through this function or through Allocate exactly once.
func Round(numerator, denominator int64) (Minor, error) {
	return mulDivRound(Minor(numerator), 1, denominator)
}

// Rate is a proportion in basis points: one hundredth of one percent, so 1800 is
// GST at 18% and 299 is the platform fee at 2.99%.
//
// Basis points rather than a decimal type because every rate this product
// applies — GST slabs, TDS under 194-I and 194-IB, the platform fee — is exact
// in hundredths of a percent, and an integer rate cannot drift the way a parsed
// decimal can. What the rates themselves are is not this ADR's business; the tax
// module owns which rate applies to which supply.
type Rate int64

// BasisPointsPerUnit is the denominator: 100% is 10,000 basis points.
const BasisPointsPerUnit int64 = 10_000

// Of applies the rate, rounding once. 2.99% of ₹25,000 is 74,750 paise exactly;
// 18% of ₹1,499 is 26,982 paise; neither ever sees a float.
func (r Rate) Of(amount Minor) (Minor, error) {
	if r < 0 {
		return 0, fmt.Errorf("money: negative rate %d bp — a credit is a side, not a sign", int64(r))
	}
	return mulDivRound(amount, int64(r), BasisPointsPerUnit)
}

// String renders the rate as a percentage: "2.99%", "18%", "0.5%".
func (r Rate) String() string {
	whole, frac := int64(r)/100, int64(r)%100
	sign := ""
	if r < 0 {
		sign, whole, frac = "-", -whole, -frac
	}
	switch {
	case frac == 0:
		return fmt.Sprintf("%s%d%%", sign, whole)
	case frac%10 == 0:
		return fmt.Sprintf("%s%d.%d%%", sign, whole, frac/10)
	default:
		return fmt.Sprintf("%s%d.%02d%%", sign, whole, frac)
	}
}

// Prorate returns the part of an amount that belongs to days out of inPeriod.
//
// This is the single-slice answer, and it is the right one when the rest of the
// amount is not also being posted — a joining tenant's first month, where the
// days before they moved in are nobody's charge. When both sides of the split
// are posted, use Allocate: two independently rounded halves can miss the whole
// by a paisa, and Allocate cannot.
func Prorate(amount Minor, days, inPeriod int) (Minor, error) {
	if days < 0 {
		return 0, fmt.Errorf("money: %d days", days)
	}
	if inPeriod <= 0 {
		return 0, fmt.Errorf("money: a period of %d days", inPeriod)
	}
	return mulDivRound(amount, int64(days), int64(inPeriod))
}

// Allocate splits an amount across weights so that the slices sum to the amount
// exactly. ADR-0007 §4.
//
// Largest remainder: every slice gets the truncated share, then the paise left
// over — always fewer than there are slices — go one each to the slices with the
// largest discarded fraction, ties to the earlier slice so the result is
// deterministic and a re-run of the same split produces the same rows.
//
// This is what makes the ledger's balance property survive division. Rounding
// each slice on its own is the classic defect: ₹27,500 split across three
// tenants rounds to three slices totalling ₹27,499.99, the entry does not
// balance, and the deferred constraint trigger rejects it at COMMIT with nothing
// to point at. Here the slices are correct by construction and the whole is
// preserved by the arithmetic rather than by a fix-up on the last line — a
// fix-up which would quietly make the last party pay for everybody's rounding.
//
// A zero weight gets zero. Weights need no particular scale: days, square feet,
// share counts and equal ones all work.
func Allocate(total Minor, weights []int64) ([]Minor, error) {
	if len(weights) == 0 {
		return nil, fmt.Errorf("money: allocating %s across no slices", total)
	}
	if err := total.Valid(); err != nil {
		return nil, err
	}

	var sum uint64
	for i, w := range weights {
		if w < 0 {
			return nil, fmt.Errorf("money: slice %d has weight %d", i, w)
		}
		if sum+uint64(w) < sum {
			return nil, fmt.Errorf("money: the weights sum beyond what can be divided by")
		}
		sum += uint64(w)
	}
	if sum == 0 {
		return nil, fmt.Errorf("money: the weights are all zero, so there is no share to compute")
	}

	negative := total < 0
	magnitude := uint64(total)
	if negative {
		magnitude = -magnitude
	}

	out := make([]Minor, len(weights))
	remainders := make([]uint64, len(weights))
	var assigned uint64
	for i, w := range weights {
		hi, lo := bits.Mul64(magnitude, uint64(w))
		if hi >= sum {
			return nil, fmt.Errorf("money: slice %d overflows while dividing %s", i, total)
		}
		share, remainder := bits.Div64(hi, lo, sum)
		out[i], remainders[i], assigned = Minor(share), remainder, assigned+share
	}

	// Fewer left over than there are slices, by the definition of truncation.
	order := make([]int, len(weights))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool { return remainders[order[a]] > remainders[order[b]] })
	for k := uint64(0); k < magnitude-assigned; k++ {
		out[order[k]]++
	}

	if negative {
		for i := range out {
			out[i] = -out[i]
		}
	}
	for i := range out {
		if err := out[i].Valid(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// AllocateEqually splits an amount into n slices of equal weight — the common
// case, and the one people otherwise write as total/n and lose the remainder to.
func AllocateEqually(total Minor, n int) ([]Minor, error) {
	if n <= 0 {
		return nil, fmt.Errorf("money: allocating %s across %d slices", total, n)
	}
	weights := make([]int64, n)
	for i := range weights {
		weights[i] = 1
	}
	return Allocate(total, weights)
}
