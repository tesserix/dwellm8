package isolationtest_test

import (
	"testing"

	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy/isolationtest"
)

// ADR-0029, against PostgreSQL. The contract lives in resident.go so that the CI
// step which plants a defect can name one test and get a red build.
func TestResidentScope(t *testing.T) {
	isolationtest.RunResidentScope(t, pool(t), platformPool(t))
}
