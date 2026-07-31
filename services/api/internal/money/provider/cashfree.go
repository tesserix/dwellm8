package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/tesserix/dwellm8/services/api/internal/money/domain"
	"github.com/tesserix/dwellm8/services/api/internal/money/domain/collect"
	"github.com/tesserix/dwellm8/services/api/internal/money/domain/mandate"
)

// Cashfree, the first real aggregator behind the seam.
//
// Everything provider-specific is in this file and its test: the vocabulary
// translation, the header names, the signature scheme, the version pin. Nothing
// in money/domain knows Cashfree exists, and the way to check that claim is
// `grep -ril cashfree internal/money/domain` returning nothing.
//
// Three of its properties drove decisions elsewhere and are worth stating where
// somebody debugging will find them:
//
//   - It signs `x-webhook-timestamp + rawBody` and base64-encodes. That is why
//     Adapter.VerifyWebhook takes a Webhook rather than a payload and a string.
//   - Its API is versioned by date in a header, not in the URL. An unpinned
//     client changes behaviour on Cashfree's release schedule rather than ours,
//     so the version is required configuration and startup fails without it.
//   - Its status vocabulary is wider than ours and not one-to-one. Anything it
//     reports that this system has no state for is an error rather than a guess:
//     ADR-0011 §4 parks unknown claims, and the way to keep that promise is for
//     the adapter to refuse to invent a status.

// CashfreeName is the value stored in payments.provider and mandates.provider.
// Data, and stable forever.
const CashfreeName = "cashfree"

// CashfreeConfig is what a deployment must supply. Every field is required
// except the clock and the HTTP client, which exist for the tests.
type CashfreeConfig struct {
	// acceptsDeliveries is derived in NewCashfree from whether a webhook secret
	// was configured. Unexported: it is a conclusion, not a setting, and a
	// deployment must not be able to claim it can verify deliveries it cannot.
	acceptsDeliveries bool

	// BaseURL is https://api.cashfree.com/pg or the sandbox equivalent. Required
	// rather than defaulted: a default would make production the thing you get
	// by forgetting to configure anything.
	BaseURL string
	// ClientID and ClientSecret come from GCP Secret Manager. Never the database,
	// never a values file, never a log line.
	ClientID     string
	ClientSecret string
	// WebhookSecret verifies deliveries. An empty one verifies nothing, which
	// means a deployment that forgot it rejects every delivery instead of
	// trusting every delivery.
	WebhookSecret string
	// APIVersion is Cashfree's dated x-api-version. Pinned, because their API
	// changes shape by date and an unpinned client is a deployment whose
	// behaviour changes on somebody else's release day.
	APIVersion string

	// ReplayWindow is how old a correctly signed delivery may be. Zero disables
	// the check, which is why NewCashfree supplies a default rather than
	// accepting the zero value.
	ReplayWindow time.Duration
	Timeout      time.Duration

	HTTP *http.Client
	Now  func() time.Time
}

// Cashfree is the adapter.
type Cashfree struct {
	cfg  CashfreeConfig
	http *http.Client
	now  func() time.Time
}

// Compile-time proof that the adapter satisfies both halves of the seam.
var (
	_ Adapter        = (*Cashfree)(nil)
	_ MandateAdapter = (*Cashfree)(nil)
)

