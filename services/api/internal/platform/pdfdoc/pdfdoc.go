// Package pdfdoc prints a legal instrument to PDF: numbered clauses, then
// places to sign. Nothing is signed online yet (#340), so the printed copy is
// what the parties execute.
package pdfdoc

import (
	"bytes"
	"errors"
	"fmt"
	"strings"

	"github.com/go-pdf/fpdf"
)

// Section is one numbered clause.
type Section struct {
	Number  string
	Heading string
	Body    string
}

// SignatureBlock is a place to sign: a role, whose name it is, and the ruled
// lines beneath it.
type SignatureBlock struct {
	Role  string
	Name  string
	Lines []string
}

// Document is what Render prints.
type Document struct {
	Title      string
	Preamble   []string
	Sections   []Section
	Signatures []SignatureBlock
	Footer     string
}

const (
	margin     = 18.0
	pageWidth  = 210.0
	bodyWidth  = pageWidth - 2*margin
	lineHeight = 5.0
)

// Render lays the document out on A4 and returns the PDF bytes.
func Render(d Document) ([]byte, error) {
	if strings.TrimSpace(d.Title) == "" {
		return nil, errors.New("a document without a title cannot be printed")
	}
	// A signature page with no clauses above it is a blank instrument.
	if len(d.Sections) == 0 {
		return nil, errors.New("a document without any clause cannot be printed")
	}

	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.SetMargins(margin, margin, margin)
	pdf.SetAutoPageBreak(true, margin)
	if d.Footer != "" {
		footer(pdf, d.Footer)
	}
	pdf.AddPage()

	pdf.SetFont("Helvetica", "B", 14)
	pdf.MultiCell(bodyWidth, 7, strings.ToUpper(d.Title), "", "C", false)
	pdf.Ln(4)

	pdf.SetFont("Helvetica", "", 10)
	for _, p := range d.Preamble {
		pdf.MultiCell(bodyWidth, lineHeight, p, "", "J", false)
		pdf.Ln(2)
	}

	for _, s := range d.Sections {
		// Keep a heading with at least the first lines of its clause.
		if pdf.GetY() > 250 {
			pdf.AddPage()
		}
		pdf.Ln(2)
		pdf.SetFont("Helvetica", "B", 10)
		pdf.MultiCell(bodyWidth, lineHeight+1, strings.TrimSpace(s.Number+" "+s.Heading), "", "L", false)
		pdf.SetFont("Helvetica", "", 10)
		pdf.MultiCell(bodyWidth, lineHeight, s.Body, "", "J", false)
	}

	if len(d.Signatures) > 0 {
		signatures(pdf, d.Signatures)
	}

	var out bytes.Buffer
	if err := pdf.Output(&out); err != nil {
		return nil, fmt.Errorf("writing the pdf: %w", err)
	}
	return out.Bytes(), nil
}

func signatures(pdf *fpdf.Fpdf, blocks []SignatureBlock) {
	pdf.Ln(8)
	if pdf.GetY() > 200 {
		pdf.AddPage()
	}
	pdf.SetFont("Helvetica", "B", 10)
	pdf.MultiCell(bodyWidth, lineHeight+1, "SIGNED BY THE PARTIES", "", "L", false)
	pdf.Ln(2)

	for _, b := range blocks {
		if pdf.GetY() > 240 {
			pdf.AddPage()
		}
		pdf.SetFont("Helvetica", "B", 10)
		pdf.MultiCell(bodyWidth, lineHeight+1, b.Role, "", "L", false)
		if strings.TrimSpace(b.Name) != "" {
			pdf.SetFont("Helvetica", "", 10)
			pdf.MultiCell(bodyWidth, lineHeight, b.Name, "", "L", false)
		}
		pdf.SetFont("Helvetica", "", 9)
		for _, l := range b.Lines {
			pdf.Ln(6)
			y := pdf.GetY()
			pdf.Line(margin, y, margin+80, y)
			pdf.SetY(y + 1)
			pdf.MultiCell(bodyWidth, lineHeight, l, "", "L", false)
		}
		pdf.Ln(6)
	}
}

func footer(pdf *fpdf.Fpdf, note string) {
	pdf.SetFooterFunc(func() {
		pdf.SetY(-12)
		pdf.SetFont("Helvetica", "I", 8)
		pdf.CellFormat(bodyWidth, 6, fmt.Sprintf("%s    Page %d", note, pdf.PageNo()), "", 0, "C", false, 0, "")
	})
}
