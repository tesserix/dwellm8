package arch

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ADR-0015 §8. Two guards, and they prevent different failures.
//
// The first confines the Temporal SDK to one adapter package, for the same reason
// ADR-0011 confines Razorpay to internal/money/provider: an SDK that leaks into
// domain code takes the domain's testability with it, and the leak happens one
// convenient import at a time.
//
// The second is about determinism, and it prevents a failure with no error in it. A
// Temporal workflow is replayed from its history after a worker restart, and every
// decision it makes must come out the same way. A wall-clock read, a random number
// or a map iteration produces a different decision on replay, and Temporal reports
// a non-determinism error — sometimes. When it does not, the workflow simply takes
// a different branch than it did the first time, which for a payout means the
// second half of a compensation running against a world that never had the first.

// Only this package may speak to Temporal. It does not exist yet — the SDK arrives
// with the first workflow implementation (#80) — and naming it now is the point:
// the seam is decided before there is pressure to put the import somewhere
// convenient.
const temporalAdapter = "platform/workflow/temporalx"

// The SDK, and everything that comes with it.
var temporalImportPrefixes = []string{
	"go.temporal.io/",
}

func TestTheTemporalSDKIsConfinedToOneAdapter(t *testing.T) {
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
		pkgDir := filepath.ToSlash(filepath.Dir(rel))
		if pkgDir == temporalAdapter {
			return nil // the one place it belongs
		}

		for _, imp := range file.Imports {
			p := strings.Trim(imp.Path.Value, `"`)
			for _, prefix := range temporalImportPrefixes {
				if strings.HasPrefix(p, prefix) {
					at := fset.Position(imp.Pos())
					t.Errorf("%s:%d: imports %q — the Temporal SDK belongs in internal/%s and nowhere "+
						"else, so the durable-operation standard stays testable without a cluster "+
						"(ADR-0015 §1)", rel, at.Line, p, temporalAdapter)
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
	t.Logf("checked %d files; the SDK is permitted only in internal/%s", checked, temporalAdapter)
}

// Packages whose code runs inside a replayed workflow. Scoped by directory rather
// than by a naming convention, because a convention is what somebody is about to
// not follow.
var deterministicPackages = []string{
	"platform/workflow",
}

// What a replayed workflow may not do. The message on each says what breaks,
// because "non-determinism" is not a thing anybody can act on at 3am.
var nonDeterministic = map[string]string{
	"time.Now":       "reads the wall clock, which differs on replay — use the workflow's own clock",
	"time.Since":     "reads the wall clock",
	"time.Sleep":     "blocks a workflow goroutine instead of yielding a durable timer",
	"time.After":     "creates a timer the replayer knows nothing about",
	"time.Tick":      "creates a timer the replayer knows nothing about",
	"time.NewTimer":  "creates a timer the replayer knows nothing about",
	"time.NewTicker": "creates a timer the replayer knows nothing about",
	"rand.Int":       "is random, so replay takes a different branch",
	"rand.Intn":      "is random, so replay takes a different branch",
	"rand.Int63":     "is random, so replay takes a different branch",
	"rand.Float64":   "is random, and is a float in a money path besides",
	"uuid.New":       "generates a new id on every replay, so a retry becomes a second request",
	"uuid.NewString": "generates a new id on every replay, so a retry becomes a second request",
}

// Imports that cannot appear at all: there is no determinism-safe way to use them
// from inside a workflow, so the import itself is the finding.
var forbiddenInWorkflows = map[string]string{
	"math/rand":               "a workflow that branches on a random number branches differently on replay",
	"math/rand/v2":            "a workflow that branches on a random number branches differently on replay",
	"database/sql":            "a workflow may not do I/O — that is what an activity is for",
	"net/http":                "a workflow may not do I/O — that is what an activity is for",
	"os":                      "a workflow may not read the environment; configuration is an input",
	"github.com/jackc/pgx/v5": "a workflow may not touch the database — that is what an activity is for",
}

func TestNothingInAWorkflowReadsAClockOrRollsADie(t *testing.T) {
	root := repoInternalDir(t)
	fset := token.NewFileSet()
	var checked int

	for _, pkg := range deterministicPackages {
		dir := filepath.Join(root, filepath.FromSlash(pkg))
		if _, err := os.Stat(dir); err != nil {
			t.Fatalf("%s does not exist, so this guard is checking nothing", pkg)
		}

		err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
				return err
			}
			// Tests are excluded, and that is a real difference from ADR-0007's
			// float guard, which includes them deliberately. A test is not replayed
			// by Temporal, so a clock read in one is not this bug — and a test that
			// needs to assert on a timeout has to be able to name a duration.
			if strings.HasSuffix(path, "_test.go") {
				return nil
			}
			file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
			if err != nil {
				return err
			}
			checked++
			rel, _ := filepath.Rel(root, path)

			report := func(pos token.Pos, format string, args ...any) {
				at := fset.Position(pos)
				t.Errorf("%s:%d:%d: %s — ADR-0015 §3: a workflow is replayed, and every decision it "+
					"makes must come out the same way", rel, at.Line, at.Column,
					fmt.Sprintf(format, args...))
			}

			for _, imp := range file.Imports {
				p := strings.Trim(imp.Path.Value, `"`)
				if why, bad := forbiddenInWorkflows[p]; bad {
					report(imp.Pos(), "imports %q, and %s", p, why)
				}
			}

			ast.Inspect(file, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				pkgIdent, ok := sel.X.(*ast.Ident)
				if !ok {
					return true
				}
				name := pkgIdent.Name + "." + sel.Sel.Name
				if why, bad := nonDeterministic[name]; bad {
					report(call.Pos(), "calls %s(), which %s", name, why)
				}
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatalf("walking %s: %v", pkg, err)
		}
	}

	if checked == 0 {
		t.Fatal("no Go files were checked, so this guard proves nothing")
	}
	t.Logf("checked %d file(s) across %v", checked, deterministicPackages)
}
