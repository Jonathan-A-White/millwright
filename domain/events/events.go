package events

import (
	"errors"
	"fmt"
	"time"
	"unicode/utf8"
)

// Event is one transition of one machine, numbered by the home's log: the
// seq is unique and increasing, and an app dedupes and orders by it, never by
// txid. The JSON tags are the field names of the `events` record's plaintext
// (postern's docs/protocol.md section 22); docs/events.md is where they are
// agreed. From and To are the machine's states, Start ("") before the first;
// both stay empty for a kind with no machine.
type Event struct {
	Seq    uint64    `json:"seq"`
	Ts     time.Time `json:"ts"`
	Kind   string    `json:"kind"`
	Bead   string    `json:"bead"`
	Actor  string    `json:"actor"`
	From   string    `json:"from"`
	To     string    `json:"to"`
	Detail string    `json:"detail"`
	Lane   string    `json:"lane"`
	// Clears is the seq of the emergency this event ends: 0 for an event that
	// ends none, which the JSON leaves out. An app takes the emergency of that seq
	// as resolved.
	Clears uint64 `json:"clears,omitempty"`
}

// The kinds a seat or a screen subscribes to (mw-6ww.55, Q4 A, plus Q7's
// scheduled jobs and bd mail).
const (
	KindBeadChanged   = "bead_changed"   // a bead's status, or anything else on it, changed
	KindCardAsked     = "card_asked"     // a question put to the Governor on a bead
	KindCardAnswered  = "card_answered"  // his answer to it
	KindCardApplied   = "card_applied"   // his answer applied to the bead
	KindMessage       = "message"        // a message in a channel
	KindTalkTurn      = "talk_turn"      // a talk opened, a turn, an answer, the talk ended
	KindHandsRan      = "hands_ran"      // a step for his hands ran
	KindMail          = "mail"           // a bd mail bead sent to a seat
	KindJob           = "job"            // a scheduled job's transition
	KindHandover      = "handover"       // a seat's session hands the seat to its successor at a seq
	KindControl       = "control"        // a word to the factory: cancel a story's session, pause a host, cap, priority
	KindActionApplied = "action_applied" // a one-tap action of the Governor's, applied from the inbox, echoed with the tap's txid
)

var kinds = []string{KindBeadChanged, KindCardAsked, KindCardAnswered, KindCardApplied, KindMessage, KindTalkTurn, KindHandsRan, KindMail, KindJob, KindHandover, KindControl, KindActionApplied}

// kindMachine is the machine each kind is a transition of; a kind missing
// here has none.
var kindMachine = map[string]Machine{
	KindBeadChanged:  MachineBead,
	KindCardAsked:    MachineCard,
	KindCardAnswered: MachineCard,
	KindCardApplied:  MachineCard,
	KindTalkTurn:     MachineTalk,
	KindJob:          MachineJob,
}

// kindTo is the state a card kind always ends in.
var kindTo = map[string]string{
	KindCardAsked:    CardAsked,
	KindCardAnswered: CardAnswered,
	KindCardApplied:  CardApplied,
}

// kindBead are the kinds whose Bead must name one.
var kindBead = map[string]bool{
	KindBeadChanged:   true,
	KindCardAsked:     true,
	KindCardAnswered:  true,
	KindCardApplied:   true,
	KindHandsRan:      true,
	KindMail:          true,
	KindActionApplied: true,
}

// Kinds lists every kind.
func Kinds() []string {
	return append([]string(nil), kinds...)
}

// KindMachine is the machine kind is a transition of, and whether kind is
// one at all. A known kind with no machine gives "" and true.
func KindMachine(kind string) (Machine, bool) {
	for _, k := range kinds {
		if k == kind {
			return kindMachine[kind], true
		}
	}
	return "", false
}

// The lanes an event travels in: a normal batch, an emergency record sent
// alone and at once, or a batch sent direct only while the chain could not be
// reached (re-sent on chain later, unchanged).
const (
	LaneNormal    = "normal"
	LaneEmergency = "emergency"
	LaneFallback  = "fallback"
)

