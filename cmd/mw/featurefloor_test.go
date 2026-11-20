package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFeatureFloorsAreReadFromTheRigFilesThatKeepOne(t *testing.T) {
	dir := t.TempDir()
	rigs := filepath.Join(dir, "rigs")
	if err := os.MkdirAll(rigs, 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"lampas.toml":   "feature_share = 0.6\nfeature_floor = 5\n",
		"postern.toml":  "feature_floor = 2\n",
		"cairn.toml":    "feature_share = 0.5\n",
		"private-terms": "not a rig file\n",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(rigs, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got := featureFloors(dir)
	if len(got) != 2 || got["lampas"] != 5 || got["postern"] != 2 {
		t.Errorf("want lampas 5 and postern 2 only, got %v", got)
	}
	if got := featureFloors(t.TempDir()); got != nil {
		t.Errorf("a vault with no rigs directory has no floor, got %v", got)
	}
}
