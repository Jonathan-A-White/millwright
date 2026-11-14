package cloud_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/Jonathan-A-White/millwright/infrastructure/cloud"
)

// mw-gq6.1296: drain writes max_boxes 0 and the old value in the home's config
// and in the vault's copy together, commits the vault's by path, and undrain
// puts both back.

const drainConfig = "vault = \"/v\"\n\n[cloud]\nmax_boxes = 4 # the most boxes\nbox_cap = 2\n\n[cloud.caps]\nlaptop = 5\n"
const drainTable = "# way back\n[cloud]\nmax_boxes = 4\nbox_cap = 2\n[cloud.caps]\nlaptop = 5\n"

func TestSetDrainedWritesMaxBoxesZeroAndTheOldValueInBothFilesAndCommitsTheVaultsByPath(t *testing.T) {
	files, vault, dir := capFiles(t, drainConfig, drainTable, "region = \"ewr\"\n")
	n, kept, drained, err := files.MaxBoxesKept(context.Background())
	if err != nil || n != 4 || drained || kept != 0 {
		t.Fatalf("expected max_boxes 4 and nothing kept, got %d, %d, %v, %v", n, kept, drained, err)
	}
	if err := files.SetDrained(context.Background(), 4); err != nil {
		t.Fatal(err)
	}
	if got, want := read(t, filepath.Join(dir, "config.toml")), "vault = \"/v\"\n\n[cloud]\ndrained_max_boxes = 4\nmax_boxes = 0 # the most boxes\nbox_cap = 2\n\n[cloud.caps]\nlaptop = 5\n"; got != want {
		t.Errorf("config: got %q, want %q", got, want)
	}
	if got, want := read(t, filepath.Join(dir, "vault", cloud.CloudTablePath)), "# way back\n[cloud]\ndrained_max_boxes = 4\nmax_boxes = 0\nbox_cap = 2\n[cloud.caps]\nlaptop = 5\n"; got != want {
		t.Errorf("table copy: got %q, want %q", got, want)
	}
	if len(vault.commits) != 1 || len(vault.commits[0]) != 2 || vault.commits[0][1] != cloud.CloudTablePath {
		t.Errorf("expected one commit of the vault's table by path, got %v", vault.commits)
	}
	n, kept, drained, err = files.MaxBoxesKept(context.Background())
	if err != nil || n != 0 || kept != 4 || !drained {
		t.Errorf("expected max_boxes 0 with 4 kept, got %d, %d, %v, %v", n, kept, drained, err)
	}
}

func TestSetUndrainedPutsMaxBoxesBackAndDropsTheKeptValueInBothFiles(t *testing.T) {
	files, vault, dir := capFiles(t, drainConfig, drainTable, "region = \"ewr\"\n")
	if err := files.SetDrained(context.Background(), 4); err != nil {
		t.Fatal(err)
	}
	if err := files.SetUndrained(context.Background(), 4); err != nil {
		t.Fatal(err)
	}
	if got := read(t, filepath.Join(dir, "config.toml")); got != drainConfig {
		t.Errorf("config: got %q, want %q", got, drainConfig)
	}
	if got := read(t, filepath.Join(dir, "vault", cloud.CloudTablePath)); got != drainTable {
		t.Errorf("table copy: got %q, want %q", got, drainTable)
	}
	if len(vault.commits) != 2 {
		t.Errorf("expected a commit for each, got %v", vault.commits)
	}
}

func TestAMaxBoxesTheConfigLacksIsTheDefaultAndIsAddedToTheTable(t *testing.T) {
	files, _, dir := capFiles(t, "[cloud]\nbox_cap = 2\n", "[cloud]\nbox_cap = 2\n", "")
	files.DefaultMaxBoxes = 2
	if n, _, _, err := files.MaxBoxesKept(context.Background()); err != nil || n != 2 {
		t.Fatalf("expected the default 2, got %d, %v", n, err)
	}
	if err := files.SetDrained(context.Background(), 2); err != nil {
		t.Fatal(err)
	}
	if got, want := read(t, filepath.Join(dir, "config.toml")), "[cloud]\ndrained_max_boxes = 2\nmax_boxes = 0\nbox_cap = 2\n"; got != want {
		t.Errorf("config: got %q, want %q", got, want)
	}
}
