package doctor_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/infrastructure/doctor"
)

// A dry run changes nothing, a check's own kept state included: what a real run
// reads next must be what it read before the dry run. Each case builds a check
// over a real Store, seeds the state a real run could have left (or none), runs
// Doctor.Run(name, dryRun) twice and finds the Store's files as they were.
// The cases are the checks whose Probe saved or forgot its own state with no
// DoctorDryRun guard (mw-6ww.185.27).

// storeFiles is every file in a Store's directory, name to contents.
func storeFiles(t *testing.T, dir string) map[string]string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{}
	for _, entry := range entries {
		body, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		files[entry.Name()] = string(body)
	}
	return files
}

func dryRunTwiceLeavesTheStateAlone(t *testing.T, name string, check application.DoctorCheck, dir string, now time.Time) {
	t.Helper()
	store := doctor.New(dir)
	before := storeFiles(t, dir)
	doc := application.Doctor{Checks: application.DoctorChecks{check}, State: store, Log: store,
		Host: "laptop", Now: func() time.Time { return now }}
	for i := 1; i <= 2; i++ {
		if _, err := doc.Run(context.Background(), name, true); err != nil {
			if _, fault := application.DoctorFaults(err); !fault {
				t.Fatalf("dry run %d: %v", i, err)
			}
		}
		if after := storeFiles(t, dir); !reflect.DeepEqual(before, after) {
			t.Fatalf("dry run %d changed the check's state: before %v, after %v", i, before, after)
		}
	}
}

func seedEpisode(t *testing.T, store *doctor.Store, state string, episode application.DoctorEpisode) {
	t.Helper()
	if err := store.Save(context.Background(), state, episode); err != nil {
		t.Fatal(err)
	}
}

var dryRunNow = time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)

func TestWifiADryRunSavesAndForgetsNothing(t *testing.T) {
	t.Run("first sight of the internet down", func(t *testing.T) {
		dir := t.TempDir()
		check := &doctor.Wifi{Reach: []string{unreachableHost(t)}, Powershell: existingFile(t), State: doctor.New(dir),
			Now: func() time.Time { return dryRunNow }}
		dryRunTwiceLeavesTheStateAlone(t, doctor.WifiName, check, dir, dryRunNow)
	})
	t.Run("the internet back while an outage is kept", func(t *testing.T) {
		dir := t.TempDir()
		store := doctor.New(dir)
		seedEpisode(t, store, "wifi-reach", application.DoctorEpisode{FirstFaulty: dryRunNow.Add(-time.Hour)})
		check := &doctor.Wifi{Reach: []string{localListener(t)}, Powershell: existingFile(t), State: store,
			Now: func() time.Time { return dryRunNow }}
		dryRunTwiceLeavesTheStateAlone(t, doctor.WifiName, check, dir, dryRunNow)
	})
}

func TestWgADryRunSavesAndForgetsNothing(t *testing.T) {
	t.Run("first sight of the hub down", func(t *testing.T) {
		ip, _ := fakeIP(t, wg0AddrFixture, 0)
		dir := t.TempDir()
		check := &doctor.Wg{Reach: []string{localListener(t)}, Hub: "10.88.0.1:22", IP: ip,
			State: doctor.New(dir), Dial: neverDial(), Now: func() time.Time { return dryRunNow }}
		dryRunTwiceLeavesTheStateAlone(t, doctor.WgName, check, dir, dryRunNow)
	})
	t.Run("the hub back while an outage is kept", func(t *testing.T) {
		ip, _ := fakeIP(t, wg0AddrFixture, 0)
		dir := t.TempDir()
		store := doctor.New(dir)
		seedEpisode(t, store, "wg-hub", application.DoctorEpisode{FirstFaulty: dryRunNow.Add(-time.Hour)})
		check := &doctor.Wg{Reach: []string{localListener(t)}, Hub: "10.88.0.1:22", IP: ip,
			State: store, Dial: alwaysDial(), Now: func() time.Time { return dryRunNow }}
		dryRunTwiceLeavesTheStateAlone(t, doctor.WgName, check, dir, dryRunNow)
	})
}

