package application_test

import (
	"reflect"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
)

func TestTheFiveProposalFormsAndNothingAreParsedTyped(t *testing.T) {
	comment := "Done.\n\nFor the rig memory:\n" +
		"gotcha [bd]: Point every bd call at the vault with -C.\n" +
		"decision [tmux]: Tests use a private socket.\n" +
		"supersede bd-dash-c: Use -C, as its own call.\n" +
		"retire old-note: The script it described is gone.\n" +
		"recheck some-fact: bd 1.4 may have changed it.\n" +
		"nothing\n"
	got := application.ParseFactProposals(comment)
	want := []application.FactProposal{
		{Verb: application.ProposeGotcha, Subject: "bd", Sentence: "Point every bd call at the vault with -C."},
		{Verb: application.ProposeDecision, Subject: "tmux", Sentence: "Tests use a private socket."},
		{Verb: application.ProposeSupersede, Slug: "bd-dash-c", Sentence: "Use -C, as its own call."},
		{Verb: application.ProposeRetire, Slug: "old-note", Sentence: "The script it described is gone."},
		{Verb: application.ProposeRecheck, Slug: "some-fact", Sentence: "bd 1.4 may have changed it."},
		{Verb: application.ProposeNothing},
	}
	if !got.Found {
		t.Fatalf("the section was not found")
	}
	if len(got.Lines) != len(want) {
		t.Fatalf("got %d lines, want %d: %+v", len(got.Lines), len(want), got.Lines)
	}
	for i, line := range got.Lines {
		if line.Malformed != "" {
			t.Errorf("line %d was flagged malformed: %q", i, line.Malformed)
		}
		if !reflect.DeepEqual(line.Proposal, want[i]) {
			t.Errorf("line %d: got %+v, want %+v", i, line.Proposal, want[i])
		}
	}
}

func TestAMalformedProposalLineIsNamed(t *testing.T) {
	for name, line := range map[string]string{
		"a missing colon":       "gotcha [bd] Point every bd call at the vault",
		"an unknown verb":       "remember [bd]: Something.",
		"an empty sentence":     "gotcha [bd]:",
		"a missing subject":     "decision: No subject given.",
		"a slug with a space":   "retire two words: Because.",
		"a verb alone":          "supersede",
		"prose that is no verb": "The tests were slow.",
	} {
		got := application.ParseFactProposals("For the rig memory:\n" + line + "\n")
		if len(got.Lines) != 1 || got.Lines[0].Malformed != line {
			t.Errorf("%s: got %+v, want one malformed line %q", name, got.Lines, line)
		}
	}
}

func TestTheSectionRunsFromItsHeadingToTheEndAndTakesListMarkers(t *testing.T) {
	got := application.ParseFactProposals("gotcha [x]: before the heading, not read.\n" +
		"For the rig memory: nothing\n")
	if len(got.Lines) != 1 || got.Lines[0].Proposal.Verb != application.ProposeNothing {
		t.Errorf("a line after the heading's colon: got %+v", got.Lines)
	}

	got = application.ParseFactProposals("For the rig memory:\n\n- gotcha [bd]: A bulleted one.\n2. nothing.\n")
	if len(got.Lines) != 2 || got.Lines[0].Proposal.Sentence != "A bulleted one." || got.Lines[1].Proposal.Verb != application.ProposeNothing {
		t.Errorf("bullets and blanks: got %+v", got.Lines)
	}

	if got := application.ParseFactProposals("Done. Nothing else."); got.Found {
		t.Errorf("a comment with no section was found to have one: %+v", got)
	}
}
