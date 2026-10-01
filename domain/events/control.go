package events

import (
	"fmt"
	"strconv"
	"strings"
)

// The words a control event carries in its detail (mw-jrx0s.16): what the
// factory tells its seats and followers to do, as against what happened.
const (
	// ControlCancel ends the session working the event's bead: "cancel".
	ControlCancel = "cancel"
	// ControlPauseHost stops a host's dispatch passes starting stories:
	// "pause-host <host>".
	ControlPauseHost = "pause-host"
	// ControlResumeHost undoes a pause-host: "resume-host <host>".
	ControlResumeHost = "resume-host"
	// ControlCap names how many sessions a host may run at once: "cap <host> <n>".
	ControlCap = "cap"
	// ControlPriority names the priority a bead should have, 0 to 4:
	// "priority <n>".
	ControlPriority = "priority"
)

// controlWords are the words, in the order the docs name them.
var controlWords = []string{ControlCancel, ControlPauseHost, ControlResumeHost, ControlCap, ControlPriority}

// ControlWords lists every word a control event may carry.
func ControlWords() []string {
	return append([]string(nil), controlWords...)
}

// Control is what a control event's detail says: its Word, and the Host and N
// the word takes.
type Control struct {
	Word string
	Host string // pause-host, resume-host, cap
	N    int    // cap: sessions; priority: 0 (most urgent) to 4
}

// Detail is the text a control event carries.
func (c Control) Detail() string {
	switch c.Word {
	case ControlPauseHost, ControlResumeHost:
		return c.Word + " " + c.Host
	case ControlCap:
		return fmt.Sprintf("%s %s %d", c.Word, c.Host, c.N)
	case ControlPriority:
		return fmt.Sprintf("%s %d", c.Word, c.N)
	}
	return c.Word
}

// ParseControl reads a control event's detail, saying why it cannot be read:
// the words are cancel, pause-host <host>, resume-host <host>, cap <host> <n>
// and priority <n>.
func ParseControl(detail string) (Control, error) {
	f := strings.Fields(detail)
	if len(f) == 0 {
		return Control{}, fmt.Errorf("a control event says nothing: its detail is one of %s", controlUsage())
	}
	c := Control{Word: f[0]}
	args := f[1:]
	bad := func() (Control, error) {
		return Control{}, fmt.Errorf("a control event %q is not one of %s", strings.Join(f, " "), controlUsage())
	}
	switch c.Word {
	case ControlCancel:
		if len(args) != 0 {
			return bad()
		}
	case ControlPauseHost, ControlResumeHost:
		if len(args) != 1 {
			return bad()
		}
		c.Host = args[0]
	case ControlCap:
		if len(args) != 2 {
			return bad()
		}
		n, err := strconv.Atoi(args[1])
		if err != nil || n < 1 {
			return Control{}, fmt.Errorf("a cap is a whole number of sessions, at least 1, not %q", args[1])
		}
		c.Host, c.N = args[0], n
	case ControlPriority:
		if len(args) != 1 {
			return bad()
		}
		n, err := strconv.Atoi(args[0])
		if err != nil || n < 0 || n > 4 {
			return Control{}, fmt.Errorf("a priority runs 0 (most urgent) to 4, not %q", args[0])
		}
		c.N = n
	default:
		return bad()
	}
	return c, nil
}

// ControlOf is the control ev carries, false when ev is no control event or
// its detail is unreadable.
func ControlOf(ev Event) (Control, bool) {
	if ev.Kind != KindControl {
		return Control{}, false
	}
	c, err := ParseControl(ev.Detail)
	return c, err == nil
}

// controlNeedsBead are the words that are about one bead.
func controlNeedsBead(word string) bool {
	return word == ControlCancel || word == ControlPriority
}

func controlUsage() string {
	return "cancel, pause-host <host>, resume-host <host>, cap <host> <n>, priority <n>"
}

// checkControl reports why ev is not a control event the factory writes.
func checkControl(ev Event) error {
	c, err := ParseControl(ev.Detail)
	if err != nil {
		return err
	}
	if controlNeedsBead(c.Word) && ev.Bead == "" {
		return fmt.Errorf("a control %s event names no bead", c.Word)
	}
	if !controlNeedsBead(c.Word) && ev.Bead != "" {
		return fmt.Errorf("a control %s event is about a host, not the bead %s", c.Word, ev.Bead)
	}
	return nil
}