func TestBeadsChurnADryRunKeepsNoSample(t *testing.T) {
	dir := t.TempDir()
	check := doctor.NewBeadsChurn(func(context.Context) (int64, error) { return 1_000_000, nil }, doctor.New(dir))
	check.Now = func() time.Time { return dryRunNow }
	dryRunTwiceLeavesTheStateAlone(t, doctor.BeadsChurnName, check, dir, dryRunNow)
}

func TestFormulasADryRunSavesAndForgetsNothing(t *testing.T) {
	t.Run("first sight of the formulas out of step", func(t *testing.T) {
		w := newFormulasWorld(t, "laptop")
		dir := t.TempDir()
		w.check.State = doctor.New(dir)
		w.write(t, w.factory, "formulas/tdd-feature.formula.json", "v2")
		w.commit(t, "two")
		dryRunTwiceLeavesTheStateAlone(t, doctor.FormulasName, w.check, dir, dryRunNow)
	})
	t.Run("the formulas back in step while a sighting is kept", func(t *testing.T) {
		w := newFormulasWorld(t, "laptop")
		dir := t.TempDir()
		store := doctor.New(dir)
		w.check.State = store
		seedEpisode(t, store, "formulas-seen", application.DoctorEpisode{SeenPaths: []string{"abc"}})
		dryRunTwiceLeavesTheStateAlone(t, doctor.FormulasName, w.check, dir, dryRunNow)
	})
}

func TestVaultDirtyADryRunSavesAndForgetsNothing(t *testing.T) {
	t.Run("a file outside runs/ seen for the first time", func(t *testing.T) {
		clone, _ := aVaultDirtyVault(t)
		vdWrite(t, clone, "seats/builder/rigs/millwright.md", "changed by hand\n")
		dir := t.TempDir()
		check := doctor.NewVaultDirty(clone, "laptop", doctor.New(dir))
		dryRunTwiceLeavesTheStateAlone(t, doctor.VaultDirtyName, check, dir, dryRunNow)
	})
	t.Run("a clean vault while a sighting is kept", func(t *testing.T) {
		clone, _ := aVaultDirtyVault(t)
		dir := t.TempDir()
		store := doctor.New(dir)
		seedEpisode(t, store, "vault-dirty-seen", application.DoctorEpisode{SeenPaths: []string{"seats/builder/rigs/millwright.md"}})
		check := doctor.NewVaultDirty(clone, "laptop", store)
		dryRunTwiceLeavesTheStateAlone(t, doctor.VaultDirtyName, check, dir, dryRunNow)
	})
}

func TestBoostReachADryRunSavesAndForgetsNothing(t *testing.T) {
	build := func(dir string, up, wgFaulty bool) *doctor.BoostReach {
		check := doctor.NewBoostReach(&apptest.FakeHomeFile{Text: "laptop 2026-09-29T00:10:00Z mw@laptop"}, "laptop",
			map[string]string{"desktop": "ssh desktop"}, doctor.New(dir))
		check.Now = func() time.Time { return dryRunNow }
		check.Ssh = func(context.Context, []string) error {
			if up {
				return nil
			}
			return errors.New("ssh: timed out")
		}
		check.WgFaulty = func(context.Context) bool { return wgFaulty }
		return check
	}
	t.Run("first sight of the Boost down", func(t *testing.T) {
		dir := t.TempDir()
		dryRunTwiceLeavesTheStateAlone(t, doctor.BoostReachName, build(dir, false, false), dir, dryRunNow)
	})
	t.Run("the Boost back while an outage is kept", func(t *testing.T) {
		dir := t.TempDir()
		seedEpisode(t, doctor.New(dir), doctor.BoostReachSeenStateName, application.DoctorEpisode{FirstFaulty: dryRunNow.Add(-time.Hour)})
		dryRunTwiceLeavesTheStateAlone(t, doctor.BoostReachName, build(dir, true, false), dir, dryRunNow)
	})
	t.Run("the home's own hub down while an outage is kept", func(t *testing.T) {
		dir := t.TempDir()
		seedEpisode(t, doctor.New(dir), doctor.BoostReachSeenStateName, application.DoctorEpisode{FirstFaulty: dryRunNow.Add(-time.Hour)})
		dryRunTwiceLeavesTheStateAlone(t, doctor.BoostReachName, build(dir, false, true), dir, dryRunNow)
	})
}
