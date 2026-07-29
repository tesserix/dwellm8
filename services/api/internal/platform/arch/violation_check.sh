#!/usr/bin/env bash
# Proves the boundary test actually fails when a boundary is crossed.
# Run by hand when changing boundaries_test.go; not part of CI.
set -euo pipefail
cd "$(dirname "$0")/../../.."
trap 'rm -f internal/lease/store/violation.go' EXIT
cat > internal/lease/store/violation.go <<'GO'
package store

import _ "github.com/tesserix/dwellm8/services/api/internal/money/store"
GO
if go test ./internal/platform/arch/ >/dev/null 2>&1; then
  echo "FAIL: the boundary test passed while lease/store imported money/store"
  exit 1
fi
echo "OK: the boundary test catches a cross-module store import"
