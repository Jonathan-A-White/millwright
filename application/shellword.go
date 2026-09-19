package application

import "strings"

// shellWord is one word of a command line a person will paste into a shell. A
// word a shell reads back unchanged is left alone; anything else is wrapped in
// single quotes, inside which every character is literal, with a single quote
// itself closed, escaped and reopened. The Claude adapter keeps the same rule
// for the lines it hands a shell, and cannot be imported from here.
func shellWord(word string) string {
	if shellSafe(word) {
		return word
	}
	return "'" + strings.ReplaceAll(word, "'", `'\''`) + "'"
}

// shellSafe reports whether a word means itself to a shell as it is written.
// The list is deliberately short: everything not on it gets quoted.
func shellSafe(word string) bool {
	if word == "" {
		return false
	}
	for _, r := range word {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case strings.ContainsRune("-_./:=@,+%", r):
		default:
			return false
		}
	}
	return true
}
