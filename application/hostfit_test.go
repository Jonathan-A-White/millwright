package application_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

var fitNow = time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)

// fitProbes is a host that is fit; a test changes the one thing it is about.
type fitProbes struct {
	commit     string
	behind     int
	units      map[string]application.HostFitUnitState
	verdictAt  time.Time
	zero       []application.HostFitZeroBytes
	envPresent bool
	wsl        bool
	winOK      bool
	winText    string
	trimAt     time.Time
	gives      bool
	renewed    time.Time
	limits     application.HostFitLimits
	errs       map[string]error
}

func fitHost() *fitProbes {
	p := &fitProbes{
		commit:     "abc1234",
		units:      map[string]application.HostFitUnitState{},
		verdictAt:  fitNow.Add(-20 * time.Minute),
		envPresent: true,
		limits:     application.HostFitLimits{Cap: 2, BuilderJobs: 4, MemTotalMB: 12000, SwapTotalMB: 8192},
		errs:       map[string]error{},
	}
	for _, u := range application.HostFitUnits {
		p.units[u] = application.HostFitUnitState{Enabled: true, LastRun: fitNow.Add(-time.Hour)}
	}
	return p
}

func (p *fitProbes) MwBehind(context.Context) (string, int, error) {
	return p.commit, p.behind, p.errs["mw"]
}
func (p *fitProbes) Unit(_ context.Context, unit string) (application.HostFitUnitState, error) {
	return p.units[unit], p.errs["unit "+unit]
}
func (p *fitProbes) DoctorVerdictAt(context.Context) (time.Time, error) {
	return p.verdictAt, p.errs["doctor"]
}
func (p *fitProbes) ZeroBytes(context.Context) ([]application.HostFitZeroBytes, error) {
	return p.zero, p.errs["zero"]
}
func (p *fitProbes) BeadsEnv(context.Context) (string, bool, error) {
	return "~/.config/mw/beads.env", p.envPresent, p.errs["env"]
}
func (p *fitProbes) WSL(context.Context) (bool, error) { return p.wsl, nil }
func (p *fitProbes) WindowsState(context.Context) (bool, string, error) {
	return p.winOK, p.winText, p.errs["windows"]
}
func (p *fitProbes) FstrimLastRun(context.Context) (time.Time, error) {
	return p.trimAt, p.errs["trim"]
}
func (p *fitProbes) GiveWay(context.Context) (bool, time.Time, error) {
	return p.gives, p.renewed, p.errs["giveway"]
}
func (p *fitProbes) Limits(context.Context) (application.HostFitLimits, error) {
	return p.limits, p.errs["limits"]
}

func runFit(p *fitProbes) application.HostFitReport {
	return application.HostFit{Probes: p, Now: func() time.Time { return fitNow }}.Run(context.Background())
}

func gapNamed(t *testing.T, r application.HostFitReport, name string) application.HostFitItem {
	t.Helper()
	gaps := r.Gaps()
	if len(gaps) != 1 || gaps[0].Name != name {
		t.Fatalf("want the one gap %q, got %+v", name, gaps)
	}
	if gaps[0].Cure == "" {
		t.Fatalf("the gap %q names no cure", name)
	}
	return gaps[0]
}

func TestHostFitFitHostHasNoGapAndOneLineEach(t *testing.T) {
	p := fitHost()
	p.wsl, p.winOK, p.winText = true, true, "windows-state current (1 minutes old)"
	p.trimAt = fitNow.Add(-5 * time.Hour)
	p.gives, p.renewed = true, fitNow.Add(-3*time.Minute)
	r := runFit(p)
	if !r.Fit() {
		t.Fatalf("a fit host has gaps: %+v", r.Gaps())
	}
	var out bytes.Buffer
	r.WriteText(&out)
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != len(r.Items) {
		t.Fatalf("want one line an item (%d), got %d:\n%s", len(r.Items), len(lines), out.String())
	}
	for _, line := range lines {
		if !strings.HasPrefix(line, "ok ") {
			t.Fatalf("a fit host prints a gap: %q", line)
		}
	}
	text := out.String()
	for _, want := range []string{"mw abc1234 is at origin/main", "mw-doctor.timer is enabled", "windows-state", "fstrim", "give-way-renew", "cap 2, builder_jobs 4, memory 12000 MB, swap 8192 MB", "beads.env is there"} {
		if !strings.Contains(text, want) {
			t.Errorf("the report lacks %q:\n%s", want, text)
		}
	}
}

