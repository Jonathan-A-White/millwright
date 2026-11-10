package application

import (
	"strconv"
	"strings"
)

// HarnessNumber is the version in what `claude --version` prints, which is
// "2.1.322 (Claude Code)": its first word, when that is numbers and dots, else
// "". `mw version` prints it after the program's name, "claude 2.1.322 (Claude
// Code)", and the program's name is dropped too.
func HarnessNumber(printed string) string {
	for _, word := range strings.Fields(printed) {
		if isVersionNumber(word) {
			return word
		}
		if word != "claude" {
			return ""
		}
	}
	return ""
}

func isVersionNumber(word string) bool {
	if word == "" {
		return false
	}
	for _, part := range strings.Split(word, ".") {
		if _, err := strconv.ParseUint(part, 10, 32); err != nil {
			return false
		}
	}
	return true
}

// HarnessBehind says whether version have is older than want, each numbers
// and dots compared part by part as numbers (2.1.99 is behind 2.1.322; 2.1.1000
// is not); a part missing counts as 0. Either that is not a version is not
// behind: a version that cannot be read is never guessed.
func HarnessBehind(have, want string) bool {
	if !isVersionNumber(have) || !isVersionNumber(want) {
		return false
	}
	h, w := strings.Split(have, "."), strings.Split(want, ".")
	for i := 0; i < max(len(h), len(w)); i++ {
		a, b := harnessPart(h, i), harnessPart(w, i)
		if a != b {
			return a < b
		}
	}
	return false
}

func harnessPart(parts []string, i int) uint64 {
	if i >= len(parts) {
		return 0
	}
	n, _ := strconv.ParseUint(parts[i], 10, 32)
	return n
}
