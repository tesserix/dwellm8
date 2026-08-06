package ops_test

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tesserix/dwellm8/services/api/internal/money/domain/merchant"
	moneyprovider "github.com/tesserix/dwellm8/services/api/internal/money/provider"
	moneyservice "github.com/tesserix/dwellm8/services/api/internal/money/service"
	moneystore "github.com/tesserix/dwellm8/services/api/internal/money/store"
	"github.com/tesserix/dwellm8/services/api/internal/platform/authz"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy/isolationtest"
	"github.com/tesserix/dwellm8/services/api/internal/surface/ops"
)

// The manager connects their own merchant account (#269). The surface's job is
// that the account number never comes back out, and that the screen is told in
// plain words whether rent can be collected yet.

type stubProvider struct {
	moneyprovider.Offline
	state merchant.State
}

func (stubProvider) Name() string { return "stub" }

func (s stubProvider) RegisterMerchant(context.Context, moneyprovider.MerchantRequest) (moneyprovider.MerchantStatus, error) {
	return moneyprovider.MerchantStatus{Ref: "MRC-1", State: s.state}, nil
}

func (s stubProvider) MerchantState(_ context.Context, ref string) (moneyprovider.MerchantStatus, error) {
	return moneyprovider.MerchantStatus{Ref: ref, State: s.state}, nil
}

func serveMerchant(t *testing.T, state merchant.State) *http.ServeMux {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connecting: %v", err)
	}
	t.Cleanup(pool.Close)

	reg := moneyprovider.NewRegistry()
	reg.Register(stubProvider{state: state})
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	mux := http.NewServeMux()
	h := ops.New(nil, nil, nil, nil, nil, log, nil).
		WithMerchants(moneyservice.NewMerchants(moneystore.NewMerchants(pool), reg))
	h.MerchantRoutes(authz.NewRegistrar(mux, &authz.Guard{}))
	return mux
}

func connectBody() map[string]any {
	return map[string]any{
		"provider": "stub", "business_name": "Menon Properties",
		"business_type": "proprietorship", "pan": "ABCDE1234F",
		"account_number": "50100123454321", "account_holder": "Menon Properties",
		"ifsc": "HDFC0001234",
	}
}

func TestConnectingAnAccountNeverEchoesTheNumberBack(t *testing.T) {
	mux := serveMerchant(t, merchant.Submitted)

	w := post(t, mux, isolationtest.OrgFirm, "/v1/ops/merchant", connectBody())
	if w.Code != http.StatusOK {
		t.Fatalf("POST merchant: %d %s", w.Code, w.Body.String())
	}
	if body := w.Body.String(); strings.Contains(body, "50100123454321") {
		t.Fatalf("the response carries the account number: %s", body)
	}
	var out struct {
		Provider     string `json:"provider"`
		State        string `json:"state"`
		Masked       string `json:"settlement_masked"`
		MayCollect   bool   `json:"may_collect"`
		NextAction   string `json:"next_action"`
		BusinessName string `json:"business_name"`
	}
	decode(t, w, &out)
	if out.Masked != "XXXXXXXXXX4321" || out.State != "submitted" {
		t.Fatalf("connected = %+v; want the masked account and the provider's verdict", out)
	}
	if out.MayCollect || out.NextAction == "" {
		t.Fatalf("a submitted account said it may collect, or said nothing about what happens next: %+v", out)
	}
}

func TestTheScreenIsToldWhenRentCanBeCollected(t *testing.T) {
	mux := serveMerchant(t, merchant.Verified)

	if w := post(t, mux, isolationtest.OrgFirm, "/v1/ops/merchant", connectBody()); w.Code != http.StatusOK {
		t.Fatalf("POST merchant: %d %s", w.Code, w.Body.String())
	}
	w := call(t, mux, isolationtest.OrgFirm, http.MethodGet, "/v1/ops/merchant")
	if w.Code != http.StatusOK {
		t.Fatalf("GET merchant: %d %s", w.Code, w.Body.String())
	}
	var out struct {
		Accounts []struct {
			Provider   string `json:"provider"`
			MayCollect bool   `json:"may_collect"`
		} `json:"accounts"`
	}
	decode(t, w, &out)
	if len(out.Accounts) == 0 || !out.Accounts[0].MayCollect {
		t.Fatalf("a verified account did not report itself collectable: %+v", out.Accounts)
	}
}

func TestAMalformedAccountIsRefusedWithSomethingTheManagerCanActOn(t *testing.T) {
	mux := serveMerchant(t, merchant.Submitted)

	bad := connectBody()
	bad["pan"] = "NOTAPAN"
	w := post(t, mux, isolationtest.OrgFirm, "/v1/ops/merchant", bad)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("a malformed PAN gave %d %s; want 422", w.Code, w.Body.String())
	}
}
