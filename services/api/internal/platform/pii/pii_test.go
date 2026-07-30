package pii_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"testing"

	"github.com/tesserix/dwellm8/services/api/internal/platform/pii"
)

// ADR-0013. The tests are mostly about leaks, because a leak is what this package
// exists to prevent and every leak has the same shape: somebody formatted a struct.
//
// A fabricated Aadhaar-shaped number is used throughout. It is shape-valid and belongs
// to nobody — the twelve digits are sequential — because a test fixture that is a real
// identifier is the bug this file is about.
const (
	fakeAadhaar = "234567890123"
	fakePAN     = "ABCDE1234F"
	fakeAccount = "000123456789"
)

// Every way a value gets out of a struct by accident. All of them must be redacted.
func TestASecretDoesNotLeakThroughAnyFormattingPath(t *testing.T) {
	s := pii.NewSecret(fakeAadhaar)

	leaks := map[string]string{
		"%v":              fmt.Sprintf("%v", s),
		"%s":              fmt.Sprintf("%s", s),
		"%q":              fmt.Sprintf("%q", s),
		"%d":              fmt.Sprintf("%d", s),
		"%x":              fmt.Sprintf("%x", s),
		"%#v":             fmt.Sprintf("%#v", s),
		"%+v":             fmt.Sprintf("%+v", s),
		"Sprint":          fmt.Sprint(s),
		"Errorf wrapping": fmt.Errorf("verifying: %v", s).Error(),
		"String()":        s.String(),
	}
	for how, got := range leaks {
		if strings.Contains(got, fakeAadhaar) {
			t.Errorf("%s leaked the identifier: %q", how, got)
		}
		if !strings.Contains(got, pii.Redaction) {
			t.Errorf("%s produced %q, which does not say it was redacted — an empty value in a log "+
				"is indistinguishable from a field nobody set", how, got)
		}
	}

	// json.Marshal, directly and nested in a struct — the debug-dump path.
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshalling: %v", err)
	}
	if strings.Contains(string(b), fakeAadhaar) {
		t.Errorf("json.Marshal leaked: %s", b)
	}
	nested, err := json.Marshal(struct {
		Subject string     `json:"subject"`
		Number  pii.Secret `json:"number"`
	}{Subject: "tenant-1", Number: s})
	if err != nil {
		t.Fatalf("marshalling a struct: %v", err)
	}
	if strings.Contains(string(nested), fakeAadhaar) {
		t.Errorf("a struct containing a Secret leaked: %s", nested)
	}
	t.Logf("as JSON: %s", nested)

	// And a structured log, which is the path that actually ships.
	var buf bytes.Buffer
	slog.New(slog.NewJSONHandler(&buf, nil)).Info("verified", "number", s, "kind", "aadhaar")
	if strings.Contains(buf.String(), fakeAadhaar) {
		t.Errorf("slog leaked: %s", buf.String())
	}
	t.Logf("as a log line: %s", strings.TrimSpace(buf.String()))

	// Reveal is the one way out, and it works — the point is that it is visible, not
	// that it is impossible.
	if s.Reveal() != fakeAadhaar {
		t.Error("Reveal did not return the value, so the type is unusable")
	}
}

// A Secret cannot be built by a struct literal, so a zero value is empty rather than
// half-initialised, and reflection-based marshallers cannot reach the field.
func TestASecretHasNoReachableField(t *testing.T) {
	var zero pii.Secret
	if !zero.Empty() {
		t.Error("the zero Secret is not empty")
	}
	if zero.String() != pii.Redaction {
		t.Error("the zero Secret does not redact")
	}
	// The whole struct rendered with %#v must show nothing, which is what a debug dump
	// of a request would do.
	if got := fmt.Sprintf("%#v", struct{ N pii.Secret }{pii.NewSecret(fakePAN)}); strings.Contains(got, fakePAN) {
		t.Errorf("%%#v of a wrapping struct leaked: %s", got)
	}
}

