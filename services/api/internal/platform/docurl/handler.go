package docurl

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/tesserix/dwellm8/services/api/internal/platform/authz"
)

// Source is where the bytes come from. The eSign flow (#62) supplies the
// production one; until it does, no valid grant can exist either, because
// issuance lives in the same flow.
type Source interface {
	// Open returns the document and its content type. Whether the ref names
	// anything must not be readable in the response — the handler answers the
	// one refusal regardless.
	Open(ctx context.Context, ref string) (data []byte, contentType string, err error)
}

// Grants is the slice of Store the handler needs — an interface so the test
// can watch exactly what gets recorded, which for this handler is most of
// what it must get right.
type Grants interface {
	Revoked(ctx context.Context, g Grant) (bool, error)
	Log(ctx context.Context, g Grant, sourceIP, userAgent, outcome string) error
}

var _ Grants = (*Store)(nil)

// Handler serves the URL an ESP presents to a signer. It sits outside
// authentication like the webhook route, and for the same shape of reason:
// the caller is not a sign-in, and the token is the credential.
type Handler struct {
	signer *Signer
	grants Grants
	source Source
	log    *slog.Logger
	now    func() time.Time // the clock, injectable for the expiry tests
}

func NewHandler(s *Signer, g Grants, src Source, log *slog.Logger) *Handler {
	return &Handler{signer: s, grants: g, source: src, log: log, now: time.Now}
}

func (h *Handler) Routes(r *authz.Registrar) {
	r.Open("GET /v1/esign/documents/{token}",
		"the token is the credential: an HMAC grant scoped to one document and one eSign transaction, expiring with the signing window (#212)",
		h.fetch)
}

func (h *Handler) fetch(w http.ResponseWriter, r *http.Request) {
	// On every answer, refusals included. no-store because an intermediary
	// that keeps the body outlives the window the whole design is about, and
	// no-referrer because the URL itself is the thing that must not travel.
	hdr := w.Header()
	hdr.Set("Cache-Control", "no-store")
	hdr.Set("Referrer-Policy", "no-referrer")
	hdr.Set("X-Content-Type-Options", "nosniff")
	hdr.Set("X-Robots-Tag", "noindex")

	token := r.PathValue("token")
	g, err := h.signer.Parse(token, h.now())
	if err != nil {
		// Expired-but-genuine still names a transaction we issued for, and an
		// attempt after the window is exactly what the log is for. Tampered
		// and forged name nothing and are recorded by nobody — attributing
		// them would let a stranger write rows into a tenant's evidence.
		if att, ok := h.signer.Attribution(token); ok {
			if lerr := h.grants.Log(r.Context(), att, clientIP(r), r.UserAgent(), "refused"); lerr != nil {
				h.log.Error("docurl: recording a refused fetch", "txn", att.TxnID, "error", lerr)
			}
		}
		h.refuse(w)
		return
	}

	revoked, err := h.grants.Revoked(r.Context(), g)
	if err != nil {
		// Fail closed: a revocation list that cannot be read might be hiding a
		// revocation. The signer retries; the alternative serves a document
		// after the window was closed on purpose.
		h.log.Error("docurl: checking revocation", "txn", g.TxnID, "error", err)
		h.refuse(w)
		return
	}
	if revoked {
		if lerr := h.grants.Log(r.Context(), g, clientIP(r), r.UserAgent(), "refused"); lerr != nil {
			h.log.Error("docurl: recording a refused fetch", "txn", g.TxnID, "error", lerr)
		}
		h.refuse(w)
		return
	}

	if h.source == nil {
		// Wired before #62 supplies a source. A valid grant arriving here means
		// issuance exists without serving — a wiring bug, worth a loud line.
		h.log.Error("docurl: a live grant arrived and no document source is wired", "txn", g.TxnID)
		h.refuse(w)
		return
	}
	data, contentType, err := h.source.Open(r.Context(), g.DocumentRef)
	if err != nil {
		h.log.Error("docurl: opening the document", "txn", g.TxnID, "error", err)
		if lerr := h.grants.Log(r.Context(), g, clientIP(r), r.UserAgent(), "refused"); lerr != nil {
			h.log.Error("docurl: recording a refused fetch", "txn", g.TxnID, "error", lerr)
		}
		h.refuse(w)
		return
	}

	// Recorded before the first byte leaves, and a fetch that cannot be
	// recorded is not served — "every fetch logged" is the accountability the
	// design trades openness for, so it cannot be best-effort.
	if err := h.grants.Log(r.Context(), g, clientIP(r), r.UserAgent(), "served"); err != nil {
		h.log.Error("docurl: recording a fetch", "txn", g.TxnID, "error", err)
		h.refuse(w)
		return
	}

	hdr.Set("Content-Type", contentType)
	hdr.Set("Content-Disposition", "inline")
	hdr.Set("Content-Length", strconv.Itoa(len(data)))
	_, _ = w.Write(data)
}

// refuse is the one answer for expired, revoked, tampered, forged and
// never-issued alike — same status, same body, per #212's failure scenario.
func (h *Handler) refuse(w http.ResponseWriter) {
	http.Error(w, `{"error":"this link is not usable"}`, http.StatusNotFound)
}

// clientIP prefers the first X-Forwarded-For hop, which is what the ingress
// records; a bare RemoteAddr behind a proxy would log the proxy forever.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		first, _, _ := strings.Cut(xff, ",")
		return strings.TrimSpace(first)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
