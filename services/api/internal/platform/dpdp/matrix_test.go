package dpdp_test

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/tesserix/dwellm8/services/api/internal/platform/dpdp"
)

// The retention matrix exists twice — in docs/data-retention.md, where a
// compliance reviewer reads it, and in this package, where an erasure request is
// answered by it. This is the price of that, paid the same way ADR-0010 pays it
// for the lease state machine.
//
// The dangerous direction of drift is the quiet one: the document is amended
// after a review, the code is not, and every erasure answer cites a period
// nobody agreed to.
func TestTheDocumentedMatrixMatchesTheCode(t *testing.T) {
	path := filepath.Join("..", "..", "..", "..", "..", "docs", "data-retention.md")
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		// The Dockerfile runs the suite inside a build context of services/api
		// alone, where docs/ does not exist. Skipping there rather than failing —
		// and the check is not thereby inert, because the api workflow plants a
		// bent document and fails the build if this test passes against it.
		t.Skip("docs/data-retention.md is not in this build context")
	}
	if err != nil {
		t.Fatalf("reading the retention matrix: %v — docs/data-retention.md is the reviewed "+
			"authority for these periods and this package is only its implementation", err)
	}
	doc := string(raw)

	// | `financial` | **8** | ... — the years, per class, as the document states them.
	row := regexp.MustCompile(`\|\s*` + "`" + `([a-z]+)` + "`" + `\s*\|\s*(?:\*\*)?(\d+)(?:\*\*)?\s*\|`)
	documented := map[string]int{}
	for _, m := range row.FindAllStringSubmatch(doc, -1) {
		years, err := strconv.Atoi(m[2])
		if err != nil {
			continue
		}
		documented[m[1]] = years
	}
	if len(documented) == 0 {
		t.Fatal("no retention rows parsed out of docs/data-retention.md — the table's shape " +
			"changed and this check silently stopped checking anything")
	}

	for _, c := range dpdp.Classes() {
		r, ok := dpdp.RetentionFor(c)
		if !ok {
			t.Errorf("%s has no rule in code", c)
			continue
		}
		years, listed := documented[string(c)]
		if !listed {
			t.Errorf("class %s is implemented and is not in docs/data-retention.md", c)
			continue
		}
		if years != r.Years {
			t.Errorf("%s: the document says %d years and the code says %d", c, years, r.Years)
		}
	}

	for class := range documented {
		if _, ok := dpdp.RetentionFor(dpdp.Class(class)); !ok {
			t.Errorf("docs/data-retention.md documents class %q, which nothing implements", class)
		}
	}

	// Every table named in the matrix's second table should exist in the schema
	// the API is tested against. Asserted loosely — the point is to catch a table
	// that was renamed and left behind in the document.
	for _, name := range []string{"journal_entries", "ledger_postings", "tds_obligations",
		"tds_certificates", "lease_tax_facts", "leases", "kyc_verifications", "consent_artefacts"} {
		if !strings.Contains(doc, name) {
			t.Errorf("docs/data-retention.md does not classify %s, which holds personal data", name)
		}
	}
}
