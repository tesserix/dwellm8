package http

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/tesserix/dwellm8/services/api/internal/money/domain/collect"
	"github.com/tesserix/dwellm8/services/api/internal/money/provider"
)

// This handler has one job that cannot be got wrong: hand the verifier the
// exact bytes that arrived. Everything else it does is plumbing.

type spy struct {
	got      provider.Webhook
	claimed  collect.Status
	ref      string
	eventID  string
	decision collect.Decision
	err      error
}

func (s *spy) IngestWebhook(_ context.Context, _ string, w provider.Webhook,
	claimed collect.Status, ref, eventID string) (collect.Decision, error) {
	s.got, s.claimed, s.ref, s.eventID = w, claimed, ref, eventID
	return s.decision, s.err
}

func discard() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func post(t *testing.T, h *Webhooks, body, stamp, sig string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/webhooks/cashfree", strings.NewReader(body))
	req.Header.Set("x-webhook-timestamp", stamp)
	req.Header.Set("x-webhook-signature", sig)
	rec := httptest.NewRecorder()
	h.Cashfree(rec, req)
	return rec
}

func signed(body, secret string, at time.Time) (stamp, sig string) {
	stamp = strconv.FormatInt(at.Unix(), 10)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(stamp))
	mac.Write([]byte(body))
	return stamp, base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// Key order and spacing json.Marshal would never reproduce. If anything on the
// path decodes and re-encodes, these bytes change and the provider's signature
// stops matching — the failure that appears only against real traffic.
const awkwardBody = `{"type":"PAYMENT_SUCCESS_WEBHOOK","data":{"order":{"order_id":"o-1"},` +
	`"payment":{"payment_status":"SUCCESS"}},  "spacing":  true}`

func TestTheRawBodyReachesTheServiceByteForByte(t *testing.T) {
	s := &spy{decision: collect.Decision{Disposition: collect.Confirm}}
	h := NewWebhooks(s, discard())
	stamp, sig := signed(awkwardBody, "whsec", time.Now())

	if rec := post(t, h, awkwardBody, stamp, sig); rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	if string(s.got.Body) != awkwardBody {
		t.Errorf("the service received\n%q\nand the request carried\n%q", s.got.Body, awkwardBody)
	}
	if s.got.Signature != sig || s.got.Timestamp != stamp {
		t.Errorf("the signature headers did not survive: %+v", s.got)
	}

	// The claim is passed through as a claim, and the order id is what the
	// payment is found by.
	if s.claimed != collect.StatusCaptured {
		t.Errorf("claimed = %s", s.claimed)
	}
	if s.ref != "o-1" {
		t.Errorf("provider ref = %q", s.ref)
	}
	if s.eventID == "" {
		t.Error("no event id was derived, so the inbox cannot deduplicate this delivery")
	}
}

// A delivery the handler cannot interpret is still passed on, with an empty
// claim, because Decide parks an unsupported claim rather than dropping it —
// and a dropped delivery is the one shape of lost money the inbox exists for.
func TestAnUnknownClaimIsPassedOnEmptyRatherThanGuessed(t *testing.T) {
	body := `{"type":"SOMETHING_NEW","data":{"order":{"order_id":"o-2"},"payment":{"payment_status":"WHAT"}}}`
	s := &spy{decision: collect.Decision{Disposition: collect.Park, Reason: collect.ParkUnsupportedEvent}}
	h := NewWebhooks(s, discard())
	stamp, sig := signed(body, "whsec", time.Now())

	rec := post(t, h, body, stamp, sig)
	if rec.Code != http.StatusOK {
		t.Errorf("status %d — a parked delivery must not ask Cashfree to retry", rec.Code)
	}
	if s.claimed != "" {
		t.Errorf("claimed = %q, want empty so Decide parks it", s.claimed)
	}
	if s.ref != "o-2" {
		t.Errorf("the delivery was passed on without its order id: %q", s.ref)
	}
}

// The status codes a provider's retry logic reads. Parked and ignored are 200:
// a bad signature does not improve on the fifth attempt, and the delivery is
// kept either way. Our own failure is a 500, because that one is worth retrying.
func TestOnlyOurOwnFailureAsksForARetry(t *testing.T) {
	stamp, sig := signed(awkwardBody, "whsec", time.Now())

	for name, tc := range map[string]struct {
		spy  *spy
		want int
	}{
		"parked":      {&spy{decision: collect.Decision{Disposition: collect.Park, Reason: collect.ParkSignatureInvalid}}, http.StatusOK},
		"ignored":     {&spy{decision: collect.Decision{Disposition: collect.Ignore}}, http.StatusOK},
		"our failure": {&spy{err: context.DeadlineExceeded}, http.StatusInternalServerError},
	} {
		rec := post(t, NewWebhooks(tc.spy, discard()), awkwardBody, stamp, sig)
		if rec.Code != tc.want {
			t.Errorf("%s: status %d, want %d", name, rec.Code, tc.want)
		}
	}
}

// Unparseable and still signed by somebody. It is answered 200 so a body that
// will never parse is not retried forever, and it does not reach the service.
func TestAnUnparseableDeliveryIsNotRetriedForever(t *testing.T) {
	s := &spy{}
	h := NewWebhooks(s, discard())
	stamp, sig := signed("not json", "whsec", time.Now())

	if rec := post(t, h, "not json", stamp, sig); rec.Code != http.StatusOK {
		t.Errorf("status %d", rec.Code)
	}
	if s.got.Body != nil {
		t.Error("an unparseable delivery reached the service")
	}
}

func TestClaimTranslationRefusesToGuess(t *testing.T) {
	for in, want := range map[string]collect.Status{
		"SUCCESS":      collect.StatusCaptured,
		"FAILED":       collect.StatusFailed,
		"USER_DROPPED": collect.StatusExpired,
		"PENDING":      collect.StatusAttempted,
	} {
		got, err := cashfreeClaimedStatus(in, "")
		if err != nil || got != want {
			t.Errorf("%s -> (%s, %v)", in, got, err)
		}
	}
	if got, err := cashfreeClaimedStatus("", "PAYMENT_SUCCESS_WEBHOOK"); err != nil || got != collect.StatusCaptured {
		t.Errorf("event-type claim -> (%s, %v)", got, err)
	}
	if _, err := cashfreeClaimedStatus("SOMETHING_NEW", "SOMETHING_ELSE"); err == nil {
		t.Error("an unknown claim was translated")
	}
}

// A body ceiling exists so a delivery cannot be a denial of service. A webhook
// is a few kilobytes.
func TestTheBodyIsBounded(t *testing.T) {
	if maxWebhookBody > 4<<20 {
		t.Errorf("the webhook body ceiling is %d bytes", maxWebhookBody)
	}
}
