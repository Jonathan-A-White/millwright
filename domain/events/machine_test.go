package events_test

import (
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/domain/events"
)

// The transitions each machine allows, written out here by hand rather than
// read back from the package, so a change to a table is a change to this test
// too. "" is the start: the bead filed, the card asked, the talk opened, the
// job scheduled. docs/events.md lists the same pairs.
var allowed = map[events.Machine][][2]string{
	events.MachineBead: {
		{"", "held"}, {"", "open"},
		{"held", "open"}, {"held", "closed"},
		{"open", "held"}, {"open", "claimed"}, {"open", "closed"},
		{"claimed", "running"}, {"claimed", "open"}, {"claimed", "held"},
		{"running", "landed"}, {"running", "refused"}, {"running", "open"}, {"running", "held"},
		{"refused", "open"}, {"refused", "held"}, {"refused", "closed"},
		{"landed", "verified"}, {"landed", "closed"}, {"landed", "open"}, {"landed", "held"},
		{"verified", "closed"},
		{"closed", "open"}, {"closed", "verified"},
	},
	events.MachineCard: {
		{"", "asked"},
		{"asked", "answered"},
		{"answered", "applied"},
	},
	events.MachineTalk: {
		{"", "open"},
		{"open", "turn"}, {"open", "ended"},
		{"turn", "turn"}, {"turn", "answer"}, {"turn", "ended"},
		{"answer", "turn"}, {"answer", "ended"},
	},
	events.MachineJob: {
		{"", "scheduled"},
		{"scheduled", "running"},
		{"running", "done"}, {"running", "failed"},
		{"done", "scheduled"}, {"failed", "scheduled"},
	},
}

var states = map[events.Machine][]string{
	events.MachineBead: {"held", "open", "claimed", "running", "landed", "refused", "verified", "closed"},
	events.MachineCard: {"asked", "answered", "applied"},
	events.MachineTalk: {"open", "turn", "answer", "ended"},
	events.MachineJob:  {"scheduled", "running", "done", "failed"},
}

func TestTheFourMachinesAreTheOnesNamed(t *testing.T) {
	got := events.Machines()
	want := []events.Machine{events.MachineBead, events.MachineCard, events.MachineTalk, events.MachineJob}
	if len(got) != len(want) {
		t.Fatalf("expected machines %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expected machines %v, got %v", want, got)
		}
	}
	for _, m := range want {
		if strings.Join(events.States(m), " ") != strings.Join(states[m], " ") {
			t.Errorf("the %s machine: expected states %v, got %v", m, states[m], events.States(m))
		}
	}
}

func TestEveryAllowedTransitionIsAccepted(t *testing.T) {
	for machine, pairs := range allowed {
		for _, p := range pairs {
			t.Run(string(machine)+":"+p[0]+"->"+p[1], func(t *testing.T) {
				if err := events.Transition(machine, p[0], p[1]); err != nil {
					t.Fatalf("expected %s -> %s accepted, got %v", p[0], p[1], err)
				}
			})
		}
	}
}

// Every pair of states (and the start) not in the table above is refused, and
// the refusal names the machine and both states.
func TestEveryOtherTransitionIsRefusedNamingTheMachineAndStates(t *testing.T) {
	for machine, pairs := range allowed {
		ok := map[[2]string]bool{}
		for _, p := range pairs {
			ok[p] = true
		}
		froms := append([]string{""}, states[machine]...)
		refused := 0
		for _, from := range froms {
			for _, to := range append([]string{""}, states[machine]...) {
				if ok[[2]string{from, to}] {
					continue
				}
				err := events.Transition(machine, from, to)
				if err == nil {
					t.Errorf("the %s machine: expected %q -> %q refused, it was accepted", machine, from, to)
					continue
				}
				refused++
				msg := err.Error()
				if !strings.Contains(msg, "the "+string(machine)+" machine") || !strings.Contains(msg, quoted(from)) || !strings.Contains(msg, quoted(to)) {
					t.Errorf("the %s machine: the refusal of %q -> %q should name the machine and both states, got %q", machine, from, to, msg)
				}
			}
		}
		if refused == 0 {
			t.Errorf("the %s machine refused nothing", machine)
		}
	}
}

func TestOneForbiddenTransitionPerMachine(t *testing.T) {
	cases := []struct {
		machine  events.Machine
		from, to string
		want     string
	}{
		{events.MachineBead, "landed", "claimed", `the bead machine has no transition from "landed" to "claimed"`},
		{events.MachineCard, "applied", "answered", `the card machine has no transition from "applied" to "answered"`},
		{events.MachineTalk, "ended", "turn", `the talk machine has no transition from "ended" to "turn"`},
		{events.MachineJob, "scheduled", "done", `the job machine has no transition from "scheduled" to "done"`},
	}
	for _, c := range cases {
		err := events.Transition(c.machine, c.from, c.to)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("expected %q, got %v", c.want, err)
		}
	}
}

func TestAnUnknownStateOrMachineIsRefusedByName(t *testing.T) {
	if err := events.Transition(events.MachineBead, "open", "merged"); err == nil || !strings.Contains(err.Error(), `the bead machine has no state "merged"`) {
		t.Errorf("expected the unknown state named, got %v", err)
	}
	if err := events.Transition("ship", "a", "b"); err == nil || !strings.Contains(err.Error(), `no machine "ship"`) {
		t.Errorf("expected the unknown machine named, got %v", err)
	}
}

func quoted(s string) string { return `"` + s + `"` }

func TestPathIsTheShortestRunOfAllowedTransitions(t *testing.T) {
	cases := []struct {
		machine  events.Machine
		from, to string
		want     []string
	}{
		{events.MachineBead, "open", "claimed", []string{"claimed"}},
		{events.MachineBead, "open", "running", []string{"claimed", "running"}},
		{events.MachineBead, "running", "closed", []string{"landed", "closed"}},
		{events.MachineBead, events.Start, "claimed", []string{"open", "claimed"}},
		{events.MachineBead, "open", "open", nil},
		{events.MachineBead, "open", "merged", nil},
		{events.MachineCard, "applied", "asked", nil},
		{"ship", "a", "b", nil},
	}
	for _, c := range cases {
		got := events.Path(c.machine, c.from, c.to)
		if strings.Join(got, " ") != strings.Join(c.want, " ") || (got == nil) != (c.want == nil) {
			t.Errorf("Path(%s, %q, %q) = %q, want %q", c.machine, c.from, c.to, got, c.want)
		}
		from := c.from
		for _, to := range got {
			if err := events.Transition(c.machine, from, to); err != nil {
				t.Errorf("Path(%s, %q, %q) takes a step the machine refuses: %v", c.machine, c.from, c.to, err)
			}
			from = to
		}
	}
}
