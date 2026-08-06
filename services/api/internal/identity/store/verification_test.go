package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/tesserix/dwellm8/services/api/internal/identity/domain/verification"
	"github.com/tesserix/dwellm8/services/api/internal/platform/auth"
)

// The code in flight, against PostgreSQL — because what is worth testing is
// that a code is consumed exactly once and that a guess costs an attempt even
// when it is wrong. Both are properties of the write, not of the arithmetic.

func TestIssueAndConsumeACode(t *testing.T) {
	s, _ := principals(t)
	ctx := context.Background()
	p := auth.Principal{UID: uid(), Surface: auth.SurfaceOps, Email: "manager@example.test", SignInProvider: "password"}

	code, err := s.IssueEmailCode(ctx, p, "manager@example.test")
	if err != nil {
		t.Fatalf("issuing: %v", err)
	}
	if len(code) != 6 {
		t.Fatalf("issued %q", code)
	}

	pending, err := s.PendingEmailCode(ctx, p)
	if err != nil {
		t.Fatalf("reading back: %v", err)
	}
	if err := verification.Check(pending, string(p.Surface), p.UID, code, time.Now()); err != nil {
		t.Fatalf("the code we issued does not check out: %v", err)
	}

	if err := s.ConsumeEmailCode(ctx, p, code); err != nil {
		t.Fatalf("consuming: %v", err)
	}
	// Verified is a fact about the principal, not about the code row: the row is
	// history and the flag is what the firm gate reads.
	verified, err := s.EmailVerified(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	if !verified {
		t.Fatal("consuming a code verifies the email")
	}

	// Once. A code that still opens the door after it has been used is a code
	// anybody who saw the email keeps.
	if err := s.ConsumeEmailCode(ctx, p, code); err == nil {
		t.Fatal("a consumed code must not be accepted twice")
	}
}

func TestAWrongGuessCostsAnAttempt(t *testing.T) {
	s, _ := principals(t)
	ctx := context.Background()
	p := auth.Principal{UID: uid(), Surface: auth.SurfaceOps, Email: "guessed@example.test", SignInProvider: "password"}

	code, err := s.IssueEmailCode(ctx, p, "guessed@example.test")
	if err != nil {
		t.Fatal(err)
	}
	wrong := "000000"
	if wrong == code {
		wrong = "111111"
	}

	for range verification.MaxAttempts {
		if err := s.ConsumeEmailCode(ctx, p, wrong); err == nil {
			t.Fatal("a wrong code was accepted")
		}
	}
	// The attempt ceiling is the whole of the security of six digits, so it must
	// hold even for the right code.
	if err := s.ConsumeEmailCode(ctx, p, code); err == nil {
		t.Fatal("the ceiling must close the code, not just the guesses")
	}
}

func TestIssuingAgainReplacesTheCodeInFlight(t *testing.T) {
	s, _ := principals(t)
	ctx := context.Background()
	p := auth.Principal{UID: uid(), Surface: auth.SurfaceOps, Email: "resent@example.test", SignInProvider: "password"}

	first, err := s.IssueEmailCode(ctx, p, "resent@example.test")
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.IssueEmailCode(ctx, p, "resent@example.test")
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Skip("the same code twice is a one-in-a-million draw, not a failure")
	}
	// The person is looking at the newest email. The older code must be dead, or
	// a forwarded email stays a key.
	if err := s.ConsumeEmailCode(ctx, p, first); err == nil {
		t.Fatal("a superseded code must not be accepted")
	}
	if err := s.ConsumeEmailCode(ctx, p, second); err != nil {
		t.Fatalf("the newest code is the one that works: %v", err)
	}
}

func TestCodesIssuedInTheWindowAreCounted(t *testing.T) {
	s, _ := principals(t)
	ctx := context.Background()
	p := auth.Principal{UID: uid(), Surface: auth.SurfaceOps, Email: "counted@example.test", SignInProvider: "password"}

	n, oldest, err := s.EmailCodesIssued(ctx, p, verification.IssueWindow)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 || !oldest.IsZero() {
		t.Fatalf("nothing sent yet reads as %d at %v", n, oldest)
	}

	for range 3 {
		if _, err := s.IssueEmailCode(ctx, p, "counted@example.test"); err != nil {
			t.Fatal(err)
		}
	}
	n, oldest, err = s.EmailCodesIssued(ctx, p, verification.IssueWindow)
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("counted %d codes, want 3", n)
	}
	// The oldest is what the window rolls off, so it has to be the earliest of
	// the three rather than the latest.
	if time.Since(oldest) > verification.IssueWindow || oldest.IsZero() {
		t.Fatalf("oldest is %v", oldest)
	}

	// A window that has already passed counts nothing — the cap must lift.
	n, _, err = s.EmailCodesIssued(ctx, p, time.Nanosecond)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("a passed window counted %d", n)
	}
}
