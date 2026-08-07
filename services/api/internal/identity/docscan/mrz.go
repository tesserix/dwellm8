// Package docscan turns the text an OCR engine returns into fields a form can
// be prefilled with. It reads; it never stores. Callers mask what they keep.
package docscan

import (
	"fmt"
	"strings"
	"time"
)

// Passport is the machine-readable zone of a TD3 travel document, once every
// check digit has agreed. Number is the whole passport number: it exists to be
// shown back to the manager and masked before anything is written down.
type Passport struct {
	Surname     string
	GivenNames  string
	Number      string
	Nationality string
	DateOfBirth string
	Sex         string
	ExpiresOn   string
}

const mrzLineLen = 44

// ParseMRZ finds a TD3 machine-readable zone in a page of OCR output and reads
// it. Every field is checked against its own digit and the line against the
// composite, because a passport number wrong by one character is worse than an
// empty field: the manager would have no reason to doubt it.
func ParseMRZ(text string) (Passport, error) {
	top, bottom, err := findZone(text)
	if err != nil {
		return Passport{}, err
	}

	for _, f := range []struct {
		what       string
		from, to   int
		checkAt    int
		allBlankOK bool
	}{
		{"number", 0, 9, 9, false},
		{"date of birth", 13, 19, 19, false},
		{"expiry", 21, 27, 27, false},
		{"personal number", 28, 42, 42, true},
	} {
		field := bottom[f.from:f.to]
		if f.allBlankOK && strings.Trim(field, "<") == "" {
			continue
		}
		if err := agrees(field, bottom[f.checkAt]); err != nil {
			return Passport{}, fmt.Errorf("docscan: the %s did not read cleanly: %w", f.what, err)
		}
	}
	composite := bottom[0:10] + bottom[13:20] + bottom[21:43]
	if err := agrees(composite, bottom[43]); err != nil {
		return Passport{}, fmt.Errorf("docscan: the zone did not read cleanly as a whole: %w", err)
	}

	surname, given := names(top[5:])
	born, err := onDate(bottom[13:19], birth)
	if err != nil {
		return Passport{}, err
	}
	expires, err := onDate(bottom[21:27], expiry)
	if err != nil {
		return Passport{}, err
	}

	return Passport{
		Surname:     surname,
		GivenNames:  given,
		Number:      strings.TrimRight(bottom[0:9], "<"),
		Nationality: strings.TrimRight(bottom[10:13], "<"),
		DateOfBirth: born,
		Sex:         strings.TrimRight(string(bottom[20]), "<"),
		ExpiresOn:   expires,
	}, nil
}

// findZone looks for two adjacent conforming lines rather than assuming the
// scan arrived alone — the camera catches the rest of the page too.
func findZone(text string) (string, string, error) {
	var candidates []string
	for _, line := range strings.Split(text, "\n") {
		candidates = append(candidates, strings.ToUpper(strings.TrimSpace(line)))
	}
	for i := 0; i+1 < len(candidates); i++ {
		top, bottom := candidates[i], candidates[i+1]
		if len(top) != mrzLineLen || len(bottom) != mrzLineLen {
			continue
		}
		if top[0] != 'P' || !onlyMRZChars(top) || !onlyMRZChars(bottom) {
			continue
		}
		return top, bottom, nil
	}
	return "", "", fmt.Errorf("docscan: no passport machine-readable zone in this image")
}

func onlyMRZChars(s string) bool {
	for _, r := range s {
		switch {
		case r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '<':
		default:
			return false
		}
	}
	return true
}

// The ICAO 9303 modulus-10 check with weights 7, 3, 1.
func agrees(field string, digit byte) error {
	weights := [3]int{7, 3, 1}
	sum := 0
	for i, r := range field {
		v, ok := mrzValue(byte(r))
		if !ok {
			return fmt.Errorf("%q is not a character this zone may contain", string(r))
		}
		sum += v * weights[i%3]
	}
	want := byte('0' + sum%10)
	if digit != want {
		return fmt.Errorf("its check digit says %q and the characters say %q",
			string(digit), string(want))
	}
	return nil
}

func mrzValue(c byte) (int, bool) {
	switch {
	case c == '<':
		return 0, true
	case c >= '0' && c <= '9':
		return int(c - '0'), true
	case c >= 'A' && c <= 'Z':
		return int(c-'A') + 10, true
	}
	return 0, false
}

func names(field string) (surname, given string) {
	parts := strings.SplitN(field, "<<", 2)
	surname = tidy(parts[0])
	if len(parts) == 2 {
		given = tidy(parts[1])
	}
	return surname, given
}

func tidy(s string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(s, "<", " ")), " ")
}

type dateKind int

const (
	birth dateKind = iota
	expiry
)

// A machine-readable zone carries two-digit years, so the century is inferred:
// nobody is born after today, and no passport in a manager's hands expired
// thirty years ago.
func onDate(yymmdd string, kind dateKind) (string, error) {
	d, err := time.Parse("060102", yymmdd)
	if err != nil {
		return "", fmt.Errorf("docscan: %q is not a date: %w", yymmdd, err)
	}
	now := time.Now().UTC()
	switch {
	case kind == birth && d.Year() > now.Year():
		d = d.AddDate(-100, 0, 0)
	case kind == expiry && d.Year() < now.Year()-30:
		d = d.AddDate(100, 0, 0)
	}
	return d.Format("2006-01-02"), nil
}
