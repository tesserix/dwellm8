package domain

import "sort"

// The chart of accounts. ADR-0006 §2.
//
// This is a mirror of the ledger_accounts table, not a second source of truth:
// the contract test in internal/money/store compares the two and fails on any
// difference, in either direction. A chart that exists in two places and is
// checked in neither is how a report starts summing an account the ledger
// stopped writing.
//
// It is one list for the whole platform rather than one per organisation. A
// landlord with a private idea of what "deposit_liability" means is a landlord
// whose statements cannot be compared, consolidated or audited.

// AccountType determines the normal side and where the account appears in a
// statement.
type AccountType string

const (
	Asset     AccountType = "asset"
	Liability AccountType = "liability"
	Income    AccountType = "income"
	Expense   AccountType = "expense"
)

// PartyKind is whose balance an account is kept per. A receivable without a
// tenant cannot be chased; a payable without an owner cannot be paid.
type PartyKind string

const (
	NoParty   PartyKind = "none"
	Tenant    PartyKind = "tenant"
	Owner     PartyKind = "owner"
	Vendor    PartyKind = "vendor"
	Platform  PartyKind = "platform"
	Statutory PartyKind = "statutory"
)

// Account codes. Referenced by every template below and by every query that
// asks what something is worth, so they are constants rather than strings
// scattered across the module.
const (
	TenantReceivable      = "tenant_receivable"
	TenantAdvance         = "tenant_advance"
	RentIncome            = "rent_income"
	LateFeeIncome         = "late_fee_income"
	DepositLiability      = "deposit_liability"
	OwnerPayable          = "owner_payable"
	PlatformFeeIncome     = "platform_fee_income"
	GSTOutput             = "gst_output"
	TDSReceivable         = "tds_receivable"
	GatewayClearing       = "gateway_clearing"
	GatewayFee            = "gateway_fee"
	GSTInput              = "gst_input"
	Bank                  = "bank"
	SocietyDuesReceivable = "society_dues_receivable"
	SinkingFund           = "sinking_fund"
	WriteOffExpense       = "write_off"
)

// Account is one line of the chart.
type Account struct {
	Code  string
	Name  string
	Type  AccountType
	Party PartyKind
}

// NormalSide is derived, never stored. An asset with a credit normal balance is
// not a preference, it is a typo, and every report that assumes the sign is then
// silently backwards. The schema generates the same column the same way.
func (a Account) NormalSide() Side {
	if a.Type == Asset || a.Type == Expense {
		return Debit
	}
	return Credit
}

var accounts = map[string]Account{
	TenantReceivable:      {TenantReceivable, "Tenant receivable", Asset, Tenant},
	TenantAdvance:         {TenantAdvance, "Tenant advance", Liability, Tenant},
	RentIncome:            {RentIncome, "Rent income", Income, Owner},
	LateFeeIncome:         {LateFeeIncome, "Late fee income", Income, Owner},
	DepositLiability:      {DepositLiability, "Security deposit held", Liability, Tenant},
	OwnerPayable:          {OwnerPayable, "Owner payable", Liability, Owner},
	PlatformFeeIncome:     {PlatformFeeIncome, "Platform fee income", Income, Platform},
	GSTOutput:             {GSTOutput, "GST payable", Liability, Statutory},
	TDSReceivable:         {TDSReceivable, "TDS receivable", Asset, Statutory},
	GatewayClearing:       {GatewayClearing, "Gateway clearing", Asset, Platform},
	GatewayFee:            {GatewayFee, "Gateway fee", Expense, Platform},
	GSTInput:              {GSTInput, "GST input credit", Asset, Statutory},
	Bank:                  {Bank, "Bank", Asset, Platform},
	SocietyDuesReceivable: {SocietyDuesReceivable, "Society dues receivable", Asset, Tenant},
	SinkingFund:           {SinkingFund, "Sinking fund", Liability, NoParty},
	WriteOffExpense:       {WriteOffExpense, "Write-off", Expense, NoParty},
}

// Lookup returns the account, or false if the code is not in the chart. No
// service may post to an account that is not here; the schema's foreign key
// says the same thing in the other direction.
func Lookup(code string) (Account, bool) {
	a, ok := accounts[code]
	return a, ok
}

// Chart returns every account, ordered by code, for the contract test and for
// anything that renders the chart itself.
func Chart() []Account {
	out := make([]Account, 0, len(accounts))
	for _, a := range accounts {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Code < out[j].Code })
	return out
}
