package ops

import (
	"strings"
	"testing"

	propertydomain "github.com/tesserix/dwellm8/services/api/internal/property/domain"
)

// Every page of a printed instrument carries its own name in the footer. It
// carried "management agreement" on all five, which is the wrong document
// named at the foot of something somebody signs (#350).
func TestTheFooterNamesTheInstrumentItIsOn(t *testing.T) {
	a, err := propertydomain.PreviewInstrument("rent_agreement")
	if err != nil {
		t.Fatalf("previewing: %v", err)
	}
	footer := printable(a).Footer
	if !strings.Contains(strings.ToLower(footer), "rent agreement") {
		t.Errorf("the footer says %q, which is not what this document is", footer)
	}
}
