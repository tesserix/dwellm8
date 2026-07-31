// Package dpdp is the privacy posture: what was consented to, what may be
// erased, and what may not. ADR-0026.
//
// The Digital Personal Data Protection Act 2023 is not a policy page. It is a
// set of things the product must be able to *do* — say what it collected and
// why, stop when consent is withdrawn, erase on request — and one thing it must
// be able to refuse, because financial and tax records have statutory retention
// periods that outlast any consent.
//
// # The hard part is the refusal
//
// A tenant with three years of receipts asks to be erased. Their name in a
// prospect record goes. The rent ledger does not, because the Income-tax Act
// requires it to exist and a deleted posting orphans an owner's return. So an
// erasure is never all-or-nothing: it is a partition of that person's data into
// what goes, what stays with a statute named against it, and what waits.
//
// The failure this package exists to prevent is the quiet one in either
// direction — erasing a record somebody is legally required to hold, or filing
// the request under "cannot comply" and never telling the requester which
// records were kept or why.
package dpdp

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/tesserix/dwellm8/services/api/internal/platform/effective"
)

// Purpose is why personal data was collected. Closed, because DPDP requires the
// notice to state the purpose and a free-text purpose is a purpose nobody can
// audit — and because withdrawal has to be answerable per purpose.
type Purpose string

const (
	// PurposeTenancy is running a tenancy: the lease, the rent, the notices.
	// Its lawful basis is the contract, so withdrawing consent does not stop it.
	PurposeTenancy Purpose = "tenancy"
	// PurposeKYC is verifying who somebody is, under ADR-0013.
	PurposeKYC Purpose = "kyc"
	// PurposePayments is collecting rent and paying owners.
	PurposePayments Purpose = "payments"
	// PurposeStatutory is meeting a legal obligation — TDS, GST, registration.
	// Consent is not its basis and cannot be withdrawn from it.
	PurposeStatutory Purpose = "statutory"
	// PurposeMarketing is anything the person could refuse without losing the
	// service: listing alerts, offers, a newsletter.
	PurposeMarketing Purpose = "marketing"
	// PurposeSupport is answering a question the person asked.
	PurposeSupport Purpose = "support"
)

// Purposes returns every purpose, ordered.
func Purposes() []Purpose {
	return []Purpose{PurposeKYC, PurposeMarketing, PurposePayments,
		PurposeStatutory, PurposeSupport, PurposeTenancy}
}

func (p Purpose) Valid() bool { return slices.Contains(Purposes(), p) }

// Withdrawable reports whether withdrawing consent actually stops the
// processing.
//
// Consent is the lawful basis for some purposes and not for others. Rent is
// processed because there is a contract; a TDS deduction is processed because
// the Act requires it. Telling a tenant they may withdraw consent to their own
// rent ledger would be a promise the product cannot keep — so the honest answer
// is that the withdrawal is recorded, the marketing stops, and the tenancy does
// not.
func (p Purpose) Withdrawable() bool {
	switch p {
	case PurposeMarketing, PurposeSupport:
		return true
	}
	return false
}

// Basis is why processing is lawful.
func (p Purpose) Basis() string {
	switch p {
	case PurposeTenancy, PurposePayments:
		return "performance of the tenancy agreement"
	case PurposeStatutory, PurposeKYC:
		return "compliance with a legal obligation"
	default:
		return "consent"
	}
}

// Consent is the artefact DPDP requires: what, why, when, under which notice,
// and how to take it back.
//
// An object rather than a boolean column, because "the user agreed" is not
// evidence of anything a year later when the notice has been reworded twice.
// ADR-0013 already required `kyc_verifications.consent_artefact_id`; this is the
// thing it was pointing at.
type Consent struct {
	ID            string
	PartyID       string
	Purpose       Purpose
	NoticeVersion string
	// Language the notice was shown in. DPDP §5(3) gives the data principal the
	// right to the notice in English or any language in the Eighth Schedule, and
	// recording which one was shown is what makes that claim checkable.
	Language string
	GivenOn  effective.Date
	// WithdrawnOn is when it was taken back. Zero while it stands.
	WithdrawnOn effective.Date
}

// ErrConsent is a consent artefact that proves nothing.
var ErrConsent = errors.New("dpdp: that is not a consent artefact")

// Validate refuses an artefact that could not be produced as evidence.
func (c Consent) Validate() error {
	switch {
	case c.PartyID == "":
		return fmt.Errorf("%w: consent from nobody", ErrConsent)
	case !c.Purpose.Valid():
		return fmt.Errorf("%w: %q is not a purpose", ErrConsent, c.Purpose)
	case strings.TrimSpace(c.NoticeVersion) == "":
		return fmt.Errorf("%w: consent to a notice with no version — the notice will be reworded, "+
			"and then nobody can say what was agreed to", ErrConsent)
	case strings.TrimSpace(c.Language) == "":
		return fmt.Errorf("%w: no record of which language the notice was shown in", ErrConsent)
	case c.GivenOn.Zero():
		return fmt.Errorf("%w: consent with no date", ErrConsent)
	case !c.WithdrawnOn.Zero() && c.WithdrawnOn.Before(c.GivenOn):
		return fmt.Errorf("%w: withdrawn on %s, before it was given on %s",
			ErrConsent, c.WithdrawnOn, c.GivenOn)
	}
	return nil
}

// Live reports whether the consent stands on a date.
func (c Consent) Live(on effective.Date) bool {
	if c.GivenOn.Zero() || on.Before(c.GivenOn) {
		return false
	}
	return c.WithdrawnOn.Zero() || on.Before(c.WithdrawnOn)
}

// Withdrawal is what actually happens when somebody withdraws consent.
type Withdrawal struct {
	Purpose Purpose
	// Stopped is whether the processing stops.
	Stopped bool
	// Because is what the person is told. A withdrawal that changes nothing must
	// say so plainly rather than be accepted in silence.
	Because string
}

// Withdraw records a withdrawal and says what it does.
//
// The edge case the story names: withdrawing consent that would break an active
// lease obligation. It does not break it. The tenancy is performed under the
// agreement, not under consent, and the answer is to say so — and to point at
// ending the tenancy, which is the thing that actually stops the processing.
func Withdraw(p Purpose) Withdrawal {
	if p.Withdrawable() {
		return Withdrawal{Purpose: p, Stopped: true,
			Because: fmt.Sprintf("Processing for %s has stopped.", p)}
	}
	return Withdrawal{Purpose: p, Stopped: false,
		Because: fmt.Sprintf("This processing is carried out on the basis of %s rather than "+
			"consent, so withdrawing consent does not stop it. It ends when the tenancy does, "+
			"and the records it produced are then kept for the periods set out in the retention "+
			"matrix.", p.Basis()),
	}
}
