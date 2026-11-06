package rig

import (
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

// GateLines are the gate commands of the config file a Checks may read again
// when its gate runs: the [tests], [related_tests] and [changed_specs] lines by
// rig name, and the [test_parts.<rig>] parts.
type GateLines struct {
	Tests   map[string]string
	Related map[string]string
	Specs   map[string]string
	Parts   map[string][]application.GatePart
}

// WithGateLinesFrom makes the Checks read the gate's commands afresh each time
// they are asked for or run, through read, instead of keeping them as they were
// when the batch began: a line written to the config while a landing runs is the
// one its gate runs (mw-gq6.1006), as for the after-landing line (mw-gq6.1001). The
// commands WithCommands, WithRelatedCommands, WithSpecCommands and WithParts name
// stay as the lines the batch began with, which a run says it differs from; a
// config that cannot be read at the gate runs those lines. now is when the read
// is said to have been made. A Checks without it keeps the lines it was given.
func WithGateLinesFrom(read func() (GateLines, error), now func() time.Time) CheckOption {
	return func(c *Checks) {
		c.read = read
		if now != nil {
			c.now = now
		}
	}
}

// gateView is the gate's commands as one asking sees them: those the batch began
// with, or those read afresh, with when they were read, or why they could not be.
type gateView struct {
	tests   map[string]string
	related map[string]string
	specs   map[string]string
	parts   map[string][]application.GatePart
	at      time.Time
	failed  string
}

// view is the gate's commands now.
func (c *Checks) view() gateView {
	began := gateView{tests: c.commands, related: c.related, specs: c.specs, parts: c.parts}
	if c.read == nil {
		return began
	}
	at := c.now()
	lines, err := c.read()
	if err != nil {
		began.failed = strings.TrimSpace(strings.Join(strings.Fields(err.Error()), " "))
		return began
	}
	return gateView{tests: nonBlank(lines.Tests), related: nonBlank(lines.Related), specs: nonBlank(lines.Specs), parts: nonEmpty(lines.Parts), at: at}
}

// nonBlank is commands without the rigs whose line is blank, as the options that
// name them have it.
func nonBlank(commands map[string]string) map[string]string {
	kept := map[string]string{}
	for rig, command := range commands {
		if strings.TrimSpace(command) != "" {
			kept[rig] = command
		}
	}
	return kept
}

// nonEmpty is parts without the rigs that name none.
func nonEmpty(parts map[string][]application.GatePart) map[string][]application.GatePart {
	kept := map[string][]application.GatePart{}
	for rig, named := range parts {
		if len(named) > 0 {
			kept[rig] = named
		}
	}
	return kept
}

// testsOf is the command line the view has for rig's tests, the default for a rig
// it does not name.
func (c *Checks) testsOf(v gateView, rig string) string {
	if command, named := v.tests[rig]; named {
		return command
	}
	return c.command
}

// readOf says how the line of the table what was read for the run of command,
// against the line the batch began with. The zero GateRead when the Checks does
// not read its lines again.
func (c *Checks) readOf(v gateView, what, command, began string) application.GateRead {
	if c.read == nil {
		return application.GateRead{}
	}
	if v.failed != "" {
		return application.GateRead{What: what, Failed: v.failed}
	}
	return application.GateRead{What: what, At: v.at, Changed: command != began}
}