// NewCashfree validates the configuration and fails at startup on anything
// missing. Every one of these checks exists because the alternative failure
// happens at 9am on the first of the month, against real tenants, in a code
// path nobody is watching.
func NewCashfree(cfg CashfreeConfig) (*Cashfree, error) {
	switch {
	case strings.TrimSpace(cfg.BaseURL) == "":
		return nil, errors.New("cashfree: no base URL — a defaulted one would make production the thing you get by forgetting to configure anything")
	case cfg.ClientID == "" || cfg.ClientSecret == "":
		return nil, errors.New("cashfree: no client credentials")
	case cfg.APIVersion == "":
		return nil, errors.New("cashfree: no x-api-version — an unpinned client changes behaviour on Cashfree's release schedule, not ours")
	}
	// A missing webhook secret is not fatal, and used to be. Confirmation is an
	// API call — Confirm asks `GET /orders/{id}/payments` and has never trusted a
	// delivery's contents — so a deployment that only polls is a working
	// deployment, and refusing to construct one made local development and any
	// cluster the provider cannot reach impossible to exercise.
	//
	// The safety property is unchanged and is now enforced where it belongs: with
	// no secret, VerifyWebhookWithReason refuses every delivery by name rather
	// than by comparing against an empty key. An empty HMAC key is a *valid* key,
	// so the old code would have verified deliveries signed with "" if the
	// constructor had ever let one through.
	if cfg.WebhookSecret == "" {
		cfg.acceptsDeliveries = false
	} else {
		cfg.acceptsDeliveries = true
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 20 * time.Second
	}
	if cfg.ReplayWindow <= 0 {
		cfg.ReplayWindow = 5 * time.Minute
	}
	c := &Cashfree{cfg: cfg, http: cfg.HTTP, now: cfg.Now}
	if c.http == nil {
		c.http = &http.Client{Timeout: cfg.Timeout}
	}
	if c.now == nil {
		c.now = time.Now
	}
	return c, nil
}

func (c *Cashfree) Name() string { return CashfreeName }

// Supports is the method vocabulary Cashfree carries for one-off collection.
//
// upi_autopay is deliberately absent: a debit under a standing authority is
// created through Debit, not through CreateOrder, and letting the registry
// route an autopay method here would produce an order with no mandate behind
// it. Offline methods are never any adapter's, per ADR-0011 §6.
func (c *Cashfree) Supports(m collect.Method) bool {
	switch m {
	case collect.MethodUPIIntent, collect.MethodUPICollect,
		collect.MethodCard, collect.MethodNetbanking:
		return true
	}
	return false
}

// SupportsRail is why Cashfree heads the chain: one product spans all three
// recurring rails, and physical NACH is the only one that reaches a tenant
// whose bank does no eNACH.
func (c *Cashfree) SupportsRail(r mandate.Rail) bool {
	switch r {
	case mandate.RailUPIAutopay, mandate.RailENACH, mandate.RailPhysicalNACH:
		return true
	}
	return false
}

// VerifyWebhook is Cashfree's scheme, with the replay window.
//
// A stale-but-genuine delivery returns false here, because this function's
// contract is "may I believe this", and the answer for a four-hour-old delivery
// is no. VerifyWebhookWithReason exists for the handler that needs to record
// *which* no it was, since a bad signature and a replayed delivery are parked
// for different reasons and only one of them is somebody attacking us.
func (c *Cashfree) VerifyWebhook(w Webhook) bool {
	ok, err := c.VerifyWebhookWithReason(w)
	return ok && err == nil
}

// VerifyWebhookWithReason reports both halves: whether the signature is the
// provider's, and whether the delivery is fresh enough to act on.
func (c *Cashfree) VerifyWebhookWithReason(w Webhook) (bool, error) {
	if !c.cfg.acceptsDeliveries {
		// Refused by name rather than by comparing against an empty key: an empty
		// HMAC key verifies anything signed with an empty key, which is a
		// signature an attacker can produce.
		return false, errors.New("cashfree: this deployment has no webhook secret, so no delivery " +
			"can be trusted — it confirms by polling instead")
	}
	return VerifyHMACSHA256Base64WithTimestamp(w, c.cfg.WebhookSecret, c.now(), c.cfg.ReplayWindow)
}

// AcceptsDeliveries reports whether this deployment can verify a webhook at all.
// A poll-only deployment answers false, and the difference is worth surfacing:
// "no deliveries arrived" and "deliveries arrived and none could be trusted" are
// different operational facts.
func (c *Cashfree) AcceptsDeliveries() bool { return c.cfg.acceptsDeliveries }

// ---------------------------------------------------------------------------
// One-off collection
// ---------------------------------------------------------------------------

