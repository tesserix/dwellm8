package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/tesserix/dwellm8/services/api/internal/platform/push"
)

// A cancelled viewing reaching the person who booked it (#332). The fact is
// already committed to the outbox beside the row it describes; this only turns
// it into a notification, so a provider outage loses a message, never a state.

// ProspectTokens is the seam to the devices a prospect has registered.
type ProspectTokens interface {
	ForProspect(ctx context.Context, prospectID string) ([]string, error)
}

// Notices tells prospects when a viewing they hold is called off, and how a
// viewing they asked for was answered (#331).
type Notices struct {
	Tokens ProspectTokens
	Sender PushSender
	Log    *slog.Logger
}

// why maps the reason on a cancellation to the sentence the prospect reads. A
// prospect's own cancellation is absent on purpose: they did it.
var why = map[string]string{
	"listing_let":        "The home has been let, so this viewing is off. There are other places to see.",
	"cancelled_by_owner": "The viewing you booked has been called off. Pick another time, or ask for one.",
}

// answers are the ends of the request loop, each saying which end it was.
var answers = map[string]struct{ title, body string }{
	"discovery.inspection.confirmed": {"Your viewing is confirmed",
		"The time you asked for is confirmed. Open the app for where to go."},
	"discovery.inspection.countered": {"Another time is offered",
		"The times you asked for do not suit, and another time is offered instead."},
	"discovery.inspection.declined": {"Your viewing request was declined",
		"Your request was declined. There are other places to see."},
}

// Handle is the consumer entry for the discovery stream.
func (n Notices) Handle(ctx context.Context, body []byte) error {
	var env struct {
		Type string `json:"type"`
		Data struct {
			By         string `json:"by"`
			ProspectID string `json:"prospect_id"`
			ListingID  string `json:"listing_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return fmt.Errorf("notices: undecodable event: %w", err)
	}
	if env.Data.ProspectID == "" {
		return nil
	}
	title, line := "Your viewing is off", ""
	if a, isAnswer := answers[env.Type]; isAnswer {
		title, line = a.title, a.body
	} else if env.Type == "discovery.inspection.cancelled" {
		line = why[env.Data.By]
	}
	if line == "" {
		return nil
	}

	tokens, err := n.Tokens.ForProspect(ctx, env.Data.ProspectID)
	if err != nil {
		return fmt.Errorf("notices: reading devices for prospect %s: %w", env.Data.ProspectID, err)
	}
	if len(tokens) == 0 {
		return nil
	}

	msgs := make([]push.Message, 0, len(tokens))
	for _, t := range tokens {
		msgs = append(msgs, push.Message{
			To: t, Title: title, Body: line,
			Data: map[string]any{"url": "/viewings"},
		})
	}
	if _, err := n.Sender.Send(ctx, msgs); err != nil {
		// The row already says the viewing is cancelled; a redelivery would
		// send it again, so log loudly rather than poison the consumer.
		n.Log.Error("notices: push send failed", "prospect", env.Data.ProspectID, "error", err)
		return nil
	}
	n.Log.Info("viewing notice sent", "type", env.Type, "prospect", env.Data.ProspectID,
		"listing", env.Data.ListingID, "devices", len(msgs))
	return nil
}
