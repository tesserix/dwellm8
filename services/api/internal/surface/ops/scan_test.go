package ops

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tesserix/dwellm8/services/api/internal/identity/docscan"
)

// The scan endpoint hands a photograph to the recogniser and gives back fields
// for the manager to confirm (#318). It writes nothing down, so everything
// below is about what it returns and what it must not keep.

// An ICAO specimen, the same one the parser's own tests read.
const specimenMRZ = "P<UTOERIKSSON<<ANNA<MARIA<<<<<<<<<<<<<<<<<<<\n" +
	"L898902C36UTO7408122F1204159ZE184226B<<<<<10"

const specimenPAN = "INCOME TAX DEPARTMENT\nGOVT OF INDIA\n" +
	"Permanent Account Number\nABCPD1234E\nName\nANJALI SHARMA\n" +
	"Father's Name\nRAMESH SHARMA\nDate of Birth\n14/08/1985"

// engine stands in for Cloud Vision: it records what it was asked to read, so a
// test can prove a refusal happened before the billed call.
type engine struct {
	text  string
	err   error
	calls int
}

func (e *engine) Text(ctx context.Context, image []byte) (string, error) {
	e.calls++
	return e.text, e.err
}

func scanHandler(t *testing.T, e *engine) (*Handler, *bytes.Buffer) {
	t.Helper()
	var logged bytes.Buffer
	log := slog.New(slog.NewTextHandler(&logged, nil))
	return New(nil, nil, nil, nil, nil, log, nil).WithScanner(docscan.New(e)), &logged
}

func aPhotograph(n int) string {
	return base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0xff, 0xd8, 0xff, 0xe0}, n/4))
}

func scan(t *testing.T, h *Handler, body any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("encoding the request: %v", err)
	}
	r := httptest.NewRequest(http.MethodPost, "/v1/ops/identity/scan", bytes.NewReader(raw))
	w := httptest.NewRecorder()
	h.ScanDocument(w, r)
	return w
}

func TestAPassportScansIntoFieldsAFormCanPrefill(t *testing.T) {
	h, _ := scanHandler(t, &engine{text: specimenMRZ})

	w := scan(t, h, map[string]any{"kind": "passport", "image": aPhotograph(4 << 10)})
	if w.Code != http.StatusOK {
		t.Fatalf("scanning a passport: %d %s", w.Code, w.Body.String())
	}
	var out struct {
		Kind     string `json:"kind"`
		Passport struct {
			Surname     string `json:"surname"`
			GivenNames  string `json:"given_names"`
			Number      string `json:"number"`
			Nationality string `json:"nationality"`
			DateOfBirth string `json:"date_of_birth"`
			ExpiresOn   string `json:"expires_on"`
		} `json:"passport"`
	}
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	for _, c := range []struct{ what, want, got string }{
		{"kind", "passport", out.Kind},
		{"surname", "ERIKSSON", out.Passport.Surname},
		{"given names", "ANNA MARIA", out.Passport.GivenNames},
		{"number", "L898902C3", out.Passport.Number},
		{"nationality", "UTO", out.Passport.Nationality},
		{"date of birth", "1974-08-12", out.Passport.DateOfBirth},
		{"expiry", "2012-04-15", out.Passport.ExpiresOn},
	} {
		if c.got != c.want {
			t.Errorf("%s: got %q, want %q", c.what, c.got, c.want)
		}
	}
}

func TestAPANCardScansIntoFieldsAFormCanPrefill(t *testing.T) {
	h, _ := scanHandler(t, &engine{text: specimenPAN})

	w := scan(t, h, map[string]any{"kind": "pan", "image": aPhotograph(4 << 10)})
	if w.Code != http.StatusOK {
		t.Fatalf("scanning a PAN card: %d %s", w.Code, w.Body.String())
	}
	var out struct {
		Kind string `json:"kind"`
		PAN  struct {
			Number      string `json:"number"`
			Name        string `json:"name"`
			DateOfBirth string `json:"date_of_birth"`
			Individual  bool   `json:"individual"`
		} `json:"pan"`
	}
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if out.PAN.Number != "ABCPD1234E" {
		t.Errorf("number: got %q", out.PAN.Number)
	}
	if out.PAN.Name != "ANJALI SHARMA" {
		t.Errorf("name: got %q", out.PAN.Name)
	}
	if !out.PAN.Individual {
		t.Error("the fourth character is P and the card did not come back as an individual's")
	}
}

// The engine is billed per image and the image is somebody's identity document.
// Anything refusable must be refused before it is sent.
func TestAnOversizedImageIsRefusedBeforeTheEngineIsAsked(t *testing.T) {
	e := &engine{text: specimenPAN}
	h, _ := scanHandler(t, e)

	w := scan(t, h, map[string]any{"kind": "pan", "image": aPhotograph(docscan.MaxImageBytes + (4 << 10))})
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("an oversized image: %d %s", w.Code, w.Body.String())
	}
	if e.calls != 0 {
		t.Errorf("the image was sent to the engine anyway (%d calls)", e.calls)
	}
}

