package events_test

import (
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/tesserix/dwellm8/services/api/internal/platform/events"
)

func valid() events.Envelope {
	return events.Envelope{
		ID:            "01J8XQZ0000000000000000000",
		Type:          "money.payment.received",
		Version:       1,
		TenantID:      "00000000-0000-0000-0000-0000000000d8",
		OccurredAt:    time.Date(2026, 8, 4, 9, 14, 22, 0, time.UTC),
		Actor:         events.Actor{Kind: events.ActorProvider},
		Subject:       events.Subject{Kind: "payment", ID: "p-1"},
		CorrelationID: "01J8XQZ0000000000000000000",
		Data:          json.RawMessage(`{"amount_minor":5866667,"currency":"INR"}`),
	}
}

func TestValidateAcceptsAWellFormedEvent(t *testing.T) {
	if err := valid().Validate(); err != nil {
		t.Fatalf("rejected a valid envelope: %v", err)
	}
}

// ADR-0002 §1: no exception for platform facts. They carry the platform
// organisation, so nothing downstream has to handle an absent tenant.
func TestValidateRequiresATenant(t *testing.T) {
	e := valid()
	e.TenantID = ""
	if err := e.Validate(); err == nil {
		t.Fatal("accepted an event with no tenant")
	}
}

// ADR-0002 §2: an event is a fact. A type named for a command is a call
// wearing a publication's clothes, and the reviewer who would have caught it
// is not always there.
func TestValidateRejectsCommandNames(t *testing.T) {
	for _, typ := range []string{
		"money.payment.collect",
		"notify.receipt.send",
		"lease.agreement.create",
	} {
		e := valid()
		e.Type = typ
		err := e.Validate()
		if !errors.Is(err, events.ErrCommandNamed) {
			t.Errorf("%s: want ErrCommandNamed, got %v", typ, err)
		}
	}
}

func TestValidateRejectsMalformedTypes(t *testing.T) {
	for _, typ := range []string{
		"payment.received",           // no module
		"Money.Payment.Received",     // upper case
		"money.payment",              // two segments
		"money.payment.received.now", // four
		"money..received",            // empty aggregate
		"",
	} {
		e := valid()
		e.Type = typ
		if err := e.Validate(); !errors.Is(err, events.ErrInvalidType) {
			t.Errorf("%q: want ErrInvalidType, got %v", typ, err)
		}
	}
}

func TestValidateRejectsAUserActorWithNoID(t *testing.T) {
	e := valid()
	e.Actor = events.Actor{Kind: events.ActorUser}
	if err := e.Validate(); err == nil {
		t.Fatal("accepted a user actor with no id")
	}
}

// The subject carries the tenant so a consumer can filter to one organisation
// without deserialising the payload. ADR-0002 §2.
func TestSubjectNameAppendsTheTenant(t *testing.T) {
	e := valid()
	want := "dwellm8.money.payment.received.00000000-0000-0000-0000-0000000000d8"
	if got := e.SubjectName(); got != want {
		t.Fatalf("subject = %q, want %q", got, want)
	}
}

// ADR-0002 §8: a breaking change is a new version published alongside the old,
// which means it needs a subject the old consumer does not match.
func TestSubjectNameSeparatesVersions(t *testing.T) {
	e := valid()
	e.Version = 2
	if got := e.SubjectName(); !strings.HasSuffix(got, ".v2") {
		t.Fatalf("v2 subject = %q, want a .v2 suffix", got)
	}
	if e.SubjectName() == valid().SubjectName() {
		t.Fatal("v2 publishes on the same subject as v1")
	}
}

func TestCausedCarriesTheChain(t *testing.T) {
	parent := valid()
	parent.ID = "01J8XQZ0000000000000000001"
	parent.CorrelationID = "01J8XQZ0000000000000000000"

	child := valid().Caused(parent)
	if child.CorrelationID != parent.CorrelationID {
		t.Errorf("correlation = %q, want the parent's %q", child.CorrelationID, parent.CorrelationID)
	}
	if child.CausationID != parent.ID {
		t.Errorf("causation = %q, want the parent's id %q", child.CausationID, parent.ID)
	}
}

// The id is the deduplication key, so ids minted inside one millisecond must
// still be distinct — and must sort in the order they were minted, because a
// replay has to see two events from one transaction the right way round.
func TestULIDsAreUniqueAndSortedWithinAMillisecond(t *testing.T) {
	at := time.Date(2026, 8, 4, 9, 14, 22, 0, time.UTC)

	const n = 1000
	ids := make([]string, n)
	seen := make(map[string]bool, n)
	for i := range ids {
		ids[i] = events.NewULID(at)
		if len(ids[i]) != 26 {
			t.Fatalf("ulid %q is %d characters, want 26", ids[i], len(ids[i]))
		}
		if seen[ids[i]] {
			t.Fatalf("duplicate ulid %q at %d", ids[i], i)
		}
		seen[ids[i]] = true
	}

	if !sort.StringsAreSorted(ids) {
		t.Fatal("ulids minted in one millisecond do not sort in mint order")
	}
}

func TestULIDsSortByTime(t *testing.T) {
	base := time.Date(2026, 8, 4, 9, 14, 22, 0, time.UTC)
	earlier := events.NewULID(base)
	later := events.NewULID(base.Add(time.Second))
	if earlier >= later {
		t.Fatalf("ulid %q (earlier) does not sort before %q", earlier, later)
	}
}

func TestULIDUsesTheCrockfordAlphabet(t *testing.T) {
	id := events.NewULID(time.Now())
	for _, c := range id {
		if strings.ContainsRune("ILOU", c) {
			t.Fatalf("ulid %q contains %q, which Crockford base32 excludes", id, c)
		}
		if !strings.ContainsRune("0123456789ABCDEFGHJKMNPQRSTVWXYZ", c) {
			t.Fatalf("ulid %q contains %q, which is not in the alphabet", id, c)
		}
	}
}

func TestNewMarshalsThePayload(t *testing.T) {
	type receipt struct {
		AmountMinor int64  `json:"amount_minor"`
		Currency    string `json:"currency"`
	}
	e, err := events.New("money.payment.received", "t-1",
		events.Subject{Kind: "payment", ID: "p-1"},
		events.Actor{Kind: events.ActorSystem},
		receipt{AmountMinor: 5866667, Currency: "INR"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := e.Validate(); err != nil {
		t.Fatalf("New produced an invalid envelope: %v", err)
	}
	// No floats in an event, ever. ADR-0002 §1.
	if !strings.Contains(string(e.Data), `"amount_minor":5866667`) {
		t.Fatalf("payload = %s, want an integer minor amount", e.Data)
	}
}
