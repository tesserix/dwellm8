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

// ADR-0007 §6. No float may appear anywhere in a money path.
//
// This is the guard the ADR's failure scenario names: a developer introduces a
// float64 in a money calculation and the build fails with the file and the line.
// It is a test rather than a linter config so it runs on a laptop the same way
// it runs in CI, and it parses the code rather than grepping it, so a `float64`
// inside a string or a comment is not a false positive and a `var x = 1.5` with
// no type named is not a false negative.
//
// The scope is the whole money module, tests included. A test that computes its
// expected value in floating point is not a lesser version of this bug; it is
// the bug, dressed as the thing that would have caught it.
const moneyModule = "money"

// Every way a float can get into a Go file.
func TestNoFloatInAMoneyPath(t *testing.T) {
	root := repoInternalDir(t)
	moneyDir := filepath.Join(root, moneyModule)
	if _, err := os.Stat(moneyDir); err != nil {
		t.Skip("money module not written yet")
	}

	fset := token.NewFileSet()
	var checked int

	err := filepath.WalkDir(moneyDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			return err
		}
		checked++
		rel, _ := filepath.Rel(root, path)

		report := func(pos token.Pos, what string) {
			at := fset.Position(pos)
			t.Errorf("%s:%d:%d: %s — money is int64 paise, and ADR-0007 §2 permits no float in this module",
				rel, at.Line, at.Column, what)
		}

		// An import of "math" brings Round, Floor, Ceil and Abs, all of which
		// take and return float64. math/bits and math/big are integer packages
		// and are allowed; mulDivRound is built on math/bits.
		for _, imp := range file.Imports {
			p := strings.Trim(imp.Path.Value, `"`)
			if p == "math" {
				report(imp.Pos(), fmt.Sprintf("imports %q, whose rounding is float rounding", p))
			}
		}

		ast.Inspect(file, func(n ast.Node) bool {
			switch node := n.(type) {
			case *ast.Ident:
				// The type, wherever it is named: a field, a parameter, a
				// return, a conversion, a type argument.
				if node.Name == "float32" || node.Name == "float64" {
					report(node.Pos(), "names "+node.Name)
				}
			case *ast.BasicLit:
				// 1.5, .5, 1e3 — a float literal with no type in sight, which
				// is how this defect usually arrives.
				if node.Kind == token.FLOAT || node.Kind == token.IMAG {
					report(node.Pos(), "is the float literal "+node.Value)
				}
			case *ast.SelectorExpr:
				// strconv.ParseFloat, FormatFloat, AppendFloat, and anything
				// else that converts through one.
				if strings.Contains(node.Sel.Name, "Float") {
					report(node.Sel.Pos(), "calls "+node.Sel.Name)
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", moneyDir, err)
	}
	if checked == 0 {
		t.Fatal("the float guard read no files, so it proves nothing")
	}
}
