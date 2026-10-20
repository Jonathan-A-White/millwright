package vault

import (
	"errors"
	"testing"
)

// Every wording the vault reads a refused push by: the ones a pull and another
// push cure, and those it leaves as failures (mw-gq6.637).
func TestPushRefusedReadsEachRefusalByItsWords(t *testing.T) {
	for name, tc := range map[string]struct {
		said string
		want bool
	}{
		"non-fast-forward":     {" ! [rejected] main -> main (non-fast-forward)", true},
		"fetch first":          {" ! [rejected] main -> main (fetch first)", true},
		"cannot lock ref":      {" ! [remote rejected] main -> main (cannot lock ref 'refs/heads/main': is at 3e031e3 but expected 9c1d0aa)", true},
		"remote rejected only": {" ! [remote rejected] main -> main (pre-receive hook declined)", false},
		"rejected, no reason":  {" ! [rejected] main -> main (hook said no)", false},
		"permission denied":    {"git@github.com: Permission denied (publickey).", false},
		"name not resolved":    {"fatal: unable to access: Could not resolve host: github.com", false},
	} {
		if got := pushRefused(errors.New(tc.said)); got != tc.want {
			t.Errorf("%s: pushRefused(%q) = %v, want %v", name, tc.said, got, tc.want)
		}
	}
}
