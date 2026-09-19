//go:build !beads_integration

package beads_test

import "testing"

// The cases that run a real bd are most of this package's clock, so they are
// behind the beads_integration tag: `make test` adds it, a plain `go test` does
// not. This one is here so that a run without the tag says so instead of
// passing without a word.
func TestBeadsIntegrationCasesAreBehindTheirTag(t *testing.T) {
	t.Skip("the real-bd cases run under -tags beads_integration, as `make test` does")
}
