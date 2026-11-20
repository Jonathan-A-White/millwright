package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/Jonathan-A-White/millwright/infrastructure/vault"
)

// featureFloors is each rig's feature_floor, read from the rig files in the
// vault at dir (rigs/<rig>.toml): the rigs that keep a floor, by name. A rig
// file that cannot be read, or holds no floor, is left out, and the vault with
// no rigs directory has none: the floor alarm is a gauge, and one rig's file
// never silences the others.
func featureFloors(dir string) map[string]int {
	entries, err := os.ReadDir(filepath.Join(dir, "rigs"))
	if err != nil {
		return nil
	}
	files := vault.New(dir)
	floors := map[string]int{}
	for _, e := range entries {
		rig, ok := strings.CutSuffix(e.Name(), ".toml")
		if !ok || e.IsDir() {
			continue
		}
		// The lenient reader: a key this mw does not know is no reason to lose the floor.
		required, _, err := files.LandingRequirements(context.Background(), rig)
		if err == nil && required.FeatureFloor > 0 {
			floors[rig] = required.FeatureFloor
		}
	}
	return floors
}
