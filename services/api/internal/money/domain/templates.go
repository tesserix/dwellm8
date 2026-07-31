package domain

import (
	"fmt"
	"sort"
	"time"
)

// The posting template for every money event. ADR-0006 §4.
//
// The templates are data, not code, for the same reason the chart is: the rule
// has to be inspectable, comparable against the copy in the database, and
// changeable without anybody re-deriving which side a deposit goes on. One
// function, apply(), turns a template plus a set of amounts into postings, so
// every event in the product goes through the same three lines of arithmetic
// and there is no per-event opportunity to get the sign wrong.
//
// What each constructor below decides is which amounts exist — that a payment
// beyond what was owed becomes an advance, that a payer who deducts TDS pays the
// net. Where those amounts land is the template's business.

// Role names which of an event's amounts a line takes.
type Role string

const (
	// RoleGross is what changes hands.
	RoleGross Role = "gross"
	// RoleNet is the part that is income, once tax is out of it.
	RoleNet Role = "net"
	// RoleTax is GST charged on a taxable supply, owed onward to the government.
	RoleTax Role = "tax"
	// RoleTDS is tax the payer deducted at source and paid on the payee's behalf.
	RoleTDS Role = "tds"
	// RoleFee is what a provider kept out of a settlement. An expense, not a
	// smaller collection: the payer paid the gross and the receivable was settled
	// in full, and a fee netted silently against income is a fee no owner can be
	// shown. ADR-0012 §4.
	RoleFee Role = "fee"
	// RolePrincipal is the part of a payment that settles an existing debt.
	RolePrincipal Role = "principal"
	// RoleAdvance is the part that does not, because there is nothing to settle.
	RoleAdvance Role = "advance"
)

// Line is one line of a template.
type Line struct {
	Account string
	Side    Side
	Role    Role
	// Optional lines are omitted when their amount is zero. Absence is not the
	// same as zero: rent to a residential tenant carries no GST, and a zero GST
	// posting on every invoice in the country would be the commonest row in the
	// table and would mean nothing.
	Optional bool
}

var templates = map[EventKind][]Line{
	// A charge is owed in full and is income now, not when it is paid. Accrual,
	// not cash: an owner's statement for March shows March's rent whether or not
	// the tenant has paid, which is the entire reason a receivable exists.
	KindInvoice: {
		{TenantReceivable, Debit, RoleGross, false},
		{RentIncome, Credit, RoleNet, false},
		{GSTOutput, Credit, RoleTax, true},
	},
	KindLateFee: {
		{TenantReceivable, Debit, RoleGross, false},
		{LateFeeIncome, Credit, RoleNet, false},
	},
	// Money in lands in clearing, not in the bank. The provider has it, and
	// pretending otherwise is how a settlement reconciliation stops closing.
	KindPayment: {
		{GatewayClearing, Debit, RoleGross, false},
		{TenantReceivable, Credit, RolePrincipal, true},
		{TenantAdvance, Credit, RoleAdvance, true},
	},
	KindPaymentWithTDS: {
		{GatewayClearing, Debit, RoleNet, false},
		{TDSReceivable, Debit, RoleTDS, false},
		{TenantReceivable, Credit, RoleGross, false},
	},
	KindSettlement: {
		{Bank, Debit, RoleGross, false},
		{GatewayClearing, Credit, RoleGross, false},
	},
	// The same event when the provider kept its charge out of the payout. The
	// clearing credit is the gross, because the gross is what the clearing
	// account was debited when the payment was captured — netting the fee against
	// it instead would leave a permanent residue that reconciliation can never
	// close, which is the defect ADR-0012 exists to prevent.
	KindSettlementWithFee: {
		{Bank, Debit, RoleNet, false},
		{GatewayFee, Debit, RoleFee, false},
		{GSTInput, Debit, RoleTax, true},
		{GatewayClearing, Credit, RoleGross, false},
	},
	// A clearing balance nobody could account for, abandoned by a decision. The
	// same shape as a receivable write-off and for the same reason: the money was
	// real, the loss is an expense, and the entry says who decided.
	KindClearingWriteOff: {
		{WriteOffExpense, Debit, RoleGross, false},
		{GatewayClearing, Credit, RoleGross, false},
	},
	// A deposit is the tenant's money held by the owner. It is a liability from
	// the moment it arrives and it is never income.
	KindDepositCollection: {
		{GatewayClearing, Debit, RoleGross, false},
		{DepositLiability, Credit, RoleGross, false},
	},
	KindDepositRefund: {
		{DepositLiability, Debit, RoleGross, false},
		{Bank, Credit, RoleGross, false},
	},
	KindPayout: {
		{OwnerPayable, Debit, RoleGross, false},
		{Bank, Credit, RoleGross, false},
	},
	KindPlatformFee: {
		{OwnerPayable, Debit, RoleGross, false},
		{PlatformFeeIncome, Credit, RoleNet, false},
		{GSTOutput, Credit, RoleTax, true},
	},
	KindGSTRemittance: {
		{GSTOutput, Debit, RoleGross, false},
		{Bank, Credit, RoleGross, false},
	},
	KindRefund: {
		{TenantReceivable, Debit, RoleGross, false},
		{Bank, Credit, RoleGross, false},
	},
	// A write-off is an expense with a reason, never a deleted invoice. The
	// receivable that was raised stays raised; what changed is the decision to
	// stop chasing it.
	KindWriteOff: {
		{WriteOffExpense, Debit, RoleGross, false},
		{TenantReceivable, Credit, RoleGross, false},
	},
	// KindReversal has no template on purpose: it is the original entry with
	// every side flipped, not a rule about accounts. Giving it lines would
	// invite somebody to change them. Reverse() is the implementation.
}

