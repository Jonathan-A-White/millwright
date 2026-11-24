package claude_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/claude"
)

// dumpEnv is the line of a grind stand-in that writes the environment it was
// started in to the file env beside it.
const dumpEnv = "env > \"$(dirname \"$0\")/env\"\n"

// envOf is the environment a stand-in running dumpEnv saw, by name.
func envOf(t *testing.T, seen string) map[string]string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(seen, "env"))
	if err != nil {
		t.Fatal(err)
	}
	env := map[string]string{}
	for _, line := range strings.Split(string(raw), "\n") {
		if name, value, ok := strings.Cut(line, "="); ok {
			env[name] = value
		}
	}
	return env
}

// grindEnvOf is the environment the claude of one grind on grinder's options,
// cold or warm, was started in.
func grindEnvOf(t *testing.T, warm bool, call func(*application.GrindCall), opts ...claude.GrinderOption) map[string]string {
	t.Helper()
	if !warm {
		program, seen := grindStandIn(t, dumpEnv+"cat <<'EOF'\n"+liveResult+"\nEOF\n")
		cold := aGrindCall(t, t.TempDir())
		call(&cold)
		if _, err := claude.NewGrinder(append(opts, claude.WithGrindProgram(program))...).Grind(context.Background(), cold); err != nil {
			t.Fatal(err)
		}
		return envOf(t, seen)
	}
	program, seen := warmStandIn(t, dumpEnv+"cat <<'EOF'\n"+streamResult(t)+"\nEOF\n")
	waiting := aWarmCall(t, t.TempDir())
	call(&waiting)
	session, err := claude.NewGrinder(append(opts, claude.WithGrindProgram(program))...).Warm(waiting)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.Answer(context.Background(), "Grind this grist.\n", 5*time.Second); err != nil {
		t.Fatal(err)
	}
	return envOf(t, seen)
}

// The variables of the process that runs the grinder which tune Claude Code or
// the Anthropic API never reach a grind, cold or warm: only the key the grinder
// sets and the rest of the environment, PATH, HOME and the locale among it, do
// (mw-gq6.1710).
func TestGrindEnvDropsModelTuningVariables(t *testing.T) {
	dropped := []string{
		"MAX_THINKING_TOKENS", "CLAUDE_CODE_MAX_OUTPUT_TOKENS", "ANTHROPIC_MODEL", "CLAUDE_CODE_FOO",
		"CLAUDE_CODE_EFFORT_LEVEL", "CLAUDE_EFFORT", "CLAUDECODE", claude.CacheTTLEnv,
		"ANTHROPIC_BASE_URL", "ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_SMALL_FAST_MODEL",
		"MAX_STRUCTURED_OUTPUT_RETRIES", "DISABLE_PROMPT_CACHING", "DISABLE_PROMPT_CACHING_SONNET",
		"ENABLE_PROMPT_CACHING_1H", "DISABLE_INTERLEAVED_THINKING", "DISABLE_COMPACT",
		"DISABLE_AUTO_COMPACT", "FALLBACK_FOR_ALL_PRIMARY_MODELS", "USE_API_CONTEXT_MANAGEMENT",
		"API_TIMEOUT_MS",
	}
	for _, name := range dropped {
		t.Setenv(name, "from-the-mill")
	}
	kept := map[string]string{
		"PATH": os.Getenv("PATH"), "HOME": t.TempDir(), "LANG": "C.UTF-8", "LC_ALL": "C.UTF-8",
		"CLAUDE_CONFIG_DIR": t.TempDir(), "HTTPS_PROXY": "http://proxy.invalid:3128",
	}
	for name, value := range kept {
		t.Setenv(name, value)
	}

	for _, warm := range []bool{false, true} {
		path := map[bool]string{false: "cold", true: "warm"}[warm]
		check := func(env map[string]string, want map[string]string) {
			t.Helper()
			for _, name := range dropped {
				if value, ok := env[name]; ok && want[name] != value {
					t.Errorf("%s: expected no %s in the grind's environment, got %q", path, name, value)
				}
			}
			for name, value := range want {
				if env[name] != value {
					t.Errorf("%s: expected %s=%q in the grind's environment, got %q", path, name, value, env[name])
				}
			}
		}
		with := func(more map[string]string) map[string]string {
			all := map[string]string{}
			for name, value := range kept {
				all[name] = value
			}
			for name, value := range more {
				all[name] = value
			}
			return all
		}

		// A grind billed to the key.
		t.Setenv(claude.APIKeyEnv, "sk-ant-from-the-mill")
		t.Setenv(claude.OAuthTokenEnv, "token-from-the-mill")
		env := grindEnvOf(t, warm, func(call *application.GrindCall) { call.APIKey = "sk-ant-the-grinders" })
		check(env, with(map[string]string{claude.APIKeyEnv: "sk-ant-the-grinders"}))
		if token, ok := env[claude.OAuthTokenEnv]; ok {
			t.Errorf("%s: expected no token beside the key, got %q", path, token)
		}

		// A grind on the subscription keeps the mill's own token, never its key.
		env = grindEnvOf(t, warm, func(*application.GrindCall) {})
		check(env, with(map[string]string{claude.OAuthTokenEnv: "token-from-the-mill"}))
		if key, ok := env[claude.APIKeyEnv]; ok {
			t.Errorf("%s: expected no key on the subscription, got %q", path, key)
		}

		// What claude.env assigns is set on purpose, tuning variables too.
		file := filepath.Join(t.TempDir(), "claude.env")
		text := claude.OAuthTokenEnv + "=token-from-the-file\nMAX_THINKING_TOKENS=2048\nCLAUDE_CODE_FOO=from-the-file\n"
		if err := os.WriteFile(file, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
		env = grindEnvOf(t, warm, func(*application.GrindCall) {}, claude.WithGrindClaudeEnvFile(file))
		check(env, with(map[string]string{
			claude.OAuthTokenEnv: "token-from-the-file", "MAX_THINKING_TOKENS": "2048", "CLAUDE_CODE_FOO": "from-the-file",
		}))
	}

	// A talk still runs with the one-hour cache the grinder sets.
	env := grindEnvOf(t, false, func(call *application.GrindCall) { call.SessionID = "0b3f6c1e-1111-4222-8333-444455556666" })
	if env[claude.CacheTTLEnv] != application.GristTalkCacheTTL {
		t.Errorf("expected a talk's cache of %s, got %q", application.GristTalkCacheTTL, env[claude.CacheTTLEnv])
	}
}
