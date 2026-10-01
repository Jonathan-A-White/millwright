package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// mwHome runs the real command in a temp vault whose home file holds text
// (none when text is empty), as the host asked.
func mwHome(t *testing.T, text, host string, args ...string) (out, errs string, status int) {
	t.Helper()
	mwConfig(t, "")
	vault := t.TempDir()
	if text != "" {
		if err := os.WriteFile(filepath.Join(vault, "home"), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("MW_VAULT", vault)
	t.Setenv("MW_HOST", host)

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	root := newRootCmd()
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.SetArgs(append([]string{"home"}, args...))
	err := root.Execute()
	return stdout.String(), stderr.String(), exitCode(err)
}

const laptopHome = "laptop 2026-09-29T00:10:00Z mw@laptop\n"

func TestHomeCheckLeavesWithZeroOnTheHomeHost(t *testing.T) {
	out, errs, status := mwHome(t, laptopHome, "laptop", "--check")
	if status != 0 || out != "" {
		t.Errorf("expected status 0 and no stdout, got %d, %q (%q)", status, out, errs)
	}
}

func TestHomeCheckLeavesWithOneOnTheOtherHost(t *testing.T) {
	out, errs, status := mwHome(t, laptopHome, "desktop", "--check")
	if status != 1 || !strings.Contains(errs, "not home") {
		t.Errorf("expected status 1 saying so, got %d (%q)", status, errs)
	}
	if out != "laptop\n" {
		t.Errorf("expected the home's name alone on stdout, got %q", out)
	}
}

func TestHomeCheckLeavesWithTwoAndSaysSoWhereThereIsNoFile(t *testing.T) {
	out, errs, status := mwHome(t, "", "laptop", "--check")
	if status != 2 || !strings.Contains(errs, "cannot tell which host is home") {
		t.Errorf("expected status 2 saying so, got %d (%q)", status, errs)
	}
	if out != "" {
		t.Errorf("expected nothing on stdout, got %q", out)
	}
}

func TestHomePrintsHomeThisHostAndYesOrNo(t *testing.T) {
	out, _, status := mwHome(t, laptopHome, "laptop")
	want := "home: laptop (changed 2026-09-29T00:10:00Z by mw@laptop)\nthis host: laptop\nthis host is home: yes\n"
	if status != 0 || out != want {
		t.Errorf("got status %d and %q, wanted %q", status, out, want)
	}
	out, _, _ = mwHome(t, laptopHome, "desktop")
	if !strings.HasSuffix(out, "this host: desktop\nthis host is home: no\n") {
		t.Errorf("got %q", out)
	}
}
