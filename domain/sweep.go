package domain

import (
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"
)

// SweepRuleFrom is when the sweep rule began: a bug filed before it is never
// asked for a sweep, a bug filed from it on is.
var SweepRuleFrom = time.Date(2026, time.October, 15, 0, 0, 0, 0, time.UTC)

// SweepVerdict is what a bug's acceptance criteria say of a sweep: a line
// beginning "Sweep:", a check over the whole class of the fault and not only the
// case seen.
type SweepVerdict int

const (
	// SweepGiven: a Sweep: line names a check, or says "none, because <reason>".
	SweepGiven SweepVerdict = iota
	// SweepMissing: no line begins with "Sweep:".
	SweepMissing
	// SweepEmpty: a Sweep: line says nothing after the colon.
	SweepEmpty
	// SweepNoneWithoutReason: "Sweep: none" with no reason after it.
	SweepNoneWithoutReason
)

// Passes says whether the acceptance meets the rule.
func (v SweepVerdict) Passes() bool { return v == SweepGiven }

// String is what is wrong, as a person reads it, or "has one" when nothing is.
func (v SweepVerdict) String() string {
	switch v {
	case SweepMissing:
		return "no line begins with 'Sweep:'"
	case SweepEmpty:
		return "its 'Sweep:' line says nothing after the colon"
	case SweepNoneWithoutReason:
		return "its 'Sweep: none' gives no reason: it must read 'Sweep: none, because <reason>'"
	}
	return "has a Sweep line"
}

// SweepRuleName is the name of the rule, as mw dispatch gives it for a bug it
// does not claim for want of a sweep.
const SweepRuleName = "no-sweep"

// Refusal is what is wrong with a bug whose acceptance this verdict is of,
// and what would be right, as mw dispatch says it.
func (v SweepVerdict) Refusal() string {
	return fmt.Sprintf("it is a bug and %s (a check over the whole class of the fault, or 'Sweep: none, because <reason>')", v)
}

// FilingRefusal is the Refusal and the rule mw dispatch would refuse the bug
// by. mw file and mw tester file give it before anything is filed, so that a
// bug is never filed only for dispatch to refuse it (mw-gq6.447).
func (v SweepVerdict) FilingRefusal() string {
	return v.Refusal() + ": mw dispatch would refuse it as " + SweepRuleName
}

// SweepOf reads the acceptance criteria for a sweep line, or a numbered Sweep
// item run on in a line after others, by LinesBeginning.
// The best line wins: one that passes beats one that does not.
func SweepOf(acceptance string) SweepVerdict {
	verdict := SweepMissing
	for _, text := range LinesBeginning(acceptance, "Sweep") {
		got := sweepVerdict(text)
		if got == SweepGiven {
			return SweepGiven
		}
		verdict = got
	}
	return verdict
}

func sweepVerdict(text string) SweepVerdict {
	if text == "" {
		return SweepEmpty
	}
	lower := strings.ToLower(text)
	if rest, none := strings.CutPrefix(lower, "none"); none {
		// "nonexistent routes" is a sweep that starts with a word, not a none.
		if rest != "" && unicode.IsLetter(rune(rest[0])) {
			return SweepGiven
		}
		reason, because := strings.CutPrefix(strings.TrimLeft(rest, ", "), "because")
		if !because || strings.Trim(reason, " \t.,:;-") == "" {
			return SweepNoneWithoutReason
		}
	}
	return SweepGiven
}

// OwesASweep says whether a story must carry a Sweep line: it is a bug (by type,
// by [bug] title or by the bug label) filed on or after SweepRuleFrom. A story
// the tracker did not date was filed before the rule.
func OwesASweep(storyType, title string, labels []string, created time.Time) bool {
	if created.IsZero() || created.Before(SweepRuleFrom) {
		return false
	}
	return IsBug(storyType, title, labels)
}

// IsBug says whether a story is a bug: by its type, its [bug] title or the bug
// label.
func IsBug(storyType, title string, labels []string) bool {
	return IsBugStory(storyType, title) || slices.Contains(labels, "bug")
}
