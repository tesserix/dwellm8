// Package pii is the identity-data standard. ADR-0013.
//
// KYC identifiers are the highest-liability data in the platform, and the default
// implementation of every vendor integration stores far too much: the SDK hands back
// the full identifier and the obvious thing to do is put it in a column.
//
// So the rule is stated before any verification feature exists, and the mechanism is
// not a review comment.
//
// # Three tiers, and only the middle one is interesting
//
//	Prohibited  never at rest, in any form, anywhere. The Aadhaar number.
//	Encrypted   at rest only as ciphertext, with no plaintext column to read from.
//	Open        a masked reference, a result, a provider transaction id, a timestamp.
//
// The first tier is enforced by making a full identifier unstorable rather than
// discouraged: the only column that holds anything derived from an Aadhaar has a CHECK
// that refuses anything but a mask, so a twelve-digit number cannot be put there by
// any code path, migration or psql prompt.
//
// # The lesson this package is built on
//
// The org has already built envelope encryption once, for HomeChef, and it is dormant:
// the flag exists, the KMS key exists, and it protects nothing — because the migration
// dual-wrote ciphertext into a new column while *reads still came from the plaintext
// one*. Encryption that leaves a plaintext column in place is a plaintext column with
// extra steps, and it will be read: by a report, by a support query, by a backup.
//
// Hence Secret, and hence the rule in §3 of the ADR: the plaintext column does not
// exist. There is nothing to read from, so nothing can accidentally read it.
package pii

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Secret wraps a value that must not be printed, logged, marshalled or interpolated.
//
// String() and MarshalJSON() both return a redaction, so the accidents that leak PII
// all fail safely: a %v in a log line, a %s in an error, a struct handed to
// json.Marshal for a debug dump, an httptest response body. Getting the value out
// requires calling Reveal(), which is grep-able — the point is not that it is hard, it
// is that it is *visible*.
//
// The field is unexported so a Secret cannot be built by a struct literal and cannot be
// read by reflection-based marshallers that ignore MarshalJSON.
type Secret struct {
	v string
}

// NewSecret wraps a value.
func NewSecret(v string) Secret { return Secret{v: v} }

// Redaction is what a Secret renders as. A fixed string rather than the empty string,
// because an empty value in a log is indistinguishable from a field nobody set, and the
// difference matters when you are trying to work out whether a leak happened.
const Redaction = "[redacted]"

// String satisfies fmt.Stringer, which is what %v and %s reach for.
func (s Secret) String() string { return Redaction }

// GoString satisfies %#v, which is what a debug dump reaches for and which ignores
// String().
func (s Secret) GoString() string { return Redaction }

// Format catches the verbs Stringer does not, including %q and %x.
func (s Secret) Format(f fmt.State, verb rune) {
	_, _ = f.Write([]byte(Redaction))
}

// MarshalJSON never emits the value. There is no UnmarshalJSON on purpose: a Secret is
// built from a value the process already has, not parsed out of a payload it is about
// to log.
func (s Secret) MarshalJSON() ([]byte, error) { return json.Marshal(Redaction) }

// Reveal returns the value. Deliberately named so that every use is findable, and every
// use is a place a reviewer has to ask what happens to the result.
func (s Secret) Reveal() string { return s.v }

// Empty reports whether there is nothing wrapped, without revealing anything.
func (s Secret) Empty() bool { return s.v == "" }

// Kind is a class of identity document or financial identifier.
type Kind string

