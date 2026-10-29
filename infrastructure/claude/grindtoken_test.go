package claude_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/infrastructure/claude"
)

// tokenFile is a claude.env holding a fixture token, as boost-bootstrap writes it.
func tokenFile(t *testing.T, token string) string {
	t.Helper()
	file := filepath.Join(t.TempDir(), "claude.env")
	if err := os.WriteFile(file, []byte("CLAUDE_CODE_OAUTH_TOKEN="+token+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return file
}

// A grind's session starts on the host's one-year token when claude.env is there:
// CLAUDE_CODE_OAUTH_TOKEN is in the child's environment and in no argument.
func TestGrindReadsTheClaudeEnvFile(t *testing.T) {
	const token = "fixture-token"
	os.Unsetenv("CLAUDE_CODE_OAUTH_TOKEN")
	program, seen := grindStandIn(t, "printf '%s' \"$CLAUDE_CODE_OAUTH_TOKEN\" > \"$(dirname \"$0\")/token\"\ncat <<'EOF'\n"+liveResult+"\nEOF\n")
	grinder := claude.NewGrinder(claude.WithGrindProgram(program), claude.WithGrindClaudeEnvFile(tokenFile(t, token)))
	if _, err := grinder.Grind(context.Background(), aGrindCall(t, t.TempDir())); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(filepath.Join(seen, "token")); string(got) != token {
		t.Errorf("expected the grind's claude to hold the token, got %q", got)
	}
	if args, _ := os.ReadFile(filepath.Join(seen, "args")); strings.Contains(string(args), token) {
		t.Errorf("expected the token on no command line, got %q", args)
	}
}

// With no claude.env, none named, or an unreadable one, the grind's environment is what it was.
func TestGrindWithoutAClaudeEnvFileHasNoToken(t *testing.T) {
	os.Unsetenv("CLAUDE_CODE_OAUTH_TOKEN")
	for name, option := range map[string]claude.GrinderOption{
		"missing": claude.WithGrindClaudeEnvFile(filepath.Join(t.TempDir(), "claude.env")),
		"empty":   claude.WithGrindClaudeEnvFile(""),
	} {
		program, seen := grindStandIn(t, "printf '%s' \"${CLAUDE_CODE_OAUTH_TOKEN-unset}\" > \"$(dirname \"$0\")/token\"\ncat <<'EOF'\n"+liveResult+"\nEOF\n")
		if _, err := claude.NewGrinder(claude.WithGrindProgram(program), option).Grind(context.Background(), aGrindCall(t, t.TempDir())); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got, _ := os.ReadFile(filepath.Join(seen, "token")); string(got) != "unset" {
			t.Errorf("%s: expected no token, got %q", name, got)
		}
	}
}

// A warm session starts on the token too.
func TestWarmSessionReadsTheClaudeEnvFile(t *testing.T) {
	const token = "fixture-token"
	os.Unsetenv("CLAUDE_CODE_OAUTH_TOKEN")
	program, seen := warmStandIn(t, "printf '%s' \"$CLAUDE_CODE_OAUTH_TOKEN\" > \"$(dirname \"$0\")/token\"\ncat <<'EOF'\n"+streamResult(t)+"\nEOF\n")
	grinder := claude.NewGrinder(claude.WithGrindProgram(program), claude.WithGrindClaudeEnvFile(tokenFile(t, token)))
	session, err := grinder.Warm(aWarmCall(t, t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.Answer(context.Background(), "go\n", 5*time.Second); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(filepath.Join(seen, "token")); string(got) != token {
		t.Errorf("expected the warm session's claude to hold the token, got %q", got)
	}
}

// ClaudeEnv reads shell assignments the way a session's shell would: export,
// quotes, comments and blank lines, and takes nothing else.
func TestClaudeEnvReadsAssignments(t *testing.T) {
	file := filepath.Join(t.TempDir(), "claude.env")
	text := "# the token\n\nexport CLAUDE_CODE_OAUTH_TOKEN='sk-1'\nOTHER=\"two words\"\nnot an assignment\n9BAD=x\n"
	if err := os.WriteFile(file, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(claude.ClaudeEnv(file), "\n")
	if want := "CLAUDE_CODE_OAUTH_TOKEN=sk-1\nOTHER=two words"; got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
	if claude.ClaudeEnv(filepath.Join(t.TempDir(), "none")) != nil || claude.ClaudeEnv("") != nil {
		t.Error("expected nothing from a file that is not there")
	}
}