func (c *Cashfree) CreateOrder(ctx context.Context, req OrderRequest) (Order, error) {
	if !c.Supports(req.Method) {
		return Order{}, fmt.Errorf("cashfree: cannot create an order for %s", req.Method)
	}
	if req.IdempotencyKey == "" {
		return Order{}, errors.New("cashfree: an order without an idempotency key cannot be retried safely")
	}
	if req.Amount <= 0 {
		return Order{}, fmt.Errorf("cashfree: an order of %s is not a collection", req.Amount)
	}
	customer := sanitiseCustomerID(req.PayerRef)
	if customer == "" {
		return Order{}, errors.New("cashfree: an order needs a payer reference — " +
			"the aggregator keys its customer record on it, and the phone number is not that key")
	}

	// order_id is our idempotency key, not a generated one. Cashfree rejects a
	// duplicate order_id, so their uniqueness and ours become the same fact and
	// a retry cannot produce a second order at their end either.
	body := map[string]any{
		"order_id":       req.IdempotencyKey,
		"order_amount":   rupees(req.Amount),
		"order_currency": orCurrency(req.Currency),
		"order_note":     req.Reference,
		"customer_details": map[string]any{
			"customer_id":    customer,
			"customer_name":  req.PayerName,
			"customer_phone": req.PayerContact,
		},
	}

	var out struct {
		OrderID     string `json:"order_id"`
		PaymentSess string `json:"payment_session_id"`
		OrderStatus string `json:"order_status"`
	}
	if err := c.call(ctx, http.MethodPost, "/orders", req.IdempotencyKey, body, &out); err != nil {
		return Order{}, err
	}
	if out.OrderID == "" {
		return Order{}, errors.New("cashfree: an order came back with no id")
	}
	// PayToken rather than PayURL: Cashfree hands back a session the payer's
	// client exchanges, and which of the two an adapter populates is the
	// canonical model's way of not caring.
	return Order{ProviderOrderID: out.OrderID, PayToken: out.PaymentSess}, nil
}

func (c *Cashfree) Confirm(ctx context.Context, providerPaymentID string) (Confirmation, error) {
	if providerPaymentID == "" {
		return Confirmation{}, errors.New("cashfree: nothing to confirm")
	}
	var out struct {
		CFPaymentID   json.Number `json:"cf_payment_id"`
		PaymentStatus string      `json:"payment_status"`
		PaymentAmount json.Number `json:"payment_amount"`
		ErrorDetails  *struct {
			ErrorCode string `json:"error_code"`
		} `json:"error_details"`
	}
	path := "/orders/" + providerPaymentID + "/payments"
	var list []json.RawMessage
	if err := c.call(ctx, http.MethodGet, path, "", nil, &list); err != nil {
		return Confirmation{}, err
	}
	if len(list) == 0 {
		// The order exists and nobody has attempted it. That is `created`, not a
		// failure, and reporting it as a failure would let a reconciliation sweep
		// close a collection the tenant is still about to make.
		return Confirmation{ProviderPaymentID: providerPaymentID, Status: collect.StatusCreated}, nil
	}
	if err := json.Unmarshal(list[len(list)-1], &out); err != nil {
		return Confirmation{}, fmt.Errorf("cashfree: unreadable payment: %w", err)
	}
	st, err := cashfreePaymentStatus(out.PaymentStatus)
	if err != nil {
		return Confirmation{}, err
	}
	amount, err := minorFromRupees(out.PaymentAmount)
	if err != nil {
		return Confirmation{}, err
	}
	conf := Confirmation{
		ProviderPaymentID: providerPaymentID,
		Status:            st,
		AmountMinor:       amount,
	}
	if out.ErrorDetails != nil {
		conf.FailureCode = out.ErrorDetails.ErrorCode
	}
	return conf, nil
}

// cashfreePaymentStatus translates their vocabulary into ours.
//
// USER_DROPPED is the one worth naming. It is a payer who opened the app and
// walked away, and it maps to expired rather than failed: nothing declined, and
// an owner reading "failed" would chase a tenant whose bank never saw a
// request. An unknown status is an error, never a guess — the caller parks it.
func cashfreePaymentStatus(s string) (collect.Status, error) {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "SUCCESS":
		return collect.StatusCaptured, nil
	case "PENDING", "NOT_ATTEMPTED":
		return collect.StatusAttempted, nil
	case "FAILED":
		return collect.StatusFailed, nil
	case "USER_DROPPED":
		return collect.StatusExpired, nil
	case "CANCELLED", "VOID":
		return collect.StatusCancelled, nil
	}
	return "", fmt.Errorf("cashfree: unknown payment status %q — parked rather than guessed", s)
}

// ---------------------------------------------------------------------------
// Mandates
// ---------------------------------------------------------------------------

