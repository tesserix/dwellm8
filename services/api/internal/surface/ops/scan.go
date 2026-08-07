package ops

import (
	"encoding/base64"
	"errors"
	"net/http"
	"strings"

	"github.com/tesserix/dwellm8/services/api/internal/identity/docscan"
	"github.com/tesserix/dwellm8/services/api/internal/platform/authz"
)

// scanBody bounds a scan request. A document photograph is capped at
// docscan.MaxImageBytes, and base64 costs a third more on top of it.
const scanBody = docscan.MaxImageBytes*4/3 + (64 << 10)

// WithScanner adds the document reader (#318). Without one the route answers
// 404: prefilling is an aid, and the form works by hand without it.
func (h *Handler) WithScanner(s *docscan.Scanner) *Handler {
	h.scanner = s
	return h
}

// ScanRoutes mounts the reader. can_operate, not can_view: it spends money at
// the engine and it is part of doing the onboarding, not of looking at it.
func (h *Handler) ScanRoutes(r *authz.Registrar) {
	r.Handle("POST /v1/ops/identity/scan", authz.Check{
		Relation: "can_operate", Object: authz.Organisation()}, h.ScanDocument)
}

type scanRequest struct {
	Kind  string `json:"kind"`
	Image string `json:"image"`
}

type scanResponse struct {
	Kind     string            `json:"kind"`
	Passport *passportResponse `json:"passport,omitempty"`
	PAN      *panResponse      `json:"pan,omitempty"`
}

type passportResponse struct {
	Surname     string `json:"surname"`
	GivenNames  string `json:"given_names"`
	Number      string `json:"number"`
	Nationality string `json:"nationality"`
	DateOfBirth string `json:"date_of_birth"`
	Sex         string `json:"sex,omitempty"`
	ExpiresOn   string `json:"expires_on"`
}

type panResponse struct {
	Number      string `json:"number"`
	Name        string `json:"name,omitempty"`
	FatherName  string `json:"father_name,omitempty"`
	DateOfBirth string `json:"date_of_birth,omitempty"`
	Individual  bool   `json:"individual"`
}

// ScanDocument reads a photographed document and returns what a form can be
// prefilled with. The fields go back to the manager holding the card and are
// kept nowhere: nothing here is written down or logged (ADR-0013).
func (h *Handler) ScanDocument(w http.ResponseWriter, r *http.Request) {
	if h.scanner == nil {
		writeError(w, http.StatusNotFound, "not here yet")
		return
	}
	var req scanRequest
	if err := decodeUpTo(w, r, &req, scanBody); err != nil {
		return
	}
	kind := strings.TrimSpace(req.Kind)
	if kind != "passport" && kind != "pan" {
		writeError(w, http.StatusUnprocessableEntity, "kind must be passport or pan")
		return
	}
	image, err := base64.StdEncoding.DecodeString(strings.TrimSpace(req.Image))
	if err != nil {
		writeError(w, http.StatusBadRequest, "image must be a base64 photograph")
		return
	}
	if len(image) > docscan.MaxImageBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "the photograph is too large to read")
		return
	}

	if kind == "passport" {
		got, err := h.scanner.Passport(r.Context(), image)
		if err != nil {
			h.refuseScan(w, "passport", err)
			return
		}
		writeJSON(w, http.StatusOK, scanResponse{Kind: kind, Passport: &passportResponse{
			Surname: got.Surname, GivenNames: got.GivenNames, Number: got.Number,
			Nationality: got.Nationality, DateOfBirth: got.DateOfBirth,
			Sex: got.Sex, ExpiresOn: got.ExpiresOn}})
		return
	}
	got, err := h.scanner.PAN(r.Context(), image)
	if err != nil {
		h.refuseScan(w, "pan", err)
		return
	}
	writeJSON(w, http.StatusOK, scanResponse{Kind: kind, PAN: &panResponse{
		Number: got.Number, Name: got.Name, FatherName: got.FatherName,
		DateOfBirth: got.DateOfBirth, Individual: got.Individual}})
}

// refuseScan keeps the reason away from both the manager and the log, because
// the parser's error quotes the text it was given, and that text is the card.
func (h *Handler) refuseScan(w http.ResponseWriter, kind string, err error) {
	if errors.Is(err, docscan.ErrEngine) {
		h.log.Error("the document engine did not answer", "kind", kind)
		writeError(w, http.StatusBadGateway, "the document reader is unavailable")
		return
	}
	writeError(w, http.StatusUnprocessableEntity,
		"the document did not read cleanly — retake the photograph, or type it in")
}
