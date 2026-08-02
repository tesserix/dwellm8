// Package push sends Expo push notifications (#126). One adapter, batch-of-100
// the way Expo's API wants it, and the one answer the caller must act on —
// which tokens are dead — surfaced rather than logged away.
package push

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Message is one notification. Data carries the deep link the tap follows.
type Message struct {
	To    string         `json:"to"`
	Title string         `json:"title,omitempty"`
	Body  string         `json:"body,omitempty"`
	Data  map[string]any `json:"data,omitempty"`
}

// Sender speaks Expo's push API.
type Sender struct {
	client *http.Client
	base   string
}

// New builds the sender. base is overridable for tests; empty means Expo.
func New(base string) *Sender {
	if base == "" {
		base = "https://exp.host"
	}
	return &Sender{client: &http.Client{Timeout: 15 * time.Second}, base: base}
}

const batch = 100

// Send delivers the messages and returns the tokens Expo declared dead
// (DeviceNotRegistered) — the caller disables those. Other per-ticket errors
// are transient on Expo's side and are not worth failing the event over.
func (s *Sender) Send(ctx context.Context, msgs []Message) (dead []string, err error) {
	for start := 0; start < len(msgs); start += batch {
		end := min(start+batch, len(msgs))
		chunk := msgs[start:end]
		d, err := s.send(ctx, chunk)
		if err != nil {
			return dead, err
		}
		dead = append(dead, d...)
	}
	return dead, nil
}

func (s *Sender) send(ctx context.Context, msgs []Message) ([]string, error) {
	payload, err := json.Marshal(msgs)
	if err != nil {
		return nil, fmt.Errorf("push: encoding: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		s.base+"/--/api/v2/push/send", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	res, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("push: sending: %w", err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if res.StatusCode >= 300 {
		return nil, fmt.Errorf("push: status %d", res.StatusCode)
	}

	var out struct {
		Data []struct {
			Status  string `json:"status"`
			Details struct {
				Error string `json:"error"`
			} `json:"details"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("push: parsing tickets: %w", err)
	}
	// Tickets come back in request order; that pairing is the contract.
	var dead []string
	for i, t := range out.Data {
		if i < len(msgs) && t.Status == "error" && t.Details.Error == "DeviceNotRegistered" {
			dead = append(dead, msgs[i].To)
		}
	}
	return dead, nil
}