func TestAnImageThatIsNotAnImageIsRefused(t *testing.T) {
	e := &engine{text: specimenPAN}
	h, _ := scanHandler(t, e)

	for _, c := range []struct{ what, image string }{
		{"not base64", "this is not base64 !!!"},
		{"empty", ""},
		{"a few bytes", base64.StdEncoding.EncodeToString([]byte("nope"))},
	} {
		w := scan(t, h, map[string]any{"kind": "pan", "image": c.image})
		if w.Code < 400 || w.Code >= 500 {
			t.Errorf("%s: got %d %s", c.what, w.Code, w.Body.String())
		}
	}
	if e.calls != 0 {
		t.Errorf("something unreadable reached the engine (%d calls)", e.calls)
	}
}

func TestADocumentNobodyNamedIsRefused(t *testing.T) {
	h, _ := scanHandler(t, &engine{text: specimenPAN})

	for _, kind := range []string{"", "aadhaar", "driving_licence"} {
		w := scan(t, h, map[string]any{"kind": kind, "image": aPhotograph(4 << 10)})
		if w.Code != http.StatusUnprocessableEntity {
			t.Errorf("kind %q: got %d %s", kind, w.Code, w.Body.String())
		}
	}
}

// A half-read passport prefills a wrong number into a tax record. The parser
// refuses one; the surface must not turn that into a partial answer.
func TestADocumentThatDidNotReadCleanlyIsRefusedRatherThanGuessed(t *testing.T) {
	h, _ := scanHandler(t, &engine{text: "GOVT OF INDIA\nsomething that is not a card at all"})

	w := scan(t, h, map[string]any{"kind": "pan", "image": aPhotograph(4 << 10)})
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("an unreadable card: %d %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "not a card at all") {
		t.Errorf("the refusal quotes what the engine read: %s", w.Body.String())
	}
}

// The engine failing is ours, not the manager's, and its message may carry the
// text it had read so far.
func TestAnEngineFailureIsNotHandedBackToTheManager(t *testing.T) {
	h, logged := scanHandler(t, &engine{err: errors.New("vision: quota exhausted for ABCPD1234E")})

	w := scan(t, h, map[string]any{"kind": "pan", "image": aPhotograph(4 << 10)})
	if w.Code != http.StatusBadGateway {
		t.Fatalf("an engine failure: %d %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "quota") {
		t.Errorf("the engine's own message reached the manager: %s", w.Body.String())
	}
	if strings.Contains(logged.String(), "ABCPD1234E") {
		t.Errorf("an identifier was logged: %s", logged.String())
	}
}

// ADR-0013: what was read is shown back once and never written down.
func TestWhatWasReadIsNeitherLoggedNorCached(t *testing.T) {
	h, logged := scanHandler(t, &engine{text: specimenPAN})

	w := scan(t, h, map[string]any{"kind": "pan", "image": aPhotograph(4 << 10)})
	if w.Code != http.StatusOK {
		t.Fatalf("scanning: %d %s", w.Code, w.Body.String())
	}
	if strings.Contains(logged.String(), "ABCPD1234E") || strings.Contains(logged.String(), "ANJALI") {
		t.Errorf("the scan was logged: %s", logged.String())
	}
	if cc := w.Header().Get("Cache-Control"); !strings.Contains(cc, "no-store") {
		t.Errorf("an identity document came back cacheable: %q", cc)
	}
}

// A body larger than any legitimate image must not be buffered whole.
func TestABodyBeyondAnyImageIsRefusedWithoutBeingRead(t *testing.T) {
	e := &engine{text: specimenPAN}
	h, _ := scanHandler(t, e)

	huge := fmt.Sprintf(`{"kind":"pan","image":"%s"}`, strings.Repeat("A", scanBody+(1<<20)))
	r := httptest.NewRequest(http.MethodPost, "/v1/ops/identity/scan", strings.NewReader(huge))
	w := httptest.NewRecorder()
	h.ScanDocument(w, r)

	if w.Code < 400 || w.Code >= 500 {
		t.Fatalf("an unbounded body: %d", w.Code)
	}
	if e.calls != 0 {
		t.Errorf("it reached the engine (%d calls)", e.calls)
	}
}

func TestScanningIsNotOfferedWhenNoEngineIsConfigured(t *testing.T) {
	log := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	h := New(nil, nil, nil, nil, nil, log, nil)

	w := scan(t, h, map[string]any{"kind": "pan", "image": aPhotograph(4 << 10)})
	if w.Code != http.StatusNotFound {
		t.Fatalf("with no engine wired: %d %s", w.Code, w.Body.String())
	}
}
