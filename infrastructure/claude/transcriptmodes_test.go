package claude

import (
	"strings"

	"context"
	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain"
	"testing"
	"time"
)

// The permission mode a session ran in is read from its newest transcript
// (mw-gq6.545): a Haiku Tester on the desktop wrote "permissionMode":"default"
// there while every other session wrote "auto".
func TestPermissionModeIsWhatTheNewestTranscriptShows(t *testing.T) {
	root := t.TempDir()
	dir := "/home/jwhite/.mw-worktrees/mw-5r3p30.386"
	morning := time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC)
	inMode := func(mode string) string {
		return `{"type":"user","permissionMode":"` + mode + `","message":{"role":"user","content":"go"}}`
	}
	toolResult := `{"type":"user","message":{"role":"user","content":[{"type":"tool_result","content":"ok"}]}}`

	writeTranscript(t, root, dir, "11111111-old", morning, inMode("default"), toolResult)
	writeTranscript(t, root, dir, "22222222-new", morning.Add(time.Hour), `{"type":"agent-name","agentName":"x"}`, inMode("auto"), toolResult, `{"type":"user","mess`)

	got, err := NewTranscripts(root).PermissionMode(context.Background(), dir)
	if err != nil || got != "auto" {
		t.Fatalf("expected the newest transcript's auto, got %q: %v", got, err)
	}

	writeTranscript(t, root, dir, "33333333-newer", morning.Add(2*time.Hour), inMode("auto"), toolResult, inMode("default"))
	got, err = NewTranscripts(root).PermissionMode(context.Background(), dir)
	if err != nil || got != "default" {
		t.Fatalf("expected the mode that is not auto to be named, got %q: %v", got, err)
	}
}

func TestPermissionModeOfADirectoryWithNoModeIsEmpty(t *testing.T) {
	root := t.TempDir()
	got, err := NewTranscripts(root).PermissionMode(context.Background(), "/work/none")
	if err != nil || got != "" {
		t.Fatalf("expected no mode and no error for no transcript, got %q: %v", got, err)
	}
	dir := "/work/silent"
	writeTranscript(t, root, dir, "44444444", time.Now(), `{"type":"user","message":{"role":"user","content":"go"}}`)
	got, err = NewTranscripts(root).PermissionMode(context.Background(), dir)
	if err != nil || got != "" {
		t.Fatalf("expected no mode for a transcript that records none, got %q: %v", got, err)
	}
}

// A story session is started on its model's full id, not on the alias: the
// alias is resolved by the Claude Code on the host, and the desktop's resolved
// haiku to claude-haiku-4-5, a model that does not run in auto mode, so its
// Haiku Tester ran in permission mode default (mw-gq6.545).
func TestAStorySessionIsStartedOnTheModelsFullID(t *testing.T) {
	for model, want := range map[string]string{
		"haiku":  "--model claude-haiku-5-5 ",
		"sonnet": "--model claude-sonnet-5-5 ",
		"opus":   "--model claude-opus-5-5 ",
		"fable":  "--model claude-fable-5-1 ",
	} {
		spec, err := New().Session(launch(func(l *application.Launch) { l.Path.Model = domain.Model(model) }))
		if err != nil {
			t.Fatalf("assembling the %s session: %v", model, err)
		}
		if line := spec.Command[2]; !strings.Contains(line, want) {
			t.Errorf("expected %q in the %s session's command, got %q", want, model, line)
		}
	}
}
