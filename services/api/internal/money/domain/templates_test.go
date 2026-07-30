package domain_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/tesserix/dwellm8/services/api/internal/money/domain"
)

// ADR-0006, tested where it can be tested without a database: the arithmetic.
//
// The database enforces the same balance rule at commit and is the authority —
// it catches anything written by any path, including a psql prompt. These tests
// are about the other half: that the templates produce the postings the ADR says
// they do, for the events issue #7 names, including the three edge cases that
// are the whole reason a payment is not just "credit the receivable".

const (
	tenantID   = "aaaaaaaa-0000-0000-0000-000000000001"
	ownerID    = "bbbbbbbb-0000-0000-0000-000000000001"
	propertyID = "cccccccc-0000-0000-0000-000000000001"
	unitID     = "dddddddd-0000-0000-0000-000000000001"
)

func place() domain.Place { return domain.Place{Property: propertyID, Unit: unitID} }

func src(key string) domain.Source {
	return domain.Source{
		Kind: "test", ID: key, IdempotencyKey: key,
		OccurredOn: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
	}
}

// posted returns the signed total for one account, which is what a balance is.
func posted(e domain.Entry, account string) domain.Minor {
	var total domain.Minor
	for _, p := range e.Postings {
		if p.Account == account {
			total += p.Signed()
		}
	}
	return total
}

func mustBalance(t *testing.T, e domain.Entry) {
	t.Helper()
	if err := e.Validate(); err != nil {
		t.Fatalf("entry %s is invalid: %v", e.Kind, err)
	}
	debits, credits := e.Totals()
	if debits != credits {
		t.Fatalf("entry %s: debits %s, credits %s", e.Kind, debits, credits)
	}
}

// Issue #7's primary scenario: a rent invoice of 25000 followed by a UPI payment
// of 25000. The receivable nets to zero and rent income is credited 25000.
func TestInvoiceThenPaymentNetsTheReceivableToZero(t *testing.T) {
	const rent = domain.Minor(2500000) // ₹25,000 in paise

	invoice, err := domain.Invoice(rent, 0, place(), tenantID, ownerID, src("inv-1"))
	if err != nil {
		t.Fatalf("invoicing: %v", err)
	}
	mustBalance(t, invoice)

	payment, err := domain.Payment(rent, rent, place(), tenantID, src("pay-1"))
	if err != nil {
		t.Fatalf("paying: %v", err)
	}
	mustBalance(t, payment)

	if got := posted(invoice, domain.TenantReceivable) + posted(payment, domain.TenantReceivable); got != 0 {
		t.Fatalf("the receivable nets to %s after an invoice and a matching payment, want 0", got)
	}
	if got := posted(invoice, domain.RentIncome); got != -rent {
		t.Fatalf("rent income moved %s, want a credit of %s", got, rent)
	}
	// The money is with the provider, not in the bank. Anything else makes a
	// settlement reconciliation unclosable.
	if got := posted(payment, domain.GatewayClearing); got != rent {
		t.Fatalf("gateway clearing moved %s, want a debit of %s", got, rent)
	}
	if got := posted(payment, domain.Bank); got != 0 {
		t.Fatalf("a payment touched the bank account (%s) before the provider settled it", got)
	}

	settlement, err := domain.Settlement(rent, place(), src("stl-1"))
	if err != nil {
		t.Fatalf("settling: %v", err)
	}
	mustBalance(t, settlement)
	if got := posted(payment, domain.GatewayClearing) + posted(settlement, domain.GatewayClearing); got != 0 {
		t.Fatalf("clearing nets to %s once settled, want 0", got)
	}
}

// An exempt supply has no GST line at all, rather than a zero one.
func TestAnExemptInvoiceHasNoTaxLine(t *testing.T) {
	e, err := domain.Invoice(2500000, 0, place(), tenantID, ownerID, src("inv-exempt"))
	if err != nil {
		t.Fatalf("invoicing: %v", err)
	}
	for _, p := range e.Postings {
		if p.Account == domain.GSTOutput {
			t.Fatal("an exempt invoice carries a GST posting — zero rows in every statement, meaning nothing")
		}
	}
	if len(e.Postings) != 2 {
		t.Fatalf("an exempt invoice has %d postings, want 2", len(e.Postings))
	}
}

