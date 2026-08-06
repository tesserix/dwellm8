// Package registration says what a property manager must produce before they
// may hold somebody else's property (#282).
//
// Managing a let for a fee is regulated work: s.9 of the Real Estate
// (Regulation and Development) Act 2016 requires the agent to register with the
// state authority, the Income-tax Act requires a TAN of anybody deducting TDS
// on an owner's behalf, and the GST Act catches the management fee itself. What
// each of those is *evidenced by* depends on how the firm is constituted, and
// that matrix is here rather than in the database because it is a rule that
// changes with the law, not with the schema.
package registration

import "time"

// Constitution is how the managing business is formed. It decides the whole
// checklist, which is why it is asked first.
type Constitution string

const (
	Individual     Constitution = "individual"
	Proprietorship Constitution = "proprietorship"
	Partnership    Constitution = "partnership"
	LLP            Constitution = "llp"
	Company        Constitution = "company"
	Trust          Constitution = "trust"
	Society        Constitution = "society"
)

func Constitutions() []Constitution {
	return []Constitution{Individual, Proprietorship, Partnership, LLP, Company, Trust, Society}
}

// Kind is a document we know how to ask for. The values match the check
// constraint on manager_documents.
type Kind string

const (
	PANCard                  Kind = "pan_card"
	AddressProof             Kind = "address_proof"
	BankProof                Kind = "bank_proof"
	Photograph               Kind = "photograph"
	SignatoryID              Kind = "signatory_id"
	RERACertificate          Kind = "rera_certificate"
	GSTCertificate           Kind = "gst_certificate"
	TANAllotment             Kind = "tan_allotment"
	ShopsEstablishments      Kind = "shops_establishments"
	UdyamRegistration        Kind = "udyam_registration"
	PartnershipDeed          Kind = "partnership_deed"
	LLPAgreement             Kind = "llp_agreement"
	IncorporationCertificate Kind = "incorporation_certificate"
	MOAAOA                   Kind = "moa_aoa"
	BoardResolution          Kind = "board_resolution"
	TrustDeed                Kind = "trust_deed"
	RegistrationCertificate  Kind = "registration_certificate"
	ProfessionalIndemnity    Kind = "professional_indemnity"
	AuthorityLetter          Kind = "authority_letter"
)

// Subject says whose document it is. The distinction is the one that gets an
// onboarding rejected: a company that uploads its director's PAN has uploaded
// the wrong PAN.
type Subject string

const (
	OfTheFirm   Subject = "firm"
	OfThePerson Subject = "person"
)

// Requirement is one line of the checklist, in the words the manager reads.
type Requirement struct {
	Kind    Kind
	Subject Subject
	Label   string
	Why     string
	// Expires marks the kinds that lapse, so the screen asks for a date.
	Expires bool
}

// Held is what we already have of one kind.
type Held struct {
	Accepted  bool
	ExpiresOn time.Time
}

// Field is a value typed rather than uploaded.
type Field string

const (
	FieldLegalName        Field = "legal_name"
	FieldConstitution     Field = "constitution"
	FieldPAN              Field = "pan"
	FieldTAN              Field = "tan"
	FieldGSTIN            Field = "gstin"
	FieldRegistrarID      Field = "registrar_id"
	FieldRegisteredOffice Field = "registered_office"
	FieldContact          Field = "contact"
	FieldRERANumber       Field = "rera_number"
)

// common is what every managing firm produces, whatever its shape.
func common(panSubject Subject, panLabel, panWhy string) []Requirement {
	return []Requirement{
		{Kind: PANCard, Subject: panSubject, Label: panLabel, Why: panWhy},
		{Kind: AddressProof, Subject: OfTheFirm, Label: "Registered office address proof",
			Why: "A recent utility bill, the rent agreement or the property tax receipt for the office. Correspondence under the Act goes to this address."},
		{Kind: BankProof, Subject: OfTheFirm, Label: "Cancelled cheque or bank statement",
			Why: "Collections settle into this account, and the aggregator will not settle to an account it has not seen named."},
		{Kind: RERACertificate, Subject: OfTheFirm, Label: "RERA agent registration certificate", Expires: true,
			Why: "s.9 of the RERA Act 2016. Facilitating a letting for another person without it is an offence, and one certificate is needed per state you work in."},
	}
}

