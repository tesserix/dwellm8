package ops_test

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/tesserix/dwellm8/services/api/internal/money/domain"
	"github.com/tesserix/dwellm8/services/api/internal/money/domain/merchant"
	moneyprovider "github.com/tesserix/dwellm8/services/api/internal/money/provider"
	moneyservice "github.com/tesserix/dwellm8/services/api/internal/money/service"
	moneystore "github.com/tesserix/dwellm8/services/api/internal/money/store"
	"github.com/tesserix/dwellm8/services/api/internal/platform/authz"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy/isolationtest"
	"github.com/tesserix/dwellm8/services/api/internal/surface/ops"
)

// The manager's settlement queue (#270). What the screen needs is what is owed
// to whom and what is late; what it must never need is a bank account number.

type payoutStub struct {
	moneyprovider.Offline
	sent []moneyprovider.TransferRequest
}

func (*payoutStub) Name() string { return "stub" }

func (p *payoutStub) Transfer(_ context.Context, req moneyprovider.TransferRequest) (moneyprovider.Transfer, error) {
	p.sent = append(p.sent, req)
	return moneyprovider.Transfer{ID: "TRF-1", State: moneyprovider.TransferPending}, nil
}

func (*payoutStub) TransferState(_ context.Context, id string) (moneyprovider.Transfer, error) {
	return moneyprovider.Transfer{ID: id, State: moneyprovider.TransferPending}, nil
}

type settlementRows struct {
	rows map[string]moneystore.Settlement
}

func (s *settlementRows) Record(context.Context, moneystore.Instruction) (moneystore.Settlement, error) {
	return moneystore.Settlement{}, nil
}

func (s *settlementRows) ByID(_ context.Context, id string) (moneystore.Settlement, error) {
	row, ok := s.rows[id]
	if !ok {
		return moneystore.Settlement{}, moneystore.ErrNoInstruction
	}
	return row, nil
}

func (s *settlementRows) ForPayment(context.Context, string) (moneystore.Settlement, error) {
	return moneystore.Settlement{}, moneystore.ErrNoInstruction
}

func (s *settlementRows) Due(context.Context, time.Time) ([]moneystore.Settlement, error) {
	out := make([]moneystore.Settlement, 0, len(s.rows))
	for _, row := range s.rows {
		out = append(out, row)
	}
	return out, nil
}

func (s *settlementRows) Instructed(_ context.Context, id, ref string) error {
	row := s.rows[id]
	row.State, row.TransferRef = moneystore.SettlementInstructed, ref
	s.rows[id] = row
	return nil
}

func (s *settlementRows) Settled(_ context.Context, id, ref string, on time.Time) error {
	row := s.rows[id]
	row.State, row.TransferRef, row.SettledOn = moneystore.SettlementSettled, ref, on
	s.rows[id] = row
	return nil
}

func (s *settlementRows) Failed(_ context.Context, id, reason string) error {
	row := s.rows[id]
	row.State, row.Reason = moneystore.SettlementFailed, reason
	s.rows[id] = row
	return nil
}

type merchantStub struct{}

func (merchantStub) ForProvider(context.Context, string) (merchant.Account, error) {
	return merchant.Account{
		Provider: "stub", MerchantRef: "MRC-1", State: merchant.Verified,
		Settlement: merchant.Settlement{Currency: "INR", Masked: "XXXXXXXXXX4321"},
	}, nil
}

func overdue() moneystore.Settlement {
	return moneystore.Settlement{
		ID: "s-1", PaymentID: "pay-1", LeaseID: "lease-1", Currency: "INR",
		Split: domain.Split{
			Gross: 3200000, Platform: 112964, Management: 256000, TDS: 64000,
			Owner: 3200000 - 112964 - 256000 - 64000, RuleID: "r1",
		},
		State: moneystore.SettlementPending, Provider: "stub",
		ExpectedOn: time.Now().AddDate(0, 0, -3),
	}
}

func serveSettlements(t *testing.T) (*http.ServeMux, *settlementRows, *payoutStub) {
	t.Helper()
	reg := moneyprovider.NewRegistry()
	p := &payoutStub{}
	reg.Register(p)
	rows := &settlementRows{rows: map[string]moneystore.Settlement{"s-1": overdue()}}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	mux := http.NewServeMux()
	h := ops.New(nil, nil, nil, nil, nil, log, nil).
		WithSettlements(moneyservice.NewSettlements(rows, merchantStub{}, reg))
	h.SettlementRoutes(authz.NewRegistrar(mux, &authz.Guard{}))
	return mux, rows, p
}

func TestTheQueueShowsWhatIsOwedAndWhatIsLate(t *testing.T) {
	mux, _, _ := serveSettlements(t)

	w := call(t, mux, isolationtest.OrgFirm, http.MethodGet, "/v1/ops/settlements")
	if w.Code != http.StatusOK {
		t.Fatalf("GET settlements: %d %s", w.Code, w.Body.String())
	}
	var out struct {
		Settlements []struct {
			ID              string `json:"id"`
			OwnerMinor      int64  `json:"owner_amount_minor"`
			ManagementMinor int64  `json:"management_amount_minor"`
			State           string `json:"state"`
			Overdue         bool   `json:"overdue"`
		} `json:"settlements"`
	}
	decode(t, w, &out)
	if len(out.Settlements) != 1 {
		t.Fatalf("queue = %+v; want the one division owed", out.Settlements)
	}
	got := out.Settlements[0]
	if got.OwnerMinor != 2767036 || got.ManagementMinor != 256000 {
		t.Fatalf("legs = %+v; want the owner's and the manager's shares", got)
	}
	if !got.Overdue || got.State != "pending" {
		t.Fatalf("a division three days past its date did not read as late: %+v", got)
	}
}

func TestReleasingPaysTheOwnersLegAndNothingElse(t *testing.T) {
	mux, rows, p := serveSettlements(t)

	w := post(t, mux, isolationtest.OrgFirm, "/v1/ops/settlements/s-1/release",
		map[string]any{"beneficiary_ref": "BENE-1"})
	if w.Code != http.StatusOK {
		t.Fatalf("POST release: %d %s", w.Code, w.Body.String())
	}
	if len(p.sent) != 1 || p.sent[0].Amount != overdue().Split.Owner {
		t.Fatalf("sent %+v; want one transfer of the owner's leg", p.sent)
	}
	if rows.rows["s-1"].State != moneystore.SettlementInstructed {
		t.Fatalf("state after release = %s", rows.rows["s-1"].State)
	}
}

func TestReleasingSomethingThatIsNotThereIsNotAServerError(t *testing.T) {
	mux, _, _ := serveSettlements(t)

	w := post(t, mux, isolationtest.OrgFirm, "/v1/ops/settlements/s-9/release",
		map[string]any{"beneficiary_ref": "BENE-1"})
	if w.Code != http.StatusNotFound {
		t.Fatalf("release of an unknown division: %d %s", w.Code, w.Body.String())
	}
}

func TestAReleaseWithNobodyToPayIsRefused(t *testing.T) {
	mux, _, p := serveSettlements(t)

	w := post(t, mux, isolationtest.OrgFirm, "/v1/ops/settlements/s-1/release",
		map[string]any{"beneficiary_ref": ""})
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("release with no beneficiary: %d %s", w.Code, w.Body.String())
	}
	if len(p.sent) != 0 {
		t.Fatalf("a transfer was sent with nobody to pay: %+v", p.sent)
	}
}
