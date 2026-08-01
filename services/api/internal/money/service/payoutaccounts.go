package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/tesserix/dwellm8/services/api/internal/money/store"
	"github.com/tesserix/dwellm8/services/api/internal/platform/pii"
)

// PayoutAccounts is the impersonated-owner control (#227): the number is
// validated, masked and fingerprinted here, and only the mask and the
// fingerprint travel further - the store cannot hold what it is never handed.
type PayoutAccounts struct {
	store *store.PayoutAccounts
	// key makes the fingerprint an HMAC rather than a hash. Empty means the
	// feature is unconfigured and every change is refused - fail closed, the
	// same posture as the authz guard.
	key []byte
	log *slog.Logger
}

// ErrUnconfigured is a change attempted with no fingerprint key on the process.
var ErrUnconfigured = errors.New("payout accounts are not configured: no fingerprint key")

func NewPayoutAccounts(s *store.PayoutAccounts, key string, log *slog.Logger) *PayoutAccounts {
	if key == "" && log != nil {
		log.Warn("payout account changes are disabled", "set", "PAYOUT_FP_KEY")
	}
	return &PayoutAccounts{store: s, key: []byte(key), log: log}
}

// Change files a new payout account for an owner. The account enters its
// 72-hour cool-off; what comes back says when it becomes payable, and the
// outbox event carries the old masked account so the notification can reach
// the channels the attacker does not control.
func (p *PayoutAccounts) Change(ctx context.Context, ownerPartyID string, account pii.Secret, ifsc string) (store.Changed, error) {
	if len(p.key) == 0 {
		return store.Changed{}, ErrUnconfigured
	}
	if ownerPartyID == "" {
		return store.Changed{}, errors.New("no owner named")
	}
	if err := pii.Validate(pii.KindBankAccount, account); err != nil {
		return store.Changed{}, err
	}
	ifsc = strings.ToUpper(strings.TrimSpace(ifsc))
	if err := pii.Validate(pii.KindIFSC, pii.NewSecret(ifsc)); err != nil {
		return store.Changed{}, err
	}
	masked, err := pii.Mask(pii.KindBankAccount, account)
	if err != nil {
		return store.Changed{}, err
	}

	out, err := p.store.Change(ctx, store.AccountChange{
		OwnerPartyID: ownerPartyID,
		Masked:       masked,
		IFSC:         ifsc,
		Fingerprint:  p.fingerprint(account, ifsc),
	})
	if err != nil {
		return store.Changed{}, fmt.Errorf("changing the payout account: %w", err)
	}
	if !out.Unchanged {
		p.log.Info("payout account changed",
			"owner", ownerPartyID, "old", out.OldMasked, "new", out.NewMasked,
			"payable_at", out.PayableAt)
	}
	return out, nil
}

// Payable is what the payout run asks before any money moves. A held account
// is an answer the run must surface, never a reason to fall back to an older
// row - the older rows are exactly what the change closed.
func (p *PayoutAccounts) Payable(ctx context.Context, ownerPartyID string) (store.Payable, error) {
	return p.store.Payable(ctx, ownerPartyID)
}

// fingerprint identifies an account across rows without storing it: HMAC under
// a key PostgreSQL never sees, so the column cannot be turned into a lookup
// table by anybody who takes a copy of the database.
func (p *PayoutAccounts) fingerprint(account pii.Secret, ifsc string) string {
	m := hmac.New(sha256.New, p.key)
	m.Write([]byte(account.Reveal()))
	m.Write([]byte("|"))
	m.Write([]byte(ifsc))
	return hex.EncodeToString(m.Sum(nil))
}
