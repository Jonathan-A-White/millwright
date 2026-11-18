package application

import "strings"

// gateTickets are the holder lines of the live tickets in a host's gate queue and
// test-runner queue, read once for a report (mw-gq6.1463).
type gateTickets struct{ holders []string }

// gateTickets reads the tickets, none when the host has no queue to read.
func (s Status) gateTickets() *gateTickets {
	t := &gateTickets{}
	if s.GateWaiters != nil {
		t.holders = s.GateWaiters()
	}
	return t
}

// hold says whether a ticket names the story: a word of its holder line is the
// story's id, as "checking mw-gq6.12" or "tests of lampas for mw-gq6.12" name it.
func (t *gateTickets) hold(story string) bool {
	for _, holder := range t.holders {
		for _, word := range strings.Fields(holder) {
			if strings.Trim(word, ",;:()") == story {
				return true
			}
		}
	}
	return false
}
