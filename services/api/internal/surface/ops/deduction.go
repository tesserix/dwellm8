package ops

import (
	"context"
	"errors"
	"net/http"
	"time"

	identitystore "github.com/tesserix/dwellm8/services/api/internal/identity/store"
	leaseservice "github.com/tesserix/dwellm8/services/api/internal/lease/service"
	leasestore "github.com/tesserix/dwellm8/services/api/internal/lease/store"
	moneydomain "github.com/tesserix/dwellm8/services/api/internal/money/domain"
	"github.com/tesserix/dwellm8/services/api/internal/platform/authz"
	"github.com/tesserix/dwellm8/services/api/internal/platform/effective"
	"github.com/tesserix/dwellm8/services/api/internal/platform/statutory"
	"github.com/tesserix/dwellm8/services/api/internal/platform/statutory/tds"
	tdsstore "github.com/tesserix/dwellm8/services/api/internal/platform/statutory/tds/store"
)

// WithTDS gives the surface the decision matrix and the section 197
// certificates a rate override rests on. Both together: a matrix that could not
// see a certificate would deduct at the section rate from a landlord holding an
// officer's determination to deduct at less.
func (h *Handler) WithTDS(m *tds.Matrix, c *tdsstore.Certificates) *Handler {
	h.tds, h.certificates = m, c
	return h
}

// DeductionRoutes mounts what a payment on this tenancy is short by, and why
// (#318). can_view: it reads facts already on the record and writes nothing.
func (h *Handler) DeductionRoutes(r *authz.Registrar) {
	r.Handle("GET /v1/ops/tenancies/{lease}/deduction", authz.Check{
		Relation: "can_view", Object: authz.Organisation()}, h.Deduction)
}

type deductionPayee struct {
	PartyID           string `json:"party_id"`
	Form              string `json:"form"`
	PANFurnished      bool   `json:"pan_furnished"`
	Rule37BCFurnished bool   `json:"rule_37bc_furnished"`
	CertificateNumber string `json:"certificate_number,omitempty"`
	// Furnished is false where nobody has recorded what this landlord gave the
	// deductor. The rate still comes back — at section 206AA's floor.
	Furnished bool `json:"profile_furnished"`
}

type deductionStep struct {
	Under   string `json:"under"`
	FromBps int    `json:"from_bps"`
	ToBps   int    `json:"to_bps"`
	RuleID  string `json:"rule_id,omitempty"`
	Because string `json:"because"`
}

type deductionResponse struct {
	LeaseID string `json:"lease_id"`
	On      string `json:"on"`

	Section string `json:"section"`
	// Note is the sentence shown beside the section, and Artefacts what the path
	// obliges — a manager on section 194-IB files Form 26QC and needs no TAN.
	Note             string   `json:"note"`
	RequiresTAN      bool     `json:"requires_tan"`
	Artefacts        []string `json:"artefacts"`
	DeductorClass    string   `json:"deductor_class"`
	Residency        string   `json:"landlord_residency"`
	MonthlyRentMinor int64    `json:"monthly_rent_minor"`
	AnnualRentMinor  int64    `json:"annual_rent_minor"`

	// RateDetermined is false where the section has no rate this platform may
	// supply — section 195, where it is the Act's or a treaty's, read with the
	// landlord's residency certificate. Nothing is deducted on a guess.
	RateDetermined bool `json:"rate_determined"`
	SectionRateBps int  `json:"section_rate_bps"`
	RateBps        int  `json:"rate_bps"`
	Deductible     bool `json:"deductible"`

	ThresholdMinor int64 `json:"threshold_minor"`
	WithheldMinor  int64 `json:"withheld_minor"`

	Payee   deductionPayee  `json:"payee"`
	Applied []deductionStep `json:"applied"`

	RateRuleID      string `json:"rate_rule_id,omitempty"`
	ThresholdRuleID string `json:"threshold_rule_id,omitempty"`
	Verification    string `json:"verification"`
	Enforcement     string `json:"enforcement"`
	Because         string `json:"because"`
}

