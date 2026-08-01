// Package activity is the readable history on every record. dwellm8#196.
//
// It owns almost nothing: the feed is the outbox read back — the same domain
// events every consumer gets, which is what "derived from events rather than
// written by each service by hand" means — plus one small table of human
// notes. It is deliberately not the audit trail: audit_events answers "who did
// what to whom" for compliance; this answers "what happened here" for the
// colleague picking up the file.
package activity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
)

// Entry is one line of the story: a domain event, or a note.
type Entry struct {
	ID          string          `json:"id"`
	Kind        string          `json:"kind"` // the event type, or "note"
	SubjectKind string          `json:"subject_kind"`
	SubjectID   string          `json:"subject_id"`
	OccurredAt  time.Time       `json:"occurred_at"`
	ActorKind   string          `json:"actor_kind"`
	ActorID     string          `json:"actor_id,omitempty"`
	Body        string          `json:"body,omitempty"` // notes only
	Data        json.RawMessage `json:"data,omitempty"` // events, org audience only
}

// Audience is who is reading.
type Audience int

const (
	// AudienceOrg is the landlord side: everything in the organisation.
	AudienceOrg Audience = iota
	// AudienceResident is a renter: lifecycle and notice facts about their own
	// lease, and notes a manager chose to share — never the owner's fees,
	// payouts or anything financial-side. #196's failure scenario.
	AudienceResident
)

// residentPrefixes is what a renter may see of the event stream. An allowlist
// rather than a blocklist, so an event type added tomorrow is invisible to
// renters until somebody decides otherwise.
var residentPrefixes = []string{"lease.tenancy.%", "lease.notice.%"}

// Query selects a slice of the feed.
type Query struct {
	// SubjectKind and SubjectID scope to one record. Both empty is the
	// whole-organisation feed; one empty is an error.
	SubjectKind, SubjectID string
	// Types keeps only these entry kinds — event types, and "note" for notes.
	// Empty keeps everything the audience may see.
	Types []string
	// From and To bound occurred_at; zero means unbounded.
	From, To time.Time
	// BeforeAt and BeforeID are the keyset cursor: entries strictly older than
	// this (occurred_at, id) pair. Both or neither — a feed with thousands of
	// entries pages by position, never by offset.
	BeforeAt time.Time
	BeforeID string
	// Limit is clamped to [1, 200]; zero means 50.
	Limit    int
	Audience Audience
}

const (
	defaultLimit = 50
	maxLimit     = 200
	// maxNoteRunes is generous for a note and hostile to a pasted document.
	maxNoteRunes = 4000
)

// Note is a human line in the story.
type Note struct {
	SubjectKind, SubjectID string
	// Author is whoever the session resolves to. Text rather than uuid for the
	// reason the outbox events carry system actors today: until #229 lands a
	// party uuid on the request principal, a label is what there is.
	Author string
	Body   string
	// Visibility is org (the default) or shared — shared reaches the renter on
	// that lease.
	Visibility string
}

// Store reads the feed and writes notes. Every query runs under the caller's
// tenant via row-level security; there is no unscoped path.
type Store struct{ pool tenancy.Pool }

func NewStore(p tenancy.Pool) *Store { return &Store{pool: p} }

// Feed returns entries newest first.
func (st *Store) Feed(ctx context.Context, q Query) ([]Entry, error) {
	if (q.SubjectKind == "") != (q.SubjectID == "") {
		return nil, errors.New("activity: a subject is a kind and an id together")
	}
	if q.Audience == AudienceResident && q.SubjectKind != "lease" {
		// A renter's feed is their lease's, structurally: there is no query
		// shape here that could wander the landlord's portfolio.
		return nil, errors.New("activity: a resident feed is scoped to one lease")
	}
	limit := q.Limit
	if limit <= 0 {
		limit = defaultLimit
	}
	if limit > maxLimit {
		limit = maxLimit
	}

	events, eventArgs := q.eventsSQL()
	notes, noteArgs := q.notesSQL(len(eventArgs))
	args := append(eventArgs, noteArgs...)

	sql := `SELECT id, kind, subject_kind, subject_id, occurred_at, actor_kind, actor_id, data, body
	          FROM (` + events + ` UNION ALL ` + notes + `) feed`
	where, args := q.cursorSQL(args)
	sql += where + fmt.Sprintf(" ORDER BY occurred_at DESC, id DESC LIMIT %d", limit)

	var out []Entry
	err := tenancy.Scoped(ctx, st.pool, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, sql, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var e Entry
			var data []byte
			if err := rows.Scan(&e.ID, &e.Kind, &e.SubjectKind, &e.SubjectID,
				&e.OccurredAt, &e.ActorKind, &e.ActorID, &data, &e.Body); err != nil {
				return err
			}
			// A renter gets the fact and its time, not the payload: event data
			// carries the landlord side's detail (parties, terms, ids), and the
			// allowlist admits the type, not its internals.
			if q.Audience != AudienceResident && len(data) > 0 && string(data) != "{}" {
				e.Data = data
			}
			out = append(out, e)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("activity: reading the feed: %w", err)
	}
	return out, nil
}