func TestATaxableInvoiceSplitsTheChargeFromTheTax(t *testing.T) {
	const net, tax = domain.Minor(5000000), domain.Minor(900000) // ₹50,000 + 18%

	e, err := domain.Invoice(net, tax, place(), tenantID, ownerID, src("inv-gst"))
	if err != nil {
		t.Fatalf("invoicing: %v", err)
	}
	mustBalance(t, e)

	if got := posted(e, domain.TenantReceivable); got != net+tax {
		t.Fatalf("the tenant is billed %s, want %s — tax is collected from the tenant, not absorbed", got, net+tax)
	}
	if got := posted(e, domain.RentIncome); got != -net {
		t.Fatalf("income is %s, want %s — GST is the government's money and was never income", got, -net)
	}
	if got := posted(e, domain.GSTOutput); got != -tax {
		t.Fatalf("GST payable is %s, want %s", got, -tax)
	}
}

// Issue #7's edge cases, in one table: partial, exact, over, and a payment that
// arrives before there is anything to pay.
func TestPaymentSplitsBetweenPrincipalAndAdvance(t *testing.T) {
	for _, tc := range []struct {
		name                       string
		received, outstanding      domain.Minor
		wantPrincipal, wantAdvance domain.Minor
		wantPostings               int
	}{
		{"partial", 1000000, 2500000, 1000000, 0, 2},
		{"exact", 2500000, 2500000, 2500000, 0, 2},
		{"overpayment", 3000000, 2500000, 2500000, 500000, 3},
		{"before its invoice", 2500000, 0, 0, 2500000, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, err := domain.Payment(tc.received, tc.outstanding, place(), tenantID, src("pay-"+tc.name))
			if err != nil {
				t.Fatalf("a payment of %s against %s outstanding was refused: %v", tc.received, tc.outstanding, err)
			}
			mustBalance(t, e)

			if got := -posted(e, domain.TenantReceivable); got != tc.wantPrincipal {
				t.Fatalf("%s settled against the receivable, want %s", got, tc.wantPrincipal)
			}
			if got := -posted(e, domain.TenantAdvance); got != tc.wantAdvance {
				t.Fatalf("%s went to advance, want %s — anything beyond what was owed is a liability, "+
					"not a receivable with a credit balance", got, tc.wantAdvance)
			}
			if len(e.Postings) != tc.wantPostings {
				t.Fatalf("%d postings, want %d", len(e.Postings), tc.wantPostings)
			}
		})
	}
}

// A payment before its invoice is a normal event, and the ADR says so. This is
// the assertion that stops somebody "fixing" it into an error.
func TestAnAdvanceIsNotAnError(t *testing.T) {
	e, err := domain.Payment(2500000, 0, place(), tenantID, src("pay-advance"))
	if err != nil {
		t.Fatalf("a payment arriving before its invoice was refused (%v) — it is an advance, not a mistake", err)
	}
	if posted(e, domain.TenantAdvance) != -2500000 {
		t.Fatal("the whole receipt should be held as an advance")
	}
}

func TestTDSSettlesTheReceivableInFull(t *testing.T) {
	const gross, tds = domain.Minor(5000000), domain.Minor(500000) // 10% on ₹50,000

	e, err := domain.PaymentWithTDS(gross, tds, place(), tenantID, src("pay-tds"))
	if err != nil {
		t.Fatalf("TDS payment: %v", err)
	}
	mustBalance(t, e)

	if got := -posted(e, domain.TenantReceivable); got != gross {
		t.Fatalf("the receivable was reduced by %s, want %s — a deduction at source is not a discount, "+
			"the tenant paid it to the government on the owner's behalf", got, gross)
	}
	if got := posted(e, domain.TDSReceivable); got != tds {
		t.Fatalf("TDS receivable is %s, want %s — the credit is an asset, not a loss", got, tds)
	}
	if got := posted(e, domain.GatewayClearing); got != gross-tds {
		t.Fatalf("cash received is %s, want %s", got, gross-tds)
	}
}

