package application

import (
	"slices"
	"strings"
)

// The usage reader asks for the pause on every pass while usage stays over the
// reserve (mw-kzxfpzw). A pause already in force that still has a host to reach
// asks that host again; it tells the Mayor only of what that changed.

// addNote keeps note on the set once: a host still down on the next pass would
// otherwise add the same note again.
func (s *PausedSet) addNote(note string) {
	if !slices.Contains(s.Notes, note) {
		s.Notes = append(s.Notes, note)
	}
}

// pausedChange is what a pause changed in a set already in force: the lines of
// the set as a person reads it that it now has and had not (added), and those
// it had and no longer has (gone). Both are empty when nothing changed.
type pausedChange struct{ added, gone []string }

func (c pausedChange) none() bool { return len(c.added) == 0 && len(c.gone) == 0 }

// changeBetween compares two sets by the lines of their String.
func changeBetween(before, after PausedSet) pausedChange {
	was, is := strings.Split(strings.TrimSpace(before.String()), "\n"), strings.Split(strings.TrimSpace(after.String()), "\n")
	var c pausedChange
	for _, line := range is {
		if !slices.Contains(was, line) {
			c.added = append(c.added, line)
		}
	}
	for _, line := range was {
		if !slices.Contains(is, line) {
			c.gone = append(c.gone, line)
		}
	}
	return c
}

// body is the change as a short mail names it.
func (c pausedChange) body(reason string) string {
	var b strings.Builder
	b.WriteString("The factory pause (" + reason + ") changed; nothing else did.\n\n")
	for _, line := range c.added {
		b.WriteString("now: " + line + "\n")
	}
	for _, line := range c.gone {
		b.WriteString("no longer: " + line + "\n")
	}
	b.WriteString("\nmw resume undoes it.")
	return b.String()
}
