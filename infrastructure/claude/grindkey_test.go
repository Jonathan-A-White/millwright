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

// envStandIn is a grind stand-in that writes the one variable it was given,
// "unset" when it had none, to the file key beside it, and the whole
// environment's names and its argv to env and args.
func envStandIn(t *testing.T) (program, seen string) {
	t.Helper()
	return grindStandIn(t, "if [ -n \"${ANTHROPIC_API_KEY+x}\" ]; then printf '%s' \"$ANTHROPIC_API_KEY\" > "+"\"$(dirname \"$0\")/key\""+"; else printf unset > \"$(dirname \"$0\")/key\"; fi\n"+
		"cat <<'EOF'\n"+liveResult+"\nEOF\n")
}

func keyOf(t *testing.T, seen string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(seen, "key"))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// A grind billed to the key is given it in its environment, and nowhere
// else: not in its argument list.
func TestAGrindBilledToTheKeyGetsItInItsEnvironmentOnly(t *testing.T) {
	program, seen := envStandIn(t)
	call := aGrindCall(t, t.TempDir())
	call.APIKey = "sk-ant-test-key"
	if _, err := claude.NewGrinder(claude.WithGrindProgram(program)).Grind(context.Background(), call); err != nil {
		t.Fatal(err)
	}
	if got := keyOf(t, seen); got != "sk-ant-test-key" {
		t.Fatalf("expected the key in the grind's environment, got %q", got)
	}
	args, _ := os.ReadFile(filepath.Join(seen, "args"))
	if strings.Contains(string(args), "sk-ant-test-key") {
		t.Fatalf("expected the key out of the argument list, got %q", args)
	}
}

// A grind billed to the subscription has no ANTHROPIC_API_KEY, even when the
// mill's own environment holds one.
func TestAGrindOnTheSubscriptionHasNoKeyEvenIfTheMillsEnvironmentDoes(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-from-the-shell")
	program, seen := envStandIn(t)
	if _, err := claude.NewGrinder(claude.WithGrindProgram(program)).Grind(context.Background(), aGrindCall(t, t.TempDir())); err != nil {
		t.Fatal(err)
	}
	if got := keyOf(t, seen); got != "unset" {
		t.Fatalf("expected no ANTHROPIC_API_KEY in the grind's environment, got %q", got)
	}
}

// A warm session is given the same: the key when its call carries one, none
// otherwise.
func TestAWarmSessionGetsTheKeyOnlyWhenItsCallCarriesOne(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-from-the-shell")
	for _, key := range []string{"sk-ant-warm-key", ""} {
		program, seen := warmStandIn(t, "if [ -n \"${ANTHROPIC_API_KEY+x}\" ]; then printf '%s' \"$ANTHROPIC_API_KEY\" > \"$(dirname \"$0\")/key\"; else printf unset > \"$(dirname \"$0\")/key\"; fi\n"+
			"cat <<'EOF'\n"+streamResult(t)+"\nEOF\n")
		call := aWarmCall(t, t.TempDir())
		call.APIKey = key
		session, err := claude.NewGrinder(claude.WithGrindProgram(program)).Warm(call)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := session.Answer(context.Background(), "Grind this grist.\n", 5*time.Second); err != nil {
			t.Fatal(err)
		}
		want := key
		if key == "" {
			want = "unset"
		}
		if got := keyOf(t, seen); got != want {
			t.Fatalf("expected %q in the warm session's environment, got %q", want, got)
		}
		args, _ := os.ReadFile(filepath.Join(seen, "args"))
		if key != "" && strings.Contains(string(args), key) {
			t.Fatalf("expected the key out of the argument list, got %q", args)
		}
	}
}
