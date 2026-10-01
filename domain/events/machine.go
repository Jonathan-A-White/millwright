// Package events holds the factory's state machines and the events that are
// their transitions, defined once here and nowhere else (mw-6ww.55, Q6 A):
// a bead's life, a card's, a talk's and a scheduled job's. The app projects
// these events; it never runs a machine of its own. docs/events.md is the
// same tables in prose, with the batch's JSON.
package events

import "fmt"

// Machine names one of the factory's state machines.
type Machine string

// The four machines.
const (
	MachineBead Machine = "bead"
	MachineCard Machine = "card"
	MachineTalk Machine = "talk"
	MachineJob  Machine = "job"
)

// A bead's life: filed held or open, claimed by a dispatch, run by a session,
// then landed or refused by mw next, verified by the Mayor, and closed.
const (
	BeadHeld     = "held"
	BeadOpen     = "open"
	BeadClaimed  = "claimed"
	BeadRunning  = "running"
	BeadLanded   = "landed"
	BeadRefused  = "refused"
	BeadVerified = "verified"
	BeadClosed   = "closed"
)

// A card's: a question put to the Governor, his answer, and the answer
// applied to its bead.
const (
	CardAsked    = "asked"
	CardAnswered = "answered"
	CardApplied  = "applied"
)

// A talk's: opened, the Governor's turn, the Mayor's answer, ended.
const (
	TalkOpen   = "open"
	TalkTurn   = "turn"
	TalkAnswer = "answer"
	TalkEnded  = "ended"
)

// A scheduled job's (dispatch pass, millhand tick, mail notify, backup,
// self-update, prune): scheduled, running, then done or failed, and
// scheduled again.
const (
	JobScheduled = "scheduled"
	JobRunning   = "running"
	JobDone      = "done"
	JobFailed    = "failed"
)

// Start is the state before a machine's first: an event from Start is a bead
// filed, a card asked, a talk opened or a job scheduled.
const Start = ""

// table is one machine: its states in order, and for each state (Start
// included) the states it may move to.
type table struct {
	states []string
	next   map[string][]string
}

var machines = []Machine{MachineBead, MachineCard, MachineTalk, MachineJob}

var tables = map[Machine]table{
	MachineBead: {
		states: []string{BeadHeld, BeadOpen, BeadClaimed, BeadRunning, BeadLanded, BeadRefused, BeadVerified, BeadClosed},
		next: map[string][]string{
			Start:       {BeadHeld, BeadOpen},
			BeadHeld:    {BeadOpen, BeadClosed},
			BeadOpen:    {BeadHeld, BeadClaimed, BeadClosed},
			BeadClaimed: {BeadRunning, BeadOpen, BeadHeld},
			BeadRunning: {BeadLanded, BeadRefused, BeadOpen, BeadHeld},
			BeadRefused: {BeadOpen, BeadHeld, BeadClosed},
			// A landing found bad is reopened or held; one with no
			// separate check closes straight away.
			BeadLanded:   {BeadVerified, BeadClosed, BeadOpen, BeadHeld},
			BeadVerified: {BeadClosed},
			BeadClosed:   {BeadOpen},
		},
	},
	MachineCard: {
		states: []string{CardAsked, CardAnswered, CardApplied},
		next: map[string][]string{
			Start:        {CardAsked},
			CardAsked:    {CardAnswered},
			CardAnswered: {CardApplied},
		},
	},
	MachineTalk: {
		states: []string{TalkOpen, TalkTurn, TalkAnswer, TalkEnded},
		next: map[string][]string{
			Start:    {TalkOpen},
			TalkOpen: {TalkTurn, TalkEnded},
			// He may speak again before the Mayor answers.
			TalkTurn:   {TalkTurn, TalkAnswer, TalkEnded},
			TalkAnswer: {TalkTurn, TalkEnded},
		},
	},
	MachineJob: {
		states: []string{JobScheduled, JobRunning, JobDone, JobFailed},
		next: map[string][]string{
			Start:        {JobScheduled},
			JobScheduled: {JobRunning},
			JobRunning:   {JobDone, JobFailed},
			JobDone:      {JobScheduled},
			JobFailed:    {JobScheduled},
		},
	},
}

// Machines lists the four machines, bead first.
func Machines() []Machine {
	return append([]Machine(nil), machines...)
}

// States lists machine's states in their order, Start not among them; none
// for a machine that does not exist.
func States(machine Machine) []string {
	return append([]string(nil), tables[machine].states...)
}

// HasState reports whether state is one of machine's states. Start is not.
func HasState(machine Machine, state string) bool {
	for _, s := range tables[machine].states {
		if s == state {
			return true
		}
	}
	return false
}

// Transition reports why machine may not move from from to to, naming the
// machine and both states; nil when its table allows it.
func Transition(machine Machine, from, to string) error {
	t, ok := tables[machine]
	if !ok {
		return fmt.Errorf("no machine %q: the machines are bead, card, talk and job", machine)
	}
	refused := fmt.Errorf("the %s machine has no transition from %q to %q", machine, from, to)
	for _, s := range []string{from, to} {
		if s != Start && !HasState(machine, s) {
			return fmt.Errorf("%w: the %s machine has no state %q", refused, machine, s)
		}
	}
	for _, s := range t.next[from] {
		if s == to {
			return nil
		}
	}
	return refused
}

// Path is the states a shortest run of machine's transitions from from to to
// passes through, to included and from not: what a watcher that saw only the
// two ends records, one event per step. It is nil when from and to are the
// same, or when the table has no way between them.
func Path(machine Machine, from, to string) []string {
	t, ok := tables[machine]
	if !ok || from == to || !HasState(machine, to) {
		return nil
	}
	came := map[string]string{from: from}
	queue := []string{from}
	for len(queue) > 0 {
		at := queue[0]
		queue = queue[1:]
		for _, next := range t.next[at] {
			if _, seen := came[next]; seen {
				continue
			}
			came[next] = at
			if next == to {
				var path []string
				for s := to; s != from; s = came[s] {
					path = append([]string{s}, path...)
				}
				return path
			}
			queue = append(queue, next)
		}
	}
	return nil
}