// RequiredDocuments is the checklist for a constitution.
func RequiredDocuments(of Constitution) []Requirement {
	switch of {
	case Individual, Proprietorship:
		reqs := common(OfThePerson, "Your PAN card",
			"A proprietorship is not separate from the proprietor, so the firm's PAN is your own.")
		return append(reqs,
			Requirement{Kind: Photograph, Subject: OfThePerson, Label: "Passport photograph",
				Why: "It goes on the agent registration and on the letter of authority an owner signs."},
			Requirement{Kind: ShopsEstablishments, Subject: OfTheFirm, Label: "Shops and Establishments registration", Expires: true,
				Why: "The state licence to run the office. Udyam registration is accepted where your state does not issue one."},
		)

	case Partnership:
		reqs := common(OfTheFirm, "The firm's PAN card",
			"A partnership firm holds its own PAN, separate from the partners'. Uploading a partner's is the commonest rejection.")
		return append(reqs,
			Requirement{Kind: PartnershipDeed, Subject: OfTheFirm, Label: "Partnership deed",
				Why: "It is what proves the firm exists and which partners may bind it."},
			Requirement{Kind: AuthorityLetter, Subject: OfTheFirm, Label: "Letter of authority",
				Why: "Signed by the partners, naming whoever will sign mandates and release payouts here."},
			Requirement{Kind: SignatoryID, Subject: OfThePerson, Label: "Authorised signatory's ID",
				Why: "The person named in the letter, so the signature on a mandate can be tied to a person."},
		)

	case LLP:
		reqs := common(OfTheFirm, "The LLP's PAN card",
			"An LLP is a body corporate with its own PAN. A designated partner's PAN is not it.")
		return append(reqs,
			Requirement{Kind: IncorporationCertificate, Subject: OfTheFirm, Label: "Certificate of incorporation",
				Why: "The MCA certificate carrying the LLPIN."},
			Requirement{Kind: LLPAgreement, Subject: OfTheFirm, Label: "LLP agreement",
				Why: "It names the designated partners and what they may commit the LLP to."},
			Requirement{Kind: SignatoryID, Subject: OfThePerson, Label: "Designated partner's ID",
				Why: "Whoever will act here — a mandate is signed by a person, not by an entity."},
		)

	case Company:
		reqs := common(OfTheFirm, "The company's PAN card",
			"A company holds its own PAN. A director's PAN is a different document and will be refused.")
		return append(reqs,
			Requirement{Kind: IncorporationCertificate, Subject: OfTheFirm, Label: "Certificate of incorporation",
				Why: "The MCA certificate carrying the CIN."},
			Requirement{Kind: MOAAOA, Subject: OfTheFirm, Label: "Memorandum and articles of association",
				Why: "Property management has to be within the objects the company was incorporated for."},
			Requirement{Kind: BoardResolution, Subject: OfTheFirm, Label: "Board resolution",
				Why: "The board naming who may sign management agreements and release money on the company's behalf."},
			Requirement{Kind: SignatoryID, Subject: OfThePerson, Label: "Authorised signatory's ID",
				Why: "The person the resolution names."},
		)

	case Trust, Society:
		reqs := common(OfTheFirm, "The trust or society's PAN card",
			"Registered under its own PAN, separate from any trustee's.")
		return append(reqs,
			Requirement{Kind: TrustDeed, Subject: OfTheFirm, Label: "Trust deed or memorandum",
				Why: "What the body was constituted to do, and by whom."},
			Requirement{Kind: RegistrationCertificate, Subject: OfTheFirm, Label: "Registration certificate",
				Why: "From the Registrar of Societies or the sub-registrar the trust is registered with."},
			Requirement{Kind: BoardResolution, Subject: OfTheFirm, Label: "Trustees' resolution",
				Why: "Naming whoever will act here."},
		)
	}
	return nil
}

// RequiredFields is what the manager types rather than uploads.
func RequiredFields(of Constitution) []Field {
	// TAN is universal here and not merely for the large: a manager collecting
	// rent for an owner deducts TDS under s.194-I, and cannot deduct without one.
	fields := []Field{
		FieldLegalName, FieldConstitution, FieldPAN, FieldTAN,
		FieldRegisteredOffice, FieldContact, FieldRERANumber,
	}
	switch of {
	case LLP, Company:
		fields = append(fields, FieldRegistrarID)
	}
	return fields
}

// Outstanding is everything still missing, all of it at once — one round trip
// per missing document is how an onboarding takes a week.
func Outstanding(of Constitution, held map[Kind]Held, on time.Time) []Requirement {
	var missing []Requirement
	for _, r := range RequiredDocuments(of) {
		h, ok := held[r.Kind]
		if !ok || !h.Accepted {
			missing = append(missing, r)
			continue
		}
		// A lapsed registration is an absent registration, whatever was uploaded
		// in 2023.
		if !h.ExpiresOn.IsZero() && !h.ExpiresOn.After(on) {
			missing = append(missing, r)
		}
	}
	return missing
}