// eventsSQL is the outbox half of the union.
func (q Query) eventsSQL() (string, []any) {
	conds, args := []string{"o.type <> ''"}, []any{}
	if q.SubjectKind != "" {
		args = append(args, q.SubjectKind, q.SubjectID)
		conds = append(conds, fmt.Sprintf("o.subject_kind = $%d AND o.subject_id = $%d", len(args)-1, len(args)))
	}
	if q.Audience == AudienceResident {
		args = append(args, residentPrefixes)
		conds = append(conds, fmt.Sprintf("o.type LIKE ANY($%d)", len(args)))
	}
	if types := q.eventTypes(); q.Types != nil {
		if len(types) == 0 {
			conds = append(conds, "false") // only notes were asked for
		} else {
			args = append(args, types)
			conds = append(conds, fmt.Sprintf("o.type = ANY($%d)", len(args)))
		}
	}
	sql := `SELECT o.id, o.type AS kind, o.subject_kind, o.subject_id, o.occurred_at,
	               o.actor_kind, coalesce(o.actor_id::text, '') AS actor_id,
	               o.payload AS data, '' AS body
	          FROM outbox o WHERE ` + strings.Join(conds, " AND ")
	return sql, args
}

// notesSQL is the notes half. offset shifts the placeholders past the event
// half's arguments, because the union is one statement.
func (q Query) notesSQL(offset int) (string, []any) {
	conds, args := []string{"n.body <> ''"}, []any{}
	ph := func() int { return offset + len(args) }
	if q.SubjectKind != "" {
		args = append(args, q.SubjectKind, q.SubjectID)
		conds = append(conds, fmt.Sprintf("n.subject_kind = $%d AND n.subject_id = $%d", ph()-1, ph()))
	}
	if q.Audience == AudienceResident {
		conds = append(conds, "n.visibility = 'shared'")
	}
	if q.Types != nil && !slices.Contains(q.Types, "note") {
		conds = append(conds, "false")
	}
	sql := `SELECT n.id::text, 'note' AS kind, n.subject_kind, n.subject_id, n.noted_at AS occurred_at,
	               'user' AS actor_kind, n.author AS actor_id, '{}'::jsonb AS data, n.body
	          FROM activity_notes n WHERE ` + strings.Join(conds, " AND ")
	return sql, args
}

// cursorSQL bounds the combined feed by time window and keyset position.
func (q Query) cursorSQL(args []any) (string, []any) {
	var conds []string
	if !q.From.IsZero() {
		args = append(args, q.From)
		conds = append(conds, fmt.Sprintf("occurred_at >= $%d", len(args)))
	}
	if !q.To.IsZero() {
		args = append(args, q.To)
		conds = append(conds, fmt.Sprintf("occurred_at <= $%d", len(args)))
	}
	if !q.BeforeAt.IsZero() && q.BeforeID != "" {
		args = append(args, q.BeforeAt, q.BeforeID)
		conds = append(conds, fmt.Sprintf("(occurred_at, id) < ($%d, $%d)", len(args)-1, len(args)))
	}
	if len(conds) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(conds, " AND "), args
}

// eventTypes is Types with "note" removed — the outbox half's filter.
func (q Query) eventTypes() []string {
	out := make([]string, 0, len(q.Types))
	for _, t := range q.Types {
		if t != "note" {
			out = append(out, t)
		}
	}
	return out
}

// AddNote appends one note. Notes are append-only — the schema refuses UPDATE
// and DELETE, so a correction is another note, which is #196's edge case
// answered structurally: history shows the correction, nothing disappears.
func (st *Store) AddNote(ctx context.Context, n Note) (string, error) {
	if n.Visibility == "" {
		n.Visibility = "org"
	}
	switch {
	case n.SubjectKind == "" || n.SubjectID == "":
		return "", errors.New("activity: a note needs the record it is about")
	case strings.TrimSpace(n.Body) == "":
		return "", errors.New("activity: a note says something")
	case len([]rune(n.Body)) > maxNoteRunes:
		return "", fmt.Errorf("activity: a note is at most %d characters", maxNoteRunes)
	case n.Author == "":
		return "", errors.New("activity: a note has an author")
	case n.Visibility != "org" && n.Visibility != "shared":
		return "", errors.New(`activity: visibility is "org" or "shared"`)
	}

	var id string
	err := tenancy.Scoped(ctx, st.pool, func(ctx context.Context, tx pgx.Tx) error {
		tenant, _ := tenancy.From(ctx)
		return tx.QueryRow(ctx, `
			INSERT INTO activity_notes (tenant_id, subject_kind, subject_id, author, body, visibility)
			VALUES ($1, $2, $3, $4, $5, $6)
			RETURNING id`,
			tenant.String(), n.SubjectKind, n.SubjectID, n.Author, n.Body, n.Visibility).Scan(&id)
	})
	if err != nil {
		return "", fmt.Errorf("activity: writing a note: %w", err)
	}
	return id, nil
}