func TestADepositIsALiabilityAndNeverIncome(t *testing.T) {
	const deposit = domain.Minor(15000000)

	in, err := domain.DepositCollection(deposit, place(), tenantID, src("dep-1"))
	if err != nil {
		t.Fatalf("collecting: %v", err)
	}
	mustBalance(t, in)
	if got := posted(in, domain.DepositLiability); got != -deposit {
		t.Fatalf("the deposit posted %s to the liability, want %s", got, -deposit)
	}
	if posted(in, domain.RentIncome) != 0 || posted(in, domain.LateFeeIncome) != 0 {
		t.Fatal("a deposit reached an income account — it is the tenant's money, held")
	}

	out, err := domain.DepositRefund(deposit, place(), tenantID, src("dep-2"))
	if err != nil {
		t.Fatalf("refunding: %v", err)
	}
	mustBalance(t, out)
	if got := posted(in, domain.DepositLiability) + posted(out, domain.DepositLiability); got != 0 {
		t.Fatalf("the deposit liability nets to %s after a full refund, want 0", got)
	}
}

func TestTheOwnerIsPaidNetOfTheFee(t *testing.T) {
	const collected, feeNet, feeTax = domain.Minor(2500000), domain.Minor(74750), domain.Minor(13455)

	fee, err := domain.PlatformFee(feeNet, feeTax, place(), ownerID, src("fee-1"))
	if err != nil {
		t.Fatalf("charging the fee: %v", err)
	}
	mustBalance(t, fee)
	if got := posted(fee, domain.OwnerPayable); got != feeNet+feeTax {
		t.Fatalf("the fee reduced what the owner is owed by %s, want %s", got, feeNet+feeTax)
	}
	if got := posted(fee, domain.PlatformFeeIncome); got != -feeNet {
		t.Fatalf("platform fee income is %s, want %s — GST on the fee is not the platform's money", got, -feeNet)
	}

	payout, err := domain.Payout(collected-feeNet-feeTax, place(), ownerID, src("out-1"))
	if err != nil {
		t.Fatalf("paying out: %v", err)
	}
	mustBalance(t, payout)
	if got := posted(payout, domain.Bank); got != -(collected - feeNet - feeTax) {
		t.Fatalf("the bank moved %s on payout", got)
	}
}

func TestAWriteOffKeepsTheInvoice(t *testing.T) {
	const debt = domain.Minor(2500000)

	e, err := domain.WriteOff(debt, place(), tenantID, src("wo-1"))
	if err != nil {
		t.Fatalf("writing off: %v", err)
	}
	mustBalance(t, e)
	if got := posted(e, domain.WriteOffExpense); got != debt {
		t.Fatalf("the write-off expense is %s, want %s — abandoning a debt costs something, "+
			"and the cost is the point", got, debt)
	}
	if got := -posted(e, domain.TenantReceivable); got != debt {
		t.Fatalf("the receivable was cleared by %s, want %s", got, debt)
	}
}

// The correction mechanism. ADR-0006 §3.
func TestReversalMirrorsEveryLineAndNeedsAReason(t *testing.T) {
	original, err := domain.Invoice(2500000, 450000, place(), tenantID, ownerID, src("inv-2"))
	if err != nil {
		t.Fatalf("invoicing: %v", err)
	}

	rev, err := domain.Reverse(original, "entry-1", domain.ReasonWrongAmount, "rev-1")
	if err != nil {
		t.Fatalf("reversing: %v", err)
	}
	mustBalance(t, rev)

	if len(rev.Postings) != len(original.Postings) {
		t.Fatalf("the reversal has %d postings, the original %d — a partial reversal leaves "+
			"a balance nobody can explain", len(rev.Postings), len(original.Postings))
	}
	for _, account := range []string{domain.TenantReceivable, domain.RentIncome, domain.GSTOutput} {
		if got := posted(original, account) + posted(rev, account); got != 0 {
			t.Fatalf("%s nets to %s after reversal, want 0", account, got)
		}
	}
	if rev.Reverses != "entry-1" || rev.ReversalReason != domain.ReasonWrongAmount {
		t.Fatal("the reversal does not name what it reversed, or why")
	}

	t.Run("an unrecorded reason is refused", func(t *testing.T) {
		if _, err := domain.Reverse(original, "entry-1", "oops", "rev-2"); err == nil {
			t.Fatal("a free-text reversal reason was accepted — every reason becomes " +
				"\"adjustment\" the moment the field is open")
		}
	})

	t.Run("a reversal cannot be reversed", func(t *testing.T) {
		if _, err := domain.Reverse(rev, "entry-2", domain.ReasonOperatorError, "rev-3"); err == nil {
			t.Fatal("a reversal was reversed — the history now reads as two corrections " +
				"rather than as what happened")
		}
	})
}

