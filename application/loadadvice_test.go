package application_test

import (
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
)

// A Builder and a batch's fixer that reproduced a load fault ran forty
// hand-made CPU burners on the home host for 40 minutes, load 55 to 65 of 20
// cores (mw-gq6.1529; the same fault as mw-gq6.483). Every prompt that starts a
// session able to run a command says the one bounded way, with its numbers.
func loadRules() []string {
	return []string{
		"contrib/mw-under-load",
		"never hand-made burners",
		"4 busy loops",
		"cores less its 1-minute load",
		"30 minutes",
	}
}

func TestEveryPromptThatStartsASessionNamesTheBoundedWayToMakeLoad(t *testing.T) {
	brief := application.BatchFixBrief{Rig: "lampas", Number: 575}
	for name, prompt := range map[string]string{
		"builder kickoff":     application.KickoffPrompt("builder", "mw-gq6.6", "/vault", nil),
		"rebase kickoff":      application.RebaseKickoffPrompt("builder", "mw-gq6.6", "/vault", "main"),
		"merge-fix kickoff":   application.MergeFixKickoffPrompt("builder", "mw-gq6.6", "/vault", "main"),
		"tester kickoff":      application.TesterKickoffPrompt("tester", "mw-gq6.6", "/vault"),
		"batch fixer kickoff": application.BatchFixKickoffPrompt("builder", brief, "/vault"),
		"batch fixer rules":   application.BatchFixRules(),
	} {
		for _, want := range loadRules() {
			if !strings.Contains(prompt, want) {
				t.Errorf("%s: expected it to contain %q, got: %s", name, want, prompt)
			}
		}
	}
}
