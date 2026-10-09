package sops_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/sops"
)

// standIn writes a stand-in sops into dir that runs body, and gives its path.
func standIn(t *testing.T, dir, body string) string {
	t.Helper()
	path := filepath.Join(dir, "sops")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// vault is a temp vault holding a .sops.yaml, and the path of a key file in it
// (written when withKey is true; a placeholder, never a key).
func vault(t *testing.T, withKey bool) (string, string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, sops.ConfigFile), []byte("creation_rules: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	key := filepath.Join(dir, "age.key")
	if withKey {
		if err := os.WriteFile(key, []byte("# placeholder\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir, key
}

func TestAFailingSopsNeverPutsTheValueInTheError(t *testing.T) {
	dir, key := vault(t, true)
	store := sops.New(dir, key)
	// A sops that says back everything it was given, then fails.
	store.Program = standIn(t, t.TempDir(), "cat >&2\necho \"$@\" >&2\nexit 1\n")
	const value = "tok-never-in-an-error \"quoted\"\nsecond line"

	err := store.Put(context.Background(), "github_token", []byte(value))
	if err == nil {
		t.Fatal("expected the failing sops to fail the put")
	}
	for _, part := range []string{"tok-never-in-an-error", "quoted", "second line"} {
		if strings.Contains(err.Error(), part) {
			t.Fatalf("the error holds the value, or part of it: %v", err)
		}
	}
	if !strings.Contains(err.Error(), "<value>") {
		t.Fatalf("expected what sops said, with the value cut out, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, sops.SecretsFile)); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("a failed first put left a file behind: %v", statErr)
	}
}

func TestSopsIsShownOnlyTheKeyFileAndNoSopsSettingOfTheCaller(t *testing.T) {
	dir, key := vault(t, true)
	if err := os.WriteFile(filepath.Join(dir, sops.SecretsFile), []byte("x: y\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SOPS_AGE_KEY", "a-key-from-the-environment")
	store := sops.New(dir, key)
	// A sops that answers a decrypt with the environment it was given.
	store.Program = standIn(t, t.TempDir(), `printf '{"env":"%s"}' "$(env | grep '^SOPS_' | tr '\n' ' ')"`+"\n")

	values, err := store.Get(context.Background(), "env")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got := strings.TrimSpace(string(values)); got != "SOPS_AGE_KEY_FILE="+key {
		t.Fatalf("expected sops to be shown SOPS_AGE_KEY_FILE alone, got %q", got)
	}
}

func TestGetAndListWithNoKeyHereSayTheKeyIsOnTheHomeOnly(t *testing.T) {
	dir, key := vault(t, false)
	if err := os.WriteFile(filepath.Join(dir, sops.SecretsFile), []byte("x: y\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	store := sops.New(dir, key)
	store.Program = standIn(t, t.TempDir(), "echo should not run >&2\nexit 9\n")

	if _, err := store.Get(context.Background(), "x"); err == nil || !strings.Contains(err.Error(), "home only") {
		t.Fatalf("expected get to name the missing key, got %v", err)
	}
	if _, err := store.Names(context.Background()); err == nil || !strings.Contains(err.Error(), "home only") {
		t.Fatalf("expected list to name the missing key, got %v", err)
	}
}

func TestNoSecretsFileIsNoNamesAndNoSecret(t *testing.T) {
	dir, key := vault(t, false)
	store := sops.New(dir, key)

	names, err := store.Names(context.Background())
	if err != nil || len(names) != 0 {
		t.Fatalf("expected no names and no error, got %v, %v", names, err)
	}
	if _, err := store.Get(context.Background(), "x"); !errors.Is(err, application.ErrNoSecret) {
		t.Fatalf("expected ErrNoSecret, got %v", err)
	}
}

func TestPutWithNoSopsConfigSaysSo(t *testing.T) {
	dir := t.TempDir()
	store := sops.New(dir, filepath.Join(dir, "age.key"))
	err := store.Put(context.Background(), "x", []byte("v"))
	if err == nil || !strings.Contains(err.Error(), sops.ConfigFile) {
		t.Fatalf("expected the put to name %s, got %v", sops.ConfigFile, err)
	}
}
