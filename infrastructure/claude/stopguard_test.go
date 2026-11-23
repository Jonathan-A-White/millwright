package claude

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
)

// stopGuardLaunch is a Builder's launch with the guard on: mw's own program and
// the words before the story's id, as the SeatBoot gives them.
func stopGuardLaunch() application.Launch {
	l := launch(nil)
	l.StepGuard = []string{"/usr/local/bin/mw", "step", "guard"}
	return l
}

func settingsOf(t *testing.T, l application.Launch, opts ...Option) string {
	t.Helper()
	spec, err := New(opts...).Session(l)
	if err != nil {
		t.Fatalf("assembling the session: %v", err)
	}
	return settingsFromLine(t, spec.Command[2])
}

// TestSessionSettingsCarriesTheStopGuard: a launch that names the guard has a
// Stop hook running it on the story, and nothing else of the settings moves.
func TestSessionSettingsCarriesTheStopGuard(t *testing.T) {
	got := settingsOf(t, stopGuardLaunch())

	var doc map[string]any
	if err := json.Unmarshal([]byte(got), &doc); err != nil {
		t.Fatalf("settings %q are not JSON: %v", got, err)
	}
	var hooks struct {
		Stop []struct {
			Hooks []struct {
				Type, Command string
			} `json:"hooks"`
		}
	}
	raw, _ := json.Marshal(doc["hooks"])
	if err := json.Unmarshal(raw, &hooks); err != nil {
		t.Fatalf("hooks %s do not read: %v", raw, err)
	}
	if len(hooks.Stop) != 1 || len(hooks.Stop[0].Hooks) != 1 {
		t.Fatalf("expected one Stop hook, got %s", raw)
	}
	hook := hooks.Stop[0].Hooks[0]
	if hook.Type != "command" || hook.Command != "/usr/local/bin/mw step guard mw-gq6.6" {
		t.Errorf("expected the command hook `/usr/local/bin/mw step guard mw-gq6.6`, got %+v", hook)
	}
	if len(doc["hooks"].(map[string]any)) != 1 {
		t.Errorf("expected no hook but Stop, got %s", raw)
	}

	// Everything else is byte for byte what a session without the guard has.
	delete(doc, "hooks")
	rest, _ := json.Marshal(doc)
	if string(rest) != SessionSettings {
		t.Errorf("the rest of the settings moved:\n got %s\nwant %s", rest, SessionSettings)
	}
}

func TestTheStopGuardComesWithTheTestsRulesAndTheDenyRules(t *testing.T) {
	got := settingsOf(t, stopGuardLaunch(), WithTests(map[string]string{"millwright": "make test"}), WithDenyRules("Bash(scp *)"))
	for _, want := range []string{`"Stop"`, `Bash(make test)`, `Bash(scp *)`} {
		if !strings.Contains(got, want) {
			t.Errorf("expected %s in %s", want, got)
		}
	}
}

// TestNoStopGuardLeavesTheSettingsAsTheyWere: a launch that names no guard (the
// knob closeout.open_steps_guard is false) has no hook, and the settings are
// SessionSettings, byte for byte.
func TestNoStopGuardLeavesTheSettingsAsTheyWere(t *testing.T) {
	if got := settingsOf(t, launch(nil)); got != SessionSettings {
		t.Errorf("expected SessionSettings, got %s", got)
	}
	with := settingsOf(t, launch(nil), WithTests(map[string]string{"millwright": "make test"}))
	if strings.Contains(with, "hooks") {
		t.Errorf("expected no hook, got %s", with)
	}
}

func TestTheStopGuardsCommandQuotesWhatNeedsIt(t *testing.T) {
	l := stopGuardLaunch()
	l.StepGuard = []string{"/opt/my tools/mw", "step", "guard"}
	spec, err := New().Session(l)
	if err != nil {
		t.Fatalf("assembling the session: %v", err)
	}
	// The hook's command is in a JSON string, which the shell line quotes again:
	// the program's own quotes come out as '\'' there.
	if want := `'\''/opt/my tools/mw'\'' step guard mw-gq6.6`; !strings.Contains(spec.Command[2], want) {
		t.Errorf("expected the program quoted as %s, got %s", want, spec.Command[2])
	}
}
