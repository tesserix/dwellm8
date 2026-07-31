package authz

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/tesserix/dwellm8/services/api/internal/platform/events"
)

// Tuple is one edge in the graph.
type Tuple struct {
	User     string `json:"user"`
	Relation string `json:"relation"`
	Object   string `json:"object"`
}

// TuplesFor maps a domain event to the graph writes and deletes it implies.
// dwellm8#151, ADR-0020 §3: tuples are projections of rows, and this function
// is the projection — the rebuild path replays domain state through the same
// mapping, which is what makes "rebuilding produces an identical graph" true
// by construction rather than by test.
//
// An event this function does not know is not an error; most facts move no
// edges. An event it knows but cannot decode is one — a projector that skipped
// it would leave access quietly wrong, and the consumer's retry-then-DLQ path
// is where that must surface.
func TuplesFor(e events.Envelope) (writes, deletes []Tuple, err error) {
	switch e.Type {
	case "lease.tenancy.started":
		w, err := agreementTuples(e)
		return w, nil, err
	case "lease.tenancy.ended", "lease.tenancy.lapsed", "lease.tenancy.settled":
		// The same edges the start wrote, removed. The event carries its own
		// parties for the reason startedEvent gives: the table may have been
		// re-dated by the time this runs.
		d, err := agreementTuples(e)
		return nil, d, err
	default:
		return nil, nil, nil
	}
}

// Party is one relationship-bearing party as an event or a scan reports it.
type Party struct {
	PartyID string `json:"party_id"`
	Role    string `json:"role"`
}

// AgreementEdges is the one mapping from a tenancy to its edges. The event
// projector, the rebuild and the reconciliation all call it, which is what
// makes them agree by construction.
func AgreementEdges(leaseID, unitID string, parties []Party) []Tuple {
	agreement := "agreement:" + leaseID
	var ts []Tuple
	if unitID != "" {
		ts = append(ts, Tuple{User: "unit:" + unitID, Relation: "unit", Object: agreement})
	}
	for _, p := range parties {
		// Owners and their organisations reach the agreement through the org
		// and the mandate; only the resident-side personas get direct edges.
		switch p.Role {
		case "tenant":
			ts = append(ts, Tuple{User: "user:" + p.PartyID, Relation: "tenant", Object: agreement})
		case "guardian":
			ts = append(ts, Tuple{User: "user:" + p.PartyID, Relation: "guardian", Object: agreement})
		}
	}
	return ts
}

func agreementTuples(e events.Envelope) ([]Tuple, error) {
	var data struct {
		UnitID  string  `json:"unit_id"`
		Parties []Party `json:"parties"`
	}
	if err := json.Unmarshal(e.Data, &data); err != nil {
		return nil, fmt.Errorf("authz: decoding %s %s: %w", e.Type, e.ID, err)
	}
	return AgreementEdges(e.Subject.ID, data.UnitID, data.Parties), nil
}

// WriteTuples applies writes then deletes, one at a time — the volumes here
// are a handful of edges per tenancy, and per-tuple calls are what make the
// two idempotency cases distinguishable: a tuple that already exists satisfies
// a write, and a tuple already gone satisfies a delete. Publishing is
// at-least-once, so the projector will be handed the same event twice and must
// land in the same place both times.
func (c *Client) WriteTuples(ctx context.Context, writes, deletes []Tuple) error {
	if c.storeID == "" || c.modelID == "" {
		return fmt.Errorf("authz: write before bootstrap")
	}
	for _, t := range writes {
		if err := c.writeOne(ctx, "writes", t); err != nil && !strings.Contains(err.Error(), "already exists") {
			return fmt.Errorf("writing %s %s %s: %w", t.User, t.Relation, t.Object, err)
		}
	}
	for _, t := range deletes {
		if err := c.writeOne(ctx, "deletes", t); err != nil && !strings.Contains(err.Error(), "does not exist") {
			return fmt.Errorf("deleting %s %s %s: %w", t.User, t.Relation, t.Object, err)
		}
	}
	return nil
}

// ReadTuples lists every stored tuple on one object, whatever the relation —
// the reconciliation read (dwellm8#151): what the graph believes, to be
// compared with what the domain database knows.
func (c *Client) ReadTuples(ctx context.Context, object string) ([]Tuple, error) {
	if c.storeID == "" {
		return nil, fmt.Errorf("authz: read before bootstrap")
	}
	var tuples []Tuple
	token := ""
	for {
		body := map[string]any{
			"tuple_key": map[string]string{"object": object},
			"page_size": 100,
		}
		if token != "" {
			body["continuation_token"] = token
		}
		var out struct {
			Tuples []struct {
				Key Tuple `json:"key"`
			} `json:"tuples"`
			ContinuationToken string `json:"continuation_token"`
		}
		if err := c.do(ctx, "POST", c.BaseURL+"/stores/"+c.storeID+"/read", body, &out); err != nil {
			return nil, fmt.Errorf("reading %s: %w", object, err)
		}
		for _, t := range out.Tuples {
			tuples = append(tuples, t.Key)
		}
		if out.ContinuationToken == "" {
			return tuples, nil
		}
		token = out.ContinuationToken
	}
}

func (c *Client) writeOne(ctx context.Context, op string, t Tuple) error {
	body := map[string]any{
		"authorization_model_id": c.modelID,
		op:                       map[string]any{"tuple_keys": []Tuple{t}},
	}
	var out struct{}
	return c.do(ctx, "POST", c.BaseURL+"/stores/"+c.storeID+"/write", body, &out)
}

// Projector drains relationship-bearing events into the graph.
//
// It is fed by a durable JetStream consumer (ADR-0002 §5): a failed write is
// redelivered, and five failures park the event on the DLQ where it alerts —
// access left more permissive than the domain is the one state that must not
// pass silently. Wiring lives in main; this stays transport-free so the
// mapping can be exercised without a broker.
type Projector struct {
	FGA *Client
	Log *slog.Logger
}

// Handle applies one event and reports whether it may be acknowledged.
func (p *Projector) Handle(ctx context.Context, body []byte) error {
	var env events.Envelope
	if err := json.Unmarshal(body, &env); err != nil {
		return fmt.Errorf("authz projector: undecodable event: %w", err)
	}
	writes, deletes, err := TuplesFor(env)
	if err != nil {
		return err
	}
	if len(writes) == 0 && len(deletes) == 0 {
		return nil
	}
	if err := p.FGA.WriteTuples(ctx, writes, deletes); err != nil {
		return err
	}
	p.Log.Info("authz tuples projected",
		"event", env.Type, "id", env.ID, "writes", len(writes), "deletes", len(deletes))
	return nil
}
