package domain

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// Compliance guardrails at publication (#143). RERA and Article 15(2) bind
// what an advertisement may claim; the check runs before a listing goes live
// and blocks with the rule cited and the wording that must change — cheaper
// than a takedown after the screenshot.

// ErrProhibitedClaim reports advertisement wording the platform may not carry.
var ErrProhibitedClaim = errors.New("listing: prohibited claim")

// Violation is one prohibited claim found in the copy.
type Violation struct {
	Rule     string // stable identifier, e.g. discriminatory-preference
	Citation string // the rule that binds, for the refusal message
	Found    string // the wording matched
	Fix      string // what must change
}

// Err renders the violation as the publication refusal.
func (v Violation) Err() error {
	return fmt.Errorf("%w: %q violates %s (%s) — %s",
		ErrProhibitedClaim, v.Found, v.Rule, v.Citation, v.Fix)
}

// communities are the identity terms a tenant preference may not turn on.
const communities = `hindus?|muslims?|christians?|sikhs?|jains?|buddhists?|dalits?|brahmins?|sc|st|obc`

var claimRules = []struct {
	rule     string
	pattern  *regexp.Regexp
	citation string
	fix      string
}{
	{
		rule:     "discriminatory-preference",
		pattern:  regexp.MustCompile(`(?i)\b(?:no|only)\s+(?:` + communities + `)\b|\b(?:` + communities + `)\s+only\b`),
		citation: "Constitution of India Art. 15(2); Model Tenancy Act 2021",
		fix:      "remove the community-based tenant preference",
	},
	{
		rule:     "assured-returns",
		pattern:  regexp.MustCompile(`(?i)\b(?:guaranteed|assured)\s+(?:returns?|rental\s+income|yield)\b`),
		citation: "RERA 2016 advertising norms",
		fix:      "remove the return guarantee — an advertisement may not promise income",
	},
	{
		rule:     "unverified-rera-claim",
		pattern:  regexp.MustCompile(`(?i)\brera[- ]?(?:approved|registered|certified)\b`),
		citation: "RERA 2016 ss. 8, 11 — registration is a number, not an adjective",
		fix:      "state the RERA registration number itself or drop the claim",
	},
}

// reraNumber recognises a registration number in the copy, which turns the
// RERA claim from an adjective into a verifiable fact.
var reraNumber = regexp.MustCompile(`\b[A-Z]{1,6}[A-Z0-9/-]*\d{3,}[A-Z0-9/-]*\b`)

// CheckClaims scans advertisement copy for prohibited claims. Empty means
// publishable; each violation carries the rule, the citation and the fix.
func CheckClaims(text string) []Violation {
	var out []Violation
	for _, r := range claimRules {
		m := r.pattern.FindString(text)
		if m == "" {
			continue
		}
		if r.rule == "unverified-rera-claim" && reraNumber.MatchString(text) {
			continue // the number is stated: the claim is checkable, not puffery
		}
		out = append(out, Violation{
			Rule: r.rule, Citation: r.citation,
			Found: strings.TrimSpace(m), Fix: r.fix,
		})
	}
	return out
}
