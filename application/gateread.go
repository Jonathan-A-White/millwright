package application

import "time"

// GateRead says how one of a rig's gate commands — its [tests], [related_tests],
// [changed_specs] or [test_parts] line — was read for a run of the gate: a landing
// reads it when the gate runs, not when the batch began (mw-gq6.1006), as it does
// the after-landing line (mw-gq6.1001). What is the table it was read from, At when
// it was read, Changed that it differs from the line the batch began with, and
// Failed why the config could not be read again, so that the line the batch began
// with was the one run. The zero GateRead is a run whose port does not read the
// config again.
type GateRead struct {
	What    string
	At      time.Time
	Changed bool
	Failed  string
}

// Lines are the lines that say which line of the config the gate ran: when it was
// read, and that it is not the one the batch began with. None when the port did
// not read the config again.
func (g GateRead) Lines(command string) []string {
	var lines []string
	if g.Failed != "" {
		lines = append(lines, "gate "+g.What+": "+command+": could not be read again from the config ("+g.What+"), so the line the batch began with ran: "+g.Failed)
	}
	if !g.At.IsZero() {
		lines = append(lines, "gate "+g.What+": "+command+": read at "+g.At.UTC().Format(time.RFC3339))
	}
	if g.Changed {
		lines = append(lines, g.What+" changed during the batch: running the new line")
	}
	return lines
}
