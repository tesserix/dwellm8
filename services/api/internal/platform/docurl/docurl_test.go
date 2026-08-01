package docurl

import (
	"strings"
	"testing"
	"time"
)

func signer(t *testing.T) *Signer {
	t.Helper()
	s, err := NewSigner(strings.Repeat("k", 32))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestARoundTripInsideTheWindow(t *testing.T) {
	s := signer(t)
	now := time.Now()
	tok, err := s.Token(Grant{Org: "org-1", DocumentRef: "d-1", TxnID: "t-1", ExpiresAt: now.Add(10 * time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	g, err := s.Parse(tok, now)
	if err != nil || g.DocumentRef != "d-1" || g.TxnID != "t-1" {
		t.Fatalf("got %+v, %v", g, err)
	}
}

// The failure scenario dwellm8#212 names: expired, tampered and never-issued
// all answer with the same refusal — a distinguishable error is an oracle.
func TestEveryRefusalIsTheSameRefusal(t *testing.T) {
	s := signer(t)
	now := time.Now()
	tok, _ := s.Token(Grant{Org: "org-1", DocumentRef: "d-1", TxnID: "t-1", ExpiresAt: now.Add(time.Minute)})

	other, _ := NewSigner(strings.Repeat("x", 32))
	forged, _ := other.Token(Grant{Org: "org-1", DocumentRef: "d-1", TxnID: "t-1", ExpiresAt: now.Add(time.Minute)})

	cases := map[string]string{
		"expired":      tok,
		"tampered":     strings.Replace(tok, ".", "A.", 1),
		"another key":  forged,
		"never issued": "bm9wZQ.bm9wZQ",
		"not a token":  "junk",
		"empty":        "",
	}
	for name, bad := range cases {
		at := now
		if name == "expired" {
			at = now.Add(2 * time.Minute)
		}
		if _, err := s.Parse(bad, at); err != ErrURL {
			t.Errorf("%s: got %v, want the one refusal", name, err)
		}
	}
}

// Expiry is exact at the boundary: a token is dead at its expiry second, not a
// second later, because maxWaitPeriod is the ESP's contract, not a suggestion.
func TestTheWindowClosesAtItsEdge(t *testing.T) {
	s := signer(t)
	exp := time.Now().Add(time.Minute).Truncate(time.Second)
	tok, _ := s.Token(Grant{Org: "org-1", DocumentRef: "d", TxnID: "t", ExpiresAt: exp})
	if _, err := s.Parse(tok, exp.Add(-time.Second)); err != nil {
		t.Fatalf("one second before expiry must serve: %v", err)
	}
	if _, err := s.Parse(tok, exp); err != ErrURL {
		t.Fatal("at expiry the link is dead")
	}
}

// Two grants for the same document differ: the URL is single-purpose, so it
// cannot be derived from the ids it names.
func TestATokenIsNotDerivableFromItsIDs(t *testing.T) {
	s := signer(t)
	a, _ := s.Token(Grant{Org: "org-1", DocumentRef: "d", TxnID: "t-a", ExpiresAt: time.Now().Add(time.Minute)})
	b, _ := s.Token(Grant{Org: "org-1", DocumentRef: "d", TxnID: "t-b", ExpiresAt: time.Now().Add(time.Minute)})
	if a == b {
		t.Fatal("two transactions produced one URL")
	}
	if !strings.Contains(a, ".") || len(strings.Split(a, ".")[1]) < 40 {
		t.Fatalf("the signature is not doing the work: %q", a)
	}
}

func TestAShortKeyIsRefusedAtConstruction(t *testing.T) {
	if _, err := NewSigner("short"); err == nil {
		t.Fatal("a short key must be refused before it signs anything")
	}
}
