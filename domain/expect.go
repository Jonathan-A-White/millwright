package domain

import (
	"errors"
	"fmt"
	"strings"
)

// FactoryRig is the rig the factory itself is built from. Every story of it
// says what it expects to move.
const FactoryRig = "millwright"

// ExpectNone is the metric a story names when it does not expect to move one.
const ExpectNone = "none"

// ErrNoExpect says acceptance criteria carry no line beginning Expect:.
var ErrNoExpect = errors.New("no line begins with 'Expect:'")

// ExpectForms are the Expect lines a story may carry, as a refusal names them.
const ExpectForms = "'Expect: <metric> up' or 'Expect: <metric> down', or 'Expect: none, because <why>'"

// Expect is what a millwright story says it should move: one metric of the
// health report and the way it should go. The metric none has no direction.
type Expect struct {
	Metric    string `json:"metric"`
	Direction string `json:"direction,omitempty"`
}

// String is the Expect as a story spells it after the colon.
func (e Expect) String() string {
	if e.Direction == "" {
		return e.Metric
	}
	return e.Metric + " " + e.Direction
}

// ExpectMetrics are the metrics a story may name, in the order the report shows them.
func ExpectMetrics() []string {
	return []string{"refusals", "attempts/landing", "bugs/landing", "dispatch-to-landing", "tokens/landing", "$/landing"}
}

// ParseExpect reads the 'Expect: <metric> <up|down>' line of a story's
// acceptance criteria, or 'Expect: none, because <why>'. It reports
// ErrNoExpect when there is no such line and another error when the first
// such line cannot be read.
func ParseExpect(acceptance string) (Expect, error) {
	lines := LinesBeginning(acceptance, "Expect")
	if len(lines) == 0 {
		return Expect{}, ErrNoExpect
	}
	text := lines[0]
	words := strings.Fields(strings.ToLower(text))
	trim := func(w string) string { return strings.TrimRight(w, ".,;:") }
	if len(words) > 0 && trim(words[0]) == ExpectNone {
		if len(words) < 3 || trim(words[1]) != "because" {
			return Expect{}, fmt.Errorf("'Expect: %s' gives no reason: it must read 'Expect: none, because <why>'", text)
		}
		return Expect{Metric: ExpectNone}, nil
	}
	if len(words) >= 2 {
		metric, direction := trim(words[0]), trim(words[1])
		if isExpectMetric(metric) && (direction == "up" || direction == "down") {
			return Expect{Metric: metric, Direction: direction}, nil
		}
	}
	return Expect{}, fmt.Errorf("'Expect: %s' is not 'Expect: <metric> <up|down>' with a metric of %s, or 'Expect: none, because <why>'",
		text, strings.Join(ExpectMetrics(), ", "))
}

func isExpectMetric(metric string) bool {
	for _, m := range ExpectMetrics() {
		if m == metric {
			return true
		}
	}
	return false
}

// OwesAnExpect says whether a story of rig must carry an Expect line: every
// story of the factory's own rig does.
func OwesAnExpect(rig string) bool { return rig == FactoryRig }

// ExpectRefusal is what mw file says of a factory story whose Expect line is
// missing or unreadable.
func ExpectRefusal(err error) string {
	return fmt.Sprintf("it is a %s story and %s: it needs a line %s", FactoryRig, err, ExpectForms)
}