func TestHostFitSaysTheBeadsEnvByNameAndNeverItsContents(t *testing.T) {
	p := fitHost()
	r := runFit(p)
	for _, item := range r.Items {
		if item.Name == "beads-env" && !strings.Contains(item.Detail, "beads.env") {
			t.Fatalf("the item does not name the file: %+v", item)
		}
	}
}

func TestHostFitGapMwBehindMain(t *testing.T) {
	p := fitHost()
	p.behind = 7
	g := gapNamed(t, runFit(p), "mw-commit")
	if !strings.Contains(g.Detail, "behind origin/main by 7") {
		t.Fatalf("detail: %q", g.Detail)
	}
}

func TestHostFitGapUnitNotEnabledNamesItsLastRun(t *testing.T) {
	p := fitHost()
	p.units["mw-doctor"] = application.HostFitUnitState{Enabled: false, LastRun: fitNow.Add(-250 * time.Hour)}
	g := gapNamed(t, runFit(p), "unit mw-doctor")
	if !strings.Contains(g.Detail, "not enabled") || !strings.Contains(g.Detail, "10 days") {
		t.Fatalf("detail: %q", g.Detail)
	}
	if !strings.Contains(g.Cure, "install-units.sh --boost --enable") {
		t.Fatalf("cure: %q", g.Cure)
	}
}

func TestHostFitGapUnitThatNeverRan(t *testing.T) {
	p := fitHost()
	p.units["mw-host-tidy"] = application.HostFitUnitState{Enabled: true}
	r := runFit(p)
	for _, item := range r.Items {
		if item.Name == "unit mw-host-tidy" && !strings.Contains(item.Detail, "last run never") {
			t.Fatalf("detail: %q", item.Detail)
		}
	}
}

func TestHostFitGapDoctorVerdictOverAnHourOld(t *testing.T) {
	p := fitHost()
	p.verdictAt = fitNow.Add(-10 * 24 * time.Hour)
	g := gapNamed(t, runFit(p), "doctor-verdict")
	if !strings.Contains(g.Detail, "10 days old") {
		t.Fatalf("detail: %q", g.Detail)
	}
	p.verdictAt = time.Time{}
	gapNamed(t, runFit(p), "doctor-verdict")
	p.verdictAt = fitNow.Add(-59 * time.Minute)
	if r := runFit(p); !r.Fit() {
		t.Fatalf("a verdict 59 minutes old is a gap: %+v", r.Gaps())
	}
}

func TestHostFitGapThreeZeroByteTrackedFiles(t *testing.T) {
	p := fitHost()
	p.zero = []application.HostFitZeroBytes{{Where: "the rig checkout", Dir: "/home/jwhite/millwright", Paths: []string{"a.go", "b.go", "c.go"}}}
	g := gapNamed(t, runFit(p), "zero-byte-files")
	for _, want := range []string{"3 tracked files", "a.go", "c.go", "the rig checkout"} {
		if !strings.Contains(g.Detail, want) {
			t.Errorf("detail lacks %q: %q", want, g.Detail)
		}
	}
	if g.Cure != "git -C /home/jwhite/millwright checkout -- a.go b.go c.go" {
		t.Fatalf("cure: %q", g.Cure)
	}
}

func TestHostFitGapZeroByteFileOfTheVault(t *testing.T) {
	p := fitHost()
	p.zero = []application.HostFitZeroBytes{
		{Where: "the rig checkout", Dir: "/r"},
		{Where: "the vault", Dir: "/v", Paths: []string{"plans/x.md"}},
	}
	g := gapNamed(t, runFit(p), "zero-byte-files")
	if !strings.Contains(g.Detail, "the vault: 1") || strings.Contains(g.Detail, "rig checkout") {
		t.Fatalf("detail: %q", g.Detail)
	}
}

