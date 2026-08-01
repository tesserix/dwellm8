package routine

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/tesserix/dwellm8/services/api/internal/platform/automation"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
)

// Consumer runs the event-triggered automations. ADR-0033 §5.
//
// A durable consumer rather than a call inside the module that published the
// fact, and the difference is the whole reason ADR-0002 has an outbox: a tenancy
// going live must not fail because a checklist template was misconfigured, and an
// automation that ran inside the activating transaction would do exactly that.
type Consumer struct {
	Runner *automation.Runner
	Log    *slog.Logger
}

// Handle decodes one event and offers it to the catalogue.
//
// The organisation comes from the envelope, and the context is scoped to it before
// anything is read: a consumer runs outside a request, so nothing else would set
// the tenant and every policy would deny — which is the correct failure and a
// confusing one to debug at three in the morning.
func (c Consumer) Handle(ctx context.Context, body []byte) error {
	var env struct {
		Type     string                    `json:"type"`
		TenantID string                    `json:"tenant_id"`
		Subject  struct{ Kind, ID string } `json:"subject"`
		Data     json.RawMessage           `json:"data"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return fmt.Errorf("automations: undecodable event: %w", err)
	}
	if len(c.Runner.Catalogue().For(env.Type)) == 0 {
		return nil
	}
	if env.TenantID == "" {
		// A fact belonging to no organisation cannot be automated for one. Not an
		// error — the outbox carries platform-level events too — and the consumer
		// must acknowledge it rather than redeliver it forever.
		return nil
	}

	data := map[string]string{}
	if len(env.Data) > 0 {
		var raw map[string]any
		if err := json.Unmarshal(env.Data, &raw); err != nil {
			return fmt.Errorf("automations: undecodable payload on %s: %w", env.Type, err)
		}
		for k, v := range raw {
			if s, ok := v.(string); ok {
				data[k] = s
			}
		}
	}

	subject := automation.Subject{
		Kind: automation.SubjectKind(env.Subject.Kind),
		ID:   env.Subject.ID,
	}
	octx := tenancy.With(ctx, tenancy.ID(env.TenantID))
	if err := c.Runner.Handle(octx, env.Type, subject, data); err != nil {
		return fmt.Errorf("automations on %s: %w", env.Type, err)
	}
	return nil
}
