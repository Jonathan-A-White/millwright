package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

func TestTalkWaitHelpRunsAndListsItsFlags(t *testing.T) {
	buf := &bytes.Buffer{}
	root := newRootCmd()
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs([]string{"talk", "wait", "--help"})
	if err := root.Execute(); err != nil {
		t.Fatalf("mw talk wait --help: %v", err)
	}
	for _, want := range []string{"--limit", "--min-backoff", "--max-backoff", TalkWaitLimitEnv} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("expected the help to name %s, got:\n%s", want, buf.String())
		}
	}
}

func TestTalkWaitLimitIsSecondsFromTheEnvironmentElseTheDefault(t *testing.T) {
	for _, tc := range []struct {
		env  string
		want time.Duration
	}{
		{"", application.DefaultTalkWaitLimit},
		{"90", 90 * time.Second},
		{"soon", application.DefaultTalkWaitLimit},
		{"0", application.DefaultTalkWaitLimit},
		{"-5", application.DefaultTalkWaitLimit},
	} {
		t.Setenv(TalkWaitLimitEnv, tc.env)
		if got := talkWaitLimit(); got != tc.want {
			t.Errorf("%s=%q: expected a limit of %s, got %s", TalkWaitLimitEnv, tc.env, tc.want, got)
		}
	}
}
