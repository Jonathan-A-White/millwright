package application

import (
	"context"
	"strings"
	"testing"
	"time"
)

// A re-scope that pins the acceptance and a Mayor comment after the newest
// commit that lacks the lines a closing comment needs: the refusal says so, and
// quotes the lines (mw-gq6.1486).
func TestAcceptanceChangedStopNamesTheLinesAMayorCommentLacks(t *testing.T) {
	cases := map[string]string{
		"no rig memory line":        "Re-scoped.\n\nInternal: nothing for the Governor to look at",
		"no how-to-check line":      "Re-scoped.\n\nFor the rig memory:\nnothing",
		"neither line (prose only)": "Re-scoped, the work is done as written, please land it.",
	}
	for name, text := range cases {
		n, c, _ := rescopedStory(func(committed time.Time) []Comment {
			return []Comment{
				{Author: "Mayor", Created: committed.Add(20 * time.Minute), Text: text},
				rescopeComment("AC reworded", "1. One.\n2. Two, reworded.", committed.Add(25*time.Minute)),
			}
		})
		refusal, refused := n.acceptanceRefusal(context.Background(), c, &NextReport{}, "main")
		if !refused || refusal.Reason != ReasonAcceptanceChanged {
			t.Fatalf("%s: expected a refusal for %s, got %+v, %v", name, ReasonAcceptanceChanged, refusal, refused)
		}
		for _, want := range []string{
			"RESCOPED-BY-MAYOR:", "closing comment", "'For the rig memory'", "'HOW TO CHECK IT'", "'Internal: nothing for the Governor to look at'",
		} {
			if !strings.Contains(refusal.Said, want) {
				t.Errorf("%s: expected the refusal to say %q, got:\n%s", name, want, refusal.Said)
			}
		}
		if strings.Contains(refusal.Said, RescopeRecipe(c.id)+" lands it") {
			t.Errorf("%s: expected no offer of the recipe the Mayor has already run, got:\n%s", name, refusal.Said)
		}
	}
}

func TestAcceptanceChangedStopWithAPinAndNoMayorCommentAfterTheCommitSaysSo(t *testing.T) {
	n, c, _ := rescopedStory(func(committed time.Time) []Comment {
		return []Comment{
			{Author: "builder@cloud2", Created: committed.Add(5 * time.Minute), Text: rescopeClosing},
			rescopeComment("AC reworded", "1. One.\n2. Two, reworded.", committed.Add(25*time.Minute)),
		}
	})
	refusal, refused := n.acceptanceRefusal(context.Background(), c, &NextReport{}, "main")
	if !refused {
		t.Fatal("expected a refusal")
	}
	for _, want := range []string{"RESCOPED-BY-MAYOR:", "'For the rig memory'", "'HOW TO CHECK IT'"} {
		if !strings.Contains(refusal.Said, want) {
			t.Errorf("expected the refusal to say %q, got:\n%s", want, refusal.Said)
		}
	}
}

func TestAcceptanceChangedStopWithNoPinSaysTheRescopeCommand(t *testing.T) {
	n, c, _ := rescopedStory(func(committed time.Time) []Comment {
		return []Comment{{Author: "Mayor", Created: committed.Add(20 * time.Minute), Text: rescopeClosing}}
	})
	refusal, refused := n.acceptanceRefusal(context.Background(), c, &NextReport{}, "main")
	if !refused || refusal.Reason != ReasonAcceptanceChanged {
		t.Fatalf("expected a refusal for %s, got %+v, %v", ReasonAcceptanceChanged, refusal, refused)
	}
	if !strings.Contains(refusal.Said, RescopeRecipe(c.id)) {
		t.Errorf("expected the refusal to say %q, got:\n%s", RescopeRecipe(c.id), refusal.Said)
	}
	if strings.Contains(refusal.Said, "pins the acceptance") {
		t.Errorf("expected no word of a pin that is not there, got:\n%s", refusal.Said)
	}
}

func TestOpenStepsWayOutNamesTheLinesAClosingCommentNeeds(t *testing.T) {
	said := OpenStepsWayOut("mw-t.1")
	for _, want := range []string{"'For the rig memory'", "'HOW TO CHECK IT'", "'Internal: nothing for the Governor to look at'"} {
		if !strings.Contains(said, want) {
			t.Errorf("expected the way out to say %q, got %q", want, said)
		}
	}
}