func (c *Cashfree) RegisterMandate(ctx context.Context, req MandateRequest) (MandateRegistration, error) {
	if !c.SupportsRail(req.Rail) {
		return MandateRegistration{}, fmt.Errorf("cashfree: cannot register a %s mandate", req.Rail)
	}
	if req.IdempotencyKey == "" {
		return MandateRegistration{}, errors.New("cashfree: a mandate without an idempotency key could be registered twice, and a duplicate authority debits a tenant twice a month forever")
	}
	if req.MaxAmount <= 0 {
		return MandateRegistration{}, fmt.Errorf("cashfree: a mandate ceiling of %s authorises nothing", req.MaxAmount)
	}

	body := map[string]any{
		"subscription_id":            req.IdempotencyKey,
		"customer_details":           map[string]any{"customer_name": req.PayerName, "customer_phone": req.PayerContact},
		"authorisation_details":      map[string]any{"authorisation_amount": 1, "payment_methods": []string{cashfreeRail(req.Rail)}},
		"subscription_note":          req.Reference,
		"subscription_expiry_time":   req.EndsOn,
		"subscription_first_charge":  req.FirstDebitOn,
		"subscription_max_amount":    rupees(req.MaxAmount),
		"subscription_payment_modes": []string{cashfreeRail(req.Rail)},
	}
	if req.PayerVPA != "" {
		body["subscription_payment_details"] = map[string]any{"upi_id": req.PayerVPA}
	}

	var out struct {
		SubscriptionID string `json:"subscription_id"`
		AuthLink       string `json:"authorisation_link"`
		SessionID      string `json:"subscription_session_id"`
	}
	if err := c.call(ctx, http.MethodPost, "/subscriptions", req.IdempotencyKey, body, &out); err != nil {
		return MandateRegistration{}, err
	}
	if out.SubscriptionID == "" {
		return MandateRegistration{}, errors.New("cashfree: a mandate came back with no id")
	}
	return MandateRegistration{
		ProviderMandateID: out.SubscriptionID,
		AuthURL:           out.AuthLink,
		AuthToken:         out.SessionID,
	}, nil
}

func (c *Cashfree) ConfirmMandate(ctx context.Context, id string) (MandateConfirmation, error) {
	if id == "" {
		return MandateConfirmation{}, errors.New("cashfree: nothing to confirm")
	}
	var out struct {
		SubscriptionID     string      `json:"subscription_id"`
		SubscriptionStatus string      `json:"subscription_status"`
		MaxAmount          json.Number `json:"subscription_max_amount"`
		FailureReason      string      `json:"failure_reason"`
	}
	if err := c.call(ctx, http.MethodGet, "/subscriptions/"+id, "", nil, &out); err != nil {
		return MandateConfirmation{}, err
	}
	st, err := cashfreeMandateStatus(out.SubscriptionStatus)
	if err != nil {
		return MandateConfirmation{}, err
	}
	ceiling, err := minorFromRupees(out.MaxAmount)
	if err != nil {
		return MandateConfirmation{}, err
	}
	return MandateConfirmation{
		ProviderMandateID: id,
		Status:            st,
		FailureCode:       out.FailureReason,
		MaxAmount:         ceiling,
	}, nil
}

// cashfreeMandateStatus translates their subscription vocabulary into ours.
//
// BANK_APPROVAL_PENDING is the state physical NACH sits in for a working week,
// and it maps to pending rather than to anything that looks like a failure. A
// sweep that treated it as stuck would cancel mandates that were about to
// activate.
func cashfreeMandateStatus(s string) (mandate.Status, error) {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "INITIALIZED":
		return mandate.StatusCreated, nil
	case "PENDING", "BANK_APPROVAL_PENDING", "ON_HOLD":
		return mandate.StatusPending, nil
	case "ACTIVE":
		return mandate.StatusActive, nil
	case "PAUSED":
		return mandate.StatusPaused, nil
	case "REJECTED", "FAILED":
		return mandate.StatusRejected, nil
	case "CANCELLED":
		return mandate.StatusRevoked, nil
	case "COMPLETED", "EXPIRED":
		return mandate.StatusExpired, nil
	}
	return "", fmt.Errorf("cashfree: unknown subscription status %q — parked rather than guessed", s)
}

