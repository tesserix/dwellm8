package verification

import (
	"testing"
	"time"
)

// A password sign-in proves possession of a password. Before a firm is minted
// under an email address, that address has to be proved to reach the person —
// an owner's payout notifications go there, and so does the password reset.

func TestNewCodeIsSixDigits(t *testing.T) {
	t.Parallel()
	seen := map[string]int{}
	for range 200 {
		c, err := NewCode()
		if err != nil {
			t.Fatal(err)
		}
		if len(c) != 6 {
			t.Fatalf("code %q is not six digits", c)
		}
		for _, r := range c {
			if r < '0' || r > '9' {
				t.Fatalf("code %q is not digits", c)
			}
		}
		seen[c]++
	}
	// Not a randomness test: a generator stuck on one value is the bug this
	// catches, and it would show as one key.
	if len(seen) < 100 {
		t.Fatalf("200 codes produced only %d distinct values", len(seen))
	}
}

func TestHashIsNotReversibleByReadingTheRow(t *testing.T) {
	t.Parallel()
	a := Hash("ops", "uid-1", "123456")
	if string(a) == "123456" {
		t.Fatal("the code is stored in the clear")
	}
	// Bound to the sign-in: a hash lifted from another row must not open this
	// one, which is what an unsalted digest of six digits would allow.
	if string(Hash("ops", "uid-2", "123456")) == string(a) {
		t.Fatal("the hash does not bind the principal")
	}
	if string(Hash("ops", "uid-1", "123456")) != string(a) {
		t.Fatal("the hash is not stable")
	}
}

func TestCheckAcceptsTheRightCodeInTime(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	p := Pending{
		Hash:      Hash("ops", "uid-1", "123456"),
		SentAt:    now,
		ExpiresAt: now.Add(TTL),
	}
	if err := Check(p, "ops", "uid-1", "123456", now.Add(time.Minute)); err != nil {
		t.Fatalf("the right code in time is accepted: %v", err)
	}
}

func TestCheckRefusals(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	base := Pending{Hash: Hash("ops", "uid-1", "123456"), SentAt: now, ExpiresAt: now.Add(TTL)}

	cases := []struct {
		name string
		p    Pending
		code string
		at   time.Time
		want error
	}{
		{"a wrong code", base, "000000", now, ErrWrongCode},
		{"an expired code", base, "123456", now.Add(TTL + time.Second), ErrExpired},
		{"a code already used", func() Pending { p := base; p.ConsumedAt = now; return p }(), "123456", now, ErrAlreadyUsed},
		{"a code guessed at", func() Pending { p := base; p.Attempts = MaxAttempts; return p }(), "123456", now, ErrTooManyAttempts},
		{"somebody else's code", base, "123456", now, ErrWrongCode},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			uid := "uid-1"
			if c.name == "somebody else's code" {
				uid = "uid-2"
			}
			if err := Check(c.p, "ops", uid, c.code, c.at); err != c.want {
				t.Fatalf("got %v, want %v", err, c.want)
			}
		})
	}
}

func TestResendIsRateLimited(t *testing.T) {
	t.Parallel()
	now := time.Now()
	if MayResend(Pending{SentAt: now}, now.Add(time.Second)) {
		t.Fatal("a resend a second later is a way to mail-bomb somebody")
	}
	if !MayResend(Pending{SentAt: now}, now.Add(ResendAfter)) {
		t.Fatal("a resend after the cooldown is allowed")
	}
	// Nothing sent yet is not a cooldown.
	if !MayResend(Pending{}, now) {
		t.Fatal("the first send is always allowed")
	}
}