// Issue #7's failure scenario, at this layer: an entry that does not balance is
// not an entry. The database asserts the same thing at commit; this is the half
// that can say which line was wrong.
func TestAnUnbalancedEntryIsRefused(t *testing.T) {
	e := domain.Entry{
		Kind: domain.KindInvoice,
		Postings: []domain.Posting{
			{Account: domain.TenantReceivable, Side: domain.Debit, Amount: 2500000,
				Party: domain.Party{Kind: domain.Tenant, ID: tenantID}},
			{Account: domain.RentIncome, Side: domain.Credit, Amount: 2000000,
				Party: domain.Party{Kind: domain.Owner, ID: ownerID}},
		},
	}
	err := e.Validate()
	if err == nil {
		t.Fatal("an entry whose debits and credits differ was accepted")
	}
	if !errors.Is(err, domain.ErrUnbalanced) {
		t.Fatalf("the error is %v, which a caller cannot distinguish from a bad input", err)
	}
}

func TestValidateRefusesTheShapesTheSchemaRefuses(t *testing.T) {
	tenant := domain.Party{Kind: domain.Tenant, ID: tenantID}
	owner := domain.Party{Kind: domain.Owner, ID: ownerID}

	for _, tc := range []struct {
		name  string
		entry domain.Entry
		want  string
	}{
		{
			name: "a single line",
			entry: domain.Entry{Kind: domain.KindInvoice, Postings: []domain.Posting{
				{Account: domain.TenantReceivable, Side: domain.Debit, Amount: 100, Party: tenant},
			}},
			want: "one line",
		},
		{
			name: "a negative amount",
			entry: domain.Entry{Kind: domain.KindInvoice, Postings: []domain.Posting{
				{Account: domain.TenantReceivable, Side: domain.Debit, Amount: -100, Party: tenant},
				{Account: domain.RentIncome, Side: domain.Credit, Amount: -100, Party: owner},
			}},
			want: "positive",
		},
		{
			name: "an account that is not in the chart",
			entry: domain.Entry{Kind: domain.KindInvoice, Postings: []domain.Posting{
				{Account: "petty_cash", Side: domain.Debit, Amount: 100, Party: tenant},
				{Account: domain.RentIncome, Side: domain.Credit, Amount: 100, Party: owner},
			}},
			want: "not in the chart",
		},
		{
			name: "a receivable with nobody on the hook for it",
			entry: domain.Entry{Kind: domain.KindInvoice, Postings: []domain.Posting{
				{Account: domain.TenantReceivable, Side: domain.Debit, Amount: 100},
				{Account: domain.RentIncome, Side: domain.Credit, Amount: 100, Party: owner},
			}},
			want: "no tenant",
		},
		{
			name: "a unit with no property",
			entry: domain.Entry{Kind: domain.KindInvoice, Unit: unitID, Postings: []domain.Posting{
				{Account: domain.TenantReceivable, Side: domain.Debit, Amount: 100, Party: tenant},
				{Account: domain.RentIncome, Side: domain.Credit, Amount: 100, Party: owner},
			}},
			want: "property",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.entry.Validate()
			if err == nil {
				t.Fatalf("%s was accepted", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not mention %q — the message is what somebody debugging reads", err, tc.want)
			}
		})
	}
}

