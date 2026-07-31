package provider

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/tesserix/dwellm8/services/api/internal/money/domain"
	"github.com/tesserix/dwellm8/services/api/internal/money/domain/collect"
	"github.com/tesserix/dwellm8/services/api/internal/money/domain/mandate"
)

// What this file is for: Cashfree is the first adapter with a real HTTP client
// behind it, and the failures worth catching are not "does it parse JSON". They
// are the ones that look fine in review — a signature verified over a
// re-marshalled body, a status guessed rather than parked, an unpinned API
// version, a revoke that fails on retry.

const (
	testSecret  = "whsec_cashfree"
	testVersion = "2023-08-01"
)

func testCashfree(t *testing.T, h http.HandlerFunc) (*Cashfree, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c, err := NewCashfree(CashfreeConfig{
		BaseURL: srv.URL, ClientID: "cid", ClientSecret: "csec",
		WebhookSecret: testSecret, APIVersion: testVersion,
		HTTP: srv.Client(),
		Now:  func() time.Time { return time.Unix(1_800_000_000, 0) },
	})
	if err != nil {
		t.Fatalf("NewCashfree: %v", err)
	}
	return c, srv
}

// A delivery signed the way Cashfree signs one.
func cashfreeSigned(body []byte, ts time.Time, secret string) Webhook {
	stamp := strconv.FormatInt(ts.Unix(), 10)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(stamp))
	mac.Write(body)
	return Webhook{
		Body:      body,
		Timestamp: stamp,
		Signature: base64.StdEncoding.EncodeToString(mac.Sum(nil)),
	}
}

func TestCashfreeSignatureVerification(t *testing.T) {
	c, _ := testCashfree(t, func(w http.ResponseWriter, r *http.Request) {})
	now := c.now()
	body := []byte(`{"type":"PAYMENT_SUCCESS_WEBHOOK","data":{"order":{"order_id":"o1"}}}`)

	good := cashfreeSigned(body, now, testSecret)
	if !c.VerifyWebhook(good) {
		t.Fatal("a genuine delivery was rejected")
	}

	// The scheme is not interchangeable with Razorpay's, and the test says so
	// explicitly: this is the exact bug the interface change exists to prevent.
	hexMAC := hmac.New(sha256.New, []byte(testSecret))
	hexMAC.Write(body)
	hexSig := hex.EncodeToString(hexMAC.Sum(nil))

	altered := append(append([]byte(nil), body...), ' ')
	wrongSecret := cashfreeSigned(body, now, "whsec_other")
	wrongStamp := good
	wrongStamp.Timestamp = strconv.FormatInt(now.Unix()+1, 10) // signature no longer covers it

	for name, w := range map[string]Webhook{
		"hex encoded, razorpay style": {Body: body, Timestamp: good.Timestamp, Signature: hexSig},
		"body alone, no timestamp":    {Body: body, Signature: good.Signature},
		"altered body":                {Body: altered, Timestamp: good.Timestamp, Signature: good.Signature},
		"altered timestamp":           wrongStamp,
		"wrong secret":                wrongSecret,
		"not base64":                  {Body: body, Timestamp: good.Timestamp, Signature: "!!!!"},
		"truncated":                   {Body: body, Timestamp: good.Timestamp, Signature: good.Signature[:len(good.Signature)-4]},
		"empty signature":             {Body: body, Timestamp: good.Timestamp},
	} {
		if c.VerifyWebhook(w) {
			t.Errorf("%s: accepted", name)
		}
	}

	// An empty secret verifies nothing. NewCashfree refuses to build with one,
	// so this asserts the primitive underneath rather than the adapter.
	if ok, _ := VerifyHMACSHA256Base64WithTimestamp(good, "", now, time.Minute); ok {
		t.Error("an empty secret verified — a misconfigured deployment would trust every delivery")
	}
}

