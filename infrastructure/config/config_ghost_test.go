package config_test

import (
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/infrastructure/config"
)

func TestGhostURLDefaultsToTheBlogAndIsReadFromTheFileAndTheEnvironment(t *testing.T) {
	t.Setenv(config.GhostURLEnv, "")
	writeConfig(t, "")
	if got, err := config.GhostURL(); err != nil || got != "https://allmymind.org" {
		t.Fatalf("got %q %v, want the blog", got, err)
	}
	writeConfig(t, "ghost_url = \"https://blog.example.org/\"\n")
	if got, _ := config.GhostURL(); got != "https://blog.example.org" {
		t.Fatalf("got %q, want the file's, without its trailing slash", got)
	}
	t.Setenv(config.GhostURLEnv, "http://127.0.0.1:2368")
	if got, err := config.GhostURL(); err != nil || got != "http://127.0.0.1:2368" {
		t.Fatalf("got %q %v, want the environment's, plain http allowed to this machine", got, err)
	}
}

func TestGhostURLRefusesPlainHttpAwayFromThisMachineAndAPath(t *testing.T) {
	writeConfig(t, "")
	for _, bad := range []string{"http://allmymind.org", "https://allmymind.org/ghost", "allmymind.org", "ftp://x.org"} {
		t.Setenv(config.GhostURLEnv, bad)
		if _, err := config.GhostURL(); err == nil || !strings.Contains(err.Error(), "ghost_url") {
			t.Errorf("%q: got %v, want a refusal naming ghost_url", bad, err)
		}
	}
}
