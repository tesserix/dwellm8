package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/tesserix/dwellm8/services/api/internal/money/domain/merchant"
	"github.com/tesserix/dwellm8/services/api/internal/money/provider"
	"github.com/tesserix/dwellm8/services/api/internal/money/store"
	"github.com/tesserix/dwellm8/services/api/internal/platform/pii"
)

// Merchants connects a manager's own account with an aggregator (#269).
//
// Dwellm8 never pools client money, so rent has to settle somewhere the manager
// owns. This service is what turns "here is my bank account" into an account
// the chosen provider will settle to: the number is validated and masked here,
// travels to the provider once, and what is stored is the mask and the
// provider's reference. Which provider is behind it is a name in configuration.
type Merchants struct {
	store    MerchantStore
	registry *provider.Registry
}

// MerchantStore is the store as this service uses it, defined here so the
// service can be tested without a database behind it.
type MerchantStore interface {
	Connect(ctx context.Context, a merchant.Account) (merchant.Account, error)
	ForProvider(ctx context.Context, provider string) (merchant.Account, error)
	List(ctx context.Context) ([]merchant.Account, error)
	Record(ctx context.Context, provider string, to merchant.State, reason string) (merchant.Account, error)
}

// ErrNoMerchant is re-exported so callers match one error rather than reaching
// into the store package for it.
var ErrNoMerchant = store.ErrNoMerchant

func NewMerchants(s MerchantStore, r *provider.Registry) *Merchants {
	return &Merchants{store: s, registry: r}
}

// ConnectRequest is what the manager fills in. The two secrets are pii.Secret
// so neither can be logged by accident.
type ConnectRequest struct {
	Provider      string
	BusinessName  string
	BusinessType  string
	Country       string
	Email         string
	Phone         string
	TaxID         pii.Secret
	GSTIN         string
	AccountNumber pii.Secret
	AccountHolder string
	IFSC          string
	Currency      string
}

// Connect submits the account to the provider and files what comes back.
func (m *Merchants) Connect(ctx context.Context, req ConnectRequest) (merchant.Account, error) {
	req.Country = strings.ToUpper(strings.TrimSpace(req.Country))
	req.IFSC = strings.ToUpper(strings.TrimSpace(req.IFSC))
	if req.Country == "" {
		req.Country = "IN"
	}
	if req.Currency == "" {
		req.Currency = "INR"
	}

	masked, err := m.validate(req)
	if err != nil {
		return merchant.Account{}, err
	}

	onboarder, err := provider.MerchantsBy(m.registry, req.Provider)
	if err != nil {
		return merchant.Account{}, err
	}
	settlement := merchant.Settlement{
		Holder: req.AccountHolder, Masked: masked,
		IFSC: req.IFSC, Currency: req.Currency,
	}
	status, err := onboarder.RegisterMerchant(ctx, provider.MerchantRequest{
		IdempotencyKey: req.Provider + ":" + masked,
		BusinessName:   req.BusinessName,
		BusinessType:   req.BusinessType,
		Country:        req.Country,
		Email:          req.Email,
		Phone:          req.Phone,
		TaxID:          req.TaxID.Reveal(),
		GSTIN:          req.GSTIN,
		Settlement:     settlement,
		// The provider needs the real number to verify it; nothing else does.
		AccountNumber: req.AccountNumber.Reveal(),
	})
	if err != nil {
		return merchant.Account{}, fmt.Errorf("registering with %s: %w", req.Provider, err)
	}

	return m.store.Connect(ctx, merchant.Account{
		Provider: req.Provider, MerchantRef: status.Ref, Country: req.Country,
		BusinessName: req.BusinessName, BusinessType: req.BusinessType,
		State: status.State, Reason: status.Reason, Settlement: settlement,
	})
}

// validate refuses everything the provider would refuse, before the number
// leaves the process, and returns the mask that will be stored.
func (m *Merchants) validate(req ConnectRequest) (string, error) {
	switch {
	case req.Provider == "":
		return "", errors.New("no provider named")
	case strings.TrimSpace(req.BusinessName) == "":
		return "", errors.New("no business name")
	case strings.TrimSpace(req.AccountHolder) == "":
		return "", errors.New("no account holder")
	}
	if err := pii.Validate(pii.KindPAN, req.TaxID); err != nil {
		return "", err
	}
	if req.GSTIN != "" {
		if err := pii.Validate(pii.KindGSTIN, pii.NewSecret(req.GSTIN)); err != nil {
			return "", err
		}
	}
	// An Indian settlement account is identified by its IFSC; elsewhere the
	// provider's own rails identify it and demanding one would be wrong.
	if req.Country == "IN" {
		if err := pii.Validate(pii.KindIFSC, pii.NewSecret(req.IFSC)); err != nil {
			return "", err
		}
	}
	if err := pii.Validate(pii.KindBankAccount, req.AccountNumber); err != nil {
		return "", err
	}
	return pii.Mask(pii.KindBankAccount, req.AccountNumber)
}

// Refresh asks the provider what is true and applies it. This is what a webhook
// triggers rather than replaces — the same rule payments follow (ADR-0011 §4).
func (m *Merchants) Refresh(ctx context.Context, name string) (merchant.Account, error) {
	current, err := m.store.ForProvider(ctx, name)
	if err != nil {
		return merchant.Account{}, err
	}
	onboarder, err := provider.MerchantsBy(m.registry, name)
	if err != nil {
		return merchant.Account{}, err
	}
	status, err := onboarder.MerchantState(ctx, current.MerchantRef)
	if err != nil {
		return merchant.Account{}, fmt.Errorf("asking %s about the account: %w", name, err)
	}
	if status.State == current.State {
		return current, nil
	}
	return m.store.Record(ctx, name, status.State, status.Reason)
}

// List is every provider this manager has connected.
func (m *Merchants) List(ctx context.Context) ([]merchant.Account, error) {
	return m.store.List(ctx)
}

// MayCollect is the check every collection path makes first, so a manager is
// told to finish their onboarding rather than shown a provider error.
func (m *Merchants) MayCollect(ctx context.Context, name string) error {
	a, err := m.store.ForProvider(ctx, name)
	if err != nil {
		return err
	}
	return provider.CheckCollectable(provider.MerchantStatus{
		Ref: a.MerchantRef, State: a.State, Reason: a.Reason,
	})
}
