package application

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
)

// The ways a bug names its check, in the order dispatch looks for them
// (mw-gq6.775).
const (
	CheckFromSuite = "the suite's spec"
	CheckFromLine  = "a Check: line"
	CheckFromEval  = "a grist eval line"
	CheckFromTitle = "a spec path in the title"

	noCheckNamed    = "no check named: dispatching"
	noGristKeyHere  = "no key on this host: dispatching"
	checkLinePrefix = "check:"
)

// BugCheck is the check a bug names for itself: one spec to run alone on its
// rig's main, or the examples of one grind to send with mw grist eval.
type BugCheck struct {
	// Source is how the bug named it (the CheckFrom words).
	Source string
	// Spec is the test file, when the check is one spec.
	Spec string
	// App, Kind and Examples are the grind and its examples, when the check is
	// mw grist eval <App> <Kind> --example <Examples>.
	App, Kind string
	Examples  []string
}

// IsExample says the check sends grist examples and not a spec.
func (c BugCheck) IsExample() bool { return c.App != "" }

// String is the check as the dispatch line and the closing reason name it.
func (c BugCheck) String() string {
	if !c.IsExample() {
		return c.Spec
	}
	return fmt.Sprintf("mw grist eval %s %s --example %s", c.App, c.Kind, strings.Join(c.Examples, " --example "))
}

// bugCheck is the check a bug names, and whether dispatch re-runs the story at
// all: a story that is not a bug, or a flaky file's bug, is never re-run, and
// says nothing of it. A bug that names no check is asked about, and found is
// false. The order is the suite spec field, a line beginning "Check:", a line
// running mw grist eval with --example, a test file in the title.
func bugCheck(detail StoryDetail, files string) (check BugCheck, found, asked bool) {
	if spec, suite := suiteSpec(detail, files); suite {
		return BugCheck{Source: CheckFromSuite, Spec: spec}, true, true
	}
	title := detail.Story.Title
	if !hasLabel(detail.Labels, LabelBug) && !strings.HasPrefix(strings.ToLower(strings.TrimSpace(title)), "[bug]") {
		return BugCheck{}, false, false
	}
	if flaky := testFileName.FindString(title); flaky != "" && strings.Contains(title, flaky+flakyOnMain) {
		return BugCheck{}, false, false
	}
	for _, text := range []string{detail.Description, detail.Acceptance} {
		for _, line := range strings.Split(text, "\n") {
			line = strings.TrimSpace(line)
			if len(line) < len(checkLinePrefix) || strings.ToLower(line[:len(checkLinePrefix)]) != checkLinePrefix {
				continue
			}
			rest := line[len(checkLinePrefix):]
			if one, ok := gristEvalCheck(rest); ok {
				one.Source = CheckFromLine
				return one, true, true
			}
			if spec := testFileName.FindString(rest); spec != "" {
				return BugCheck{Source: CheckFromLine, Spec: spec}, true, true
			}
		}
	}
	for _, text := range []string{detail.Description, detail.Acceptance} {
		if one, ok := gristEvalCheck(text); ok {
			one.Source = CheckFromEval
			return one, true, true
		}
	}
	if spec := testFileName.FindString(title); spec != "" {
		return BugCheck{Source: CheckFromTitle, Spec: spec}, true, true
	}
	return BugCheck{}, false, true
}

// gristEvalCheck reads the first `mw grist eval <app> <kind> --example <name>`
// in text: the app and the kind, then every example named. A line that reads a
// ref (--ref), which is no check of main, or names no example, is none.
func gristEvalCheck(text string) (BugCheck, bool) {
	for _, line := range strings.Split(text, "\n") {
		_, rest, found := strings.Cut(line, "mw grist eval ")
		if !found {
			continue
		}
		var positional, examples []string
		fields := strings.Fields(strings.NewReplacer("`", " ", "'", " ", `"`, " ").Replace(rest))
		ref := false
		for i := 0; i < len(fields); i++ {
			field := strings.TrimRight(fields[i], ".,;:)")
			switch {
			case field == "--example" && i+1 < len(fields):
				i++
				examples = append(examples, strings.TrimRight(fields[i], ".,;:)"))
			case strings.HasPrefix(field, "--example="):
				examples = append(examples, strings.TrimPrefix(field, "--example="))
			case field == "--ref" || strings.HasPrefix(field, "--ref="):
				ref = true
			case field == "--wait" && i+1 < len(fields):
				i++
			case strings.HasPrefix(field, "-"):
			case len(examples) == 0 && len(positional) < 2:
				positional = append(positional, field)
			}
		}
		if len(positional) == 2 && len(examples) > 0 && !ref {
			return BugCheck{App: positional[0], Kind: positional[1], Examples: examples}, true
		}
	}
	return BugCheck{}, false
}

// ErrNoGristKey is why a grist example was not sent: this host holds no grist
// test key.
var ErrNoGristKey = errors.New("no grist test key on this host")

// ExampleRechecker sends the examples of an app's grind of kind as they stand on
// the app's main. SpecRecheck.Passed is whether every example met its expect;
// an error is a send that could not be made, ErrNoGristKey among them.
type ExampleRechecker interface {
	RecheckExamples(ctx context.Context, app, kind string, examples []string) (SpecRecheck, error)
}

// GristExampleRecheck is the ExampleRechecker that runs mw grist eval <app> <kind>
// --example against the app's main through the backend, as the test key. It sends
// nothing from a host without the key.
type GristExampleRecheck struct {
	Smoke GristSmoke
	// HasKey says whether this host holds the grist test key; nil holds it.
	HasKey func() bool
}

var _ ExampleRechecker = GristExampleRecheck{}

// RecheckExamples implements ExampleRechecker.
func (g GristExampleRecheck) RecheckExamples(ctx context.Context, app, kind string, examples []string) (SpecRecheck, error) {
	if g.HasKey != nil && !g.HasKey() {
		return SpecRecheck{}, ErrNoGristKey
	}
	_, commit, err := g.Smoke.open(ctx, "mw grist eval", app)
	if err != nil {
		return SpecRecheck{}, err
	}
	var out bytes.Buffer
	err = GristEvalExamples{Smoke: g.Smoke, Out: &out}.Run(ctx, app, kind, examples)
	found := SpecRecheck{
		Spec: BugCheck{App: app, Kind: kind, Examples: examples}.String(), Sha: commit,
		Tail: lastLines(out.String(), SpecRecheckTail),
	}
	switch {
	case err == nil:
		found.Passed = true
	case errors.Is(err, ErrGristEvalFailed):
	default:
		return SpecRecheck{}, err
	}
	return found, nil
}

// lastLines is the last n non-empty lines of text.
func lastLines(text string, n int) string {
	var lines []string
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
