package cloud_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/infrastructure/cloud"
)

func TestSetCapRunsTheWrapperForTheBox(t *testing.T) {
	command, calls := standIn(t, "echo cap = 3")
	if err := (cloud.Vultr{Command: command}).SetCap(context.Background(), "cloud2", 3); err != nil {
		t.Fatalf("set cap: %v", err)
	}
	if got, want := read(t, calls), "set-cap --name cloud2 --cap 3\n"; got != want {
		t.Errorf("expected the wrapper run as %q, got %q", want, got)
	}
}

func TestSetCapNamesWhyTheBoxCouldNotBeReached(t *testing.T) {
	command, _ := standIn(t, "echo 'vultr-boost: could not set the cap on cloud2 (mw@10.88.0.5)' >&2; exit 1")
	err := cloud.Vultr{Command: command}.SetCap(context.Background(), "cloud2", 3)
	if err == nil || !strings.Contains(err.Error(), "could not set the cap on cloud2") {
		t.Errorf("expected the wrapper's last line, got %v", err)
	}
}

// committer is a vault that holds the files in a directory and notes its commits.
type committer struct {
	dir     string
	commits [][]string
	err     error
}

func (c *committer) Dir() string { return c.dir }
func (c *committer) Commit(_ context.Context, msg string, paths []string) ([]string, error) {
	c.commits = append(c.commits, append([]string{msg}, paths...))
	return paths, c.err
}

func capFiles(t *testing.T, config, table, tfvars string) (cloud.CapFiles, *committer, string) {
	t.Helper()
	dir := t.TempDir()
	vault := filepath.Join(dir, "vault")
	if err := os.MkdirAll(filepath.Join(vault, "hosts", "vultr"), 0o755); err != nil {
		t.Fatal(err)
	}
	for path, text := range map[string]string{
		filepath.Join(dir, "config.toml"):           config,
		filepath.Join(vault, cloud.CloudTablePath):  table,
		filepath.Join(vault, cloud.BoostTfvarsPath): tfvars,
	} {
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	v := &committer{dir: vault}
	return cloud.CapFiles{Config: filepath.Join(dir, "config.toml"), Vault: v}, v, dir
}

func TestSetBoxCapChangesThreePlacesAndKeepsTheRest(t *testing.T) {
	files, vault, dir := capFiles(t,
		"vault = \"/v\"\ncap = 4\n\n[cloud]\nmax_boxes = 4 # why\nbox_cap = 2   # sessions\nsnapshot = \"s\"\n\n[cloud.caps]\nbox_cap = 9\n",
		"# way back\n[cloud]\nbox_cap         = 2\n[cloud.caps]\nlaptop = 5\n",
		"region = \"ewr\"\ncap         = \"2\"\ngit_name = \"x\"\n")
	if got, err := files.BoxCap(context.Background()); err != nil || got != 2 {
		t.Fatalf("expected box_cap 2, got %d, %v", got, err)
	}
	if err := files.SetBoxCap(context.Background(), 3); err != nil {
		t.Fatal(err)
	}
	if got, want := read(t, filepath.Join(dir, "config.toml")), "vault = \"/v\"\ncap = 4\n\n[cloud]\nmax_boxes = 4 # why\nbox_cap = 3   # sessions\nsnapshot = \"s\"\n\n[cloud.caps]\nbox_cap = 9\n"; got != want {
		t.Errorf("config: got %q, want %q", got, want)
	}
	if got, want := read(t, filepath.Join(dir, "vault", cloud.CloudTablePath)), "# way back\n[cloud]\nbox_cap         = 3\n[cloud.caps]\nlaptop = 5\n"; got != want {
		t.Errorf("table copy: got %q, want %q", got, want)
	}
	if got, want := read(t, filepath.Join(dir, "vault", cloud.BoostTfvarsPath)), "region = \"ewr\"\ncap         = \"3\"\ngit_name = \"x\"\n"; got != want {
		t.Errorf("tfvars: got %q, want %q", got, want)
	}
	if len(vault.commits) != 1 || len(vault.commits[0]) != 3 {
		t.Errorf("expected one commit of the two vault files, got %v", vault.commits)
	}
}

func TestSetBoxCapAddsTheLinesATableLacks(t *testing.T) {
	files, _, dir := capFiles(t, "vault = \"/v\"\n[cloud]\nmax_boxes = 4\n", "[cloud]\nmax_boxes = 4\n", "region = \"ewr\"\n")
	if got, err := files.BoxCap(context.Background()); err != nil || got != 0 {
		t.Fatalf("expected no box_cap, got %d, %v", got, err)
	}
	if err := files.SetBoxCap(context.Background(), 5); err != nil {
		t.Fatal(err)
	}
	if got, want := read(t, filepath.Join(dir, "config.toml")), "vault = \"/v\"\n[cloud]\nbox_cap = 5\nmax_boxes = 4\n"; got != want {
		t.Errorf("config: got %q, want %q", got, want)
	}
	if got, want := read(t, filepath.Join(dir, "vault", cloud.BoostTfvarsPath)), "cap = \"5\"\nregion = \"ewr\"\n"; got != want {
		t.Errorf("tfvars: got %q, want %q", got, want)
	}
}

func TestAFailedVaultCommitLeavesTheHomesConfigAsItWas(t *testing.T) {
	files, vault, dir := capFiles(t, "[cloud]\nbox_cap = 2\n", "[cloud]\nbox_cap = 2\n", "cap = \"2\"\n")
	vault.err = errors.New("git is stuck")
	err := files.SetBoxCap(context.Background(), 3)
	if err == nil || !strings.Contains(err.Error(), "git is stuck") {
		t.Fatalf("expected the commit's failure, got %v", err)
	}
	if got := read(t, filepath.Join(dir, "config.toml")); got != "[cloud]\nbox_cap = 2\n" {
		t.Errorf("expected the home's config untouched, got %q", got)
	}
}
