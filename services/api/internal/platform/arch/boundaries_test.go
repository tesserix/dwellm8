// Package arch keeps ADR-0001's boundaries honest.
//
// The rule it enforces is the one the whole architecture rests on: a module may
// call another module's service interface, and may not touch its store. Without
// this test the modular monolith quietly becomes the unstructured monolith the
// ADR rejected — and it does so one convenient import at a time.
package arch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var modules = []string{
	"identity", "property", "lease", "money",
	"maintenance", "community", "discovery", "notify",
}

const modulePrefix = "github.com/tesserix/dwellm8/services/api/internal/"

func TestNoModuleImportsAnotherModulesStoreOrDomain(t *testing.T) {
	root := repoInternalDir(t)

	for _, mod := range modules {
		modDir := filepath.Join(root, mod)
		if _, err := os.Stat(modDir); err != nil {
			continue // module not written yet
		}

		err := filepath.WalkDir(modDir, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
				return err
			}
			src, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for _, other := range modules {
				if other == mod {
					continue
				}
				for _, forbidden := range []string{"/store", "/domain", "/events"} {
					needle := modulePrefix + other + forbidden
					if strings.Contains(string(src), needle) {
						rel, _ := filepath.Rel(root, path)
						t.Errorf(
							"%s imports %s\n\t%s may only reach %s through %s%s/service — see ADR-0001 §3",
							rel, needle, mod, other, modulePrefix, other,
						)
					}
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walking %s: %v", mod, err)
		}
	}
}

func TestDomainDoesNotImportAFramework(t *testing.T) {
	// Aggregates and rules must be testable without a server or a database.
	banned := []string{
		"github.com/gin-gonic/gin",
		"net/http",
		"database/sql",
		"github.com/jackc/pgx",
	}
	root := repoInternalDir(t)

	for _, mod := range modules {
		domainDir := filepath.Join(root, mod, "domain")
		if _, err := os.Stat(domainDir); err != nil {
			continue
		}
		err := filepath.WalkDir(domainDir, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
				return err
			}
			src, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for _, b := range banned {
				if strings.Contains(string(src), `"`+b+`"`) {
					rel, _ := filepath.Rel(root, path)
					t.Errorf("%s imports %q — domain holds rules, not plumbing (ADR-0001 §5)", rel, b)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walking %s/domain: %v", mod, err)
		}
	}
}

// repoInternalDir returns services/api/internal, whatever directory the test runs from.
func repoInternalDir(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	// .../internal/platform/arch -> .../internal
	return filepath.Dir(filepath.Dir(wd))
}