// A correctly signed delivery from four hours ago is a different problem from a
// forged one, and the two are parked for different reasons.
func TestAReplayedDeliveryIsGenuineAndStillRefused(t *testing.T) {
	c, _ := testCashfree(t, func(w http.ResponseWriter, r *http.Request) {})
	old := cashfreeSigned([]byte(`{"type":"PAYMENT_SUCCESS_WEBHOOK"}`),
		c.now().Add(-4*time.Hour), testSecret)

	if c.VerifyWebhook(old) {
		t.Error("a four-hour-old delivery was accepted")
	}
	ok, err := c.VerifyWebhookWithReason(old)
	if !ok {
		t.Error("the signature itself should verify — it really is Cashfree's")
	}
	if !errors.Is(err, ErrStaleDelivery) {
		t.Errorf("a replayed delivery reported %v, which a handler cannot park differently from a forgery", err)
	}
}

// The bug that only ever shows up in production: verifying a signature over a
// body that has been through encoding/json.
func TestVerificationIsOverRawBytesAndNotReMarshalledJSON(t *testing.T) {
	c, _ := testCashfree(t, func(w http.ResponseWriter, r *http.Request) {})
	raw := []byte(`{"z":1,"a":2,  "nested":{"b":  3}}`) // key order and spacing as sent
	good := cashfreeSigned(raw, c.now(), testSecret)

	if !c.VerifyWebhook(good) {
		t.Fatal("the raw body did not verify")
	}

	var any map[string]any
	if err := json.Unmarshal(raw, &any); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	reMarshalled, err := json.Marshal(any)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	replayed := good
	replayed.Body = reMarshalled
	if c.VerifyWebhook(replayed) {
		t.Error("a re-marshalled body verified — then a handler that decodes before verifying would appear to work")
	}
}

func TestCashfreeRefusesToStartWithoutItsPins(t *testing.T) {
	full := CashfreeConfig{BaseURL: "https://sandbox.cashfree.com/pg", ClientID: "c",
		ClientSecret: "s", WebhookSecret: "w", APIVersion: testVersion}

	for name, mangle := range map[string]func(*CashfreeConfig){
		"no base URL":      func(c *CashfreeConfig) { c.BaseURL = "" },
		"no client id":     func(c *CashfreeConfig) { c.ClientID = "" },
		"no client secret": func(c *CashfreeConfig) { c.ClientSecret = "" },
		"no api version":   func(c *CashfreeConfig) { c.APIVersion = "" },
	} {
		cfg := full
		mangle(&cfg)
		if _, err := NewCashfree(cfg); err == nil {
			t.Errorf("%s: started anyway", name)
		}
	}
	if _, err := NewCashfree(full); err != nil {
		t.Errorf("a complete configuration was refused: %v", err)
	}
}

// A deployment with no webhook secret is a poll-only deployment, and it works:
// confirmation is an API call and never trusted a delivery's contents. What it
// must not do is trust a delivery.
//
// The subtle part is why the check moved rather than being deleted. An empty
// HMAC key is a *valid* key — a signature computed with "" verifies against ""
// — so a construction that quietly kept an empty secret would verify deliveries
// an attacker can sign. Refusing by name is not the same as comparing against
// nothing.
func TestAPollOnlyDeploymentWorksAndTrustsNoDelivery(t *testing.T) {
	c, err := NewCashfree(CashfreeConfig{
		BaseURL: "https://sandbox.cashfree.com/pg", ClientID: "c",
		ClientSecret: "s", APIVersion: testVersion,
	})
	if err != nil {
		t.Fatalf("a poll-only deployment was refused: %v — confirmation is an API call, so a "+
			"deployment the provider cannot reach is still a working one", err)
	}
	if c.AcceptsDeliveries() {
		t.Error("a deployment with no webhook secret claims it can verify deliveries")
	}

	// Signed with the empty key, which is what an attacker would try against a
	// deployment that kept one.
	body := []byte(`{"type":"PAYMENT_SUCCESS_WEBHOOK"}`)
	stamp := strconv.FormatInt(time.Now().Unix(), 10)
	mac := hmac.New(sha256.New, []byte(""))
	mac.Write([]byte(stamp))
	mac.Write(body)
	w := Webhook{
		Body:      body,
		Timestamp: stamp,
		Signature: base64.StdEncoding.EncodeToString(mac.Sum(nil)),
	}

	if c.VerifyWebhook(w) {
		t.Fatal("a delivery signed with the empty key verified — an empty HMAC key is a real key, " +
			"which is why this is refused by name rather than by comparison")
	}
	ok, err := c.VerifyWebhookWithReason(w)
	if ok || err == nil {
		t.Error("the refusal does not say why no delivery can be trusted here")
	}
}

