package infrastructure_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// mw-2ky9evf (after mw-gq6.1746): a command that has ended is given more than
// one second to close its output, as a loaded host can take longer and report
// a command that exited 0 as failed. No adapter sets WaitDelay to one second.
func TestNoAdapterAllowsOneSecondForAnEndedCommandToCloseItsOutput(t *testing.T) {
	oneSecond := regexp.MustCompile(`WaitDelay\s*=\s*time\.Second\b`)
	var found []string
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for i, line := range strings.Split(string(body), "\n") {
			if oneSecond.MatchString(line) {
				found = append(found, path+":"+strconv.Itoa(i+1))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("reading the adapters: %v", err)
	}
	if len(found) > 0 {
		t.Errorf("WaitDelay is one second at %s; give an ended command ten seconds to close its output", strings.Join(found, ", "))
	}
}