const (
	// KindAadhaar is the Aadhaar number. Prohibited at rest in every form. It may be
	// transmitted to an authorised provider over TLS, validated, and discarded — and
	// what is kept is the provider's answer, not the number.
	KindAadhaar Kind = "aadhaar"
	// KindPAN is the income-tax Permanent Account Number. Encrypted at rest, because a
	// TDS return needs it and no mask will do.
	KindPAN Kind = "pan"
	// KindBankAccount is an account number. Encrypted, for the same reason: a payout
	// needs the real one.
	KindBankAccount Kind = "bank_account"
	// KindIFSC is a bank branch code. Open — it identifies a branch, not a person, and
	// it is published by the RBI.
	KindIFSC Kind = "ifsc"
	// KindUPIVPA is a UPI virtual payment address. Open, because it is designed to be
	// shared, and masked in display anyway because it usually contains a phone number.
	KindUPIVPA Kind = "upi_vpa"
	// KindPassport, KindDrivingLicence and KindVoterID are alternative identity
	// documents. Encrypted rather than prohibited: unlike Aadhaar there is no statutory
	// bar on holding them, and a landlord's police verification needs the number.
	KindPassport       Kind = "passport"
	KindDrivingLicence Kind = "driving_licence"
	KindVoterID        Kind = "voter_id"
	// KindGSTIN is a business registration. Open: it is a public register.
	KindGSTIN Kind = "gstin"
)

// Tier is how a kind may exist at rest.
type Tier string

const (
	// TierProhibited may never be stored, in any form, anywhere — including a log line,
	// a telemetry attribute, an analytics extract and a document filename.
	TierProhibited Tier = "prohibited"
	// TierEncrypted may be stored only as ciphertext, with no plaintext column beside
	// it. See the package comment for why the second half of that matters more.
	TierEncrypted Tier = "encrypted"
	// TierOpen may be stored as it is. It identifies an institution or is a published
	// register, not a person's identity document.
	TierOpen Tier = "open"
)

// spec is what may be done with each kind.
type spec struct {
	tier Tier
	// shape is what a valid raw value looks like. Validation happens in the process and
	// the value is then discarded or encrypted; nothing about validating requires
	// storing.
	shape *regexp.Regexp
	// keep is how many trailing characters a mask retains. Four is the industry norm
	// and is what a person can recognise their own number by; more starts to be
	// identifying on its own.
	keep int
	// why is the reason for the tier, because a list of classifications with no reasons
	// is a list somebody will reclassify.
	why string
}

var specs = map[Kind]spec{
	KindAadhaar: {
		tier: TierProhibited, keep: 4,
		shape: regexp.MustCompile(`^[2-9][0-9]{11}$`),
		why: "the Aadhaar Act and the UIDAI regulations bar storage by anyone who is not an " +
			"authorised agency, and a database of them is the honeypot this ADR exists to prevent",
	},
	KindPAN: {
		tier: TierEncrypted, keep: 4,
		shape: regexp.MustCompile(`^[A-Z]{5}[0-9]{4}[A-Z]$`),
		why:   "a TDS return needs the real number, so it cannot be masked away",
	},
	KindBankAccount: {
		tier: TierEncrypted, keep: 4,
		shape: regexp.MustCompile(`^[0-9]{9,18}$`),
		why:   "a payout needs the real account, so it cannot be masked away",
	},
	KindIFSC: {
		tier: TierOpen, keep: 11,
		shape: regexp.MustCompile(`^[A-Z]{4}0[A-Z0-9]{6}$`),
		why:   "it identifies a bank branch, not a person, and the RBI publishes the whole list",
	},
	KindUPIVPA: {
		tier: TierOpen, keep: 256,
		shape: regexp.MustCompile(`^[a-zA-Z0-9.\-_]{2,256}@[a-zA-Z]{2,64}$`),
		why: "a VPA is designed to be shared and a payer types it in. A UI abbreviates it " +
			"because it usually contains a phone number, which is a display concern",
	},
	KindPassport: {
		tier: TierEncrypted, keep: 3,
		shape: regexp.MustCompile(`^[A-PR-WY][0-9]{7}$`),
		why:   "police verification needs the number, and no statute bars holding it",
	},
	KindDrivingLicence: {
		tier: TierEncrypted, keep: 4,
		shape: regexp.MustCompile(`^[A-Z]{2}[0-9]{2}[0-9]{11}$`),
		why:   "an accepted address proof whose number a verification report cites",
	},
	KindVoterID: {
		tier: TierEncrypted, keep: 4,
		shape: regexp.MustCompile(`^[A-Z]{3}[0-9]{7}$`),
		why:   "an accepted identity proof whose number a verification report cites",
	},
	KindGSTIN: {
		tier: TierOpen, keep: 15,
		shape: regexp.MustCompile(`^[0-9]{2}[A-Z]{5}[0-9]{4}[A-Z][0-9A-Z][Z][0-9A-Z]$`),
		why:   "a public register, and an invoice has to print it",
	},
}

