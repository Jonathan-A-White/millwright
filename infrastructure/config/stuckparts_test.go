package config_test

import (
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/infrastructure/config"
)

// mw-gq6.1645: [stuck] thawable = false turns off the mail dispatch sends the
// Mayor when a host has had room for 15 minutes with nothing to start; it is on
// when the table says nothing.
func TestStuckThawableIsOnUnlessTheTableTurnsItOff(t *testing.T) {
	writeConfig(t, "host = \"laptop\"\n")
	if got, err := config.StuckThawable(); err != nil || !got {
		t.Fatalf("expected the idle-host mail on by default, got %v, %v", got, err)
	}
	for said, want := range map[string]bool{"false": false, "true": true, "\"false\"": false} {
		writeConfig(t, "[stuck]\nthawable = "+said+"\n")
		if got, err := config.StuckThawable(); err != nil || got != want {
			t.Errorf("thawable = %s: got %v, %v, want %v", said, got, err, want)
		}
	}
	writeConfig(t, "[dispatch]\nthawable = false\n")
	if got, err := config.StuckThawable(); err != nil || !got {
		t.Errorf("a thawable key outside [stuck] must not turn it off: got %v, %v", got, err)
	}
	for _, bad := range []string{"thawable = maybe\n", "thawable = 0.5\n"} {
		writeConfig(t, "[stuck]\n"+bad)
		if _, err := config.StuckThawable(); err == nil || !strings.Contains(err.Error(), "thawable") {
			t.Errorf("expected %q refused naming the key, got %v", bad, err)
		}
	}
}