func TestHostFitGapBeadsEnvMissing(t *testing.T) {
	p := fitHost()
	p.envPresent = false
	g := gapNamed(t, runFit(p), "beads-env")
	if !strings.Contains(g.Detail, "beads.env is missing") || !strings.Contains(g.Cure, "his hands") {
		t.Fatalf("gap: %+v", g)
	}
}

func TestHostFitWSLGaps(t *testing.T) {
	t.Run("the Windows side is not as it must be", func(t *testing.T) {
		p := fitHost()
		p.wsl, p.winOK, p.winText = true, false, "windows-state is 3 hours old"
		p.trimAt = fitNow.Add(-time.Hour)
		g := gapNamed(t, runFit(p), "windows-state")
		if !strings.Contains(g.Detail, "3 hours old") || !strings.Contains(g.Cure, "his hands") || !strings.Contains(g.Cure, "wsl-flag --install-line") {
			t.Fatalf("gap: %+v", g)
		}
	})
	t.Run("fstrim never ran", func(t *testing.T) {
		p := fitHost()
		p.wsl, p.winOK, p.winText = true, true, "current"
		g := gapNamed(t, runFit(p), "fstrim")
		if !strings.Contains(g.Detail, "never run") {
			t.Fatalf("detail: %q", g.Detail)
		}
	})
	t.Run("fstrim ran over 8 days ago", func(t *testing.T) {
		p := fitHost()
		p.wsl, p.winOK, p.winText = true, true, "current"
		p.trimAt = fitNow.Add(-9 * 24 * time.Hour)
		gapNamed(t, runFit(p), "fstrim")
	})
	t.Run("a host that is not WSL has none of these", func(t *testing.T) {
		p := fitHost()
		p.winOK = false
		for _, item := range runFit(p).Items {
			if item.Name == "windows-state" || item.Name == "fstrim" || item.Name == "wsl-disk-fits" {
				t.Fatalf("a host that is not WSL says %q", item.Name)
			}
		}
	})
	t.Run("wsl-disk-fits, when the build has it", func(t *testing.T) {
		p := fitHost()
		p.wsl, p.winOK, p.winText, p.trimAt = true, true, "current", fitNow
		fit := application.HostFit{Probes: p, Now: func() time.Time { return fitNow },
			DiskFits: func(context.Context) (bool, string, error) {
				return false, "the disk is 1007 GB on a 286 GB drive", nil
			}}
		r := fit.Run(context.Background())
		g := gapNamed(t, r, "wsl-disk-fits")
		if !strings.Contains(g.Detail, "1007 GB") {
			t.Fatalf("detail: %q", g.Detail)
		}
	})
}

func TestHostFitGiveWayRenewUnderTenMinutes(t *testing.T) {
	p := fitHost()
	p.gives, p.renewed = true, fitNow.Add(-25*time.Minute)
	g := gapNamed(t, runFit(p), "give-way-renew")
	if !strings.Contains(g.Detail, "25 minutes") || !strings.Contains(g.Cure, "giveway --install-line") {
		t.Fatalf("gap: %+v", g)
	}
	p.renewed = time.Time{}
	gapNamed(t, runFit(p), "give-way-renew")
	p.gives = false
	for _, item := range runFit(p).Items {
		if item.Name == "give-way-renew" {
			t.Fatalf("a host that does not give way is held to a renew: %+v", item)
		}
	}
}

func TestHostFitAThingThatCannotBeReadIsAGapAndNeverLeavesTheContents(t *testing.T) {
	p := fitHost()
	p.errs["doctor"] = errors.New("permission\ndenied")
	g := gapNamed(t, runFit(p), "doctor-verdict")
	if !strings.Contains(g.Detail, "could not be read: permission denied") {
		t.Fatalf("detail: %q", g.Detail)
	}
}

func TestHostFitPrintsAGapWithItsCure(t *testing.T) {
	p := fitHost()
	p.behind = 2
	var out bytes.Buffer
	runFit(p).WriteText(&out)
	if !strings.Contains(out.String(), "GAP  mw-commit: mw abc1234 is behind origin/main by 2 (cure: git pull --ff-only") {
		t.Fatalf("output:\n%s", out.String())
	}
}