func TestEveryRequestCarriesTheVersionPinAndCredentials(t *testing.T) {
	var got http.Header
	c, _ := testCashfree(t, func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		fmt.Fprint(w, `{"order_id":"rent-2026-08-u1","payment_session_id":"sess_1"}`)
	})

	if _, err := c.CreateOrder(context.Background(), OrderRequest{
		IdempotencyKey: "rent-2026-08-u1", Amount: 1_200_000,
		Currency: domain.Currency, Method: collect.MethodUPIIntent,
		PayerRef: "payer-1",
	}); err != nil {
		t.Fatalf("CreateOrder: %v", err)
	}

	for header, want := range map[string]string{
		"x-client-id":       "cid",
		"x-client-secret":   "csec",
		"x-api-version":     testVersion,
		"x-idempotency-key": "rent-2026-08-u1",
	} {
		if got.Get(header) != want {
			t.Errorf("%s = %q, want %q", header, got.Get(header), want)
		}
	}
}

// The idempotency key is the order id at Cashfree's end too, so a retry cannot
// create a second order even before our unique index is reached.
func TestARetriedOrderIsTheSameOrderAtCashfree(t *testing.T) {
	seen := map[string]int{}
	c, _ := testCashfree(t, func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			OrderID string      `json:"order_id"`
			Amount  json.Number `json:"order_amount"`
		}
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Errorf("unreadable request: %v", err)
		}
		if body.Amount.String() != "12000.00" {
			t.Errorf("order_amount = %s, want 12000.00 — paise arrived as something else", body.Amount)
		}
		seen[body.OrderID]++
		if seen[body.OrderID] > 1 {
			w.WriteHeader(http.StatusConflict)
			fmt.Fprint(w, `{"code":"order_already_exists","message":"order_id already exists"}`)
			return
		}
		fmt.Fprintf(w, `{"order_id":%q,"payment_session_id":"sess_1"}`, body.OrderID)
	})

	req := OrderRequest{IdempotencyKey: "rent-2026-08-u1", Amount: 1_200_000,
		Currency: domain.Currency, Method: collect.MethodUPICollect, PayerRef: "payer-1"}
	o, err := c.CreateOrder(context.Background(), req)
	if err != nil || o.ProviderOrderID != "rent-2026-08-u1" {
		t.Fatalf("first order = (%+v, %v)", o, err)
	}
	if _, err := c.CreateOrder(context.Background(), req); err == nil {
		t.Error("a duplicate order id was accepted at the provider")
	}
}

