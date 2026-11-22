package main

import (
	"os"
	"strings"
	"testing"
)

// mw-gq6.1645: the timer's dispatch reads [stuck] thawable into the Dispatch, so
// that thawable = false turns off the mail to the Mayor of an idle host. The only
// dispatch cmd/mw builds is the one in dispatch.go; a Dispatch with no word of it
// sends nothing.
func TestTheTimersDispatchReadsTheStuckThawableKey(t *testing.T) {
	raw, err := os.ReadFile("dispatch.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "dispatch.IdleHostMail, err = config.StuckThawable()") {
		t.Error("cmd/mw/dispatch.go does not set the dispatch's IdleHostMail from config.StuckThawable(): the [stuck] thawable key would turn nothing off, and the mail would never be sent")
	}
}