// The three tiers, and the reason each kind is in the one it is in. A classification
// list with no reasons is a list somebody reclassifies.
func TestEveryKindIsClassifiedWithAReason(t *testing.T) {
	kinds := pii.Kinds()
	if len(kinds) == 0 {
		t.Fatal("no kinds are classified")
	}
	for _, k := range kinds {
		tier, ok := pii.TierOf(k)
		if !ok {
			t.Errorf("%s has no tier", k)
			continue
		}
		switch tier {
		case pii.TierProhibited, pii.TierEncrypted, pii.TierOpen:
		default:
			t.Errorf("%s has tier %q", k, tier)
		}
		if len(pii.Why(k)) < 40 {
			t.Errorf("%s is %s because %q, which is a label rather than a reason", k, tier, pii.Why(k))
		}
		t.Logf("%-16s %-11s %s", k, tier, pii.Why(k))
	}

	// The one that must never move without an ADR.
	if !pii.Prohibited(pii.KindAadhaar) {
		t.Error("the Aadhaar number is not prohibited at rest, which is the whole of this ADR")
	}
	// And the ones that genuinely cannot be masked away, because a return or a payout
	// needs the real value.
	for _, k := range []pii.Kind{pii.KindPAN, pii.KindBankAccount} {
		if tier, _ := pii.TierOf(k); tier != pii.TierEncrypted {
			t.Errorf("%s is %s — a TDS return and a payout need the real value", k, tier)
		}
	}
	// And the ones that are not personal identifiers at all.
	for _, k := range []pii.Kind{pii.KindIFSC, pii.KindGSTIN} {
		if tier, _ := pii.TierOf(k); tier != pii.TierOpen {
			t.Errorf("%s is %s and is a public register", k, tier)
		}
	}
}

// The primary acceptance criterion: a completed verification holds only result, masked
// reference, provider transaction id, timestamp and consent artefact — and there is
// nowhere for a full identifier to go.
func TestAVerificationRecordHoldsNoFullIdentifier(t *testing.T) {
	masked, err := pii.Mask(pii.KindAadhaar, pii.NewSecret(fakeAadhaar))
	if err != nil {
		t.Fatalf("Mask: %v", err)
	}
	if strings.Contains(masked, fakeAadhaar[:8]) {
		t.Errorf("the mask %q still contains the leading digits", masked)
	}
	if !strings.HasSuffix(masked, fakeAadhaar[len(fakeAadhaar)-4:]) {
		t.Errorf("the mask %q does not keep the last four, so a subject cannot recognise their "+
			"own document", masked)
	}
	if len(masked) != len(fakeAadhaar) {
		t.Errorf("the mask is %d characters and the identifier is %d — a different length is a "+
			"different fact", len(masked), len(fakeAadhaar))
	}
	t.Logf("masked: %s", masked)

	v := pii.Verification{
		Kind: pii.KindAadhaar, MaskedReference: masked, Result: pii.ResultVerified,
		Provider: "digilocker", ProviderTxnID: "txn-7781", ConsentArtefact: "consent-114",
	}
	if err := v.Validate(); err != nil {
		t.Fatalf("a complete verification was refused: %v", err)
	}

	// The record, rendered every way something might render it, contains no identifier.
	b, _ := json.Marshal(v)
	for _, rendered := range []string{fmt.Sprintf("%+v", v), fmt.Sprintf("%#v", v), string(b)} {
		if strings.Contains(rendered, fakeAadhaar) {
			t.Errorf("the verification record leaked the identifier: %s", rendered)
		}
	}
	t.Logf("the whole record: %s", b)
}