// The sandbox found this one, which is the argument for having a smoke test at
// all: every recorded-shape test passed while the adapter was sending the
// payer's phone number as Cashfree's customer id. Their API rejects it — a
// customer id must be alphanumeric with underscores or hyphens — and a leading
// + is not. It was also wrong for a reason their validation does not care
// about: it hands a contact detail to a system that needs an opaque key, and it
// breaks when a tenant changes number.
func TestTheCustomerIdIsAPayerReferenceAndNotAPhoneNumber(t *testing.T) {
	var sent struct {
		Customer struct {
			ID    string `json:"customer_id"`
			Phone string `json:"customer_phone"`
		} `json:"customer_details"`
	}
	c, _ := testCashfree(t, func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &sent); err != nil {
			t.Errorf("unreadable request: %v", err)
		}
		fmt.Fprint(w, `{"order_id":"o1","payment_session_id":"s"}`)
	})

	if _, err := c.CreateOrder(context.Background(), OrderRequest{
		IdempotencyKey: "o1", Amount: 1_200_000, Currency: domain.Currency,
		Method: collect.MethodUPIIntent, PayerContact: "9000000000",
		PayerRef: "b3f1c2d4-0000-4000-8000-000000000001",
	}); err != nil {
		t.Fatalf("CreateOrder: %v", err)
	}
	if sent.Customer.ID == sent.Customer.Phone {
		t.Error("the customer id is the phone number")
	}
	if sent.Customer.ID != "b3f1c2d4-0000-4000-8000-000000000001" {
		t.Errorf("customer_id = %q", sent.Customer.ID)
	}

	// An order with no payer reference is refused here rather than by Cashfree,
	// so the error names what the caller left out.
	if _, err := c.CreateOrder(context.Background(), OrderRequest{
		IdempotencyKey: "o2", Amount: 1_200_000, Currency: domain.Currency,
		Method: collect.MethodUPIIntent, PayerContact: "9000000000",
	}); err == nil {
		t.Error("an order with no payer reference was sent")
	}
}

func TestSanitisingACustomerId(t *testing.T) {
	for in, want := range map[string]string{
		"b3f1c2d4-0000-4000-8000-000000000001": "b3f1c2d4-0000-4000-8000-000000000001",
		"payer_42":                             "payer_42",
		"+919000000000":                        "919000000000",
		"a b/c":                                "abc",
		"+++":                                  "",
	} {
		if got := sanitiseCustomerID(in); got != want {
			t.Errorf("sanitiseCustomerID(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestConfirmTranslatesTheirVocabularyAndRefusesToGuess(t *testing.T) {
	for their, want := range map[string]collect.Status{
		"SUCCESS":       collect.StatusCaptured,
		"PENDING":       collect.StatusAttempted,
		"NOT_ATTEMPTED": collect.StatusAttempted,
		"FAILED":        collect.StatusFailed,
		"USER_DROPPED":  collect.StatusExpired,
		"CANCELLED":     collect.StatusCancelled,
	} {
		got, err := cashfreePaymentStatus(their)
		if err != nil || got != want {
			t.Errorf("%s -> (%s, %v), want %s", their, got, err, want)
		}
	}
	if _, err := cashfreePaymentStatus("SOMETHING_NEW"); err == nil {
		t.Error("an unknown status was translated — ADR-0011 §4 parks what it has no state for")
	}

	c, _ := testCashfree(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `[{"cf_payment_id":998,"payment_status":"SUCCESS","payment_amount":12000.00}]`)
	})
	conf, err := c.Confirm(context.Background(), "rent-2026-08-u1")
	if err != nil {
		t.Fatalf("Confirm: %v", err)
	}
	if conf.Status != collect.StatusCaptured {
		t.Errorf("status = %s", conf.Status)
	}
	if conf.AmountMinor != 1_200_000 {
		t.Errorf("amount = %s, want 12000.00 back in paise", conf.AmountMinor)
	}
}

// An order nobody has paid yet is `created`. Reporting it as failed would let
// the reconciliation sweep close a collection the tenant is about to make.
func TestAnUnattemptedOrderIsCreatedRatherThanFailed(t *testing.T) {
	c, _ := testCashfree(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `[]`)
	})
	conf, err := c.Confirm(context.Background(), "o1")
	if err != nil || conf.Status != collect.StatusCreated {
		t.Errorf("confirm of an unattempted order = (%+v, %v)", conf, err)
	}
}

