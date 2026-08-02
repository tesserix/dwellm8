package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/tesserix/dwellm8/services/api/internal/discovery/store"
	"github.com/tesserix/dwellm8/services/api/internal/platform/push"
)

// Saved-search alerts over push (#144's delivery, #126's first slice). The
// published fact fans out to every opted-in search it satisfies; the store's
// watermark makes a redelivery a no-op, and the notification carries only
// what the public listing page already shows — nothing on a lock screen that
// a stranger could not read on the site.

// PushSender is the seam to the push provider.
type PushSender interface {
	Send(ctx context.Context, msgs []push.Message) (dead []string, err error)
}

// Alerts turns listing publications into notifications.
type Alerts struct {
	Searches *store.Searches
	Tokens   *store.PushTokens
	Sender   PushSender
	Log      *slog.Logger
}

// Handle is the consumer entry for the discovery stream.
func (a Alerts) Handle(ctx context.Context, body []byte) error {
	var env struct {
		Type    string `json:"type"`
		Subject struct {
			ID string `json:"id"`
		} `json:"subject"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return fmt.Errorf("alerts: undecodable event: %w", err)
	}
	if env.Type != "discovery.listing.published" || env.Subject.ID == "" {
		return nil
	}

	l, targets, err := a.Searches.AlertsForListing(ctx, env.Subject.ID)
	if err != nil {
		return fmt.Errorf("alerts: matching for listing %s: %w", env.Subject.ID, err)
	}
	if len(targets) == 0 {
		return nil
	}

	msgs := make([]push.Message, 0, len(targets))
	for _, t := range targets {
		msgs = append(msgs, push.Message{
			To:    t.Token,
			Title: "New home matches your search",
			Body:  fmt.Sprintf("%s — %s, ₹%d/mo", l.Headline, l.Locality, l.Rent/100),
			Data:  map[string]any{"url": "/listing?id=" + l.ID},
		})
	}
	dead, err := a.Sender.Send(ctx, msgs)
	if err != nil {
		// The watermark has advanced, so a retry would alert nobody — log
		// loudly rather than poison the consumer over a provider blip.
		a.Log.Error("alerts: push send failed", "listing", l.ID, "targets", len(msgs), "error", err)
		return nil
	}
	if len(dead) > 0 {
		if err := a.Tokens.Disable(ctx, dead); err != nil {
			a.Log.Error("alerts: disabling dead tokens", "count", len(dead), "error", err)
		}
	}
	a.Log.Info("saved-search alerts sent", "listing", l.ID, "sent", len(msgs)-len(dead), "dead", len(dead))
	return nil
}