// The mechanism, rather than the intention. Putting a full identifier in the masked
// reference is refused, so the column cannot hold one whatever the caller believed.
func TestAFullIdentifierCannotBeStoredAsAReference(t *testing.T) {
	for _, tc := range []struct {
		name string
		v    pii.Verification
	}{
		{
			name: "the raw number in the reference field",
			v: pii.Verification{Kind: pii.KindAadhaar, MaskedReference: fakeAadhaar,
				Result: pii.ResultVerified, Provider: "p", ProviderTxnID: "t", ConsentArtefact: "c"},
		},
		{
			name: "a mask that keeps too much",
			v: pii.Verification{Kind: pii.KindAadhaar, MaskedReference: "XX" + fakeAadhaar[2:],
				Result: pii.ResultVerified, Provider: "p", ProviderTxnID: "t", ConsentArtefact: "c"},
		},
		{
			name: "a PAN in an Aadhaar record's reference",
			v: pii.Verification{Kind: pii.KindAadhaar, MaskedReference: fakePAN,
				Result: pii.ResultVerified, Provider: "p", ProviderTxnID: "t", ConsentArtefact: "c"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.v.Validate()
			if err == nil {
				t.Fatalf("accepted: %s", tc.name)
			}
			if !errors.Is(err, pii.ErrProhibited) {
				t.Errorf("refused, but not as a prohibited store: %v", err)
			}
			// And the error itself must not quote the value it refused.
			if strings.Contains(err.Error(), fakeAadhaar) {
				t.Errorf("the error leaked the identifier it was refusing: %v", err)
			}
		})
	}
}

// A validation error must never quote the value, because an error is a log line. This is
// the mistake every validation function makes.
func TestAValidationErrorNeverQuotesTheValue(t *testing.T) {
	for _, tc := range []struct {
		kind pii.Kind
		raw  string
	}{
		{pii.KindAadhaar, "123456789012"}, // starts with 1, which UIDAI does not issue
		{pii.KindPAN, "ABCDE1234"},
		{pii.KindBankAccount, "12345"},
		{pii.KindIFSC, "HDFC1234567"},
	} {
		err := pii.Validate(tc.kind, pii.NewSecret(tc.raw))
		if err == nil {
			t.Errorf("%s accepted %q", tc.kind, tc.raw)
			continue
		}
		if strings.Contains(err.Error(), tc.raw) {
			t.Errorf("the %s error quoted the value: %v", tc.kind, err)
		}
		if !errors.Is(err, pii.ErrShape) {
			t.Errorf("not reported as a shape problem: %v", err)
		}
	}

	// And the valid shapes are accepted, or the whole thing is unusable.
	for kind, raw := range map[pii.Kind]string{
		pii.KindAadhaar:     fakeAadhaar,
		pii.KindPAN:         fakePAN,
		pii.KindBankAccount: fakeAccount,
		pii.KindIFSC:        "HDFC0001234",
		pii.KindUPIVPA:      "somebody@okhdfcbank",
		pii.KindGSTIN:       "27ABCDE1234F1Z5",
	} {
		if err := pii.Validate(kind, pii.NewSecret(raw)); err != nil {
			t.Errorf("%s rejected a valid value: %v", kind, err)
		}
	}
}

// Every kind's mask must satisfy its own pattern, because the schema's CHECK is built
// from that pattern and a mask the column refuses is a verification that fails at the
// counter.
func TestEveryMaskSatisfiesItsOwnPattern(t *testing.T) {
	samples := map[pii.Kind]string{
		pii.KindAadhaar:        fakeAadhaar,
		pii.KindPAN:            fakePAN,
		pii.KindBankAccount:    fakeAccount,
		pii.KindIFSC:           "HDFC0001234",
		pii.KindUPIVPA:         "somebody@okhdfcbank",
		pii.KindPassport:       "A1234567",
		pii.KindDrivingLicence: "KA0120110012345",
		pii.KindVoterID:        "ABC1234567",
		pii.KindGSTIN:          "27ABCDE1234F1Z5",
	}
	for _, k := range pii.Kinds() {
		raw, ok := samples[k]
		if !ok {
			t.Fatalf("%s has no sample, so its mask is untested", k)
		}
		masked, err := pii.Mask(k, pii.NewSecret(raw))
		if err != nil {
			t.Errorf("masking %s: %v", k, err)
			continue
		}
		pattern, err := pii.MaskPattern(k)
		if err != nil {
			t.Errorf("pattern for %s: %v", k, err)
			continue
		}
		re, err := regexp.Compile(pattern)
		if err != nil {
			t.Errorf("the %s pattern does not compile: %v", k, err)
			continue
		}
		if !re.MatchString(masked) {
			t.Errorf("%s masks to %q, which its own pattern %q refuses", k, masked, pattern)
		}
		// A mask must not be the value, for anything that is not an open register.
		if tier, _ := pii.TierOf(k); tier != pii.TierOpen && masked == strings.ToUpper(raw) {
			t.Errorf("%s masks to itself", k)
		}
		t.Logf("%-16s %-20s %s", k, masked, pattern)
	}
}

