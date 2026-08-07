package docscan_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/tesserix/dwellm8/services/api/internal/identity/docscan"
)

type fakeEngine struct {
	text string
	err  error
	saw  []byte
}

func (f *fakeEngine) Text(_ context.Context, image []byte) (string, error) {
	f.saw = image
	return f.text, f.err
}

func TestAScannedPassportComesBackAsFields(t *testing.T) {
	engine := &fakeEngine{text: mrzLine1 + "\n" + mrzLine2}

	got, err := docscan.New(engine).Passport(context.Background(), someImage())
	if err != nil {
		t.Fatalf("scanning: %v", err)
	}
	if got.Number != "L898902C3" {
		t.Errorf("number is %q", got.Number)
	}
	if len(engine.saw) == 0 {
		t.Error("the engine was never given the image")
	}
}

func TestAScannedPANCardComesBackAsFields(t *testing.T) {
	got, err := docscan.New(&fakeEngine{text: panCard}).PAN(context.Background(), someImage())
	if err != nil {
		t.Fatalf("scanning: %v", err)
	}
	if got.Number != "ABCPD1234E" || got.Name != "ANJALI MENON" {
		t.Errorf("read %+v", got)
	}
}

// A camera roll picture is megabytes and a hostile upload is more. The engine is
// billed per call and holds an identity document, so the guard is before it.
func TestAnImageTooLargeOrTooSmallNeverReachesTheEngine(t *testing.T) {
	for _, c := range []struct {
		what  string
		image []byte
	}{
		{"nothing at all", nil},
		{"a truncated upload", []byte("abc")},
		{"more than the ceiling", make([]byte, docscan.MaxImageBytes+1)},
	} {
		engine := &fakeEngine{text: panCard}
		if _, err := docscan.New(engine).PAN(context.Background(), c.image); err == nil {
			t.Errorf("%s was scanned anyway", c.what)
		}
		if engine.saw != nil {
			t.Errorf("%s reached the engine", c.what)
		}
	}
}

func TestAnEngineFailureIsPassedOnRatherThanReadAsAnEmptyDocument(t *testing.T) {
	boom := errors.New("the vision endpoint is down")

	_, err := docscan.New(&fakeEngine{err: boom}).PAN(context.Background(), someImage())
	if !errors.Is(err, boom) {
		t.Fatalf("the engine's failure reads as %v, which a caller cannot retry on", err)
	}
}

// Nothing this package returns may carry the document back with it: the image is
// the identifier, and an error string is logged.
func TestAnUnreadableDocumentIsReportedWithoutQuotingWhatWasRead(t *testing.T) {
	_, err := docscan.New(&fakeEngine{text: "ABCPD1234E is the number"}).Passport(
		context.Background(), someImage())
	if err == nil {
		t.Fatal("text with no machine-readable zone was read as a passport")
	}
	if strings.Contains(err.Error(), "ABCPD1234E") {
		t.Errorf("the error quotes the document: %v", err)
	}
}

func someImage() []byte { return make([]byte, 4096) }