func TestATransportFailureIsUnavailableAndAClientErrorIsNot(t *testing.T) {
	down, _ := testCashfree(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		fmt.Fprint(w, "upstream is having a day")
	})
	_, err := down.Confirm(context.Background(), "o1")
	if !errors.Is(err, ErrUnavailable) {
		t.Errorf("a 502 returned %v — the offline offer in ADR-0011 §6 depends on this being distinguishable", err)
	}

	rejecting, _ := testCashfree(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"code":"order_amount_invalid","message":"order_amount must be greater than 1"}`)
	})
	_, err = rejecting.Confirm(context.Background(), "o1")
	if errors.Is(err, ErrUnavailable) {
		t.Error("a 400 was reported as the provider being down, which would retry a request that will never succeed")
	}
	if err == nil || !strings.Contains(err.Error(), "order_amount_invalid") {
		t.Errorf("the error does not name what Cashfree objected to: %v", err)
	}
}

func TestMandateRegistrationAndTheRailsCashfreeCarries(t *testing.T) {
	c, _ := testCashfree(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"subscription_id":"mnd-u1","authorisation_link":"https://pay/auth","subscription_session_id":"sess"}`)
	})

	for _, rail := range mandate.Rails() {
		if !c.SupportsRail(rail) {
			t.Errorf("cashfree does not carry %s, and heading the chain was argued on it carrying all three", rail)
		}
	}

	reg, err := c.RegisterMandate(context.Background(), MandateRequest{
		IdempotencyKey: "mandate-u1", Rail: mandate.RailUPIAutopay,
		MaxAmount: 1_500_000, Currency: domain.Currency,
		Reference: "Rent, Flat 402", PayerName: "A Tenant", PayerContact: "+919000000000",
	})
	if err != nil {
		t.Fatalf("RegisterMandate: %v", err)
	}
	if reg.ProviderMandateID != "mnd-u1" || reg.AuthURL == "" {
		t.Errorf("registration = %+v", reg)
	}

	// The one that must not be silently allowed: an authority with no key behind
	// it could be registered twice, and a duplicate mandate debits a tenant twice
	// a month for the length of the tenancy.
	if _, err := c.RegisterMandate(context.Background(), MandateRequest{
		Rail: mandate.RailUPIAutopay, MaxAmount: 1_500_000,
	}); err == nil {
		t.Error("a mandate without an idempotency key was registered")
	}
}

func TestMandateStatusTranslation(t *testing.T) {
	for their, want := range map[string]mandate.Status{
		"INITIALIZED":           mandate.StatusCreated,
		"BANK_APPROVAL_PENDING": mandate.StatusPending,
		"ON_HOLD":               mandate.StatusPending,
		"ACTIVE":                mandate.StatusActive,
		"PAUSED":                mandate.StatusPaused,
		"REJECTED":              mandate.StatusRejected,
		"CANCELLED":             mandate.StatusRevoked,
		"EXPIRED":               mandate.StatusExpired,
	} {
		got, err := cashfreeMandateStatus(their)
		if err != nil || got != want {
			t.Errorf("%s -> (%s, %v), want %s", their, got, err, want)
		}
	}
	if _, err := cashfreeMandateStatus("SOME_NEW_STATE"); err == nil {
		t.Error("an unknown subscription status was translated rather than parked")
	}
}

