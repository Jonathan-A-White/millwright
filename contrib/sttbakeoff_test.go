package contrib_test

// The speech bake-off's tests are Python, in contrib/stt-bakeoff/tests, run by
// contrib/stt-bakeoff/test.sh: its scoring and its table, standard library
// only. The gate never needs a speech model; it skips without python3.

import (
	"os/exec"
	"testing"
)

func TestSttBakeoffScoring(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 is not installed")
	}
	if out, err := exec.Command("stt-bakeoff/test.sh").CombinedOutput(); err != nil {
		t.Fatalf("contrib/stt-bakeoff/test.sh: %v\n%s", err, out)
	}
}
