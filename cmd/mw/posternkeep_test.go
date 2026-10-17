package main

import (
	"strings"
	"testing"
)

// The help of mw postern inbox names the file a message is kept in and --show.
func TestPosternInboxHelpNamesTheKeptMessageAndShow(t *testing.T) {
	cmd := newPosternInboxCmd()
	for _, want := range []string{"<txid>.message.json", "<txid>.transcript.txt", "--show <txid>"} {
		if !strings.Contains(cmd.Long, want) {
			t.Errorf("expected the Long help to name %q", want)
		}
	}
	if cmd.Flags().Lookup("show") == nil {
		t.Error("expected a --show flag")
	}
}