// Revoking twice succeeds. A tenant cancelling autopay must not see a retry
// read as a refusal.
func TestRevokeIsIdempotent(t *testing.T) {
	calls := 0
	c, _ := testCashfree(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			fmt.Fprint(w, `{"subscription_status":"CANCELLED"}`)
			return
		}
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"code":"subscription_invalid","message":"subscription already cancelled"}`)
	})

	if err := c.Revoke(context.Background(), "mnd-u1"); err != nil {
		t.Fatalf("first revoke: %v", err)
	}
	if err := c.Revoke(context.Background(), "mnd-u1"); err != nil {
		t.Errorf("second revoke: %v — a retried cancellation reads to the tenant as a refusal", err)
	}
}

func TestDebitRequiresAKeyAndALiveMandate(t *testing.T) {
	c, _ := testCashfree(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"cf_payment_id":1234,"payment_id":"debit-2026-08-u1"}`)
	})

	o, err := c.Debit(context.Background(), DebitRequest{
		IdempotencyKey: "debit-2026-08-u1", ProviderMandateID: "mnd-u1",
		Amount: 1_200_000, Currency: domain.Currency, NotifyOn: "2026-08-31",
	})
	if err != nil || o.ProviderOrderID != "debit-2026-08-u1" {
		t.Fatalf("Debit = (%+v, %v)", o, err)
	}

	for name, req := range map[string]DebitRequest{
		"no mandate": {IdempotencyKey: "k", Amount: 100},
		"no key":     {ProviderMandateID: "mnd-u1", Amount: 100},
		"no amount":  {IdempotencyKey: "k", ProviderMandateID: "mnd-u1"},
	} {
		if _, err := c.Debit(context.Background(), req); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// The registry side: Cashfree resolves for mandates, offline never does, and an
// existing authority resolves by name even after the chain has moved on.
func TestTheRegistryResolvesMandatesToCashfreeAndNeverToOffline(t *testing.T) {
	c, _ := testCashfree(t, func(w http.ResponseWriter, r *http.Request) {})
	r := NewRegistry()
	r.Register(c)
	if err := r.SetChain(CashfreeName, collect.OfflineProvider); err != nil {
		t.Fatalf("SetChain: %v", err)
	}

	for _, rail := range mandate.Rails() {
		a, err := r.MandateFor(rail)
		if err != nil || a.Name() != CashfreeName {
			t.Errorf("%s resolved to (%v, %v)", rail, a, err)
		}
	}

	if _, err := r.MandateBy(collect.OfflineProvider); !errors.Is(err, ErrNoMandates) {
		t.Errorf("offline resolved as a mandate adapter: %v", err)
	}

	// After a migration away from Cashfree, an authority it registered is still
	// confirmed and revoked against Cashfree.
	if err := r.SetChain(); err != nil {
		t.Fatalf("SetChain: %v", err)
	}
	if a, err := r.MandateBy(CashfreeName); err != nil || a.Name() != CashfreeName {
		t.Errorf("MandateBy(cashfree) after a chain change = (%v, %v)", a, err)
	}
	if _, err := r.MandateFor(mandate.RailENACH); err == nil {
		t.Error("an empty chain still registered a mandate")
	}
}

// Paise in, decimal rupees out, and back — with no float anywhere in either
// direction. The arch guard from ADR-0007 caught the first version of this
// boundary, which parsed a float64 because that is the obvious way to read
// "12000.00". It is also the way a rounding error gets from an aggregator into
// a ledger, so the conversion is string and integer arithmetic and this test
// holds it to that.
func TestTheRupeeBoundary(t *testing.T) {
	for _, tc := range []struct {
		minor domain.Minor
		want  string
	}{
		{1_200_000, "12000.00"},
		{1, "0.01"},
		{99, "0.99"},
		{100, "1.00"},
		{4_500_050, "45000.50"},
		{-2_500, "-25.00"},
	} {
		if got := rupees(tc.minor).String(); got != tc.want {
			t.Errorf("rupees(%d) = %s, want %s", tc.minor, got, tc.want)
		}
		back, err := minorFromRupees(json.Number(tc.want))
		if err != nil || back != tc.minor {
			t.Errorf("round trip of %s = (%d, %v), want %d", tc.want, back, err, tc.minor)
		}
	}

	// What a provider might actually send, including shapes json.Number permits
	// and a real payload occasionally carries.
	for in, want := range map[string]domain.Minor{
		"12000":     1_200_000,
		"12000.5":   1_200_050,
		"0":         0,
		".50":       50,
		"1234.565":  123_457, // rounded half away from zero, once, here
		"1234.564":  123_456,
		"-1234.565": -123_457,
	} {
		got, err := minorFromRupees(json.Number(in))
		if err != nil || got != want {
			t.Errorf("minorFromRupees(%q) = (%d, %v), want %d", in, got, err, want)
		}
	}

	// Scientific notation from a payments API is not a rounding problem to
	// solve; it means something upstream already went through a float.
	for _, bad := range []string{"", "  ", "1.2e5", "twelve", "12,000.00"} {
		if got, err := minorFromRupees(json.Number(bad)); err == nil {
			t.Errorf("minorFromRupees(%q) = %d, want an error", bad, got)
		}
	}
}
