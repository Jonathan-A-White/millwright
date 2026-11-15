package postern_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// TestVectorsAreCurrent holds millwright's hand copies of postern's test
// fixtures equal to postern's docs/fixtures, so a copy that goes stale fails a
// test even where no other test reads the part that changed (the "hands"
// vector of protocol-vectors.json, "landedWithoutVersion" of
// grist-vectors.json). It reads ~/postern, or the checkout POSTERN_CHECKOUT
// names, and skips when there is none. It is the same pairing as
// scripts/check-postern-fixtures.sh: every file under a testdata directory of
// millwright whose name is also under postern's docs/fixtures.
func TestVectorsAreCurrent(t *testing.T) {
	checkout := os.Getenv("POSTERN_CHECKOUT")
	if checkout == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			t.Skipf("no home directory to find the postern checkout in: %v", err)
		}
		checkout = filepath.Join(home, "postern")
	}
	fixtures := filepath.Join(checkout, "docs", "fixtures")
	if info, err := os.Stat(fixtures); err != nil || !info.IsDir() {
		t.Skipf("no postern checkout at %s", fixtures)
	}
	copies := []string{
		"testdata",
		filepath.Join("..", "..", "application", "testdata"),
		filepath.Join("..", "..", "domain", "events", "testdata"),
	}
	compared := 0
	for _, dir := range copies {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("read %s: %v", dir, err)
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			theirs, err := os.ReadFile(filepath.Join(fixtures, e.Name()))
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				t.Fatalf("read postern's %s: %v", e.Name(), err)
			}
			ours, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				t.Fatalf("read %s: %v", filepath.Join(dir, e.Name()), err)
			}
			compared++
			if !bytes.Equal(ours, theirs) {
				t.Errorf("%s differs from %s: recopy it with cp and update the README's commit",
					filepath.Join(dir, e.Name()), filepath.Join(fixtures, e.Name()))
			}
		}
	}
	if compared == 0 {
		t.Errorf("no copy under %v has a namesake in %s", copies, fixtures)
	}
}
