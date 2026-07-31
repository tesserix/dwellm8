package arch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The line between a machine's privileged path and a person's. Issue #226.
//
// tenancy.Platform() is legitimate for paths with no human actor: the webhook
// inbox records a delivery before it knows whose money it concerns,
// reconciliation sweeps every organisation, the harness seeds fixtures. Auditing
// those would bury the rows that matter under the rows that do not.
//
// A module's *handlers* are the other case. Anything reachable from a request is
// something a person did, and tenancy.Support() is the one that cannot be called
// without naming who, to what and why. Without this test the distinction lasts
// until the first time Support() is inconvenient.
func TestNoHandlerTakesTheUnauditedPlatformPath(t *testing.T) {
	root := repoInternalDir(t)

	for _, mod := range modules {
		httpDir := filepath.Join(root, mod, "http")
		if _, err := os.Stat(httpDir); err != nil {
			continue // module has no handlers yet
		}

		err := filepath.WalkDir(httpDir, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
				return err
			}
			if strings.HasSuffix(path, "_test.go") {
				return nil
			}
			src, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if strings.Contains(string(src), "tenancy.Platform(") {
				rel, _ := filepath.Rel(root, path)
				t.Errorf("%s calls tenancy.Platform()\n\ta handler is something a person did, so it "+
					"takes tenancy.Support(), which cannot be called without naming who, to what "+
					"and why — Platform() is for paths with no human actor (issue #226)", rel)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walking %s: %v", httpDir, err)
		}
	}
}
