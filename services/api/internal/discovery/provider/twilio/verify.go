// Package twilio adapts Twilio Verify to the discovery module's PhoneVerifier
// seam (#22, the verification point of #137). The OTP itself — length, expiry,
// retry policy — is the Verify service's, configured on Twilio's side; this
// adapter sends, checks, and refuses to hold a phone number any longer than
// the two calls take.
package twilio

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ErrCodeMismatch is the code not matching — the one failure the caller shows
// as the person's own to fix.
var ErrCodeMismatch = errors.New("twilio: the code did not match")

// Verifier speaks the Verify v2 API.
type Verifier struct {
	accountSID string
	authToken  string
	verifySID  string
	client     *http.Client
	base       string
}

// New builds the verifier. base is overridable for tests; empty means Twilio.
func New(accountSID, authToken, verifySID, base string) *Verifier {
	if base == "" {
		base = "https://verify.twilio.com"
	}
	return &Verifier{
		accountSID: accountSID, authToken: authToken, verifySID: verifySID,
		client: &http.Client{Timeout: 10 * time.Second},
		base:   strings.TrimRight(base, "/"),
	}
}

// Start sends the OTP over SMS.
func (v *Verifier) Start(ctx context.Context, phone string) error {
	_, err := v.post(ctx, "/v2/Services/"+v.verifySID+"/Verifications", url.Values{
		"To": {phone}, "Channel": {"sms"},
	})
	if err != nil {
		return fmt.Errorf("twilio: starting verification: %w", err)
	}
	return nil
}

// Confirm checks the code. The contact reference is a hash of the number, the
// same shape the dev verifier returns — the module never persists a phone, and
// the masked form is all a human ever sees again.
func (v *Verifier) Confirm(ctx context.Context, phone, code string) (string, string, error) {
	body, err := v.post(ctx, "/v2/Services/"+v.verifySID+"/VerificationCheck", url.Values{
		"To": {phone}, "Code": {code},
	})
	if err != nil {
		return "", "", fmt.Errorf("twilio: checking the code: %w", err)
	}
	var out struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", "", fmt.Errorf("twilio: parsing the check: %w", err)
	}
	if out.Status != "approved" {
		return "", "", ErrCodeMismatch
	}
	sum := sha256.Sum256([]byte(phone))
	return "twv-" + hex.EncodeToString(sum[:8]), "XXXXXX" + phone[len(phone)-4:], nil
}

func (v *Verifier) post(ctx context.Context, path string, form url.Values) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, v.base+path,
		strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(v.accountSID, v.authToken)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	res, err := v.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	// 404 on VerificationCheck is Twilio's "expired or never sent" — the code
	// cannot match, which is the caller's mismatch case, not an outage.
	if res.StatusCode == http.StatusNotFound && strings.Contains(path, "VerificationCheck") {
		return nil, ErrCodeMismatch
	}
	if res.StatusCode >= 300 {
		return nil, fmt.Errorf("status %d: %s", res.StatusCode, truncate(body))
	}
	return body, nil
}

func truncate(b []byte) string {
	const max = 300
	if len(b) > max {
		return string(b[:max]) + "…"
	}
	return string(b)
}
