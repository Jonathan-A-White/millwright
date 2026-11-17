package features

import "testing"

// TestHarnessStopOverloadIsGivenBackWhole runs the scenarios of
// features/harness_overload.feature alone (mw-gq6.1382), so that
// `go test ./features/... -run Overload` reads them.
func TestHarnessStopOverloadIsGivenBackWhole(t *testing.T) {
	runFeatures(t, []string{"harness_overload.feature"})
}
