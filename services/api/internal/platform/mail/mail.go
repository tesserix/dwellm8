// Package mail sends the few emails this API originates itself — today, the
// one-time code that proves a manager's address before a firm is minted (#282).
//
// Resend is the only transport, for the reason it is the only one in HomeChef:
// the sending domain publishes Resend's DKIM selectors, so a message handed to
// anything else fails DMARC and lands in spam rather than bouncing visibly.
package mail

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

const endpoint = "https://api.resend.com/emails"

// Resend is the account to send through. Read from the environment, which reads
// it from Secret Manager — never from a file in the image.
type Resend struct {
	APIKey string
	// From is the whole sender, "Name <address>", because that is what the API
	// takes and a display name is not worth a second field to assemble.
	From string
}

// Configured reads the credentials themselves rather than a flag somebody has
// to remember to set alongside them.
func (c Resend) Configured() bool { return c.APIKey != "" && c.From != "" }

// Message is one email.
type Message struct {
	From    string
	To      string
	Subject string
	Body    string
}

// Sender is what callers depend on, so a handler can be tested without a relay.
type Sender interface {
	Send(ctx context.Context, m Message) error
}

type sender struct {
	cfg      Resend
	endpoint string
	client   *http.Client
}

func New(cfg Resend) Sender {
	return &sender{cfg: cfg, endpoint: endpoint, client: &http.Client{Timeout: 15 * time.Second}}
}

var errUnconfigured = errors.New("mail: no mail provider is configured")

type request struct {
	From    string   `json:"from"`
	To      []string `json:"to"`
	Subject string   `json:"subject"`
	Text    string   `json:"text"`
}

func (s *sender) Send(ctx context.Context, m Message) error {
	if !s.cfg.Configured() {
		// Refusing is the point. A mailer that silently drops looks like a
		// working sign-up until somebody waits ten minutes for a code.
		return errUnconfigured
	}
	if m.From == "" {
		m.From = s.cfg.From
	}

	body, err := json.Marshal(request{From: m.From, To: []string{m.To}, Subject: m.Subject, Text: m.Body})
	if err != nil {
		return fmt.Errorf("mail: encoding the message: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("mail: building the request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+s.cfg.APIKey)
	req.Header.Set("Content-Type", "application/json")

	res, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("mail: sending to %s: %w", m.To, err)
	}
	defer res.Body.Close()

	if res.StatusCode < 200 || res.StatusCode > 299 {
		said, _ := io.ReadAll(io.LimitReader(res.Body, 2048))
		return fmt.Errorf("mail: Resend returned %d: %s", res.StatusCode, bytes.TrimSpace(said))
	}
	return nil
}
