package doctor_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/infrastructure/doctor"
)

const boostFitHome = "laptop 2026-09-29T00:10:00Z mw@laptop"

// fitAnswer is what a host's `mw host fit --json` prints, or how ssh ends.
type fitAnswer struct {
	gaps []application.HostFitItem
	exit int
	out  string
	err  error
}

type boostFitRig struct {
	t       *testing.T
	store   *doctor.Store
	now     time.Time
	answers map[string]fitAnswer
	mu      sync.Mutex
	asked   []string
	cloud   []string
	retired []string
	notes   []string
}

func newBoostFitRig(t *testing.T) *boostFitRig {
	return &boostFitRig{t: t, store: doctor.New(t.TempDir()), now: time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC),
		answers: map[string]fitAnswer{"desktop": {}}}
}

func (r *boostFitRig) check(host string) *doctor.BoostFit {
	reach := map[string]string{"desktop": "ssh desktop", "laptop": "ssh laptop"}
	for _, name := range append(append([]string{}, r.retired...), r.cloud...) {
		reach[name] = "ssh " + name
	}
	check := doctor.NewBoostFit(&apptest.FakeHomeFile{Text: boostFitHome}, host, reach, r.store)
	check.Retired = r.retired
	check.Cloud = func(context.Context) ([]string, error) { return r.cloud, nil }
	check.Now = func() time.Time { return r.now }
	check.Note = func(_ context.Context, text string) error { r.notes = append(r.notes, text); return nil }
	check.Ask = func(_ context.Context, argv []string) ([]byte, int, error) {
		r.mu.Lock()
		defer r.mu.Unlock()
		host := argv[1]
		r.asked = append(r.asked, host+": "+argv[len(argv)-1])
		a, ok := r.answers[host]
		if !ok {
			r.t.Errorf("asked %s, which is not to be asked", host)
		}
		if a.err != nil {
			return nil, 0, a.err
		}
		if a.out != "" || a.exit != 0 {
			return []byte(a.out), a.exit, nil
		}
		report := application.HostFitReport{Commit: "abc1234", Items: []application.HostFitItem{{Name: "mw-commit", OK: true, Detail: "fine"}}}
		report.Items = append(report.Items, a.gaps...)
		out, _ := json.Marshal(report)
		return out, 0, nil
	}
	return check
}

func (r *boostFitRig) probe(host string) (application.Verdict, string) {
	return r.check(host).Probe(context.Background())
}

func TestBoostFitAFitHostIsOkAndIsAskedOverSsh(t *testing.T) {
	r := newBoostFitRig(t)
	verdict, reason := r.probe("laptop")
	if verdict != application.DoctorOK || reason != "" {
		t.Fatalf("a fit Boost: %v %q", verdict, reason)
	}
	want := "desktop: bash -lc 'mw host fit --json'"
	if len(r.asked) != 1 || r.asked[0] != want {
		t.Fatalf("asked %v, want [%s]", r.asked, want)
	}
}

func TestBoostFitNamesADisabledDoctorTimerWithItsCure(t *testing.T) {
	r := newBoostFitRig(t)
	r.answers["desktop"] = fitAnswer{gaps: []application.HostFitItem{
		{Name: "unit mw-doctor", Detail: "mw-doctor.timer is not enabled; last run 10 days ago", Cure: application.HostFitCureUnits},
	}}
	verdict, reason := r.probe("laptop")
	if verdict != application.DoctorFaulty {
		t.Fatalf("verdict %v, reason %q", verdict, reason)
	}
	for _, want := range []string{"desktop is not fit, 1 gaps", "mw-doctor.timer is not enabled", "(cure: sh scripts/install-units.sh --boost --enable"} {
		if !strings.Contains(reason, want) {
			t.Errorf("reason lacks %q: %s", want, reason)
		}
	}
}

func TestBoostFitNamesThreeZeroByteTrackedFiles(t *testing.T) {
	r := newBoostFitRig(t)
	r.answers["desktop"] = fitAnswer{gaps: []application.HostFitItem{{
		Name: "zero-byte-files", Detail: "3 tracked files are empty on disk: the rig checkout: 3 (a.go, b.go, c.go)",
		Cure: "git -C /home/jwhite/millwright checkout -- a.go b.go c.go",
	}}}
	verdict, reason := r.probe("laptop")
	if verdict != application.DoctorFaulty {
		t.Fatalf("verdict %v, reason %q", verdict, reason)
	}
	for _, want := range []string{"3 tracked files are empty", "a.go, b.go, c.go", "(cure: git -C /home/jwhite/millwright checkout -- a.go b.go c.go)"} {
		if !strings.Contains(reason, want) {
			t.Errorf("reason lacks %q: %s", want, reason)
		}
	}
}