// Masking is not a hash, and that is a decision rather than an omission: the Aadhaar
// space is small enough to enumerate, so a hash column is a lookup table for anybody who
// takes a copy.
func TestMaskingIsIrreversibleAndCollides(t *testing.T) {
	a, err := pii.Mask(pii.KindAadhaar, pii.NewSecret("234567890123"))
	if err != nil {
		t.Fatalf("Mask: %v", err)
	}
	b, err := pii.Mask(pii.KindAadhaar, pii.NewSecret("999999990123"))
	if err != nil {
		t.Fatalf("Mask: %v", err)
	}
	// Two different numbers ending the same way mask identically. That is the point: a
	// mask is not an identifier, so it cannot be joined on to re-identify anybody.
	if a != b {
		t.Errorf("two Aadhaar numbers sharing their last four masked differently (%q, %q), so the "+
			"mask carries information from the part that was supposed to be removed", a, b)
	}
}

// The prohibited-name list has one home, read by the schema assertion and the arch test.
func TestTheProhibitedNameListIsNotEmpty(t *testing.T) {
	names := pii.ProhibitedColumnNames()
	if len(names) < 4 {
		t.Fatalf("%d prohibited names, which is too few to catch a spelling", len(names))
	}
	seen := map[string]bool{}
	for _, n := range names {
		if n != strings.ToLower(n) {
			t.Errorf("%q is not lower case, and the checks that read this list compare lower case", n)
		}
		if seen[n] {
			t.Errorf("%q appears twice", n)
		}
		seen[n] = true
	}
	// The obvious spellings, because the careless case is the common one.
	for _, must := range []string{"aadhaar", "aadhar"} {
		if !seen[must] {
			t.Errorf("%q is not on the prohibited list", must)
		}
	}
}

// A verification is incomplete without the things that make it checkable later.
func TestAVerificationMustBeCheckableLater(t *testing.T) {
	masked, _ := pii.Mask(pii.KindPAN, pii.NewSecret(fakePAN))
	good := pii.Verification{
		Kind: pii.KindPAN, MaskedReference: masked, Result: pii.ResultVerified,
		Provider: "nsdl", ProviderTxnID: "txn-1", ConsentArtefact: "consent-1",
	}
	if err := good.Validate(); err != nil {
		t.Fatalf("a complete record was refused: %v", err)
	}

	for _, tc := range []struct {
		name string
		mut  func(*pii.Verification)
		want string
	}{
		{"no provider", func(v *pii.Verification) { v.Provider = "" }, "provider"},
		{"no transaction id", func(v *pii.Verification) { v.ProviderTxnID = "" }, "transaction"},
		{"no consent artefact", func(v *pii.Verification) { v.ConsentArtefact = "" }, "consent"},
		{"no masked reference", func(v *pii.Verification) { v.MaskedReference = "" }, "masked reference"},
		{"an invented result", func(v *pii.Verification) { v.Result = "probably" }, "result"},
		{"an invented kind", func(v *pii.Verification) { v.Kind = "iris_scan" }, "kind"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := good
			tc.mut(&v)
			err := v.Validate()
			if err == nil {
				t.Fatalf("accepted: %s", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not mention %q", err, tc.want)
			}
		})
	}

	// Every result is a legal value, so a failed check can be recorded — a verification
	// that can only be stored when it succeeded is a verification nobody can audit.
	for _, r := range pii.Results() {
		v := good
		v.Result = r
		if err := v.Validate(); err != nil {
			t.Errorf("result %s was refused: %v", r, err)
		}
	}
}
