// Package recon reconciles what a provider says it settled against what this
// system says it collected. ADR-0012.
//
// It sits beside collect for the same reason collect sits beside domain: a
// settlement batch is a different thing from a payment, and a payment is a
// different thing from an entry. collect.Payment is a collection with a
// lifecycle; recon.Batch is a provider's account of what it paid onward.
//
// The shape of the problem, and the reason this is a package rather than a
// nightly query:
//
// There are three accounts of the same money — what the provider settled, what
// payments says was collected, and what the ledger's clearing balance says is
// owed to us — so there are three pairwise comparisons and they fail differently.
// A provider line with no payment is the provider's fact and our gap. A captured
// payment with no line is money somebody else is holding. A clearing balance that
// does not equal the captured-and-unsettled total is our own bug, and no
// settlement file will ever reveal it.
//
// Reconciliation has two directions and only one of them has a row. A line that
// will not match is a row that can be flagged; a payment the provider never
// settled is an *absence*, and an absence cannot be found by looking at the
// table that was delivered. That is why Reconcile takes the captured payments as
// well as the lines, and why the missing direction needs a clock.
//
// Nothing in this package names an aggregator, writes a status, or posts an
// entry. It classifies. What may then be posted is Class.Posts(), and everything
// else waits for a person.
package recon

import (
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/tesserix/dwellm8/services/api/internal/money/domain"
	"github.com/tesserix/dwellm8/services/api/internal/money/domain/collect"
)

// Direction is which way the money moved in a settlement line, from our side.
// Amounts are positive everywhere in this module and the direction carries the
// sign, exactly as a ledger posting's side does.
type Direction string

const (
	// Inward is money the provider paid to us.
	Inward Direction = "inward"
	// Outward is money the provider took back or kept.
	Outward Direction = "outward"
)

// LineKind is what a row of a settlement file is about.
type LineKind string

const (
	// LinePayment is a collection settled onward to a bank account. The only kind
	// that can settle a payment.
	LinePayment LineKind = "payment"
	// LineRefund is money returned to a payer and deducted from the batch.
	LineRefund LineKind = "refund"
	// LineChargeback is money clawed back by the payer's bank. It is not a refund:
	// nobody here decided it, and it can arrive months later.
	LineChargeback LineKind = "chargeback"
	// LineFee is a charge billed as its own row rather than netted against a
	// payment — which is how providers bill anything not per-transaction.
	LineFee LineKind = "fee"
	// LineAdjustment is the provider correcting itself. It may go either way, and
	// it never matches a payment: an adjustment that names one is a fee or a
	// refund mislabelled.
	LineAdjustment LineKind = "adjustment"
)

var lineKinds = map[LineKind]Direction{
	LinePayment:    Inward,
	LineRefund:     Outward,
	LineChargeback: Outward,
	LineFee:        Outward,
	LineAdjustment: "", // either, and the only kind for which that is true
}