// MaxEmergencyDetail is the most bytes an emergency event's detail may hold:
// it is sent alone, in a record of one that must stay under postern's payload
// limit, and the shipper cannot split it.
const MaxEmergencyDetail = 2000

// CutDetail is text brought down to MaxEmergencyDetail bytes at a rune
// boundary, an ellipsis the last, or text itself when it already fits.
func CutDetail(text string) string {
	if len(text) <= MaxEmergencyDetail {
		return text
	}
	const ellipsis = "…"
	cut := text[:MaxEmergencyDetail-len(ellipsis)]
	for len(cut) > 0 && !utf8.ValidString(cut) {
		cut = cut[:len(cut)-1]
	}
	return cut + ellipsis
}

func checkLane(lane string) error {
	switch lane {
	case LaneNormal, LaneEmergency, LaneFallback:
		return nil
	}
	return fmt.Errorf("no lane %q: the lanes are normal, emergency and fallback", lane)
}

// Validate reports why e is not an event the factory writes: it has no seq or
// time, an unknown kind or lane, an emergency detail past MaxEmergencyDetail, names no bead where its kind needs one, or is
// not a transition its kind's machine allows. A bead_changed event whose From
// and To are the same state is a change that left the status alone (a
// comment, an edited field).
func (e Event) Validate() error {
	switch {
	case e.Seq == 0:
		return errors.New("the event has no seq")
	case e.Ts.IsZero():
		return fmt.Errorf("event seq %d has no time", e.Seq)
	}
	if err := checkLane(e.Lane); err != nil {
		return err
	}
	if e.Lane == LaneEmergency && len(e.Detail) > MaxEmergencyDetail {
		return fmt.Errorf("an emergency event's detail is at most %d bytes, not %d", MaxEmergencyDetail, len(e.Detail))
	}
	machine, ok := KindMachine(e.Kind)
	if !ok {
		return fmt.Errorf("no kind %q", e.Kind)
	}
	if kindBead[e.Kind] && e.Bead == "" {
		return fmt.Errorf("a %s event names no bead", e.Kind)
	}
	if machine == "" {
		if e.Kind == KindControl {
			if err := checkControl(e); err != nil {
				return err
			}
		}
		if e.From != "" || e.To != "" {
			return fmt.Errorf("a %s event is no transition: its from and to stay empty, not %q and %q", e.Kind, e.From, e.To)
		}
		return nil
	}
	if to, ok := kindTo[e.Kind]; ok && e.To != to {
		return fmt.Errorf("a %s event ends in %q, not %q", e.Kind, to, e.To)
	}
	if e.Kind == KindBeadChanged && e.From == e.To && HasState(machine, e.To) {
		return nil
	}
	return Transition(machine, e.From, e.To)
}

// Batch is the plaintext of one `events` record: the events numbered From to
// To, every one of them, in order, all in the batch's Lane. An emergency
// record is a batch of one.
type Batch struct {
	From   uint64  `json:"from"`
	To     uint64  `json:"to"`
	Lane   string  `json:"lane"`
	Events []Event `json:"events"`
}

// Validate reports why b is not a batch the factory sends.
func (b Batch) Validate() error {
	if err := checkLane(b.Lane); err != nil {
		return err
	}
	switch {
	case len(b.Events) == 0:
		return errors.New("the batch holds no events")
	case b.From == 0:
		return errors.New("the batch starts at seq 0: seqs start at 1")
	case b.To < b.From:
		return fmt.Errorf("the batch runs from %d to %d", b.From, b.To)
	case uint64(len(b.Events)) != b.To-b.From+1:
		return fmt.Errorf("the batch holds %d events for seqs %d to %d", len(b.Events), b.From, b.To)
	}
	for i, e := range b.Events {
		n := i + 1
		if want := b.From + uint64(i); e.Seq != want {
			return fmt.Errorf("event %d has seq %d, not %d", n, e.Seq, want)
		}
		if e.Lane != b.Lane {
			return fmt.Errorf("event %d is in the %s lane, the batch in the %s lane", n, e.Lane, b.Lane)
		}
		if err := e.Validate(); err != nil {
			return fmt.Errorf("event %d (seq %d): %w", n, e.Seq, err)
		}
	}
	return nil
}