func cashfreeRail(r mandate.Rail) string {
	switch r {
	case mandate.RailUPIAutopay:
		return "upi"
	case mandate.RailENACH:
		return "enach"
	case mandate.RailPhysicalNACH:
		return "pnach"
	}
	return ""
}

func (c *Cashfree) Debit(ctx context.Context, req DebitRequest) (Order, error) {
	switch {
	case req.ProviderMandateID == "":
		return Order{}, errors.New("cashfree: a debit against no mandate")
	case req.IdempotencyKey == "":
		return Order{}, errors.New("cashfree: a debit without an idempotency key could take the rent twice")
	case req.Amount <= 0:
		return Order{}, fmt.Errorf("cashfree: a debit of %s is not a collection", req.Amount)
	}

	body := map[string]any{
		"subscription_id":       req.ProviderMandateID,
		"payment_id":            req.IdempotencyKey,
		"payment_amount":        rupees(req.Amount),
		"payment_remarks":       req.Reference,
		"payment_schedule_date": req.NotifyOn,
	}
	var out struct {
		CFPaymentID json.Number `json:"cf_payment_id"`
		PaymentID   string      `json:"payment_id"`
	}
	if err := c.call(ctx, http.MethodPost,
		"/subscriptions/"+req.ProviderMandateID+"/payments", req.IdempotencyKey, body, &out); err != nil {
		return Order{}, err
	}
	id := out.PaymentID
	if id == "" {
		id = out.CFPaymentID.String()
	}
	if id == "" {
		return Order{}, errors.New("cashfree: a debit came back with no payment id")
	}
	return Order{ProviderOrderID: id}, nil
}

func (c *Cashfree) Revoke(ctx context.Context, id string) error {
	if id == "" {
		return errors.New("cashfree: nothing to revoke")
	}
	err := c.call(ctx, http.MethodPost, "/subscriptions/"+id+"/manage",
		"", map[string]any{"action": "CANCEL"}, nil)
	if err == nil {
		return nil
	}
	// Revoking something already revoked is success. The alternative is a
	// tenant's cancellation failing on a retry and reading, to them, as a refusal
	// to let them cancel.
	var apiErr *cashfreeError
	if errors.As(err, &apiErr) && apiErr.alreadyTerminal() {
		return nil
	}
	return err
}

// ---------------------------------------------------------------------------
// HTTP
// ---------------------------------------------------------------------------

type cashfreeError struct {
	Status  int
	Code    string
	Message string
	Type    string
}

func (e *cashfreeError) Error() string {
	return fmt.Sprintf("cashfree: %d %s: %s", e.Status, e.Code, e.Message)
}

// alreadyTerminal reports the "you asked me to end something already ended"
// family, which is success for an idempotent revoke.
func (e *cashfreeError) alreadyTerminal() bool {
	m := strings.ToLower(e.Message + " " + e.Code)
	return strings.Contains(m, "already cancelled") ||
		strings.Contains(m, "already_cancelled") ||
		strings.Contains(m, "not active")
}

