package docscan

import (
	"fmt"
	"regexp"
	"strings"
)

// PANCard is what a permanent account number card yields. Number is the whole
// number, held only long enough to be shown back and masked (ADR-0013).
type PANCard struct {
	Number      string
	Name        string
	FatherName  string
	DateOfBirth string

	// The fourth character is the holder's constitution, which decides whether
	// the tenancy is deducted on as an individual's.
	Individual bool
}

var (
	// Whole runs, not any ten characters inside one: an eleven-character run is
	// not a PAN with a stray letter beside it, it is something else.
	panToken   = regexp.MustCompile(`[A-Za-z0-9]+`)
	panExact   = regexp.MustCompile(`^[A-Z]{5}[0-9]{4}[A-Z]$`)
	panDate    = regexp.MustCompile(`\b(\d{2})[/-](\d{2})[/-](\d{4})\b`)
	panFather  = regexp.MustCompile(`(?i)father` + "'" + `?s?\s+name`)
	panNameLbl = regexp.MustCompile(`(?i)^\s*name\s*:?\s*$`)
	panNameish = regexp.MustCompile(`^[A-Z][A-Z .]{2,}$`)
)

// Boilerplate every card carries, so it is never mistaken for the holder.
var panPrinted = []string{
	"INCOME TAX", "DEPARTMENT", "GOVT", "GOVERNMENT", "INDIA",
	"PERMANENT ACCOUNT", "SIGNATURE", "DATE OF BIRTH", "NAME",
}

// ParsePANCard reads a permanent account number card out of OCR text. It
// returns an error rather than a partial guess when no number is found: a blank
// field is corrected by the manager, and a confidently wrong one is not.
func ParsePANCard(text string) (PANCard, error) {
	number, ok := findPAN(text)
	if !ok {
		return PANCard{}, fmt.Errorf("docscan: no permanent account number in this image")
	}

	card := PANCard{Number: number, Individual: number[3] == 'P'}
	card.Name, card.FatherName = panNames(text, number)
	if m := panDate.FindStringSubmatch(text); m != nil {
		card.DateOfBirth = m[3] + "-" + m[2] + "-" + m[1]
	}
	return card, nil
}

// findPAN takes the cleanest candidate rather than the first. A PAN's shape is
// fixed — five letters, four digits, a letter — so a character read in the wrong
// alphabet is recoverable by position, and the number of corrections needed is
// how much the reading should be trusted.
func findPAN(text string) (string, bool) {
	best, bestCost := "", 3
	for _, token := range panToken.FindAllString(text, -1) {
		fixed, cost := coercePAN(strings.ToUpper(token))
		if cost < bestCost {
			best, bestCost = fixed, cost
		}
	}
	return best, best != ""
}

// Confusions an OCR engine actually makes, in each direction. Anything outside
// these pairs is a misreading this package will not invent its way past.
var (
	asLetter = map[byte]byte{'0': 'O', '1': 'I', '2': 'Z', '4': 'A', '5': 'S', '6': 'G', '8': 'B'}
	asDigit  = map[byte]byte{'O': '0', 'Q': '0', 'D': '0', 'I': '1', 'L': '1', 'Z': '2',
		'A': '4', 'S': '5', 'G': '6', 'T': '7', 'B': '8'}
)

func coercePAN(token string) (string, int) {
	if len(token) != 10 {
		return "", 3
	}
	out, cost := []byte(token), 0
	for i := range out {
		wantDigit := i >= 5 && i <= 8
		c := out[i]
		switch {
		case wantDigit && c >= '0' && c <= '9':
		case !wantDigit && c >= 'A' && c <= 'Z':
		default:
			table := asLetter
			if wantDigit {
				table = asDigit
			}
			fixed, ok := table[c]
			if !ok {
				return "", 3
			}
			out[i], cost = fixed, cost+1
		}
	}
	if !panExact.Match(out) {
		return "", 3
	}
	return string(out), cost
}

// The holder's name and the father's, which sit one under the other on every
// card. Labels are followed where the card has them, because the alternative —
// taking the first two name-shaped lines — puts the father's name on the record
// whenever the printing order differs.
func panNames(text, number string) (name, father string) {
	lines := strings.Split(text, "\n")
	for i, raw := range lines {
		line := strings.TrimSpace(raw)
		next := ""
		if i+1 < len(lines) {
			next = strings.TrimSpace(lines[i+1])
		}
		switch {
		case panFather.MatchString(line) && father == "":
			father = next
		case panNameLbl.MatchString(line) && name == "":
			name = next
		}
	}
	if name != "" {
		return name, father
	}

	var found []string
	for _, raw := range lines {
		if line := strings.TrimSpace(raw); nameish(line, number) {
			found = append(found, line)
		}
	}
	switch len(found) {
	case 0:
		return "", father
	case 1:
		return found[0], father
	default:
		if father == "" {
			father = found[1]
		}
		return found[0], father
	}
}

func nameish(line, number string) bool {
	if line == number || !panNameish.MatchString(line) {
		return false
	}
	for _, printed := range panPrinted {
		if strings.Contains(line, printed) {
			return false
		}
	}
	return true
}
