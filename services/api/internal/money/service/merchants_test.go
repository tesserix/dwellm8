package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/tesserix/dwellm8/services/api/internal/money/domain/merchant"
	"github.com/tesserix/dwellm8/services/api/internal/money/provider"
	"github.com/tesserix/dwellm8/services/api/internal/money/service"
	"github.com/tesserix/dwellm8/services/api/internal/platform/pii"
)

// Connecting the manager's own account (#269). The provider is a fake because
// this is about what the service does with the answer, not about Cashfree's
// JSON: the number is masked before anything is stored, an unverified account
// cannot be collected into, and a provider that cannot onboard says so.

type fakeProvider struct {
	provider.Offline
	name       string
	registered provider.MerchantRequest
	state      merchant.State
	reason     string
	err        error
}

func (f *fakeProvider) Name() string { return f.name }

func (f *fakeProvider) RegisterMerchant(_ context.Context, req provider.MerchantRequest) (provider.MerchantStatus, error) {
	if f.err != nil {
		return provider.MerchantStatus{}, f.err
	}
	f.registered = req
	return provider.MerchantStatus{Ref: "MRC-9", State: f.state, Reason: f.reason}, nil
}

func (f *fakeProvider) MerchantState(_ context.Context, ref string) (provider.MerchantStatus, error) {
	return provider.MerchantStatus{Ref: ref, State: f.state, Reason: f.reason}, nil
}

type recorder struct {
	connected merchant.Account
	state     merchant.State
	reason    string
	stored    merchant.Account
	missing   bool
}

func (r *recorder) Connect(_ context.Context, a merchant.Account) (merchant.Account, error) {
	r.connected = a
	r.stored = a
	return a, nil
}

func (r *recorder) ForProvider(_ context.Context, name string) (merchant.Account, error) {
	if r.missing {
		return merchant.Account{}, service.ErrNoMerchant
	}
	a := r.stored
	a.Provider = name
	return a, nil
}

func (r *recorder) List(context.Context) ([]merchant.Account, error) {
	return []merchant.Account{r.stored}, nil
}

func (r *recorder) Record(_ context.Context, _ string, to merchant.State, reason string) (merchant.Account, error) {
	r.state, r.reason = to, reason
	out := r.stored
	out.State, out.Reason = to, reason
	r.stored = out
	return out, nil
}

func merchants(t *testing.T, p *fakeProvider) (*service.Merchants, *recorder) {
	t.Helper()
	reg := provider.NewRegistry()
	reg.Register(p)
	rec := &recorder{}
	return service.NewMerchants(rec, reg), rec
}

func request() service.ConnectRequest {
	return service.ConnectRequest{
		Provider: "fake", BusinessName: "Menon Properties", BusinessType: "proprietorship",
		Country: "IN", Phone: "+919847012345", Email: "meera@example.test",
		TaxID:         pii.NewSecret("ABCDE1234F"),
		AccountNumber: pii.NewSecret("50100123454321"),
		AccountHolder: "Menon Properties", IFSC: "HDFC0001234", Currency: "INR",
	}
}

// The account number reaches the provider and nothing else. What is stored is
// a mask, which is the whole posture ADR-0013 takes for every identifier.
func TestTheAccountNumberGoesToTheProviderAndIsNeverStored(t *testing.T) {
	p := &fakeProvider{name: "fake", state: merchant.Submitted}
	s, rec := merchants(t, p)

	out, err := s.Connect(context.Background(), request())
	if err != nil {
		t.Fatalf("connecting: %v", err)
	}
	if p.registered.AccountNumber != "50100123454321" {
		t.Fatalf("the provider was sent %q; it needs the real number to verify it", p.registered.AccountNumber)
	}
	if p.registered.Settlement.Masked == "50100123454321" {
		t.Fatal("the full account number travelled as the settlement mask too")
	}
	if rec.connected.Settlement.Masked != "XXXXXXXXXX4321" {
		t.Fatalf("stored mask = %q; want the number masked", rec.connected.Settlement.Masked)
	}
	if rec.connected.MerchantRef != "MRC-9" || out.State != merchant.Submitted {
		t.Fatalf("stored = %+v, returned %+v; want the provider's ref and its verdict", rec.connected, out)
	}
}

func TestAMalformedAccountOrPANIsRefusedBeforeTheProviderSeesIt(t *testing.T) {
	p := &fakeProvider{name: "fake", state: merchant.Submitted}
	s, _ := merchants(t, p)

	bad := request()
	bad.TaxID = pii.NewSecret("NOTAPAN")
	if _, err := s.Connect(context.Background(), bad); err == nil {
		t.Fatal("a malformed PAN was accepted")
	}
	bad = request()
	bad.IFSC = "hdfc-1234"
	if _, err := s.Connect(context.Background(), bad); err == nil {
		t.Fatal("a malformed IFSC was accepted")
	}
	if p.registered.BusinessName != "" {
		t.Fatal("a refused request still reached the provider")
	}
}

func TestAProviderThatCannotOnboardIsAConfigurationAnswer(t *testing.T) {
	reg := provider.NewRegistry()
	s := service.NewMerchants(&recorder{}, reg)

	req := request()
	req.Provider = "offline"
	if _, err := s.Connect(context.Background(), req); !errors.Is(err, provider.ErrCapability) {
		t.Fatalf("connecting through an adapter that cannot onboard = %v; want ErrCapability", err)
	}
}

// Refresh is what a webhook and the poller both call: ask the provider, apply
// its verdict, and never move the account on our own say-so.
func TestRefreshAppliesTheProvidersVerdict(t *testing.T) {
	p := &fakeProvider{name: "fake", state: merchant.Submitted}
	s, rec := merchants(t, p)
	if _, err := s.Connect(context.Background(), request()); err != nil {
		t.Fatalf("connecting: %v", err)
	}

	p.state, p.reason = merchant.Suspended, "PAN mismatch"
	if _, err := s.Refresh(context.Background(), "fake"); err != nil {
		t.Fatalf("refreshing: %v", err)
	}
	if rec.state != merchant.Suspended || rec.reason != "PAN mismatch" {
		t.Fatalf("recorded %s/%q; want the provider's suspension and its words", rec.state, rec.reason)
	}
}

func TestCollectingNeedsAVerifiedAccount(t *testing.T) {
	p := &fakeProvider{name: "fake", state: merchant.Submitted}
	s, rec := merchants(t, p)
	if _, err := s.Connect(context.Background(), request()); err != nil {
		t.Fatalf("connecting: %v", err)
	}

	if err := s.MayCollect(context.Background(), "fake"); !errors.Is(err, merchant.ErrUnverified) {
		t.Fatalf("collecting into a submitted account = %v; want ErrUnverified", err)
	}
	if _, err := rec.Record(context.Background(), "fake", merchant.Verified, ""); err != nil {
		t.Fatalf("verifying: %v", err)
	}
	if err := s.MayCollect(context.Background(), "fake"); err != nil {
		t.Fatalf("collecting into a verified account was refused: %v", err)
	}

	rec.missing = true
	if err := s.MayCollect(context.Background(), "fake"); !errors.Is(err, service.ErrNoMerchant) {
		t.Fatalf("collecting with nothing connected = %v; want ErrNoMerchant", err)
	}
}
