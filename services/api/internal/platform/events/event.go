// Package events is the domain event backbone. ADR-0002.
//
// The envelope, the outbox that makes publishing survive a crash, and the relay
// that drains it. Nothing here imports a broker SDK: the relay talks to a
// Transport, and the JetStream implementation of it lives in events/natsx, for
// the same reason ADR-0015 confines the Temporal SDK to one adapter.
package events

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// ActorKind is who caused a fact. A fact with no author cannot be explained
// during a support call, so the field is required and closed.
type ActorKind string

const (
	ActorUser     ActorKind = "user"
	ActorSystem   ActorKind = "system"
	ActorProvider ActorKind = "provider"
	ActorSupport  ActorKind = "support"
)

var actorKinds = map[ActorKind]bool{
	ActorUser: true, ActorSystem: true, ActorProvider: true, ActorSupport: true,
}

// Actor is who caused the fact. ID is empty for system and provider actors.
type Actor struct {
	Kind ActorKind `json:"kind"`
	ID   string    `json:"id,omitempty"`
}

// Subject is what the fact is about.
type Subject struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

// Envelope is what every consumer may rely on. ADR-0002 §1.
type Envelope struct {
	ID            string          `json:"id"`
	Type          string          `json:"type"`
	Version       int             `json:"version"`
	TenantID      string          `json:"tenant_id"`
	OccurredAt    time.Time       `json:"occurred_at"`
	Actor         Actor           `json:"actor"`
	Subject       Subject         `json:"subject"`
	CorrelationID string          `json:"correlation_id"`
	CausationID   string          `json:"causation_id,omitempty"`
	Data          json.RawMessage `json:"data"`
}

// typePattern is ADR-0002 §2: module.aggregate.past-tense-verb, lower case.
var typePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*\.[a-z][a-z0-9_]*\.[a-z][a-z0-9_]*$`)

// commandVerbs are the present-tense openings that turn a fact into an
// instruction. An event named for a command is one a producer expects somebody
// to obey, which is a call disguised as a publication.
var commandVerbs = map[string]bool{
	"create": true, "update": true, "delete": true, "send": true, "issue": true,
	"post": true, "pay": true, "collect": true, "cancel": true, "process": true,
	"handle": true, "make": true, "do": true, "run": true, "start": true,
}

var (
	// ErrInvalidType reports a type that is not module.aggregate.past-tense-verb.
	ErrInvalidType = errors.New("events: invalid type")
	// ErrCommandNamed reports an event named as an instruction rather than a fact.
	ErrCommandNamed = errors.New("events: type names a command, not a fact")
)

// Validate reports whether the envelope may be written to the outbox.
//
// It is deliberately strict about the things a consumer cannot recover from: an
// absent tenant, an unsortable id, a type that will not map to a subject.
func (e Envelope) Validate() error {
	switch {
	case e.ID == "":
		return errors.New("events: id is required")
	case e.TenantID == "":
		// ADR-0002 §1: platform facts carry the platform organisation rather
		// than no organisation, so nothing downstream has to special-case them.
		return errors.New("events: tenant_id is required, platform facts carry the platform organisation")
	case e.Version < 1:
		return errors.New("events: version starts at 1")
	case e.OccurredAt.IsZero():
		return errors.New("events: occurred_at is required")
	case e.Subject.Kind == "" || e.Subject.ID == "":
		return errors.New("events: subject kind and id are required")
	case e.CorrelationID == "":
		return errors.New("events: correlation_id is required")
	case !actorKinds[e.Actor.Kind]:
		return fmt.Errorf("events: actor kind %q is not one of user, system, provider, support", e.Actor.Kind)
	case e.Actor.Kind == ActorUser && e.Actor.ID == "":
		return errors.New("events: a user actor has an id")
	case len(e.Data) == 0:
		return errors.New("events: data is required, use {} for a fact with no payload")
	}

	if !typePattern.MatchString(e.Type) {
		return fmt.Errorf("%w: %q is not module.aggregate.past_tense_verb", ErrInvalidType, e.Type)
	}
	if verb := e.Type[strings.LastIndex(e.Type, ".")+1:]; commandVerbs[verb] {
		return fmt.Errorf("%w: %q — name the fact that happened, not the instruction", ErrCommandNamed, e.Type)
	}
	if !json.Valid(e.Data) {
		return errors.New("events: data is not valid JSON")
	}
	return nil
}

// SubjectName is the NATS subject for the event. ADR-0002 §2: the type with the
// tenant appended, so a consumer can filter to one organisation without
// deserialising the payload.
func (e Envelope) SubjectName() string {
	base := "dwellm8." + e.Type + "." + e.TenantID
	if e.Version > 1 {
		return base + fmt.Sprintf(".v%d", e.Version)
	}
	return base
}

// Module is the first segment of the type, which selects the stream.
func (e Envelope) Module() string {
	if i := strings.Index(e.Type, "."); i > 0 {
		return e.Type[:i]
	}
	return ""
}
