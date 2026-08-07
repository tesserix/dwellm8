package pdfdoc_test

import (
	"bytes"
	"testing"

	"github.com/tesserix/dwellm8/services/api/internal/platform/pdfdoc"
)

func aDocument() pdfdoc.Document {
	return pdfdoc.Document{
		Title:    "PROPERTY MANAGEMENT AGREEMENT",
		Preamble: []string{"This agreement is made at Kochi on 2026-08-08."},
		Sections: []pdfdoc.Section{
			{Number: "4.2", Heading: "No authority to sell or deal",
				Body: "The Property Manager has no authority to sell the Property."},
		},
		Signatures: []pdfdoc.SignatureBlock{
			{Role: "Owner", Name: "Anjali Menon", Lines: []string{"Signature", "Date"}},
		},
		Footer: "Dwellm8",
	}
}

func TestItRendersAPDF(t *testing.T) {
	out, err := pdfdoc.Render(aDocument())
	if err != nil {
		t.Fatalf("rendering: %v", err)
	}
	if !bytes.HasPrefix(out, []byte("%PDF-")) {
		t.Fatalf("that is not a PDF: %q", out[:min(8, len(out))])
	}
	if len(out) < 1000 {
		t.Errorf("a one-clause agreement is not %d bytes", len(out))
	}
}

// A document with no clause in it is a signature page: better to refuse than
// to hand somebody a blank instrument to sign.
func TestItRefusesADocumentWithNothingInIt(t *testing.T) {
	if _, err := pdfdoc.Render(pdfdoc.Document{Title: "Nothing"}); err == nil {
		t.Fatal("a document with no sections must be refused")
	}
}

func TestItRefusesADocumentWithNoTitle(t *testing.T) {
	d := aDocument()
	d.Title = "  "
	if _, err := pdfdoc.Render(d); err == nil {
		t.Fatal("a document with no title must be refused")
	}
}