// Every template, exercised. A template that only ever posts to one side would
// produce entries the database rejects at commit, which is a failure a customer
// finds rather than a build.
func TestEveryTemplateProducesABalancedEntry(t *testing.T) {
	for _, kind := range domain.Kinds() {
		lines, _ := domain.Template(kind)
		var debits, credits int
		for _, l := range lines {
			if l.Side == domain.Debit {
				debits++
			} else {
				credits++
			}
		}
		if debits == 0 || credits == 0 {
			t.Errorf("template %s has %d debit and %d credit lines", kind, debits, credits)
		}
		for _, l := range lines {
			if _, ok := domain.Lookup(l.Account); !ok {
				t.Errorf("template %s posts to %q, which is not in the chart", kind, l.Account)
			}
		}
	}

	built := []domain.Entry{}
	for _, e := range buildOneOfEach(t) {
		mustBalance(t, e)
		built = append(built, e)
	}
	if len(built) != len(domain.Kinds()) {
		t.Fatalf("built %d entries for %d templates — an event kind has no constructor",
			len(built), len(domain.Kinds()))
	}
}

// buildOneOfEach constructs a representative entry per event kind. It doubles as
// the check that every kind in the vocabulary has a way to be produced.
func buildOneOfEach(t *testing.T) []domain.Entry {
	t.Helper()
	p, tn, ow := place(), tenantID, ownerID

	type built struct {
		kind domain.EventKind
		make func() (domain.Entry, error)
	}
	all := []built{
		{domain.KindInvoice, func() (domain.Entry, error) { return domain.Invoice(2500000, 450000, p, tn, ow, src("a")) }},
		{domain.KindLateFee, func() (domain.Entry, error) { return domain.LateFee(50000, p, tn, ow, src("b")) }},
		{domain.KindPayment, func() (domain.Entry, error) { return domain.Payment(2500000, 2000000, p, tn, src("c")) }},
		{domain.KindPaymentWithTDS, func() (domain.Entry, error) { return domain.PaymentWithTDS(2500000, 250000, p, tn, src("d")) }},
		{domain.KindSettlement, func() (domain.Entry, error) { return domain.Settlement(2500000, p, src("e")) }},
		{domain.KindDepositCollection, func() (domain.Entry, error) { return domain.DepositCollection(5000000, p, tn, src("f")) }},
		{domain.KindDepositRefund, func() (domain.Entry, error) { return domain.DepositRefund(5000000, p, tn, src("g")) }},
		{domain.KindPayout, func() (domain.Entry, error) { return domain.Payout(2000000, p, ow, src("h")) }},
		{domain.KindPlatformFee, func() (domain.Entry, error) { return domain.PlatformFee(74750, 13455, p, ow, src("i")) }},
		{domain.KindGSTRemittance, func() (domain.Entry, error) { return domain.GSTRemittance(450000, p, src("j")) }},
		{domain.KindRefund, func() (domain.Entry, error) { return domain.Refund(2500000, p, tn, src("k")) }},
		{domain.KindWriteOff, func() (domain.Entry, error) { return domain.WriteOff(2500000, p, tn, src("l")) }},
	}

	out := make([]domain.Entry, 0, len(all))
	for _, b := range all {
		e, err := b.make()
		if err != nil {
			t.Fatalf("building a %s: %v", b.kind, err)
		}
		if e.Kind != b.kind {
			t.Fatalf("constructor for %s produced a %s", b.kind, e.Kind)
		}
		out = append(out, e)
	}
	return out
}

func TestNormalSideIsDerivedFromTheAccountType(t *testing.T) {
	for _, a := range domain.Chart() {
		want := domain.Credit
		if a.Type == domain.Asset || a.Type == domain.Expense {
			want = domain.Debit
		}
		if a.NormalSide() != want {
			t.Errorf("%s is a %s with normal side %s, want %s", a.Code, a.Type, a.NormalSide(), want)
		}
	}
}

func TestRupeesRendersWithoutAFloat(t *testing.T) {
	for _, tc := range []struct {
		in   domain.Minor
		want string
	}{
		{0, "0.00"},
		{5, "0.05"},
		{99, "0.99"},
		{100, "1.00"},
		{2500000, "25000.00"},
		{-2500001, "-25000.01"},
	} {
		if got := tc.in.Rupees(); got != tc.want {
			t.Errorf("Minor(%d).Rupees() = %q, want %q", tc.in, got, tc.want)
		}
	}
}
