package activity

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/tesserix/dwellm8/services/api/internal/platform/auth"
	"github.com/tesserix/dwellm8/services/api/internal/platform/authz"
)

// maxBody bounds a note request. A note is a paragraph; anything near this is
// a pasted document that belongs in document storage.
const maxBody = 16 << 10

// Feeder is the slice of Store the handler needs, an interface so the tests
// can watch the queries the handler builds — which is most of what it must
// get right.
type Feeder interface {
	Feed(ctx context.Context, q Query) ([]Entry, error)
	AddNote(ctx context.Context, n Note) (string, error)
}

var _ Feeder = (*Store)(nil)

// Handler serves the org-side feed. The renter's feed lives on the resident
// surface, which composes this package with its own session scoping.
type Handler struct {
	store Feeder
	log   *slog.Logger
}

func NewHandler(s Feeder, log *slog.Logger) *Handler {
	return &Handler{store: s, log: log}
}

// Routes mounts the feed. One route per subject kind rather than a wildcard,
// because each names a different ADR-0020 object type — a feed reached
// through a lease is checked as that agreement, not as "some activity".
func (h *Handler) Routes(r *authz.Registrar) {
	r.Handle("GET /v1/activity", authz.Check{
		Relation: "can_view", Object: authz.Organisation()}, h.orgFeed)
	r.Handle("GET /v1/activity/lease/{id}", authz.Check{
		Relation: "can_view", Object: authz.PathObject("agreement", "id")}, h.subjectFeed("lease"))
	r.Handle("POST /v1/activity/lease/{id}/notes", authz.Check{
		Relation: "can_edit", Object: authz.PathObject("agreement", "id")}, h.addNote("lease"))
	r.Handle("GET /v1/activity/property/{id}", authz.Check{
		Relation: "can_view", Object: authz.PathObject("property", "id")}, h.subjectFeed("property"))
	r.Handle("POST /v1/activity/property/{id}/notes", authz.Check{
		Relation: "can_operate", Object: authz.PathObject("property", "id")}, h.addNote("property"))
	r.Handle("GET /v1/activity/checklist/{id}", authz.Check{
		Relation: "can_view", Object: authz.PathObject("checklist", "id")}, h.subjectFeed("checklist"))
	r.Handle("POST /v1/activity/checklist/{id}/notes", authz.Check{
		Relation: "can_operate", Object: authz.PathObject("checklist", "id")}, h.addNote("checklist"))
}

type feedResponse struct {
	Entries []Entry `json:"entries"`
	// NextBeforeAt and NextBeforeID are the cursor for the next page, present
	// only when the page was full — a feed with thousands of entries pages by
	// position, and an offset would reread the whole story every screen.
	NextBeforeAt string `json:"next_before_at,omitempty"`
	NextBeforeID string `json:"next_before_id,omitempty"`
}

func (h *Handler) orgFeed(w http.ResponseWriter, r *http.Request) {
	q, ok := parseQuery(w, r)
	if !ok {
		return
	}
	h.serve(w, r, q)
}

func (h *Handler) subjectFeed(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q, ok := parseQuery(w, r)
		if !ok {
			return
		}
		q.SubjectKind, q.SubjectID = kind, r.PathValue("id")
		h.serve(w, r, q)
	}
}

func (h *Handler) serve(w http.ResponseWriter, r *http.Request, q Query) {
	entries, err := h.store.Feed(r.Context(), q)
	if err != nil {
		h.log.Error("reading the activity feed", "subject", q.SubjectKind, "error", err)
		writeError(w, http.StatusInternalServerError, "could not read the feed")
		return
	}
	out := feedResponse{Entries: entries}
	if out.Entries == nil {
		out.Entries = []Entry{}
	}
	limit := q.Limit
	if limit <= 0 {
		limit = defaultLimit
	}
	if len(entries) == limit {
		last := entries[len(entries)-1]
		out.NextBeforeAt = last.OccurredAt.Format(time.RFC3339Nano)
		out.NextBeforeID = last.ID
	}
	writeJSON(w, http.StatusOK, out)
}

type noteRequest struct {
	Body       string `json:"body"`
	Visibility string `json:"visibility,omitempty"`
}

func (h *Handler) addNote(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req noteRequest
		body, err := io.ReadAll(io.LimitReader(r.Body, maxBody))
		if err != nil {
			writeError(w, http.StatusBadRequest, "could not read the request")
			return
		}
		dec := json.NewDecoder(bytes.NewReader(body))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "could not read the request: "+err.Error())
			return
		}

		// The author is the session's principal. A label until #229 puts a
		// party uuid on the request — the same trade the outbox actors make.
		author := "unknown"
		if p, ok := auth.From(r.Context()); ok && p.UID != "" {
			author = p.UID
		}

		id, err := h.store.AddNote(r.Context(), Note{
			SubjectKind: kind, SubjectID: r.PathValue("id"),
			Author: author, Body: req.Body, Visibility: req.Visibility,
		})
		if err != nil {
			if strings.HasPrefix(err.Error(), "activity: a note") ||
				strings.HasPrefix(err.Error(), "activity: visibility") {
				writeError(w, http.StatusUnprocessableEntity, err.Error())
				return
			}
			h.log.Error("writing a note", "subject", kind, "error", err)
			writeError(w, http.StatusInternalServerError, "could not write the note")
			return
		}
		writeJSON(w, http.StatusCreated, map[string]string{"id": id})
	}
}

// parseQuery reads the filter parameters shared by every feed route.
func parseQuery(w http.ResponseWriter, r *http.Request) (Query, bool) {
	q := Query{Audience: AudienceOrg}
	v := r.URL.Query()

	if types := v.Get("types"); types != "" {
		q.Types = strings.Split(types, ",")
	}
	for name, dst := range map[string]*time.Time{
		"from": &q.From, "to": &q.To, "before_at": &q.BeforeAt,
	} {
		if s := v.Get(name); s != "" {
			t, err := time.Parse(time.RFC3339Nano, s)
			if err != nil {
				writeError(w, http.StatusBadRequest, name+" is an RFC3339 timestamp")
				return Query{}, false
			}
			*dst = t
		}
	}
	q.BeforeID = v.Get("before_id")
	if (q.BeforeAt.IsZero()) != (q.BeforeID == "") {
		writeError(w, http.StatusBadRequest, "before_at and before_id page together")
		return Query{}, false
	}
	if s := v.Get("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 {
			writeError(w, http.StatusBadRequest, "limit is a positive number")
			return Query{}, false
		}
		q.Limit = n
	}
	return q, true
}

func writeJSON(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store, private")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}
