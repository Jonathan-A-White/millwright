package main

import (
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestParseBoxPanesKeepsTheRecentPanesWithTheirText(t *testing.T) {
	now := time.Date(2026, 11, 12, 20, 0, 0, 0, time.UTC)
	out := "@@PANE\tmw-a\t" + strconv.FormatInt(now.Add(-time.Hour).Unix(), 10) + "\nBun v1.4.4\nSegmentation fault\n" +
		"@@PANE\tmw-old\t" + strconv.FormatInt(now.Add(-48*time.Hour).Unix(), 10) + "\nold\n" +
		"@@PANE\tmw-b\t" + strconv.FormatInt(now.Add(-time.Minute).Unix(), 10) + "\nfine\n"
	panes := parseBoxPanes(out, now.Add(-24*time.Hour))
	if len(panes) != 2 || panes[0].Window != "mw-a" || panes[1].Window != "mw-b" {
		t.Fatalf("expected the two recent panes, got %+v", panes)
	}
	if !strings.Contains(panes[0].Text, "Segmentation fault") || strings.Contains(panes[0].Text, "old") {
		t.Errorf("expected each pane to hold its own text, got %q", panes[0].Text)
	}
}
