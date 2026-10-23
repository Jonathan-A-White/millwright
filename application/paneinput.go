package application

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// The marks of Claude Code's screen a pane is read by. Its input line is a
// prompt mark and, when nothing is typed, a no-break space; while it is
// working it says how to interrupt it.
const (
	PaneInputMark     = "❯"
	PaneWorkingMarker = "esc to interrupt"
)

// paneMenuChoice is a numbered option of a menu the harness puts up, marked
// by the selection arrow that is also the input line's mark: "❯ 1. Yes".
var paneMenuChoice = regexp.MustCompile(`^\d+\.\s`)

// PaneHoldsOnlyDraft is the one reading of a pane that is parked on text
// nobody sent: no turn is running (the screen does not say how to interrupt
// it), it is not stopped at the usage limit, and the lowest line that begins
// with the input mark holds undimmed text that is not a menu's numbered
// choice. A session in that state takes no mail and works nothing by itself;
// the reaper's idle test (the terminal's PaneDraft) and mw doctor's
// mayor-stale (input left on the prompt line is stuck there) both ask it, so
// that they call the same pane the same thing (mw-gq6.709). The screen may
// carry its escapes, capture-pane -e, or not: without them a dim suggestion
// reads as text, so a caller that can should keep them.
func PaneHoldsOnlyDraft(screen string) bool {
	text := StripEscapes(screen)
	if strings.Contains(text, PaneWorkingMarker) {
		return false
	}
	if _, limited := PaneUsageLimit(text); limited {
		return false
	}
	draft := strings.TrimSpace(strings.ReplaceAll(InputDraft(screen), "\u00a0", " "))
	return draft != "" && !paneMenuChoice.MatchString(draft)
}

// InputDraft is the draft on the last prompt line of a screen, read by the
// same rule as InputLineHoldsDraft: "" when the line is empty or the screen
// has no prompt line at all.
func InputDraft(screen string) string {
	lines := strings.Split(screen, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if isPrompt, draft := ReadInputLine(lines[i]); isPrompt {
			return strings.TrimSpace(draft)
		}
	}
	return ""
}

// InputLineHoldsDraft reads one line of a capture-pane -e screen. isPrompt is
// whether the line, its escapes aside, starts with the prompt mark. holdsDraft
// is whether anything undimmed and not blank follows the mark: an empty input
// line is the mark and blank space (Claude Code's is a no-break space), and
// Claude Code's suggested next prompt is drawn after it as dim text, SGR 2,
// which is not a draft either. This is the one rule for "is the input line
// empty"; contrib/mail-notify's pane_idle is the same rule in shell.
func InputLineHoldsDraft(line string) (isPrompt, holdsDraft bool) {
	isPrompt, draft := ReadInputLine(line)
	return isPrompt, strings.TrimSpace(draft) != ""
}

// ReadInputLine is InputLineHoldsDraft's reading, keeping the undimmed text
// after the mark rather than only whether there is any.
func ReadInputLine(line string) (isPrompt bool, draft string) {
	dim := false
	seenMark := false
	var text strings.Builder
	for i := 0; i < len(line); {
		if line[i] == 0x1b {
			n, params, isSGR := escapeAt(line[i:])
			if isSGR {
				dim = sgrDim(params, dim)
			}
			i += n
			continue
		}
		r, size := utf8.DecodeRuneInString(line[i:])
		i += size
		switch {
		case !seenMark:
			if r != []rune(PaneInputMark)[0] {
				return false, ""
			}
			seenMark = true
		case !dim:
			text.WriteRune(r)
		}
	}
	return seenMark, text.String()
}

// escapeAt reads the escape sequence at the start of s and says how long it is;
// for a CSI ending in m, an SGR, it gives the parameters as well.
func escapeAt(s string) (n int, params string, isSGR bool) {
	if len(s) < 2 {
		return len(s), "", false
	}
	switch s[1] {
	case '[':
		for n = 2; n < len(s); n++ {
			if s[n] >= 0x40 && s[n] <= 0x7e {
				return n + 1, s[2:n], s[n] == 'm'
			}
		}
		return len(s), "", false
	case ']': // an operating system command, to a bell or ESC \
		for n = 2; n < len(s); n++ {
			if s[n] == 0x07 {
				return n + 1, "", false
			}
			if s[n] == 0x1b && n+1 < len(s) && s[n+1] == '\\' {
				return n + 2, "", false
			}
		}
		return len(s), "", false
	}
	return 2, "", false
}

// sgrDim is whether text is dim after an SGR sequence with these parameters,
// given whether it was before. The colour codes 38 and 48 carry arguments that
// are not attributes of their own: 38;2;r;g;b holds a 2 that is no dim.
func sgrDim(params string, dim bool) bool {
	args := strings.FieldsFunc(params, func(r rune) bool { return r == ';' || r == ':' })
	if len(args) == 0 {
		return false
	}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "0", "00", "22":
			dim = false
		case "2", "02":
			dim = true
		case "38", "48", "58":
			if i+1 < len(args) && args[i+1] == "5" {
				i += 2
			} else if i+1 < len(args) && args[i+1] == "2" {
				i += 4
			}
		}
	}
	return dim
}

// StripEscapes is the text of a screen without the escape sequences drawn in it.
func StripEscapes(screen string) string {
	var text strings.Builder
	for i := 0; i < len(screen); {
		if screen[i] == 0x1b {
			n, _, _ := escapeAt(screen[i:])
			i += n
			continue
		}
		text.WriteByte(screen[i])
		i++
	}
	return text.String()
}
