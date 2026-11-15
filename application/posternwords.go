package application

import "strings"

// wordsOutsideFences counts the words of text, split on white space, leaving out
// the lines of each closed fenced code block (a line that opens with three
// backticks) and the fence lines themselves. A fence that is never closed hides
// nothing: the lines after its opening count, so a stray fence cannot hide text.
func wordsOutsideFences(text string) int {
	lines := strings.Split(text, "\n")
	isFence := func(line string) bool { return strings.HasPrefix(strings.TrimSpace(line), "```") }
	n := 0
	for i := 0; i < len(lines); i++ {
		if !isFence(lines[i]) {
			n += len(strings.Fields(lines[i]))
			continue
		}
		closing := -1
		for j := i + 1; j < len(lines); j++ {
			if isFence(lines[j]) {
				closing = j
				break
			}
		}
		if closing >= 0 {
			i = closing
		}
	}
	return n
}
