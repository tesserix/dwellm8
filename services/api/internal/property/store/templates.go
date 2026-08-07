package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
)

// ErrNoTemplate is a template this organisation cannot see, or none of that
// kind at all — the same answer either way (#341).
var ErrNoTemplate = errors.New("property: no such document template")

// Template is one instrument a firm issues (353_document_templates.sql). The
// .docx lives in the bucket; this is the record of which instrument it is and
// what it declares.
type Template struct {
	ID           string
	Kind         string
	Jurisdiction string
	Name         string
	Version      int
	IsDefault    bool
	ObjectPath   string
	Filename     string
	ContentType  string
	MergeFields  []string
	PublishedAt  string
	SupersededAt string
	UploadedBy   string
}

// Templates reads and revises the instrument library.
type Templates struct {
	pool tenancy.Pool
}

func NewTemplates(pool tenancy.Pool) *Templates {
	return &Templates{pool: pool}
}

const selectTemplate = `
	SELECT id::text, kind, jurisdiction, name, version, is_default,
	       object_path, filename, content_type, merge_fields,
	       coalesce(to_char(published_at, 'YYYY-MM-DD"T"HH24:MI:SSOF'), ''),
	       coalesce(to_char(superseded_at, 'YYYY-MM-DD"T"HH24:MI:SSOF'), ''),
	       uploaded_by
	  FROM document_templates`

// Live is what the firm issues today: its own revision of an instrument where
// it has one, and the platform's where it has not. DISTINCT ON does the
// resolution in the database, because a firm reading its own list and the
// generator picking a template must not answer this differently.
func (s *Templates) Live(ctx context.Context, kind, jurisdiction string) ([]Template, error) {
	return s.query(ctx, selectTemplate+`
		 WHERE superseded_at IS NULL
		   AND published_at IS NOT NULL
		   AND jurisdiction = $1
		   AND ($2 = '' OR kind = $2)
		 ORDER BY kind, is_default, version DESC`, true, jurisdiction, kind)
}

// History is every version of one instrument, newest first — what was issued
// before, beside what is issued now.
func (s *Templates) History(ctx context.Context, kind, jurisdiction string) ([]Template, error) {
	return s.query(ctx, selectTemplate+`
		 WHERE jurisdiction = $1
		   AND ($2 = '' OR kind = $2)
		 ORDER BY kind, is_default, version DESC`, false, jurisdiction, kind)
}

func (s *Templates) query(ctx context.Context, sql string, oncePerKind bool, args ...any) ([]Template, error) {
	var out []Template
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, sql, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		seen := map[string]bool{}
		for rows.Next() {
			var t Template
			if err := rows.Scan(&t.ID, &t.Kind, &t.Jurisdiction, &t.Name, &t.Version,
				&t.IsDefault, &t.ObjectPath, &t.Filename, &t.ContentType, &t.MergeFields,
				&t.PublishedAt, &t.SupersededAt, &t.UploadedBy); err != nil {
				return err
			}
			if oncePerKind {
				if seen[t.Kind] {
					continue
				}
				seen[t.Kind] = true
			}
			out = append(out, t)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("property: reading document templates: %w", err)
	}
	return out, nil
}

// Get reads one template by id, the firm's own or the library's.
func (s *Templates) Get(ctx context.Context, id string) (Template, error) {
	var t Template
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, selectTemplate+` WHERE id = $1::uuid`, id).Scan(
			&t.ID, &t.Kind, &t.Jurisdiction, &t.Name, &t.Version, &t.IsDefault,
			&t.ObjectPath, &t.Filename, &t.ContentType, &t.MergeFields,
			&t.PublishedAt, &t.SupersededAt, &t.UploadedBy)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Template{}, ErrNoTemplate
	}
	if err != nil {
		return Template{}, fmt.Errorf("property: reading document template %s: %w", id, err)
	}
	return t, nil
}

// Revise publishes a firm's own version of an instrument, superseding whatever
// the firm had live. One transaction, because between the supersede and the
// insert the firm issues nothing of that kind at all.
func (s *Templates) Revise(ctx context.Context, tenantID string, t Template) (Template, error) {
	var out Template
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			UPDATE document_templates SET superseded_at = now()
			 WHERE tenant_id = $1::uuid AND kind = $2 AND jurisdiction = $3
			   AND NOT is_default AND superseded_at IS NULL`,
			tenantID, t.Kind, t.Jurisdiction); err != nil {
			return err
		}
		// The version counts on from the highest anyone has issued of this
		// instrument, the platform's included: a firm's first revision of a
		// v3 default is a v4, not a second v1.
		return tx.QueryRow(ctx, `
			INSERT INTO document_templates
			    (tenant_id, kind, jurisdiction, name, version, object_path, filename,
			     content_type, merge_fields, published_at, uploaded_by)
			VALUES ($1::uuid, $2, $3, $4,
			        (SELECT coalesce(max(version), 0) + 1 FROM document_templates
			          WHERE kind = $2 AND jurisdiction = $3),
			        $5, $6, $7, $8, now(), $9)
			RETURNING id::text, kind, jurisdiction, name, version, is_default,
			          object_path, filename, content_type, merge_fields,
			          to_char(published_at, 'YYYY-MM-DD"T"HH24:MI:SSOF'), '', uploaded_by`,
			tenantID, t.Kind, t.Jurisdiction, t.Name, t.ObjectPath, t.Filename,
			t.ContentType, t.MergeFields, t.UploadedBy).Scan(
			&out.ID, &out.Kind, &out.Jurisdiction, &out.Name, &out.Version, &out.IsDefault,
			&out.ObjectPath, &out.Filename, &out.ContentType, &out.MergeFields,
			&out.PublishedAt, &out.SupersededAt, &out.UploadedBy)
	})
	if err != nil {
		return Template{}, fmt.Errorf("property: revising the %s template: %w", t.Kind, err)
	}
	return out, nil
}
