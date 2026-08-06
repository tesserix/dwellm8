// Package verification is the one-time code sent to a manager's email (#282).
//
// The rules are all here rather than in the handler because they are the
// security of the thing: six digits is 20 bits, so what makes it safe is the
// attempt ceiling and the expiry, not the code.
package verification

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"math/big"
	"time"
)

const (
	// Long enough to switch apps and read an email, short enough that a code
	// left in an inbox is not a standing key.
	TTL = 10 * time.Minute
	// Six digits is a million: five guesses is the whole of the security.
	MaxAttempts = 5
	ResendAfter = 60 * time.Second
	// The minute cooldown alone permits sixty codes an hour — sixty emails to
	// somebody's inbox and sixty codes to guess at. This is the real ceiling.
	MaxPerHour  = 5
	IssueWindow = time.Hour
)

var (
	ErrWrongCode       = errors.New("that code is not right")
	ErrExpired         = errors.New("that code has expired")
	ErrAlreadyUsed     = errors.New("that code has already been used")
	ErrTooManyAttempts = errors.New("too many attempts — ask for a new code")
	ErrTooManyCodes    = errors.New("too many codes have been sent — try again later")
)

// Pending is the code in flight, as the row holds it.
type Pending struct {
	Hash       []byte
	Attempts   int
	SentAt     time.Time
	ExpiresAt  time.Time
	ConsumedAt time.Time
}

// NewCode is a six-digit code from the cryptographic source. math/rand here
// would be seeded predictably enough to enumerate.
func NewCode() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return "", fmt.Errorf("drawing a code: %w", err)
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}

// Hash binds the code to the sign-in it was sent for, so a hash read out of one
// row cannot be replayed into another — an unsalted digest of six digits is a
// rainbow table with a million entries.
func Hash(surface, uid, code string) []byte {
	sum := sha256.Sum256([]byte(surface + "\x00" + uid + "\x00" + code))
	return sum[:]
}

// Check is every reason a code may be refused, in the order they matter.
func Check(p Pending, surface, uid, code string, at time.Time) error {
	if !p.ConsumedAt.IsZero() {
		return ErrAlreadyUsed
	}
	if p.Attempts >= MaxAttempts {
		return ErrTooManyAttempts
	}
	if !at.Before(p.ExpiresAt) {
		return ErrExpired
	}
	// Constant time: a comparison that returns early leaks the prefix.
	if subtle.ConstantTimeCompare(p.Hash, Hash(surface, uid, code)) != 1 {
		return ErrWrongCode
	}
	return nil
}

// NextIssue caps how many codes one sign-in may draw in a rolling hour, and
// says how long is owed when the cap binds — a window that rolls off the oldest
// code rather than a fixed hour nobody can see the edge of.
func NextIssue(recent int, oldest, at time.Time) (time.Duration, error) {
	if recent < MaxPerHour {
		return 0, nil
	}
	wait := oldest.Add(IssueWindow).Sub(at)
	if wait <= 0 {
		return 0, nil
	}
	return wait, ErrTooManyCodes
}

// MayResend keeps a resend button from being a way to mail-bomb somebody.
func MayResend(p Pending, at time.Time) bool {
	if p.SentAt.IsZero() {
		return true
	}
	return !at.Before(p.SentAt.Add(ResendAfter))
}
