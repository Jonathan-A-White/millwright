package application_test

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
)

// A landed story mw could not close is left with a hint: the bd command that
// closes it under the name that holds the claim. A person pastes that line into
// a shell, so it has to mean the same argv there that mw meant.

// landedButOpen is the report of a story that landed and could not be closed
// because somebody else's name holds the claim.
func landedButOpen(assignee string) application.NextReport {
	return application.NextReport{
		StoryID:   "mw-gq6.45",
		Host:      "laptop",
		Landed:    true,
		NotClosed: "assignee is " + assignee + ", actor is mw@laptop",
		Assignee:  assignee,
	}
}

// hintLine is the line of the report that carries the bd command, with the
// command cut out of its backticks.
func hintLine(t *testing.T, report string) string {
	t.Helper()
	for _, line := range strings.Split(report, "\n") {
		open := strings.Index(line, "`")
		close := strings.LastIndex(line, "`")
		if open >= 0 && close > open && strings.Contains(line, "bd ") {
			return line[open+1 : close]
		}
	}
	t.Fatalf("expected the report to carry a bd command in backticks, got:\n%s", report)
	return ""
}

// argvAsAShellReadsIt runs the command with a shell, with bd standing in as a
// program that prints what it was given one word per line, and returns those
// words.
func argvAsAShellReadsIt(t *testing.T, command string) []string {
	t.Helper()
	if !strings.HasPrefix(command, "bd ") {
		t.Fatalf("expected the hint to be a bd command, got %q", command)
	}
	script := "bd() { for word in \"$@\"; do printf '%s\\n' \"$word\"; done; }\n" + command
	out, err := exec.Command("sh", "-c", script).Output()
	if err != nil {
		t.Fatalf("expected %q to run in a shell, got: %v", command, err)
	}
	return strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
}

func TestTheHintForAClaimUnderANameWithASpaceIsOneShellCommandAsItStands(t *testing.T) {
	said := landedButOpen("Jonathan White").String()

	got := argvAsAShellReadsIt(t, hintLine(t, said))

	want := []string{"--actor", "Jonathan White", "close", "mw-gq6.45", "--reason", "landed"}
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("expected a shell to read the hint as %q, got %q", want, got)
	}
}

func TestTheHintSurvivesANameWithAQuoteAndAShellCharacterInIt(t *testing.T) {
	for _, name := range []string{"O'Brien", "a b;c", "$HOME", `back\slash`, "two  spaces"} {
		got := argvAsAShellReadsIt(t, hintLine(t, landedButOpen(name).String()))
		if len(got) < 2 || got[0] != "--actor" || got[1] != name {
			t.Errorf("expected a shell to read %q as one word after --actor, got %q", name, got)
		}
	}
}

func TestTheHintLeavesAPlainNameUnquoted(t *testing.T) {
	said := landedButOpen("root").String()
	if !strings.Contains(said, "`bd --actor root close mw-gq6.45 --reason landed`") {
		t.Errorf("expected a plain name to be left as it is, got:\n%s", said)
	}
}

func TestTheRefusalSaysAClaimUnderAnotherNamePredatesMwsOwn(t *testing.T) {
	said := landedButOpen("Jonathan White").String()
	for _, want := range []string{"Jonathan White", "mw@laptop", "predates"} {
		if !strings.Contains(said, want) {
			t.Errorf("expected the report to say %q, got:\n%s", want, said)
		}
	}
}

func TestAClaimUnderMwsOwnNameIsNotSaidToPredateIt(t *testing.T) {
	said := landedButOpen("mw@laptop").String()
	if strings.Contains(said, "predates") {
		t.Errorf("expected no talk of a claim predating mw's name when mw holds it, got:\n%s", said)
	}
}

func TestAStoryThatWasClosedSaysNothingOfAClaim(t *testing.T) {
	report := landedButOpen("Jonathan White")
	report.Closed, report.NotClosed = true, ""
	said := report.String()
	if strings.Contains(said, "--actor") || strings.Contains(said, "predates") {
		t.Errorf("expected a closed story to carry no hint, got:\n%s", said)
	}
}
