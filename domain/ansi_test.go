package domain_test

import (
	"testing"

	"github.com/Jonathan-A-White/millwright/domain"
)

func TestStripANSIRemovesEscapeSequencesAndLeavesTheText(t *testing.T) {
	for _, c := range []struct {
		why, in, want string
	}{
		{"a colour pair around a tick", "\x1b[32m✓\x1b[39m ok", "✓ ok"},
		{"dim and reset around a count", "\x1b[2m(4)\x1b[22m", "(4)"},
		{"several parameters in one sequence", "\x1b[33m 998\x1b[2mms\x1b[22m\x1b[39m", " 998ms"},
		{"a bare reset", "a\x1b[mb", "ab"},
		{"cursor movement is a CSI too", "\x1b[2K\x1b[1Gdone", "done"},
		{"an OSC title ended by BEL", "\x1b]0;vitest\x07ready", "ready"},
		{"an OSC title ended by ST", "\x1b]0;vitest\x1b\\ready", "ready"},
		{"a lone ESC", "a\x1bb", "ab"},
		{"an ESC at the very end", "tail\x1b", "tail"},
		{"an unfinished CSI at the end", "tail\x1b[3", "tail"},
		{"plain text", "plain text\nover two lines", "plain text\nover two lines"},
		{"UTF-8 is not an escape", "✓ ❯ ⎯⎯⎯ FAIL", "✓ ❯ ⎯⎯⎯ FAIL"},
		{"a bracket with no ESC before it", "[32m is only text", "[32m is only text"},
		{"nothing", "", ""},
	} {
		if got := domain.StripANSI(c.in); got != c.want {
			t.Errorf("%s: StripANSI(%q) = %q, want %q", c.why, c.in, got, c.want)
		}
	}
}