// Template returns the lines for an event kind, for the contract test and for
// anything rendering the rule.
func Template(kind EventKind) ([]Line, bool) {
	l, ok := templates[kind]
	return l, ok
}

// Kinds returns every event kind that has a template, ordered.
func Kinds() []EventKind {
	out := make([]EventKind, 0, len(templates))
	for k := range templates {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// Place is where an entry's money sits: a property, and optionally a unit
// inside it. It is what the row-level security policy on ledger_postings is
// judged against, so an entry that omits it is an entry no delegated session
// will ever see.
type Place struct {
	Property string
	Unit     string
}

// Parties supplies the party for each kind an account is kept per. A template
// line takes its party from its account's party_kind, so a caller states "the
// tenant is X, the owner is Y" once rather than per line.
type Parties map[PartyKind]string

// apply builds the entry. The one piece of arithmetic in the module.
func apply(kind EventKind, amounts map[Role]Minor, place Place, parties Parties, src Source) (Entry, error) {
	lines, ok := templates[kind]
	if !ok {
		return Entry{}, fmt.Errorf("money: no posting template for %q", kind)
	}

	e := Entry{
		Kind:            kind,
		TemplateVersion: 1,
		OccurredOn:      src.OccurredOn,
		Property:        place.Property,
		Unit:            place.Unit,
		Lease:           src.Lease,
		SourceKind:      src.Kind,
		SourceID:        src.ID,
		IdempotencyKey:  src.IdempotencyKey,
		Memo:            src.Memo,
	}

	for _, l := range lines {
		amount, given := amounts[l.Role]
		switch {
		case !given && !l.Optional:
			return Entry{}, fmt.Errorf("money: %s needs an amount for %q", kind, l.Role)
		case amount == 0 && l.Optional:
			continue
		case amount == 0:
			return Entry{}, fmt.Errorf("money: %s has a zero %q, and that line is not optional", kind, l.Role)
		case amount < 0:
			return Entry{}, fmt.Errorf("money: %s has a negative %q (%s): the side carries the direction",
				kind, l.Role, amount)
		}

		acct, ok := Lookup(l.Account)
		if !ok {
			return Entry{}, fmt.Errorf("money: template %s names account %q, which is not in the chart", kind, l.Account)
		}
		p := Posting{Account: l.Account, Side: l.Side, Amount: amount}
		if acct.Party != NoParty {
			id, ok := parties[acct.Party]
			if !ok || id == "" {
				return Entry{}, fmt.Errorf("money: %s posts to %s and no %s was given", kind, l.Account, acct.Party)
			}
			p.Party = Party{Kind: acct.Party, ID: id}
		}
		e.Postings = append(e.Postings, p)
	}

	if err := e.Validate(); err != nil {
		return Entry{}, err
	}
	return e, nil
}

// Source is what caused an entry, and when it belongs.
//
// IdempotencyKey is the caller's natural key for the event. A webhook delivered
// twice, a retried workflow activity and a double-clicked button all arrive with
// the same key and must produce one entry; the unique index on
// (tenant_id, idempotency_key) is what makes that true rather than intended.
type Source struct {
	Kind           string
	ID             string
	IdempotencyKey string
	OccurredOn     time.Time
	Memo           string
	// Lease is the tenancy the event concerns. On Source, not Place: Place is what
	// the lines' RLS policy is judged against and a lease scopes nothing.
	Lease string
}

// Invoice raises a charge. Net is the part that is income; Tax is the GST on it,
// which may be zero — rent to a residential tenant is exempt, and most invoices
// in this product will have no tax line at all.
func Invoice(net, tax Minor, place Place, tenant, owner string, src Source) (Entry, error) {
	if net <= 0 {
		return Entry{}, fmt.Errorf("money: an invoice of %s is not a charge", net)
	}
	if tax < 0 {
		return Entry{}, fmt.Errorf("money: negative tax %s", tax)
	}
	return apply(KindInvoice,
		map[Role]Minor{RoleGross: net + tax, RoleNet: net, RoleTax: tax},
		place,
		Parties{Tenant: tenant, Owner: owner, Statutory: statutoryParty},
		src)
}

// LateFee is a charge under the lease. Separate from Invoice because it is a
// different income account and every owner wants it reported apart from rent.
func LateFee(amount Minor, place Place, tenant, owner string, src Source) (Entry, error) {
	return apply(KindLateFee,
		map[Role]Minor{RoleGross: amount, RoleNet: amount},
		place, Parties{Tenant: tenant, Owner: owner}, src)
}

// Payment applies money received against what was outstanding.
//
// This is the one constructor that makes a decision rather than moving amounts
// around, and it is the decision issue #7's edge cases are about. Anything
// beyond the outstanding balance becomes an advance — a liability — rather than
// a negative receivable, because a receivable with a credit balance misstates
// both sides of the sheet and no ageing report knows what to do with it.
//
// Outstanding of zero is the "payment before its invoice" case: the whole
// receipt is an advance, and it is a normal entry rather than an error.
func Payment(received, outstanding Minor, place Place, tenant string, src Source) (Entry, error) {
	if received <= 0 {
		return Entry{}, fmt.Errorf("money: a payment of %s is not a payment", received)
	}
	if outstanding < 0 {
		return Entry{}, fmt.Errorf("money: outstanding is %s: a credit balance is an advance, not a negative debt", outstanding)
	}
	principal := received
	if outstanding < received {
		principal = outstanding
	}
	return apply(KindPayment,
		map[Role]Minor{RoleGross: received, RolePrincipal: principal, RoleAdvance: received - principal},
		place, Parties{Tenant: tenant, Platform: platformParty}, src)
}

// PaymentWithTDS is a payer — a company tenant, typically — paying the net and
// depositing the deduction with the government. The receivable is settled in
// full: the deduction is not a discount, it is tax paid on the payee's behalf
// and creditable against the payee's own liability.
func PaymentWithTDS(gross, tds Minor, place Place, tenant string, src Source) (Entry, error) {
	if tds <= 0 {
		return Entry{}, fmt.Errorf("money: a TDS payment with a deduction of %s is an ordinary payment", tds)
	}
	if tds >= gross {
		return Entry{}, fmt.Errorf("money: TDS %s is not less than the gross %s", tds, gross)
	}
	return apply(KindPaymentWithTDS,
		map[Role]Minor{RoleGross: gross, RoleNet: gross - tds, RoleTDS: tds},
		place, Parties{Tenant: tenant, Platform: platformParty, Statutory: statutoryParty}, src)
}

// Settlement is the provider paying out to a real bank account. Until this
// happens the money is the provider's problem, and the clearing balance is what
// a settlement reconciliation (issue #14) compares against.
func Settlement(amount Minor, place Place, src Source) (Entry, error) {
	return apply(KindSettlement, map[Role]Minor{RoleGross: amount},
		place, Parties{Platform: platformParty}, src)
}

// SettlementWithFee is the same event when the provider deducted its charge on
// the way. ADR-0012 §4.
//
// gross is what the clearing account holds for these payments — not what landed
// in the bank. The provider's own figure is authoritative for fee and tax, for
// the plain reason that they took it; comparing it against a contracted rate card
// is a separate story and is not what stops a reconciliation closing.
//
// tax is the GST on the fee, which is an input credit rather than a cost. It is
// optional because a provider that issues a consolidated monthly invoice settles
// the fee without tax on the line, and a zero input-credit posting on every
// settlement in the country would mean nothing.
func SettlementWithFee(gross, fee, tax Minor, place Place, src Source) (Entry, error) {
	if fee <= 0 {
		return Entry{}, fmt.Errorf("money: a settlement with a fee of %s is an ordinary settlement", fee)
	}
	if tax < 0 {
		return Entry{}, fmt.Errorf("money: negative tax %s on a settlement fee", tax)
	}
	net := gross - fee - tax
	if net <= 0 {
		return Entry{}, fmt.Errorf("money: a settlement of %s with %s of fee and %s of tax on it pays out %s — "+
			"a provider that keeps the whole batch is a parsing error, not a settlement", gross, fee, tax, net)
	}
	return apply(KindSettlementWithFee,
		map[Role]Minor{RoleGross: gross, RoleNet: net, RoleFee: fee, RoleTax: tax},
		place, Parties{Platform: platformParty, Statutory: statutoryParty}, src)
}

// ClearingWriteOff abandons a clearing balance that reconciliation could not
// account for. ADR-0012 §7.
//
// It is the last resort and it is deliberately an expense rather than a
// correction: the payer's money was real and the receivable it settled stays
// settled. What is being written off is our claim on the provider.
func ClearingWriteOff(amount Minor, place Place, src Source) (Entry, error) {
	return apply(KindClearingWriteOff, map[Role]Minor{RoleGross: amount},
		place, Parties{Platform: platformParty}, src)
}

// DepositCollection takes a security deposit. Never income.
func DepositCollection(amount Minor, place Place, tenant string, src Source) (Entry, error) {
	return apply(KindDepositCollection, map[Role]Minor{RoleGross: amount},
		place, Parties{Tenant: tenant, Platform: platformParty}, src)
}

// DepositRefund returns it, in whole or in part. A deduction for damage is a
// separate charge and a separate entry, not a smaller refund — otherwise the
// tenant's statement shows a number nobody can explain.
func DepositRefund(amount Minor, place Place, tenant string, src Source) (Entry, error) {
	return apply(KindDepositRefund, map[Role]Minor{RoleGross: amount},
		place, Parties{Tenant: tenant, Platform: platformParty}, src)
}

// Payout disburses what the owner is owed.
func Payout(amount Minor, place Place, owner string, src Source) (Entry, error) {
	return apply(KindPayout, map[Role]Minor{RoleGross: amount},
		place, Parties{Owner: owner, Platform: platformParty}, src)
}

// PlatformFee charges Dwellm8's fee against what the owner is owed, with GST on
// the fee where it applies. A management fee is a supply of services and is
// taxable even when the rent underneath it is not.
func PlatformFee(net, tax Minor, place Place, owner string, src Source) (Entry, error) {
	return apply(KindPlatformFee,
		map[Role]Minor{RoleGross: net + tax, RoleNet: net, RoleTax: tax},
		place, Parties{Owner: owner, Platform: platformParty, Statutory: statutoryParty}, src)
}

// GSTRemittance pays collected GST to the government.
func GSTRemittance(amount Minor, place Place, src Source) (Entry, error) {
	return apply(KindGSTRemittance, map[Role]Minor{RoleGross: amount},
		place, Parties{Statutory: statutoryParty, Platform: platformParty}, src)
}

// Refund returns money to the payer. The receivable goes back up: the charge
// still happened, and what is being undone is the payment.
func Refund(amount Minor, place Place, tenant string, src Source) (Entry, error) {
	return apply(KindRefund, map[Role]Minor{RoleGross: amount},
		place, Parties{Tenant: tenant, Platform: platformParty}, src)
}

// WriteOff abandons a receivable. An expense with a reason — the invoice stays,
// which is the difference between a decision and a deletion.
func WriteOff(amount Minor, place Place, tenant string, src Source) (Entry, error) {
	return apply(KindWriteOff, map[Role]Minor{RoleGross: amount},
		place, Parties{Tenant: tenant}, src)
}

// The platform and the government are single, known parties. They are ids
// rather than nulls so that a statutory or clearing balance is queried the same
// way as anybody else's, with no special case in the balance query.
//
// A party is supplied for every kind a template could post to, whether or not
// the optional line that needs it is taken: apply() reads the party only when it
// emits the line, so an exempt invoice never touches the statutory one.
const (
	platformParty  = "00000000-0000-0000-0000-0000000000d8"
	statutoryParty = "00000000-0000-0000-0000-000000000101"
)
