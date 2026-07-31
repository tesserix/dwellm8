package arch

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The broker gets the same treatment as Temporal, for the same reason: the
// outbox and the relay are where the interesting failures live — a claim that
// double-publishes, a backoff that never fires, a row marked published that was
// not — and none of them need a running NATS to be wrong. Confining the SDK to
// the adapter keeps that logic testable on a laptop. ADR-0002 §4.
const natsAdapter = "platform/events/natsx"

var natsImportPrefixes = []string{
	"github.com/nats-io/",
}

func TestTheNATSSDKIsConfinedToOneAdapter(t *testing.T) {
	root := repoInternalDir(t)
	fset := token.NewFileSet()
	var checked int

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		file, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		checked++
		rel, _ := filepath.Rel(root, path)
		if filepath.ToSlash(filepath.Dir(rel)) == natsAdapter {
			return nil
		}

		for _, imp := range file.Imports {
			p := strings.Trim(imp.Path.Value, `"`)
			for _, prefix := range natsImportPrefixes {
				if strings.HasPrefix(p, prefix) {
					at := fset.Position(imp.Pos())
					t.Errorf("%s:%d: imports %q — the NATS SDK belongs in internal/%s and nowhere "+
						"else, so the outbox and relay stay testable without a broker (ADR-0002 §4)",
						rel, at.Line, p, natsAdapter)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking internal/: %v", err)
	}
	if checked == 0 {
		t.Fatal("no Go files were checked, so this guard proves nothing")
	}
}

// A module that writes its own events straight to the broker has re-created the
// gap the outbox exists to close: the state change commits, the publish fails,
// and the fact is gone. Modules append to the outbox; only the relay publishes.
func TestOnlyTheRelayPublishes(t *testing.T) {
	root := repoInternalDir(t)
	fset := token.NewFileSet()

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		pkgDir := filepath.ToSlash(filepath.Dir(rel))
		// The adapter implements publishing; the events package owns the relay.
		if pkgDir == natsAdapter || pkgDir == "platform/events" {
			return nil
		}
		file, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, imp := range file.Imports {
			p := strings.Trim(imp.Path.Value, `"`)
			if strings.HasSuffix(p, "/internal/platform/events/natsx") {
				at := fset.Position(imp.Pos())
				t.Errorf("%s:%d: imports the JetStream adapter — a module appends to the outbox "+
					"and lets the relay publish, so an event and the state change it describes "+
					"commit together (ADR-0002 §3)", rel, at.Line)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking internal/: %v", err)
	}
}
