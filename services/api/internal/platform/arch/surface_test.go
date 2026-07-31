package arch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Surfaces are above the modules. ADR-0001 §3, extended by ADR-0029.
//
// A surface composes several modules into one screen — the tenant view is the
// first, and it needs identity, lease and money at once. That is a legitimate
// thing to want and an illegitimate thing to put inside any of the three: money
// importing lease and identity to render a page would couple the three rule-sets
// permanently, which is the coupling ADR-0001 exists to prevent.
//
// So a surface is a caller with no rules of its own, and the boundary it must
// respect is the same one modules respect: it reaches a module through its
// service package. A surface that reached into a store would be a second,
// unowned writer of somebody else's table, and it would not show up as a
// dependency anybody reviews.
//
// Domain is permitted here and is not permitted between modules, and the
// difference is real: a module importing another's domain adopts its rules, and
// a surface naming a domain type is a caller spelling out the argument the
// service signature already asks for. money/service.CollectRequest holds a
// domain.Minor; there is no way to call it without naming one.
func TestASurfaceReachesModulesThroughServiceOnly(t *testing.T) {
	root := repoInternalDir(t)
	surfaceDir := filepath.Join(root, "surface")
	if _, err := os.Stat(surfaceDir); err != nil {
		t.Skip("no surfaces yet")
	}

	err := filepath.WalkDir(surfaceDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		// A test wires the surface to real stores, which is the same act main()
		// performs and is the only way to exercise it against a database. The
		// rule is about the shipped dependency graph.
		if strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		for _, mod := range modules {
			for _, forbidden := range []string{"/events"} {
				needle := modulePrefix + mod + forbidden
				if strings.Contains(string(src), needle) {
					t.Errorf("%s imports %s\n\ta surface composes modules through %s%s/service — "+
						"reaching past it makes the surface a second, unreviewed writer of %s's own "+
						"vocabulary (ADR-0001 §3)", rel, needle, modulePrefix, mod, mod)
				}
			}
			// A store is the hard line. One exception is allowed and named: a
			// caller has to be able to recognise a module's sentinel errors, and
			// those live beside the queries that return them.
			needle := modulePrefix + mod + "/store"
			if strings.Contains(string(src), needle) && !errorsOnly(string(src), mod) {
				t.Errorf("%s imports %s and uses more than its sentinel errors\n\ta surface must not "+
					"query another module's tables (ADR-0001 §3)", rel, needle)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking surface/: %v", err)
	}
}

// errorsOnly reports whether a file's use of a module's store package is limited
// to its exported error values.
//
// Crude on purpose: it looks for `<mod>store.` uses and requires every one of
// them to name an identifier starting with Err. That catches the case worth
// catching — a surface reaching for a repository constructor or a query — and
// does not pretend to be a type checker.
func errorsOnly(src, mod string) bool {
	prefix := mod + "store."
	for rest := src; ; {
		i := strings.Index(rest, prefix)
		if i < 0 {
			return true
		}
		rest = rest[i+len(prefix):]
		if !strings.HasPrefix(rest, "Err") {
			return false
		}
	}
}

// A surface owns no rules, so it owns no domain package. The moment one appears,
// the surface has become a module and has to argue for itself in an ADR — with a
// role, an owner and a table — rather than growing one file at a time.
func TestASurfaceHasNoDomain(t *testing.T) {
	root := repoInternalDir(t)
	surfaceDir := filepath.Join(root, "surface")
	entries, err := os.ReadDir(surfaceDir)
	if err != nil {
		t.Skip("no surfaces yet")
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		for _, banned := range []string{"domain", "store", "events"} {
			if _, err := os.Stat(filepath.Join(surfaceDir, e.Name(), banned)); err == nil {
				t.Errorf("surface/%s has a %s/ package — a surface composes modules and owns nothing; "+
					"if it needs rules or a table it is a module, and ADR-0001 lists the modules",
					e.Name(), banned)
			}
		}
	}
}