// LineKinds returns every kind, ordered, for the contract test.
func LineKinds() []LineKind {
	out := make([]LineKind, 0, len(lineKinds))
	for k := range lineKinds {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// MatchClass is what happened when one settlement line was held against the
// payment it names.
//
// Note what is not here: a "timing" class, which the story asked for. A timing
// difference is not a different kind of match — it is the same match arriving
// later than expected, and a line can be both late and fee-adjusted. Making
// lateness a class forces a choice between the two and one of them is then lost,
// so it is Match.Late and a DriftLate row instead. ADR-0012 §5.
type MatchClass string

const (
	// MatchExact is the line settling the payment to the paisa.
	MatchExact MatchClass = "exact"
	// MatchFeeAdjusted is the line's gross settling the payment and the provider
	// keeping a fee out of the payout. The receivable was settled in full; the fee
	// is an expense of ours.
	MatchFeeAdjusted MatchClass = "fee_adjusted"
	// MatchPartial is a split settlement: this line settles some of the payment
	// and more must arrive. It posts nothing, because a settlement entry for part
	// of a payment leaves a clearing residue no later line can clear.
	MatchPartial MatchClass = "partial"
	// MatchUnknown is a line naming a provider payment id this system has never
	// issued. The same fact as ADR-0011's parked webhook, arriving by the other
	// channel, and it is the one line that must never be dropped.
	MatchUnknown MatchClass = "unknown_payment"
	// MatchDuplicate is a payment settled in a batch and settled again in this
	// one. Posting it twice would credit a bank balance the bank does not have.
	MatchDuplicate MatchClass = "duplicate"
	// MatchAmountDrift is matched by id, and the money does not reconcile even
	// once the fee is accounted for. The loudest class and the only one that
	// alerts on a single occurrence.
	MatchAmountDrift MatchClass = "amount_drift"
)

var matchClasses = map[MatchClass]bool{
	MatchExact: true, MatchFeeAdjusted: true, MatchPartial: true,
	MatchUnknown: true, MatchDuplicate: true, MatchAmountDrift: true,
}

// Posts reports whether this class may produce a settlement entry without a
// person looking at it.
//
// Two classes may, and both of them account for the whole gross that the
// clearing balance is carrying. Everything else waits, because every other class
// would post an entry that leaves clearing wrong in a way the next batch cannot
// fix.
func (c MatchClass) Posts() bool {
	return c == MatchExact || c == MatchFeeAdjusted
}

// MatchClasses returns every class, ordered.
func MatchClasses() []MatchClass {
	out := make([]MatchClass, 0, len(matchClasses))
	for c := range matchClasses {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// DriftKind is a way the three accounts of the money can disagree. Each names
// which pair disagreed, because the fix is different for each and a single
// "unreconciled" bucket loses that.
type DriftKind string

const (
	// DriftMissing is a payment captured here that no settlement file has ever
	// mentioned. Provider against payments, and the direction with no row in it:
	// this one is found by a clock, not by a file.
	DriftMissing DriftKind = "missing_settlement"
	// DriftUnknownLine is a settled line naming a payment this system does not
	// have. Provider against payments, the other way round.
	DriftUnknownLine DriftKind = "unknown_line"
	// DriftAmount is a matched payment whose money does not reconcile.
	DriftAmount DriftKind = "amount_mismatch"
	// DriftDuplicate is a payment settled twice.
	DriftDuplicate DriftKind = "duplicate_settlement"
	// DriftLate is money that arrived, later than the settlement SLA. Not a loss
	// and not an error — but the difference between "late" and "lost" is a
	// judgement nobody can make without seeing how late, so it is recorded.
	DriftLate DriftKind = "late_settlement"
	// DriftClearingBalance is the ledger's clearing balance disagreeing with the
	// captured-and-unsettled total. Payments against ledger: our own bug, and the
	// one kind of drift no provider file can reveal.
	DriftClearingBalance DriftKind = "clearing_balance"
)

var driftKinds = map[DriftKind]bool{
	DriftMissing: true, DriftUnknownLine: true, DriftAmount: true,
	DriftDuplicate: true, DriftLate: true, DriftClearingBalance: true,
}

// DriftKinds returns every kind, ordered.
func DriftKinds() []DriftKind {
	out := make([]DriftKind, 0, len(driftKinds))
	for k := range driftKinds {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// Resolvable reports whether this kind of drift needs a person to close it.
// DriftLate does not: the money arrived, and the row exists so the ageing report
// can show that it was slow.
func (k DriftKind) Resolvable() bool { return k != DriftLate }

// DriftState is where a drift row has got to. The vocabulary is short on
// purpose: an operator either explained it, recovered it, or decided to stop.
type DriftState string

const (
	DriftOpen       DriftState = "open"
	DriftResolved   DriftState = "resolved"
	DriftWrittenOff DriftState = "written_off"
)

// AgeBucket is how a drift row is reported. Derived from the age, never stored —
// a stored bucket is wrong the day after it is written, and the whole point of an
// ageing report is that yesterday's three-day-old item is four days old today.
type AgeBucket string

const (
	BucketSameDay AgeBucket = "same_day"
	Bucket1To3    AgeBucket = "1_3_days"
	Bucket4To7    AgeBucket = "4_7_days"
	Bucket8To30   AgeBucket = "8_30_days"
	BucketOver30  AgeBucket = "over_30_days"
)

// Bucket places an age. The boundaries match the schema's ageing view, and the
// contract test compares the two over a table of ages.
//
// Compared as durations rather than as whole days, which is not the obvious way to
// write it. The obvious way truncates — int(age.Hours()/24) — and then an item
// three days and twenty-three hours old is "3 days" in Go while PostgreSQL's
// `<= interval '3 days'` puts it in the next bucket. Measured before this was
// fixed: an age of 73h bucketed as 1_3_days in Go and 4_7_days in the database.
// The view is what an operator queries, so the view's semantics win.
func Bucket(age time.Duration) AgeBucket {
	const day = 24 * time.Hour
	switch {
	case age < day:
		return BucketSameDay
	case age <= 3*day:
		return Bucket1To3
	case age <= 7*day:
		return Bucket4To7
	case age <= 30*day:
		return Bucket8To30
	default:
		return BucketOver30
	}
}

// Buckets returns every bucket, oldest last.
func Buckets() []AgeBucket {
	return []AgeBucket{BucketSameDay, Bucket1To3, Bucket4To7, Bucket8To30, BucketOver30}
}

// Batch is one settlement the provider made to a bank account, and its own
// account of what it consists of.
type Batch struct {
	Provider        string
	ProviderBatchID string
	// UTR is the bank reference the provider says it paid under. Not decorative:
	// it is the only handle that connects this batch to a line on a bank
	// statement, and a batch without one cannot be checked against the bank at
	// all.
	UTR       string
	SettledOn time.Time

	// The provider's own totals, which must add up before any line of the file is
	// believed. See Validate.
	GrossMinor  domain.Minor
	RefundMinor domain.Minor
	FeeMinor    domain.Minor
	TaxMinor    domain.Minor
	NetMinor    domain.Minor
}

// ErrBatchArithmetic is what a caller checks for to tell "we parsed this file
// wrong" from "this file disagrees with our records". They are different
// incidents with different responses, and the first one must never be allowed to
// look like the second.
var ErrBatchArithmetic = errors.New("recon: the settlement batch does not add up")

// Validate asserts the batch's own arithmetic, which is the first thing checked
// and the thing that stops everything if it fails.
//
// gross − refunds − fee − tax = net, from the provider's own numbers. A file we
// parsed wrong — a column misread, an amount in rupees where paise were expected
// — fails here, at ingestion, rather than as five hundred inexplicable drift rows
// tomorrow. The schema states the same constraint, so a file inserted by any path
// is checked the same way.
func (b Batch) Validate() error {
	if b.Provider == "" {
		return errors.New("recon: a settlement batch must name the adapter it came from")
	}
	if b.ProviderBatchID == "" {
		return errors.New("recon: a settlement batch with no provider id cannot be deduplicated")
	}
	if b.SettledOn.IsZero() {
		return errors.New("recon: a settlement batch must say when it settled")
	}
	for name, amount := range map[string]domain.Minor{
		"gross": b.GrossMinor, "refunds": b.RefundMinor,
		"fee": b.FeeMinor, "tax": b.TaxMinor, "net": b.NetMinor,
	} {
		if amount < 0 {
			return fmt.Errorf("recon: batch %s has a negative %s (%s): the direction is the line's kind, not its sign",
				b.ProviderBatchID, name, amount)
		}
		if err := amount.Valid(); err != nil {
			return fmt.Errorf("batch %s %s: %w", b.ProviderBatchID, name, err)
		}
	}
	if want := b.GrossMinor - b.RefundMinor - b.FeeMinor - b.TaxMinor; want != b.NetMinor {
		return fmt.Errorf("%w: batch %s says gross %s less refunds %s, fee %s and tax %s, "+
			"which is %s, and calls it %s",
			ErrBatchArithmetic, b.ProviderBatchID, b.GrossMinor, b.RefundMinor,
			b.FeeMinor, b.TaxMinor, want, b.NetMinor)
	}
	return nil
}

// Line is one row of a settlement file, normalised by the adapter.
type Line struct {
	// ProviderLineID is the provider's own id for this row. It is what
	// deduplicates a re-ingested file, so a provider that supplies none forces the
	// adapter to synthesise a stable one rather than leaving it empty.
	ProviderLineID string
	Kind           LineKind
	Direction      Direction
	// ProviderPaymentID is what this line is about, for the kinds that are about
	// a payment.
	ProviderPaymentID string

	// AmountMinor is the line's gross — what the payer paid, not what reached the
	// bank. Matching is against the gross for the reason in ADR-0012 §4: the gross
	// is what clearing is carrying.
	AmountMinor domain.Minor
	FeeMinor    domain.Minor
	TaxMinor    domain.Minor

	SettledOn time.Time
}

// Validate asserts a line's shape before it is matched against anything.
func (l Line) Validate() error {
	if l.ProviderLineID == "" {
		return errors.New("recon: a settlement line with no provider line id cannot be deduplicated, " +
			"and a re-ingested file would double every amount in it")
	}
	want, known := lineKinds[l.Kind]
	if !known {
		return fmt.Errorf("recon: unknown settlement line kind %q", l.Kind)
	}
	if l.Direction != Inward && l.Direction != Outward {
		return fmt.Errorf("recon: line %s has direction %q", l.ProviderLineID, l.Direction)
	}
	// An adjustment may go either way; nothing else may. A refund that claims to
	// be inward is a file we have misread, and it would inflate a settlement.
	if want != "" && l.Direction != want {
		return fmt.Errorf("recon: line %s is a %s and claims to be %s, and a %s is always %s",
			l.ProviderLineID, l.Kind, l.Direction, l.Kind, want)
	}
	if l.AmountMinor <= 0 {
		return fmt.Errorf("recon: line %s settles %s, which is not an amount", l.ProviderLineID, l.AmountMinor)
	}
	if err := l.AmountMinor.Valid(); err != nil {
		return fmt.Errorf("line %s: %w", l.ProviderLineID, err)
	}
	if l.FeeMinor < 0 || l.TaxMinor < 0 {
		return fmt.Errorf("recon: line %s has a negative fee or tax", l.ProviderLineID)
	}
	if l.FeeMinor+l.TaxMinor >= l.AmountMinor {
		return fmt.Errorf("recon: line %s settles %s and keeps %s of it: a provider that keeps the whole "+
			"collection is a parsing error, not a settlement",
			l.ProviderLineID, l.AmountMinor, l.FeeMinor+l.TaxMinor)
	}
	if l.Kind == LinePayment && l.ProviderPaymentID == "" {
		return fmt.Errorf("recon: line %s settles a payment and does not say which", l.ProviderLineID)
	}
	// An adjustment that names a payment is a fee or a refund that has been
	// mislabelled, and treating it as an adjustment loses the payment it concerns.
	if l.Kind == LineAdjustment && l.ProviderPaymentID != "" {
		return fmt.Errorf("recon: line %s is an adjustment and names payment %s — "+
			"an adjustment against a payment is a fee or a refund under the wrong label",
			l.ProviderLineID, l.ProviderPaymentID)
	}
	return nil
}

// Captured is a payment this system believes it collected and does not believe
// has settled. The other side of the comparison.
type Captured struct {
	PaymentID         string
	TenantID          string
	ProviderPaymentID string
	AmountMinor       domain.Minor
	CapturedAt        time.Time
	// Method, because an offline payment never appears in a settlement file and
	// its absence is not drift. Reconciling cash against a gateway's report would
	// produce one alert per cash payment, every night, until somebody turned the
	// alerting off — which is the failure mode that matters.
	Method collect.Method
}

// Match is one line held against the payment it names.
type Match struct {
	ProviderLineID    string
	ProviderPaymentID string
	PaymentID         string
	TenantID          string
	Class             MatchClass

	// GrossMinor is what the line settles; FeeMinor and TaxMinor what the provider
	// kept. These are what a posting is built from, and only when Class.Posts().
	GrossMinor domain.Minor
	FeeMinor   domain.Minor
	TaxMinor   domain.Minor

	// DriftMinor is signed: positive when the provider settled more than was
	// captured. Zero for every class but MatchAmountDrift.
	DriftMinor domain.Minor

	// Late is orthogonal to Class — see MatchClass. A late match still posts.
	Late bool
	Age  time.Duration
}

// Drift is one disagreement, in whichever direction it was found.
type Drift struct {
	Kind DriftKind
	// TenantID is empty for a line naming a payment this system does not have, and
	// for the clearing-balance check. Those rows belong to no organisation, and
	// the schema's nullable tenant_id is what lets them be kept rather than
	// guessed at. ADR-0012 §6.
	TenantID          string
	PaymentID         string
	ProviderLineID    string
	ProviderPaymentID string

	// AmountMinor is the money at stake, always positive.
	AmountMinor domain.Minor
	// Age is how long it has been wrong, at the moment of the run.
	Age time.Duration
}

// Bucket places this drift row in the ageing report.
func (d Drift) Bucket() AgeBucket { return Bucket(d.Age) }

// Input is everything one reconciliation run compares.
//
// Both sides are required, and that is the design. A run given only the lines can
// find every line that will not match and cannot find a single payment the
// provider forgot — which is the direction that loses money.
type Input struct {
	Provider string
	// AsOf is the run's clock. Passed in rather than read from the machine, so a
	// run over yesterday's file produces yesterday's answer and a test is a test.
	AsOf time.Time
	// SLA is how long after capture a settlement is expected. Beyond it, silence
	// stops being normal.
	SLA time.Duration

	Lines    []Line
	Captured []Captured
	// SettledEarlier is the provider payment ids already settled by a previous
	// batch. It is how a duplicate is detected across runs, which is the only way
	// it can be: a re-sent file is deduplicated by line id, but a provider
	// genuinely settling the same payment twice sends two different line ids.
	SettledEarlier map[string]bool
}

// Result is what a run found.
type Result struct {
	Matches []Match
	Drift   []Drift

	LinesRead    int
	LinesMatched int
	// SettledMinor is the gross of everything that matched well enough to post.
	SettledMinor domain.Minor
	// SkippedOffline is how many captured payments were excluded because no
	// provider will ever settle them. Reported rather than silent: a number that
	// suddenly jumps is a method being recorded as offline that is not.
	SkippedOffline int
}

// Unresolved counts the drift a person still has to do something about. DriftLate
// is excluded: the money arrived.
func (r Result) Unresolved() int {
	n := 0
	for _, d := range r.Drift {
		if d.Kind.Resolvable() {
			n++
		}
	}
	return n
}

// UnresolvedMinor is the money those rows are worth. The count and the amount are
// both needed and neither substitutes for the other — see Thresholds.
func (r Result) UnresolvedMinor() domain.Minor {
	var total domain.Minor
	for _, d := range r.Drift {
		if d.Kind.Resolvable() {
			total += d.AmountMinor
		}
	}
	return total
}

// Reconcile compares the two accounts of the money. It is a pure function: it
// reads no clock, opens no connection, and writes nothing.
//
// The order below is the whole rule, and step 2 is the one most implementations
// do not have.
func Reconcile(in Input) (Result, error) {
	if in.AsOf.IsZero() {
		return Result{}, errors.New("recon: a run needs an as-of instant, or an ageing report ages against nothing")
	}
	if in.SLA <= 0 {
		return Result{}, errors.New("recon: a settlement SLA of zero would make every unsettled payment " +
			"missing the moment it was captured")
	}

	byProviderID := make(map[string]Captured, len(in.Captured))
	var res Result
	for _, c := range in.Captured {
		// Offline money has no provider to settle it, and comparing it against a
		// gateway's file produces one alert per cash payment forever.
		if c.Method.IsOffline() {
			res.SkippedOffline++
			continue
		}
		if c.ProviderPaymentID == "" {
			// Captured, with no provider id: nothing can ever match it, and it is
			// not the provider's fault. Surfaced as missing so it is visible rather
			// than skipped, since a captured payment we cannot identify to the
			// provider is money we cannot chase.
			res.Drift = append(res.Drift, Drift{
				Kind: DriftMissing, TenantID: c.TenantID, PaymentID: c.PaymentID,
				AmountMinor: c.AmountMinor, Age: in.AsOf.Sub(c.CapturedAt),
			})
			continue
		}
		byProviderID[c.ProviderPaymentID] = c
	}

	// 1. Every line, held against the payment it names.
	//
	// The three running totals are per provider payment id, not per line, because
	// a split settlement's completing line has to post the whole payment: clearing
	// was debited once with the gross when the payment was captured, so it has to
	// be credited once with the gross or it keeps a residue no later line clears.
	settledThisRun := map[string]domain.Minor{}
	feeThisRun := map[string]domain.Minor{}
	taxThisRun := map[string]domain.Minor{}
	for _, l := range in.Lines {
		if err := l.Validate(); err != nil {
			return Result{}, err
		}
		res.LinesRead++
		// Only a payment line settles a payment. A fee or an adjustment is the
		// batch's arithmetic, already checked by Batch.Validate.
		if l.Kind != LinePayment {
			continue
		}

		m := Match{
			ProviderLineID:    l.ProviderLineID,
			ProviderPaymentID: l.ProviderPaymentID,
			GrossMinor:        l.AmountMinor,
			FeeMinor:          l.FeeMinor,
			TaxMinor:          l.TaxMinor,
		}

		c, known := byProviderID[l.ProviderPaymentID]
		switch {
		case !known:
			// The provider settled money against something this system cannot
			// find. Kept, never dropped, and attributed to nobody.
			m.Class = MatchUnknown
			res.Matches = append(res.Matches, m)
			res.Drift = append(res.Drift, Drift{
				Kind: DriftUnknownLine, ProviderLineID: l.ProviderLineID,
				ProviderPaymentID: l.ProviderPaymentID, AmountMinor: l.AmountMinor,
				Age: in.AsOf.Sub(l.SettledOn),
			})
			continue

		case in.SettledEarlier[l.ProviderPaymentID]:
			m.Class = MatchDuplicate
			m.PaymentID, m.TenantID = c.PaymentID, c.TenantID
			res.Matches = append(res.Matches, m)
			res.Drift = append(res.Drift, Drift{
				Kind: DriftDuplicate, TenantID: c.TenantID, PaymentID: c.PaymentID,
				ProviderLineID: l.ProviderLineID, ProviderPaymentID: l.ProviderPaymentID,
				AmountMinor: l.AmountMinor, Age: in.AsOf.Sub(l.SettledOn),
			})
			continue
		}

		m.PaymentID, m.TenantID = c.PaymentID, c.TenantID
		cumulative := settledThisRun[l.ProviderPaymentID] + l.AmountMinor
		settledThisRun[l.ProviderPaymentID] = cumulative
		feeThisRun[l.ProviderPaymentID] += l.FeeMinor
		taxThisRun[l.ProviderPaymentID] += l.TaxMinor

		// The gross is what clearing carries, so the gross is what must reconcile.
		// There is no tolerance here on purpose: see ADR-0012's alternative C.
		switch {
		case cumulative > c.AmountMinor:
			m.Class = MatchAmountDrift
			m.DriftMinor = cumulative - c.AmountMinor
			res.Drift = append(res.Drift, Drift{
				Kind: DriftAmount, TenantID: c.TenantID, PaymentID: c.PaymentID,
				ProviderLineID: l.ProviderLineID, ProviderPaymentID: l.ProviderPaymentID,
				AmountMinor: m.DriftMinor, Age: in.AsOf.Sub(l.SettledOn),
			})
		case cumulative < c.AmountMinor:
			// A split settlement, and it posts nothing until it is whole: an entry
			// for part of a payment leaves a clearing residue no later line clears.
			m.Class = MatchPartial
			m.DriftMinor = cumulative - c.AmountMinor
		default:
			// Whole. The posting is built from the running totals rather than from
			// this line, so the completing half of a split settlement clears the
			// whole gross and carries every fee taken out of it along the way.
			m.GrossMinor = cumulative
			m.FeeMinor = feeThisRun[l.ProviderPaymentID]
			m.TaxMinor = taxThisRun[l.ProviderPaymentID]
			if m.FeeMinor > 0 || m.TaxMinor > 0 {
				m.Class = MatchFeeAdjusted
			} else {
				m.Class = MatchExact
			}
		}

		if !l.SettledOn.IsZero() && l.SettledOn.Sub(c.CapturedAt) > in.SLA {
			m.Late = true
			m.Age = l.SettledOn.Sub(c.CapturedAt)
			res.Drift = append(res.Drift, Drift{
				Kind: DriftLate, TenantID: c.TenantID, PaymentID: c.PaymentID,
				ProviderLineID: l.ProviderLineID, ProviderPaymentID: l.ProviderPaymentID,
				AmountMinor: l.AmountMinor, Age: m.Age,
			})
		}

		if m.Class.Posts() {
			res.LinesMatched++
			res.SettledMinor += m.GrossMinor
		}
		res.Matches = append(res.Matches, m)
	}

	// 2. The direction with no row in it. A captured payment that no line
	//    mentioned, or that the lines did not finish settling, and that has been
	//    waiting longer than the SLA. This is the step that finds the three
	//    payments missing from a file of five hundred, and it is the step that is
	//    absent from every reconciliation that only reads the file.
	for id, c := range byProviderID {
		if settled := settledThisRun[id]; settled >= c.AmountMinor {
			continue
		}
		age := in.AsOf.Sub(c.CapturedAt)
		if age <= in.SLA {
			continue // not yet late; silence is still normal
		}
		outstanding := c.AmountMinor - settledThisRun[id]
		res.Drift = append(res.Drift, Drift{
			Kind: DriftMissing, TenantID: c.TenantID, PaymentID: c.PaymentID,
			ProviderPaymentID: id, AmountMinor: outstanding, Age: age,
		})
	}

	// Deterministic order: map iteration above is not, and a run whose output
	// order changes between identical inputs cannot be diffed or tested.
	sort.SliceStable(res.Drift, func(i, j int) bool {
		a, b := res.Drift[i], res.Drift[j]
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		if a.PaymentID != b.PaymentID {
			return a.PaymentID < b.PaymentID
		}
		return a.ProviderLineID < b.ProviderLineID
	})
	return res, nil
}

// ClearingCheck compares the ledger's clearing balance against what payments says
// is captured and unsettled. ADR-0012 §3.
//
// This is the third comparison, and it is the only one that finds our own bugs.
// A payment marked settled whose settlement entry was never posted, or an entry
// posted twice, shows up here and nowhere else — no settlement file disagrees
// with us, because the provider did nothing wrong.
//
// balance is the clearing account's balance, which is a debit-normal asset, so it
// is positive when the provider is holding our money.
// It carries no age: the disagreement is true at the instant it is computed, and
// how long it has been true is not knowable from two balances.
func ClearingCheck(balance, capturedUnsettled domain.Minor) *Drift {
	if balance == capturedUnsettled {
		return nil
	}
	diff := balance - capturedUnsettled
	if diff < 0 {
		diff = -diff
	}
	return &Drift{Kind: DriftClearingBalance, AmountMinor: diff}
}

// RunState is where one provider-day's reconciliation got to.
//
// The vocabulary is the acceptance criterion made into data. Note that
// StateReconciled does not mean "everything matched" — a day with three missing
// payments is fully reconciled in the sense that matters, which is that the
// comparison ran and named what is wrong. StateIncomplete is the one that must
// not be reachable by a job that did not see the file, and the schema refuses it
// rather than trusting the job. ADR-0012 §8.
type RunState string

const (
	// StateRunning is a run in progress. A run that stays here is a run that died,
	// and the watchdog treats it exactly as it treats a run that never started.
	StateRunning RunState = "running"
	// StateReconciled is the comparison completed with nothing outstanding.
	StateReconciled RunState = "reconciled"
	// StateDrift is the comparison completed and something is outstanding. A
	// finished state, not a failed one: the job did its job.
	StateDrift RunState = "drift"
	// StateIncomplete is the file never arrived, or arrived partially. Explicitly
	// not reconciled, however long it stays this way — a day that quietly becomes
	// reconciled because nobody looked is the failure this state exists to make
	// impossible.
	StateIncomplete RunState = "incomplete"
	// StateFailed is the run erroring for a reason that is ours.
	StateFailed RunState = "failed"
)

var runStates = map[RunState]bool{
	StateRunning: true, StateReconciled: true, StateDrift: true,
	StateIncomplete: true, StateFailed: true,
}

// RunStates returns every state, ordered.
func RunStates() []RunState {
	out := make([]RunState, 0, len(runStates))
	for s := range runStates {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// Settled reports whether a run reached an ending. A run that has not is a run
// the watchdog is still counting against the clock.
func (s RunState) Settled() bool {
	return s == StateReconciled || s == StateDrift
}

// StateFor is the state a completed run is in, given what it found. It exists so
// that the answer is computed from the result rather than chosen by the caller —
// a job that decides for itself whether it reconciled the day is a job that will
// eventually decide wrong.
func StateFor(res Result, filePresent bool) RunState {
	if !filePresent {
		return StateIncomplete
	}
	if res.Unresolved() > 0 {
		return StateDrift
	}
	return StateReconciled
}

// Thresholds is when an operator is woken up. ADR-0012 §9.
type Thresholds struct {
	// Count and AmountMinor are ORed, not ANDed, and both are needed. One payment
	// of five lakh and three hundred payments of fifty rupees are both incidents,
	// and either threshold alone misses one of them.
	Count       int
	AmountMinor domain.Minor
	// StaleAfter is how long a provider-day may go without a settled run before
	// the absence itself alerts.
	StaleAfter time.Duration
}

// The ceilings. Configuration decides the thresholds; it does not get to decide
// that there is effectively no alerting. A threshold set to a number no incident
// will reach is indistinguishable from alerting being switched off, and it
// survives review because it looks like a number rather than like a decision.
const (
	MaxAlertCount       = 25
	MaxAlertAmountMinor = domain.Minor(10_000_000) // ₹1,00,000
	MaxStaleAfter       = 48 * time.Hour
)

// Validate enforces the ceilings at startup, like the provider chain does. A
// deployment whose alerting could never fire fails to boot rather than going
// quiet.
func (t Thresholds) Validate() error {
	if t.Count < 0 || t.AmountMinor < 0 || t.StaleAfter < 0 {
		return errors.New("recon: a negative alert threshold")
	}
	if t.Count > MaxAlertCount {
		return fmt.Errorf("recon: an alert threshold of %d unreconciled items is above the ceiling of %d — "+
			"a threshold no incident reaches is alerting switched off in the shape of a number",
			t.Count, MaxAlertCount)
	}
	if t.AmountMinor > MaxAlertAmountMinor {
		return fmt.Errorf("recon: an alert threshold of %s is above the ceiling of %s",
			t.AmountMinor, MaxAlertAmountMinor)
	}
	if t.StaleAfter > MaxStaleAfter {
		return fmt.Errorf("recon: waiting %s before alerting on an unreconciled day is above the ceiling of %s — "+
			"the story asks for unmatched money to be detected within a day",
			t.StaleAfter, MaxStaleAfter)
	}
	return nil
}

// Alert is what is raised. It names the count and the amount because that is
// what the person reading it at 3am needs in the first line, and because an alert
// that says "reconciliation drift detected" and nothing else is an alert people
// learn to close.
type Alert struct {
	Kind     DriftKind
	Provider string
	Count    int
	Minor    domain.Minor
	Oldest   AgeBucket
	Message  string
}

// Alerts is what a completed run raises.
//
// Grouped by drift kind rather than emitted per row: three missing payments is
// one incident with three items in it, and three alerts is how a pager gets
// muted. An amount mismatch is the exception — it alerts on a single occurrence
// regardless of thresholds, because the money does not add up and no volume makes
// that acceptable.
func Alerts(provider string, res Result, th Thresholds) []Alert {
	type group struct {
		count  int
		minor  domain.Minor
		oldest time.Duration
	}
	groups := map[DriftKind]*group{}
	for _, d := range res.Drift {
		if !d.Kind.Resolvable() {
			continue
		}
		g := groups[d.Kind]
		if g == nil {
			g = &group{}
			groups[d.Kind] = g
		}
		g.count++
		g.minor += d.AmountMinor
		if d.Age > g.oldest {
			g.oldest = d.Age
		}
	}

	var out []Alert
	for _, kind := range DriftKinds() {
		g := groups[kind]
		if g == nil {
			continue
		}
		// The money not adding up is not a matter of degree.
		always := kind == DriftAmount || kind == DriftDuplicate || kind == DriftClearingBalance
		if !always && g.count <= th.Count && g.minor <= th.AmountMinor {
			continue
		}
		out = append(out, Alert{
			Kind: kind, Provider: provider, Count: g.count, Minor: g.minor,
			Oldest:  Bucket(g.oldest),
			Message: fmt.Sprintf("%s: %d item(s) worth %s, oldest %s", kind, g.count, g.minor, Bucket(g.oldest)),
		})
	}
	return out
}

// RunSummary is what the watchdog reads: one provider-day and where it got to.
type RunSummary struct {
	Provider  string
	AsOfDate  time.Time
	State     RunState
	UpdatedAt time.Time
}

// StaleRuns is the alert for a day that was never reconciled, and it is
// deliberately computed from the run table alone.
//
// The reason is the one thing about this design that is easy to get wrong: the
// reconciler cannot be the thing that alerts on the reconciler being down. A job
// that never ran raises no alerts, and a job that died mid-run leaves a `running`
// row and raises none either. So the check that matters most reads only what the
// runs recorded, has no dependency on a run happening, and treats a run stuck in
// `running` exactly as it treats a day with no run at all.
func StaleRuns(runs []RunSummary, asOf time.Time, th Thresholds) []Alert {
	var out []Alert
	for _, r := range runs {
		if r.State.Settled() {
			continue
		}
		since := asOf.Sub(r.AsOfDate)
		if since <= th.StaleAfter {
			continue
		}
		out = append(out, Alert{
			Kind:     DriftMissing,
			Provider: r.Provider,
			Count:    1,
			Oldest:   Bucket(since),
			Message: fmt.Sprintf("%s has no settled reconciliation for %s (%s for %s) — "+
				"the day is not reconciled and the money in it is unaccounted for",
				r.Provider, r.AsOfDate.Format("2006-01-02"), r.State, Bucket(since)),
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Message < out[j].Message })
	return out
}