// Kinds returns every kind, ordered.
func Kinds() []Kind {
	out := make([]Kind, 0, len(specs))
	for k := range specs {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// TierOf returns how a kind may exist at rest.
func TierOf(k Kind) (Tier, bool) {
	s, ok := specs[k]
	if !ok {
		return "", false
	}
	return s.tier, true
}

// Why returns the reason for a kind's tier.
func Why(k Kind) string { return specs[k].why }

// Prohibited reports whether a kind may never be stored.
func Prohibited(k Kind) bool { return specs[k].tier == TierProhibited }

// ErrProhibited is what a caller gets for trying to persist a prohibited identifier. It
// is distinguishable because the correct response is to change the design, not to retry.
var ErrProhibited = errors.New("pii: this identifier may not be stored")

// ErrShape is an identifier that is not one.
var ErrShape = errors.New("pii: not a valid identifier of that kind")

// Validate checks the shape without storing anything.
//
// It takes a Secret, so a caller cannot accidentally log the thing they are validating,
// and the error never quotes the value — which is the mistake every validation function
// makes: `fmt.Errorf("invalid PAN %q", pan)` puts the identifier straight into the log
// that the error is written to.
func Validate(k Kind, v Secret) error {
	s, ok := specs[k]
	if !ok {
		return fmt.Errorf("pii: unknown identifier kind %q", k)
	}
	if v.Empty() {
		return fmt.Errorf("%w: %s is empty", ErrShape, k)
	}
	if !s.shape.MatchString(strings.ToUpper(strings.TrimSpace(v.Reveal()))) {
		// The value is deliberately absent from this message. An error is a log line.
		return fmt.Errorf("%w: %s", ErrShape, k)
	}
	return nil
}

// MaskPrefix is what a mask is padded with. 'X' rather than '*' because a masked
// reference goes into a column with a CHECK on it, and '*' collides with the wildcard
// somebody will eventually write in a LIKE.
const MaskPrefix = 'X'

// Mask returns the only representation of an identifier that may be stored openly:
// trailing characters preserved, everything before them replaced.
//
// It is deliberately not reversible and deliberately not a hash. A hash of an Aadhaar
// number is not anonymous — the space is small enough to enumerate, so a hash column is
// a lookup table for anybody who takes a copy. That is alternative D in the ADR.
func Mask(k Kind, v Secret) (string, error) {
	s, ok := specs[k]
	if !ok {
		return "", fmt.Errorf("pii: unknown identifier kind %q", k)
	}
	if err := Validate(k, v); err != nil {
		return "", err
	}
	raw := strings.ToUpper(strings.TrimSpace(v.Reveal()))

	// An open kind is stored as it is, whatever `keep` says — that field is about how a
	// UI abbreviates a VPA, which is not this function's job. Conflating the two is what
	// made this return a string of X's for a value the column's own pattern then refused.
	if s.tier == TierOpen {
		return raw, nil
	}
	keep := s.keep
	if keep > len(raw) {
		keep = len(raw)
	}
	// A mask that keeps more than half of a short identifier is not a mask.
	if s.tier != TierOpen && keep*2 > len(raw) {
		return "", fmt.Errorf("pii: masking %s would keep %d of %d characters, which is not a mask",
			k, keep, len(raw))
	}
	return strings.Repeat(string(MaskPrefix), len(raw)-keep) + raw[len(raw)-keep:], nil
}

// MaskPattern is the regular expression a masked reference of this kind must match. It
// is the source of the schema's CHECK, and the store contract test compares the two —
// so a mask Go can produce and the column would refuse is a build failure rather than a
// verification that fails at the counter.
func MaskPattern(k Kind) (string, error) {
	s, ok := specs[k]
	if !ok {
		return "", fmt.Errorf("pii: unknown identifier kind %q", k)
	}
	if s.tier == TierOpen {
		// An open kind's stored form is the value itself, so the pattern is its shape.
		return "^" + strings.TrimPrefix(strings.TrimSuffix(s.shape.String(), "$"), "^") + "$", nil
	}
	return fmt.Sprintf("^%c+[0-9A-Z]{%d}$", MaskPrefix, s.keep), nil
}

// Result is what a verification concluded. Closed, because "pending" and "failed" get
// treated the same way by a caller that accepts free text.
type Result string

const (
	ResultVerified   Result = "verified"
	ResultFailed     Result = "failed"
	ResultExpired    Result = "expired"
	ResultWithdrawn  Result = "withdrawn"
	ResultUnverified Result = "unverified"
)

var results = map[Result]bool{
	ResultVerified: true, ResultFailed: true, ResultExpired: true,
	ResultWithdrawn: true, ResultUnverified: true,
}

// Results returns every result, ordered.
func Results() []Result {
	out := make([]Result, 0, len(results))
	for r := range results {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// Verification is the record a completed check leaves behind — and it is the whole
// record. ADR-0013's primary acceptance criterion is that this struct has no field for
// a full identifier, so there is nowhere for one to go.
type Verification struct {
	Kind Kind
	// MaskedReference is Mask()'s output. The only representation kept.
	MaskedReference string
	Result          Result
	// Provider and ProviderTxnID are the audit trail at the other end: if the result is
	// disputed, this is what a provider is asked about.
	Provider      string
	ProviderTxnID string
	// ConsentArtefact references the consent record — DPDP requires one, and a
	// verification with no consent behind it is a verification that should not have
	// happened.
	ConsentArtefact string
}

// Validate asserts the record is complete and holds nothing it should not.
func (v Verification) Validate() error {
	s, ok := specs[v.Kind]
	if !ok {
		return fmt.Errorf("pii: unknown identifier kind %q", v.Kind)
	}
	if !results[v.Result] {
		return fmt.Errorf("pii: %q is not a verification result", v.Result)
	}
	if v.Provider == "" || v.ProviderTxnID == "" {
		return errors.New("pii: a verification must name the provider and its transaction, " +
			"or a disputed result cannot be checked with anybody")
	}
	if v.ConsentArtefact == "" {
		return errors.New("pii: a verification with no consent artefact is one that should not " +
			"have happened")
	}
	if v.MaskedReference == "" {
		return errors.New("pii: a verification must carry the masked reference, or the subject " +
			"cannot recognise which of their documents was checked")
	}
	// The check that matters: whatever is in MaskedReference must be a mask, not the
	// thing it was made from. The schema's CHECK says the same, for anything that did
	// not come through here.
	pattern, err := MaskPattern(v.Kind)
	if err != nil {
		return err
	}
	if !regexp.MustCompile(pattern).MatchString(v.MaskedReference) {
		return fmt.Errorf("%w: the reference stored for %s is not a mask — a full identifier "+
			"must never reach a column", ErrProhibited, v.Kind)
	}
	// And a prohibited kind may not be stored even masked *unless* the mask is all this
	// record holds, which is exactly what the check above guarantees. So the remaining
	// rule is that its raw form never appears: enforced by there being no field for it.
	_ = s
	return nil
}

// ProhibitedColumnNames are the column and field names that must never exist, in the
// schema or in Go. It is a name check, which is weak on its own — a determined developer
// calls the column `national_id` — and it is here because the strong mechanism (the mask
// CHECK, and there being no plaintext column) covers the determined case while this
// covers the careless one, which is the common one.
//
// Both the schema's assertion 15 and the arch test read this list, so there is one copy.
func ProhibitedColumnNames() []string {
	return []string{
		"aadhaar", "aadhar", "adhaar", "adhar",
		"aadhaar_number", "aadhaar_no", "uidai", "vid",
	}
}