func (c *Cashfree) call(ctx context.Context, method, path, idempotencyKey string, in, out any) error {
	var body io.Reader
	if in != nil {
		buf, err := json.Marshal(in)
		if err != nil {
			return fmt.Errorf("cashfree: encoding the request: %w", err)
		}
		body = bytes.NewReader(buf)
	}

	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.cfg.BaseURL, "/")+path, body)
	if err != nil {
		return fmt.Errorf("cashfree: building the request: %w", err)
	}
	req.Header.Set("x-client-id", c.cfg.ClientID)
	req.Header.Set("x-client-secret", c.cfg.ClientSecret)
	req.Header.Set("x-api-version", c.cfg.APIVersion)
	req.Header.Set("accept", "application/json")
	if in != nil {
		req.Header.Set("content-type", "application/json")
	}
	if idempotencyKey != "" {
		// Theirs where they honour it; ours regardless, because the unique index
		// on (tenant_id, idempotency_key) is the guarantee we actually rely on.
		req.Header.Set("x-idempotency-key", idempotencyKey)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		// Every transport failure is ErrUnavailable, and that is the distinction
		// ADR-0011 §6 turns into an offer of offline recording made to a human.
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("%w: reading the response: %v", ErrUnavailable, err)
	}

	if resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests {
		// Their problem, not ours, and retryable. Same class as a dial failure.
		return fmt.Errorf("%w: %d %s", ErrUnavailable, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	if resp.StatusCode >= 400 {
		e := &cashfreeError{Status: resp.StatusCode}
		var parsed struct {
			Message string `json:"message"`
			Code    string `json:"code"`
			Type    string `json:"type"`
		}
		if json.Unmarshal(raw, &parsed) == nil {
			e.Message, e.Code, e.Type = parsed.Message, parsed.Code, parsed.Type
		}
		if e.Message == "" {
			e.Message = strings.TrimSpace(string(raw))
		}
		return e
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("cashfree: unreadable response: %w", err)
	}
	return nil
}

// rupees renders minor units for an API that speaks decimal rupees.
//
// The conversion happens here, at the edge, and nowhere else. ADR-0007's
// position is that money is int64 paise everywhere inside the system, and the
// arch guard enforces it by refusing float64 anywhere in this module — which it
// did to the first draft of this file. That was the guard working: the obvious
// way to read "12000.00" is to parse a float, and a float is exactly how a
// rounding error gets from an aggregator into a ledger.
//
// So both directions are string and integer arithmetic. json.Number carries the
// provider's digits verbatim rather than through a float64 on the way in.
func rupees(m domain.Minor) json.Number {
	sign := ""
	v := int64(m)
	if v < 0 {
		sign, v = "-", -v
	}
	return json.Number(fmt.Sprintf("%s%d.%02d", sign, v/100, v%100))
}

// minorFromRupees parses decimal rupees into paise without touching a float.
//
// A third decimal place should never arrive — the rails deal in paise — but if
// one does it is rounded half away from zero rather than truncated, and more
// importantly it is rounded here, once, where it can be tested, instead of
// wherever the value is first added to something.
func minorFromRupees(n json.Number) (domain.Minor, error) {
	s := strings.TrimSpace(n.String())
	if s == "" {
		return 0, errors.New("cashfree: an amount with no digits")
	}
	neg := false
	switch s[0] {
	case '-':
		neg, s = true, s[1:]
	case '+':
		s = s[1:]
	}
	if strings.ContainsAny(s, "eE") {
		// Scientific notation from a payments API is not a rounding problem to
		// solve, it is a signal that something upstream went through a float.
		return 0, fmt.Errorf("cashfree: %q is not a decimal amount", n.String())
	}

	whole, frac, _ := strings.Cut(s, ".")
	if whole == "" {
		whole = "0"
	}
	rupeesPart, err := strconv.ParseInt(whole, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("cashfree: unreadable amount %q: %w", n.String(), err)
	}

	var paise int64
	switch {
	case frac == "":
	case len(frac) == 1:
		if paise, err = strconv.ParseInt(frac, 10, 64); err != nil {
			return 0, fmt.Errorf("cashfree: unreadable amount %q: %w", n.String(), err)
		}
		paise *= 10
	default:
		if paise, err = strconv.ParseInt(frac[:2], 10, 64); err != nil {
			return 0, fmt.Errorf("cashfree: unreadable amount %q: %w", n.String(), err)
		}
		if len(frac) > 2 && frac[2] >= '5' && frac[2] <= '9' {
			paise++
		}
	}

	total := rupeesPart*100 + paise
	if neg {
		total = -total
	}
	m := domain.Minor(total)
	if err := m.Valid(); err != nil {
		return 0, fmt.Errorf("cashfree: %q: %w", n.String(), err)
	}
	return m, nil
}

// sanitiseCustomerID keeps what Cashfree accepts as a customer id — alphanumeric,
// underscore, hyphen — and drops everything else rather than sending a value
// their API will reject.
//
// It is not a general escaper and does not need to be: the input is our own
// payer id, so the only characters this ever removes are the ones a malformed
// caller passed. Anything that reduces to nothing is refused above rather than
// silently becoming an order with no customer behind it.
func sanitiseCustomerID(ref string) string {
	var b strings.Builder
	for _, r := range ref {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
			b.WriteRune(r)
		}
	}
	return b.String()
}

func orCurrency(c string) string {
	if c == "" {
		return domain.Currency
	}
	return c
}
