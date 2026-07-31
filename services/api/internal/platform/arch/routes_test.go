package arch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Every route states its authorisation. ADR-0020, dwellm8#150.
//
// The real enforcement is the type system: authz.Registrar has no method that
// takes a handler without a Check or a reasoned Open. What this test closes is
// the bypass — a module or surface registering on *http.ServeMux directly,
// which would compile fine and never meet the guard. Registration on a raw mux
// belongs to exactly two places: cmd (which wires the registrars) and the
// platform packages (health, and authz itself).
func TestARouteDeclaresItsAuthorisation(t *testing.T) {
	root := repoInternalDir(t)
	watched := []string{filepath.Join(root, "surface")}
	for _, mod := range modules {
		watched = append(watched, filepath.Join(root, mod, "http"))
	}

	for _, dir := range watched {
		if _, err := os.Stat(dir); err != nil {
			continue
		}
		err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			src, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(root, path)
			for _, needle := range []string{"*http.ServeMux", "http.NewServeMux", ".HandleFunc("} {
				if strings.Contains(string(src), needle) {
					t.Errorf("%s uses %s\n\troutes register through authz.Registrar, which requires each "+
						"pattern to declare its check or the reason it has none (ADR-0020) — a raw mux "+
						"is a route the guard never sees", rel, needle)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
