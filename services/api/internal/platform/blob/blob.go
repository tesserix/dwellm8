// Package blob is GCS-backed storage for documents and photos, isolated per
// app and per organisation.
//
// Isolation has two layers, because this API is one deployable serving all
// six apps (ADR-0001) — there is no per-app pod to hand a separate service
// account to. The first layer is the bucket: one per auth.Surface
// ("dwellm8-own-assets", "dwellm8-live-assets", ...), so a bug in one app's
// upload path cannot even name an object in another app's bucket. The second
// is the object path, prefixed by organisation id — the isolation RLS gives
// every other table in this schema, applied here in application code because
// GCS has no row-level security to inherit.
//
// No exported service-account key ever touches this process. It signs V4
// URLs by asking the IAM Credentials API to sign on behalf of a dedicated
// signer service account, which its own Workload Identity SA may impersonate
// (roles/iam.serviceAccountTokenCreator) — see the provisioning commands
// this feature shipped with. A leaked pod credential can mint upload URLs
// for its own app's bucket and nothing else; it can never export a key.
package blob

import (
	"context"
	"fmt"
	"path"
	"time"

	credentials "cloud.google.com/go/iam/credentials/apiv1"
	"cloud.google.com/go/iam/credentials/apiv1/credentialspb"
	"cloud.google.com/go/storage"
	"github.com/google/uuid"
	"github.com/tesserix/dwellm8/services/api/internal/platform/auth"
)

// uploadTTL and downloadTTL bound how long a signed URL is usable. Short: a
// URL is a bearer credential for exactly one object, and there is no revoking
// one once it exists.
const (
	uploadTTL   = 10 * time.Minute
	downloadTTL = 10 * time.Minute
)

// DownloadTTL is downloadTTL, so a caller sharing a link can say how long it
// lives rather than guess (#350).
const DownloadTTL = downloadTTL

// Store mints signed URLs against per-app GCS buckets.
type Store struct {
	client       *storage.Client
	signer       *credentials.IamCredentialsClient
	signerEmail  string
	bucketPrefix string
}

// New dials both clients under ambient Workload Identity credentials — never
// a key file. Non-fatal to construct with bad network reachability; the
// first call surfaces that, the same way the NATS connection in main.go does.
func New(ctx context.Context, bucketPrefix, signerEmail string) (*Store, error) {
	client, err := storage.NewClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("blob: storage client: %w", err)
	}
	signer, err := credentials.NewIamCredentialsClient(ctx)
	if err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("blob: iam credentials client: %w", err)
	}
	return &Store{client: client, signer: signer, signerEmail: signerEmail, bucketPrefix: bucketPrefix}, nil
}

// Close releases both clients.
func (s *Store) Close() error {
	err := s.client.Close()
	if sErr := s.signer.Close(); sErr != nil && err == nil {
		err = sErr
	}
	return err
}

// Bucket names the app's bucket. Exported so provisioning and this package
// agree on the name from one formula rather than two copies of it.
func (s *Store) Bucket(surface auth.Surface) string {
	return fmt.Sprintf("%s-%s-assets", s.bucketPrefix, surface)
}

// UploadURL mints a short-lived V4 PUT URL for a new object and the path it
// will land at. The object path is chosen here, not accepted from the
// caller — a client-supplied path is a client-chosen location to overwrite
// in another organisation's prefix, and this way there is no such path to
// supply.
func (s *Store) UploadURL(ctx context.Context, surface auth.Surface, orgID, filename, contentType string) (uploadURL, objectPath string, err error) {
	objectPath = fmt.Sprintf("org/%s/documents/%s%s", orgID, uuid.NewString(), path.Ext(filename))
	uploadURL, err = s.sign(ctx, surface, objectPath, "PUT", contentType, uploadTTL)
	return uploadURL, objectPath, err
}

// Put writes an object this service generated itself — the management
// agreement it prints (#340), not anything a client sent. The path is chosen
// here for the same reason UploadURL chooses it.
func (s *Store) Put(ctx context.Context, surface auth.Surface, orgID, filename, contentType string, data []byte) (string, error) {
	objectPath := fmt.Sprintf("org/%s/documents/%s%s", orgID, uuid.NewString(), path.Ext(filename))
	w := s.client.Bucket(s.Bucket(surface)).Object(objectPath).NewWriter(ctx)
	w.ContentType = contentType
	if _, err := w.Write(data); err != nil {
		_ = w.Close()
		return "", fmt.Errorf("blob: writing %s: %w", objectPath, err)
	}
	if err := w.Close(); err != nil {
		return "", fmt.Errorf("blob: closing %s: %w", objectPath, err)
	}
	return objectPath, nil
}

// Exists says whether the object is really there. Signing is arithmetic and
// succeeds for an object that was never uploaded, which is how the template
// library handed out links to a NoSuchKey page (#350).
func (s *Store) Exists(ctx context.Context, surface auth.Surface, objectPath string) bool {
	_, err := s.client.Bucket(s.Bucket(surface)).Object(objectPath).Attrs(ctx)
	return err == nil
}

// DownloadURL mints a short-lived V4 GET URL for an existing object.
func (s *Store) DownloadURL(ctx context.Context, surface auth.Surface, objectPath string) (string, error) {
	return s.sign(ctx, surface, objectPath, "GET", "", downloadTTL)
}

func (s *Store) sign(ctx context.Context, surface auth.Surface, objectPath, method, contentType string, ttl time.Duration) (string, error) {
	opts := &storage.SignedURLOptions{
		GoogleAccessID: s.signerEmail,
		Method:         method,
		Expires:        time.Now().Add(ttl),
		Scheme:         storage.SigningSchemeV4,
		SignBytes: func(b []byte) ([]byte, error) {
			resp, err := s.signer.SignBlob(ctx, &credentialspb.SignBlobRequest{
				Name:    "projects/-/serviceAccounts/" + s.signerEmail,
				Payload: b,
			})
			if err != nil {
				return nil, fmt.Errorf("blob: signing via IAM: %w", err)
			}
			return resp.GetSignedBlob(), nil
		},
	}
	if contentType != "" {
		opts.ContentType = contentType
	}
	url, err := s.client.Bucket(s.Bucket(surface)).SignedURL(objectPath, opts)
	if err != nil {
		return "", fmt.Errorf("blob: signing a %s URL for %s: %w", method, objectPath, err)
	}
	return url, nil
}
