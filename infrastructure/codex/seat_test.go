package codex

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain"
)

// seatLaunch is a complete seat launch, with whatever a test changes applied.
func seatLaunch(change func(*application.SeatLaunch)) application.SeatLaunch {
	l := application.SeatLaunch{
		Seat:    "mayor",
		Name:    "mayor-2026-10-28-01",
		Dir:     "/home/luke/luke-vault",
		Charter: "/home/luke/luke-vault/seats/mayor/charter.md",
		Model:   "gpt-6.1-sol",
		Effort:  domain.EffortHigh,
		Kickoff: "Boot by the charter. Newest handoff: seats/mayor/handoffs/2026-10-28-00.md",
	}
	if change != nil {
		change(&l)
	}
	return l
}

func TestASeatSessionIsTheInteractiveCodexLine(t *testing.T) {
	spec, err := New().SeatSession(seatLaunch(nil))
	if err != nil {
		t.Fatalf("assembling the seat's session: %v", err)
	}
	want := []string{
		"codex", "-m", "gpt-6.1-sol", "-c", "model_reasoning_effort=high",
		"--sandbox", "workspace-write", "-c", "sandbox_workspace_write.network_access=true",
		"-a", "never", seatLaunch(nil).Kickoff,
	}
	if !reflect.DeepEqual(spec.Command, want) {
		t.Errorf("the seat's command is\n%q\nwant\n%q", spec.Command, want)
	}
	if spec.Name != "mayor-2026-10-28-01" || spec.Dir != "/home/luke/luke-vault" {
		t.Errorf("expected the window to be named after the session and to run in the vault, got %+v", spec)
	}
	if got := spec.Env[application.SeatEnv]; got != "mayor" {
		t.Errorf("expected %s to be the seat itself, got %q", application.SeatEnv, got)
	}
}

func TestACodexSeatSessionIsInteractiveAndCarriesNoCharterFlag(t *testing.T) {
	spec, err := New().SeatSession(seatLaunch(nil))
	if err != nil {
		t.Fatal(err)
	}
	line := strings.Join(spec.Command, " ")
	// Codex has no system-prompt flag: the charter is reached by AGENTS.md at
	// the vault root, so the line names the charter nowhere.
	for _, unwanted := range []string{" exec", "--json", "--print", "charter.md", "timeout"} {
		if strings.Contains(line, unwanted) {
			t.Errorf("expected an interactive Codex session with no %q, got %q", unwanted, line)
		}
	}
}

func TestAnAttendedCodexSeatSessionAsksOnRequest(t *testing.T) {
	spec, err := New().SeatSession(seatLaunch(func(l *application.SeatLaunch) { l.Attended = true }))
	if err != nil {
		t.Fatal(err)
	}
	line := strings.Join(spec.Command, " ")
	if !strings.Contains(line, "-a on-request") || strings.Contains(line, "-a never") {
		t.Errorf("expected an attended seat to be asked (-a on-request), got %q", line)
	}
}

func TestACodexSeatSessionLeavesOutWhatItWasNotGiven(t *testing.T) {
	spec, err := New().SeatSession(seatLaunch(func(l *application.SeatLaunch) { l.Model, l.Effort = "", "" }))
	if err != nil {
		t.Fatal(err)
	}
	line := strings.Join(spec.Command, " ")
	for _, unwanted := range []string{"-m ", "model_reasoning_effort"} {
		if strings.Contains(line, unwanted) {
			t.Errorf("expected no %q when none was given, got %q", unwanted, line)
		}
	}
}

func TestACodexSeatSessionRefusesAModelOrEffortItCannotRun(t *testing.T) {
	for name, change := range map[string]func(*application.SeatLaunch){
		"a Claude model":    func(l *application.SeatLaunch) { l.Model = domain.ModelOpus },
		"an unknown effort": func(l *application.SeatLaunch) { l.Effort = "furious" },
		"no kickoff":        func(l *application.SeatLaunch) { l.Kickoff = "" },
	} {
		if _, err := New().SeatSession(seatLaunch(change)); err == nil {
			t.Errorf("expected %s to be refused", name)
		}
	}
}

func TestACodexSeatSessionNamesTheProgramTheHostHas(t *testing.T) {
	spec, err := New(WithProgram("/opt/codex/bin/codex")).SeatSession(seatLaunch(nil))
	if err != nil {
		t.Fatal(err)
	}
	if spec.Command[0] != "/opt/codex/bin/codex" {
		t.Errorf("expected the host's codex, got %q", spec.Command[0])
	}
}

// TestACodexSeatSessionReadsTheBeadsEnvironmentFirstAndPassesTheKickoffWhole
// runs the command with a fake codex in place of the real one: the env file is
// read, the server host is exported, and the kickoff, which holds quotes and a
// dollar sign, arrives as one argument.
func TestACodexSeatSessionReadsTheBeadsEnvironmentFirstAndPassesTheKickoffWhole(t *testing.T) {
	dir := t.TempDir()
	fake := filepath.Join(dir, "codex")
	out := filepath.Join(dir, "seen")
	writeScript(t, fake, `printf '%s\n' "$BEADS_FROM_FILE" "$BEADS_DOLT_SERVER_HOST" > `+out+`; for a in "$@"; do printf '%s\n' "$a"; done >> `+out)
	env := filepath.Join(dir, "beads.env")
	if err := os.WriteFile(env, []byte("BEADS_FROM_FILE=read\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	kickoff := "say \"hi\" to $HOME; it's 'fine'"
	h := New(WithProgram(fake), WithEnvFile(env), WithBeadsServerHost("127.0.0.1"))
	spec, err := h.SeatSession(seatLaunch(func(l *application.SeatLaunch) { l.Kickoff = kickoff }))
	if err != nil {
		t.Fatal(err)
	}
	if err := exec.Command(spec.Command[0], spec.Command[1:]...).Run(); err != nil {
		t.Fatalf("running the seat's command: %v", err)
	}
	seen, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(seen), "\n"), "\n")
	if lines[0] != "read" || lines[1] != "127.0.0.1" {
		t.Errorf("expected the env file read and the server host exported, got %q", lines[:2])
	}
	if last := lines[len(lines)-1]; last != kickoff {
		t.Errorf("expected the kickoff whole as the last argument, got %q", last)
	}
}
