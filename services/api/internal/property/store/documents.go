package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
)

// Document is one row of the documents table (300_documents.sql) — a record
// that an upload happened and which property it belongs to. The object
// itself lives in GCS (internal/platform/blob); this is what makes a bucket
// listing into an authorization boundary.
type Document struct {
	ID          string
	PropertyID  string
	ObjectPath  string
	Filename    string
	ContentType string
	UploadedBy  string
	CreatedAt   string // RFC3339 — this module has no reason to hand out a time.Time a caller could misformat
}

// Documents lists what's on file for a property, newest first.
func (s *Properties) Documents(ctx context.Context, propertyID string) ([]Document, error) {
	var out []Document
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT id::text, property_id::text, object_path, filename, content_type,
			       uploaded_by, to_char(created_at, 'YYYY-MM-DD"T"HH24:MI:SSOF')
			  FROM documents
			 WHERE property_id = $1::uuid
			 ORDER BY created_at DESC`, propertyID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var d Document
			if err := rows.Scan(&d.ID, &d.PropertyID, &d.ObjectPath, &d.Filename,
				&d.ContentType, &d.UploadedBy, &d.CreatedAt); err != nil {
				return err
			}
			out = append(out, d)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("property: listing documents for %s: %w", propertyID, err)
	}
	return out, nil
}

// RecordDocument writes the row confirming an upload landed at objectPath.
// Called after the client's PUT succeeds — there is no GCS event notification
// wired in Phase 1, so this is the client attesting to a fact rather than the
// system observing one; a client that uploads and never confirms leaves
// nothing behind but an unreferenced object, which is the failure mode worth
// having here.
func (s *Properties) RecordDocument(ctx context.Context, tenantID, propertyID, objectPath, filename, contentType, uploadedBy string) (string, error) {
	var id string
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			INSERT INTO documents (tenant_id, property_id, object_path, filename, content_type, uploaded_by)
			VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6)
			RETURNING id::text`,
			tenantID, propertyID, objectPath, filename, contentType, uploadedBy).Scan(&id)
	})
	if err != nil {
		return "", fmt.Errorf("property: recording a document on %s: %w", propertyID, err)
	}
	return id, nil
}