// Deduction answers what one month's rent on this tenancy is short by.
//
// It is the composition ADR-0024 was written for, and the first caller the
// matrix has had: the section comes from the tenancy's own facts, the rate from
// the registry as at the date asked about, and everything that moves that rate —
// a PAN, rule 37BC's particulars, a section 197 certificate — from the payee's
// own record rather than from the lease.
func (h *Handler) Deduction(w http.ResponseWriter, r *http.Request) {
	if h.tds == nil || h.owners == nil {
		writeError(w, http.StatusNotFound, "not here yet")
		return
	}
	lease := r.PathValue("lease")
	on, ok := h.askedDate(w, r)
	if !ok {
		return
	}
	day := effective.DateOf(on, time.UTC)
	ctx := r.Context()

	let, err := h.leases.Tenancy(ctx, lease, day)
	if errors.Is(err, leasestore.ErrNoLease) {
		writeError(w, http.StatusNotFound, "no such tenancy")
		return
	}
	if err != nil {
		h.log.Error("reading a tenancy for its deduction", "lease", lease, "error", err)
		writeError(w, http.StatusInternalServerError, "could not read the tenancy")
		return
	}

	path, facts, err := h.leases.TaxPath(ctx, lease, day)
	switch {
	case errors.Is(err, leaseservice.ErrNoTaxFacts):
		writeError(w, http.StatusUnprocessableEntity,
			"this tenancy has no tax facts in force on that date, so no section governs it")
		return
	case err != nil:
		h.log.Error("selecting the section", "lease", lease, "error", err)
		writeError(w, http.StatusUnprocessableEntity, "the tenancy's tax facts do not select a section")
		return
	}

	_, ownerParty, err := h.leases.PartiesOf(ctx, lease, day)
	if err != nil {
		h.log.Error("resolving the landlord", "lease", lease, "error", err)
		writeError(w, http.StatusUnprocessableEntity,
			"this tenancy has no recorded landlord, so there is nobody to deduct from")
		return
	}
	// The mandate check the route's authz cannot express: the lease came off the
	// path, and whose books it is in is settled here (see actsForParty).
	if !h.actsForParty(w, r, ownerParty) {
		return
	}

	payee, furnished, err := h.payeeProfile(ctx, ownerParty, path.Section, on, day)
	if err != nil {
		h.log.Error("assembling the payee", "party", ownerParty, "error", err)
		writeError(w, http.StatusInternalServerError, "could not read what the landlord has furnished")
		return
	}

	annual, err := h.leases.AnnualRentToOwner(ctx, ownerParty, day)
	if err != nil {
		h.log.Error("aggregating the year's rent", "party", ownerParty, "error", err)
		writeError(w, http.StatusInternalServerError, "could not total the year's rent")
		return
	}

	out := deductionResponse{
		LeaseID: lease, On: on.Format("2006-01-02"),
		Section: string(path.Section), Note: path.Note, RequiresTAN: path.RequiresTAN,
		DeductorClass:    string(facts.Deductor),
		Residency:        string(facts.Residency),
		MonthlyRentMinor: let.RentMinor, AnnualRentMinor: annual,
		Payee: deductionPayee{
			PartyID: ownerParty, Form: string(payee.Form), PANFurnished: payee.HasPAN,
			Rule37BCFurnished: payee.Rule37BCFurnished, Furnished: furnished,
		},
	}
	for _, a := range path.Artefacts {
		out.Artefacts = append(out.Artefacts, string(a))
	}
	if payee.Certificate != nil {
		out.Payee.CertificateNumber = payee.Certificate.Number
	}

	rent := tds.Rent{MonthlyMinor: let.RentMinor, AnnualMinor: annual}
	assessed, err := h.tds.Assess(facts, payee, rent, day)
	switch {
	case errors.Is(err, statutory.ErrNoRule):
		// Section 195's gap, and the one answer this platform is entitled to give:
		// the rate is the Act's or a treaty's, read with the landlord's residency
		// certificate, and a plausible number here would be wrong with authority.
		out.Because = "Section " + string(path.Section) + ": no rate is determined here. " +
			"The rate for a payment to a non-resident comes from the Act or from the treaty " +
			"their residency certificate is issued under, and this tenancy's must be settled " +
			"with the landlord's tax adviser before rent is paid out."
		writeJSON(w, http.StatusOK, out)
		return
	case err != nil:
		h.log.Error("assessing a deduction", "lease", lease, "error", err)
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	out.RateDetermined = true
	out.SectionRateBps, out.RateBps = assessed.Rate.SectionBps, assessed.RateBps
	out.Deductible, out.ThresholdMinor = assessed.Deductible(), assessed.ThresholdMinor
	out.RateRuleID, out.ThresholdRuleID = assessed.RateRuleID, assessed.ThresholdRuleID
	out.Verification, out.Enforcement = string(assessed.Verification), string(assessed.Enforcement)
	out.Because = assessed.Because
	for _, a := range assessed.Rate.Applied {
		out.Applied = append(out.Applied, deductionStep{
			Under: a.Under, FromBps: a.FromBps, ToBps: a.ToBps, RuleID: a.RuleID, Because: a.Because,
		})
	}

	// The one multiplication. ADR-0024 §3 keeps it out of the matrix, and the
	// money module owns the rounding — a rate is a ratio until something applies it.
	if assessed.Deductible() {
		withheld, err := moneydomain.Rate(assessed.RateBps).Of(moneydomain.Minor(let.RentMinor))
		if err != nil {
			h.log.Error("applying the rate", "lease", lease, "error", err)
			writeError(w, http.StatusInternalServerError, "could not apply the rate to the rent")
			return
		}
		out.WithheldMinor = int64(withheld)
	}
	writeJSON(w, http.StatusOK, out)
}

// payeeProfile is the landlord as the rate sees them: what they furnished, and
// the certificate they hold. A landlord nobody has asked is not an error — the
// zero profile deducts at section 206AA's floor, which is the safe direction —
// so the bool says whether anybody has asked rather than the answer failing.
func (h *Handler) payeeProfile(
	ctx context.Context, party string, section tds.Section, on time.Time, day effective.Date,
) (tds.PayeeProfile, bool, error) {
	payee := tds.PayeeProfile{Ref: party, Form: tds.FormIndividual}

	got, err := h.owners.TaxProfile(ctx, party, on)
	furnished := true
	switch {
	case errors.Is(err, identitystore.ErrNoTaxProfile):
		furnished = false
	case err != nil:
		return tds.PayeeProfile{}, false, err
	default:
		payee.HasPAN = got.PANFurnished
		payee.Rule37BCFurnished = got.Rule37BCFurnished
		if got.PayeeForm == string(tds.FormCompany) {
			payee.Form = tds.FormCompany
		}
	}

	if h.certificates != nil {
		if payee, err = h.certificates.Profile(ctx, payee, party, section, day); err != nil {
			return tds.PayeeProfile{}, furnished, err
		}
	}
	return payee, furnished, nil
}

// askedDate is the payment date the question is about — today unless the caller
// says otherwise, because a deduction recomputed in November has to resolve the
// rate that was in force in April.
func (h *Handler) askedDate(w http.ResponseWriter, r *http.Request) (time.Time, bool) {
	asked := r.URL.Query().Get("on")
	if asked == "" {
		return h.now(), true
	}
	on, err := time.Parse("2006-01-02", asked)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "on must be a date, as YYYY-MM-DD")
		return time.Time{}, false
	}
	return on, true
}
