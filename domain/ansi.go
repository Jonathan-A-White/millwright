package domain

import "strings"

const (
	esc = '\x1b'
	bel = '\x07'
)

// StripANSI is text with the terminal escape sequences taken out, for output
// that is recorded for a person to read: a test runner that prints colour into
// a pipe leaves "ESC[32m✓ESC[39m" in what mw keeps, and a bead, a mail or a
// comment shows the codes as "[32m✓[39m".
//
// Three things go: a CSI sequence (ESC [, parameter and intermediate bytes, one
// final byte), an OSC sequence (ESC ], up to a BEL or ESC \, or to the end of
// the line when it is never ended), and a lone ESC. Everything else, UTF-8
// included, is kept as it was.
func StripANSI(s string) string {
	if !strings.ContainsRune(s, esc) {
		return s
	}
	var out strings.Builder
	out.Grow(len(s))
	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		if runes[i] != esc {
			out.WriteRune(runes[i])
			continue
		}
		if i+1 >= len(runes) {
			break
		}
		switch runes[i+1] {
		case '[':
			i += 2
			for i < len(runes) && runes[i] >= 0x30 && runes[i] <= 0x3f {
				i++
			}
			for i < len(runes) && runes[i] >= 0x20 && runes[i] <= 0x2f {
				i++
			}
			if i >= len(runes) || runes[i] < 0x40 || runes[i] > 0x7e {
				i-- // not a sequence after all: what was read is dropped, the byte that broke it is not
			}
		case ']':
			i += 2
			for ; i < len(runes); i++ {
				if runes[i] == bel {
					break
				}
				if runes[i] == esc && i+1 < len(runes) && runes[i+1] == '\\' {
					i++
					break
				}
				if runes[i] == '\n' {
					i--
					break
				}
			}
		}
	}
	return out.String()
}