func TestBoostFitOneLinePerHostAndNothingForAHostThatIsFit(t *testing.T) {
	r := newBoostFitRig(t)
	r.answers["desktop"] = fitAnswer{gaps: []application.HostFitItem{{Name: "fstrim", Detail: "never run", Cure: application.HostFitCureTrim}}}
	r.answers["spare"] = fitAnswer{gaps: []application.HostFitItem{{Name: "beads-env", Detail: "missing", Cure: application.HostFitCureBeadsEnv}}}
	r.answers["fine"] = fitAnswer{}
	check := doctor.NewBoostFit(&apptest.FakeHomeFile{Text: boostFitHome}, "laptop",
		map[string]string{"desktop": "ssh desktop", "spare": "ssh spare", "fine": "ssh fine"}, r.store)
	check.Now = func() time.Time { return r.now }
	check.Ask = r.check("laptop").Ask
	_, reason := check.Probe(context.Background())
	lines := strings.Split(reason, " | ")
	if len(lines) != 2 || !strings.HasPrefix(lines[0], "desktop is not fit") || !strings.HasPrefix(lines[1], "spare is not fit") {
		t.Fatalf("one line a host that is not fit, in name order; got %q", reason)
	}
}

func TestBoostFitIsSilentForAHostThatDoesNotAnswer(t *testing.T) {
	for name, answer := range map[string]fitAnswer{
		"ssh failed to connect":       {exit: 255, out: "ssh: connect to host desktop port 22: Connection timed out"},
		"ssh could not even be run":   {err: errors.New("fork/exec ssh: no such file")},
		"the answer is boost-reach's": {exit: 255},
	} {
		t.Run(name, func(t *testing.T) {
			r := newBoostFitRig(t)
			r.answers["desktop"] = answer
			verdict, reason := r.probe("laptop")
			if verdict != application.DoctorOK || reason != "" {
				t.Fatalf("a host that does not answer: %v %q", verdict, reason)
			}
		})
	}
}

func TestBoostFitAHostWhoseMwHasNoHostFitIsAGapWithTheUpdateCure(t *testing.T) {
	r := newBoostFitRig(t)
	r.answers["desktop"] = fitAnswer{exit: 1, out: "Error: unknown command \"host fit\" for \"mw\"\nRun 'mw --help'"}
	verdict, reason := r.probe("laptop")
	if verdict != application.DoctorFaulty {
		t.Fatalf("verdict %v, reason %q", verdict, reason)
	}
	for _, want := range []string{"desktop:", "did not run there (exit 1", "unknown command", "git pull --ff-only"} {
		if !strings.Contains(reason, want) {
			t.Errorf("reason lacks %q: %s", want, reason)
		}
	}
	if strings.Contains(reason, "Run 'mw --help'") {
		t.Errorf("only the first line of the error is said: %s", reason)
	}
}

func TestBoostFitRunsMwThroughALoginShellSoMwIsOnItsPath(t *testing.T) {
	r := newBoostFitRig(t)
	r.probe("laptop")
	if len(r.asked) != 1 || !strings.Contains(r.asked[0], "bash -lc 'mw host fit --json'") {
		t.Fatalf("the command is not run through a login shell: %v", r.asked)
	}
}

func TestBoostFitExit127IsMwNotOnThePathOverSshNotMwBehindMain(t *testing.T) {
	r := newBoostFitRig(t)
	r.answers["desktop"] = fitAnswer{exit: 127, out: "bash: mw: command not found"}
	verdict, reason := r.probe("laptop")
	if verdict != application.DoctorFaulty {
		t.Fatalf("verdict %v, reason %q", verdict, reason)
	}
	if !strings.Contains(reason, "mw not found on PATH over ssh") {
		t.Errorf("reason lacks the gap: %s", reason)
	}
	for _, not := range []string{"git pull", "make build", "behind"} {
		if strings.Contains(reason, not) {
			t.Errorf("reason names %q, a cure for an old mw: %s", not, reason)
		}
	}
}

func TestBoostFitAsksAtMostOnceAnHourAndSaysAgainWhatItFound(t *testing.T) {
	r := newBoostFitRig(t)
	r.answers["desktop"] = fitAnswer{gaps: []application.HostFitItem{{Name: "fstrim", Detail: "never run", Cure: "c"}}}
	_, first := r.probe("laptop")
	r.now = r.now.Add(59 * time.Minute)
	r.answers["desktop"] = fitAnswer{}
	verdict, again := r.probe("laptop")
	if len(r.asked) != 1 {
		t.Fatalf("asked %d times inside the hour: %v", len(r.asked), r.asked)
	}
	if verdict != application.DoctorFaulty || again != first {
		t.Fatalf("inside the hour it says again %q, got %v %q", first, verdict, again)
	}
	r.now = r.now.Add(2 * time.Minute)
	verdict, reason := r.probe("laptop")
	if len(r.asked) != 2 || verdict != application.DoctorOK || reason != "" {
		t.Fatalf("after the hour it asks again and finds it fit: %v %v %q", r.asked, verdict, reason)
	}
}

