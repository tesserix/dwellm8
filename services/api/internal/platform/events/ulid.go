package events

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"sync"
	"time"
)

// Crockford base32, the ULID alphabet: no I, L, O or U, so a transcribed id
// cannot become a different valid one.
const encoding = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

var (
	mu       sync.Mutex
	lastMs   uint64
	lastRand [10]byte
)

// NewULID returns a lexicographically sortable id for the given instant.
//
// Ids minted in the same millisecond increment the random component rather than
// redrawing it, so two events written in one transaction sort in the order they
// were appended — which is the order a replay must see them in.
func NewULID(at time.Time) string {
	ms := uint64(at.UnixMilli())

	mu.Lock()
	defer mu.Unlock()

	if ms == lastMs {
		if err := increment(&lastRand); err != nil {
			// Exhausting 80 bits inside one millisecond is not reachable in
			// practice; borrowing the next millisecond keeps ids sorted anyway.
			lastMs++
			ms = lastMs
			mustRandom(&lastRand)
		}
	} else {
		lastMs = ms
		mustRandom(&lastRand)
	}

	return encode(ms, lastRand)
}

var errOverflow = errors.New("events: ulid random component exhausted")

func increment(b *[10]byte) error {
	for i := len(b) - 1; i >= 0; i-- {
		b[i]++
		if b[i] != 0 {
			return nil
		}
	}
	return errOverflow
}

func mustRandom(b *[10]byte) {
	if _, err := rand.Read(b[:]); err != nil {
		panic("events: crypto/rand unavailable: " + err.Error())
	}
}

// encode writes the 48-bit timestamp and 80-bit payload as 26 base32 characters.
func encode(ms uint64, entropy [10]byte) string {
	var raw [16]byte
	binary.BigEndian.PutUint64(raw[0:8], ms<<16)
	copy(raw[6:], entropy[:])

	out := make([]byte, 26)
	// 26 characters of 5 bits covers 130 bits; the first character holds the
	// top 3 bits of the 128, so the whole value is read from a 130-bit window.
	var bits uint
	var acc uint32
	pos := 25
	for i := len(raw) - 1; i >= 0; i-- {
		acc |= uint32(raw[i]) << bits
		bits += 8
		for bits >= 5 {
			out[pos] = encoding[acc&0x1f]
			pos--
			acc >>= 5
			bits -= 5
		}
	}
	for pos >= 0 {
		out[pos] = encoding[acc&0x1f]
		pos--
		acc >>= 5
	}
	return string(out)
}
