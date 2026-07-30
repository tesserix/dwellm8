package arch

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tesserix/dwellm8/services/api/internal/platform/pii"
)

// ADR-0013's failure scenario, on the Go side. The schema's assertion 15 refuses a
// column named after a prohibited identifier; this refuses a struct field, a JSON tag or
// a variable named for one, because the value has to be held in Go before it can reach a
// column and a field called AadhaarNumber is a field somebody will log.
//
// It is a name check and it is weak on its own — somebody determined calls it
// `NationalID`. The strong mechanism is that there is no column for a full identifier and
// the one that could hold one has a CHECK refusing anything but a mask. This covers the
// careless case, which is the common one: the vendor SDK returns `aadhaarNumber` and the
// obvious struct field follows it.
//
// The list is internal/platform/pii's, read by both this test and the schema assertion,
// so there is one copy.
func TestNothingIsNamedAfterAProhibitedIdentifier(t *testing.T) {
	root := repoInternalDir(t)
	fset := token.NewFileSet()
	prohibited := pii.ProhibitedColumnNames()
	var checked int

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		// internal/platform/pii names them itself, in the list and in the classification
		// of the kind. That is the one place the words belong.
		if strings.Contains(filepath.ToSlash(path), "/platform/pii/") {
			return nil
		}
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		checked++
		rel, _ := filepath.Rel(root, path)

		report := func(pos token.Pos, what, name string) {
			at := fset.Position(pos)
			t.Errorf("%s:%d:%d: %s %q is named after a prohibited identifier — the Aadhaar number "+
				"may not be held at rest in any form, and a field named for it is a field that "+
				"reaches a log (ADR-0013 §2)", rel, at.Line, at.Column, what, name)
		}

		// A name matches on a *token*, not a substring.
		//
		// The first version matched substrings and flagged every `Provider` in the money
		// module, because "provider" contains "vid" — the Aadhaar Virtual ID. A
		// three-letter token cannot be looked for inside arbitrary identifiers, and a
		// guard with false positives is a guard somebody deletes.
		//
		// So: split camelCase and snake_case into words, and match a token exactly. A
		// prohibited name of six characters or more is also matched as a substring of a
		// token, which catches `aadhaarnumber` written without a separator while staying
		// long enough not to collide with an English word.
		bad := func(name string) bool {
			for _, tok := range tokenise(name) {
				for _, p := range prohibited {
					if tok == p || (len(p) >= 6 && strings.Contains(tok, p)) {
						return true
					}
				}
			}
			return false
		}

		ast.Inspect(file, func(n ast.Node) bool {
			switch node := n.(type) {
			case *ast.Field:
				for _, nm := range node.Names {
					if bad(nm.Name) {
						report(nm.Pos(), "struct field", nm.Name)
					}
				}
				// And the JSON tag, which is what actually appears in a payload.
				if node.Tag != nil && bad(node.Tag.Value) {
					report(node.Tag.Pos(), "field tag", strings.Trim(node.Tag.Value, "`"))
				}
			case *ast.ValueSpec:
				for _, nm := range node.Names {
					if bad(nm.Name) {
						report(nm.Pos(), "identifier", nm.Name)
					}
				}
			case *ast.AssignStmt:
				for _, lhs := range node.Lhs {
					if id, ok := lhs.(*ast.Ident); ok && bad(id.Name) {
						report(id.Pos(), "variable", id.Name)
					}
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walking internal/: %v", err)
	}
	if checked == 0 {
		t.Fatal("no Go files were checked, so this guard proves nothing")
	}
	t.Logf("checked %d files against %d prohibited names", checked, len(prohibited))
}

// tokenise splits an identifier into lower-case words on camelCase boundaries and on any
// non-letter, so AadhaarNumber, aadhaar_no and `json:"aadhaar_number"` all yield an
// "aadhaar" token and Provider yields only "provider".
func tokenise(name string) []string {
	var out []string
	var cur strings.Builder
	flush := func() {
		if cur.Len() > 0 {
			out = append(out, strings.ToLower(cur.String()))
			cur.Reset()
		}
	}
	runes := []rune(name)
	for i, r := range runes {
		switch {
		case r >= 'A' && r <= 'Z':
			// A capital starts a new word, unless it is inside a run of capitals like ID.
			if i > 0 && !(runes[i-1] >= 'A' && runes[i-1] <= 'Z') {
				flush()
			}
			cur.WriteRune(r)
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			cur.WriteRune(r)
		default:
			flush()
		}
	}
	flush()
	return out
}