func TestBoostFitOnABoostItselfIsOkAskedByTheHome(t *testing.T) {
	r := newBoostFitRig(t)
	verdict, reason := r.probe("desktop")
	if verdict != application.DoctorOK || reason != "asked by the home" {
		t.Fatalf("on a Boost: %v %q", verdict, reason)
	}
	if len(r.asked) != 0 {
		t.Fatalf("a Boost asked %v", r.asked)
	}
}

func TestBoostFitWhereThereIsNoBoostIsOk(t *testing.T) {
	r := newBoostFitRig(t)
	lone := doctor.NewBoostFit(&apptest.FakeHomeFile{Text: boostFitHome}, "laptop", map[string]string{"laptop": "ssh laptop"}, r.store)
	if verdict, reason := lone.Probe(context.Background()); verdict != application.DoctorOK || !strings.Contains(reason, "no Boost") {
		t.Fatalf("a home with no Boost: %v %q", verdict, reason)
	}
	none := doctor.NewBoostFit(&apptest.FakeHomeFile{Text: boostFitHome}, "laptop", nil, r.store)
	if verdict, _ := none.Probe(context.Background()); verdict != application.DoctorOK {
		t.Fatalf("no [hands_hosts] at all: %v", verdict)
	}
}

func TestBoostFitAsksNeitherARetiredHostNorACloudBox(t *testing.T) {
	r := newBoostFitRig(t)
	r.retired = []string{"old"}
	r.cloud = []string{"box-1"}
	r.answers["old"] = fitAnswer{gaps: []application.HostFitItem{{Name: "x", Detail: "y", Cure: "z"}}}
	r.answers["box-1"] = r.answers["old"]
	verdict, _ := r.probe("laptop")
	if verdict != application.DoctorOK {
		t.Fatalf("verdict %v", verdict)
	}
	if len(r.asked) != 1 || !strings.HasPrefix(r.asked[0], "desktop") {
		t.Fatalf("asked %v, want only the desktop", r.asked)
	}
}

func TestBoostFitCureIsOneNoteNamingTheGapsAndLeavesTheFault(t *testing.T) {
	r := newBoostFitRig(t)
	r.answers["desktop"] = fitAnswer{gaps: []application.HostFitItem{{Name: "fstrim", Detail: "never run", Cure: "his hands"}}}
	check := r.check("laptop")
	if verdict, _ := check.Probe(context.Background()); verdict != application.DoctorFaulty {
		t.Fatal("not faulty")
	}
	if err := check.Cure(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(r.notes) != 1 || !strings.Contains(r.notes[0], "desktop is not fit") {
		t.Fatalf("notes: %v", r.notes)
	}
	if !check.CureLeavesFault() {
		t.Fatal("the note changes nothing on the Boost")
	}
}

func TestBoostFitADryRunAsksAndSavesNothingSoItsHourIsLeftAlone(t *testing.T) {
	r := newBoostFitRig(t)
	r.answers["desktop"] = fitAnswer{gaps: []application.HostFitItem{{Name: "fstrim", Detail: "never run", Cure: "c"}}}
	doc := application.Doctor{Checks: application.DoctorChecks{r.check("laptop")}, State: r.store, Log: r.store,
		Host: "laptop", Now: func() time.Time { return r.now }}
	for i := 0; i < 2; i++ {
		if _, err := doc.Run(context.Background(), doctor.BoostFitName, true); err != nil {
			t.Fatal(err)
		}
		kept, err := r.store.Load(context.Background(), doctor.BoostFitSeenStateName)
		if err != nil {
			t.Fatal(err)
		}
		if !kept.FirstFaulty.IsZero() || len(kept.SeenPaths) != 0 {
			t.Fatalf("dry run %d saved the hourly state: %+v", i+1, kept)
		}
	}
	if len(r.asked) != 2 {
		t.Errorf("with nothing kept, each dry run asks afresh: asked %v", r.asked)
	}
}

func TestBoostFitADryRunInsideTheHourLeavesTheKeptClockAlone(t *testing.T) {
	r := newBoostFitRig(t)
	r.answers["desktop"] = fitAnswer{gaps: []application.HostFitItem{{Name: "fstrim", Detail: "never run", Cure: "c"}}}
	r.probe("laptop")
	before, err := r.store.Load(context.Background(), doctor.BoostFitSeenStateName)
	if err != nil || before.FirstFaulty.IsZero() {
		t.Fatalf("a real run keeps its clock: %+v %v", before, err)
	}
	r.now = r.now.Add(61 * time.Minute)
	doc := application.Doctor{Checks: application.DoctorChecks{r.check("laptop")}, State: r.store, Log: r.store,
		Host: "laptop", Now: func() time.Time { return r.now }}
	if _, err := doc.Run(context.Background(), doctor.BoostFitName, true); err != nil {
		t.Fatal(err)
	}
	after, _ := r.store.Load(context.Background(), doctor.BoostFitSeenStateName)
	if !after.FirstFaulty.Equal(before.FirstFaulty) {
		t.Errorf("a dry run moved the kept clock from %v to %v", before.FirstFaulty, after.FirstFaulty)
	}
}
