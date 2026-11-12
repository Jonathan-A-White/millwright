package application_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

func aFactoryUpdate() (application.SelfUpdate, *fakeCheckout, *fakeAfter, fakeBuilt) {
	checkout := &fakeCheckout{head: "027f977aaaaaaaa", tip: "bfd385cbbbbbbbb"}
	after := &fakeAfter{command: "make build"}
	built := fakeBuilt{"millwright.deployed": "027f977aaaaaaaa"}
	return application.SelfUpdate{
		Rigs: map[string]string{"millwright": "/rigs/millwright"}, Checkout: checkout, After: after, Built: built,
	}, checkout, after, built
}

// mw-gq6.1009: the self-update's passing build of the factory rig is a deploy on
// this host, so it moves the deployed mark mw verify reads.
func TestASelfUpdateThatBuildsTheFactoryRigMovesItsDeployedMark(t *testing.T) {
	update, _, _, built := aFactoryUpdate()

	update.Run(context.Background())

	if built["millwright"] != "bfd385cbbbbbbbb" {
		t.Errorf("expected the build remembered at the new head, got %v", built)
	}
	if built["millwright.deployed"] != "bfd385cbbbbbbbb" {
		t.Errorf("expected the deployed mark moved to the new head, got %q", built["millwright.deployed"])
	}
}

func TestASelfUpdateWhoseBuildFailsLeavesTheDeployedMarkWhereItWas(t *testing.T) {
	update, _, after, built := aFactoryUpdate()
	after.ran = application.Ran{Command: "make build", Status: 2, Output: "boom\n"}

	update.Run(context.Background())

	if built["millwright.deployed"] != "027f977aaaaaaaa" {
		t.Errorf("expected the deployed mark left at the old head, got %q", built["millwright.deployed"])
	}
}

func TestMwVerifyReadsDeployOKAfterTheSelfUpdateBuiltTheLanding(t *testing.T) {
	update, _, _, built := aFactoryUpdate()
	update.Run(context.Background())

	beads := &apptest.FakeVerifyBeads{
		Details: map[string]application.StoryDetail{"mw-1": {
			Story:       domain.Story{ID: "mw-1", Title: "a landing", Overrides: domain.Path{Rig: "millwright"}},
			Status:      "closed",
			CloseReason: "landed on main at bfd385cbbbbbbbb",
		}},
		Comments: map[string][]application.Comment{},
	}
	out := &strings.Builder{}
	_, err := application.VerifyDeploy{
		Beads: beads, Rules: apptest.NewFakeEpicRules(), Marks: deployMarks(built), Out: out,
		After: deployAfter{"millwright": "make build"}, History: deployHistory{},
	}.Run(context.Background(), "mw-1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "deploy OK") || strings.Contains(out.String(), "MISMATCH") {
		t.Errorf("expected deploy OK, got:\n%s", out.String())
	}
}

func TestALevelFactoryRigBuiltAtHeadHasItsStaleDeployedMarkCaughtUp(t *testing.T) {
	update, checkout, after, built := aFactoryUpdate()
	checkout.head = checkout.tip
	built["millwright"] = checkout.tip

	update.Run(context.Background())

	if built["millwright.deployed"] != checkout.tip || after.runs != 0 {
		t.Errorf("expected the mark caught up with no build, got %q after %d builds", built["millwright.deployed"], after.runs)
	}
}

// mw-gq6.1125: a checkout whose pack is corrupt fails every fetch; the host
// moves it aside, clones it again and builds.

const corruptPackSaid = "git fetch origin in /rigs/millwright: exit 128: error: unknown object type 0 at offset 25297795 in .git/objects/pack/pack-ef795bcaf95c577366a4ba7d3e2ee0f83b4e2d97.pack"

type fakeHealer struct {
	checkout   *fakeCheckout
	unpushed   int
	unpushErr  error
	recloneErr error
	recloned   []string
}

func (f *fakeHealer) Unpushed(context.Context, string) (int, error) { return f.unpushed, f.unpushErr }
func (f *fakeHealer) Reclone(_ context.Context, dir, aside, branch string) (string, error) {
	f.recloned = append(f.recloned, dir+" -> "+aside+" @"+branch)
	if f.recloneErr != nil {
		return "", f.recloneErr
	}
	f.checkout.head = f.checkout.tip
	return aside, nil
}

func aCorruptCheckout(box string) (application.SelfUpdate, *fakeCheckout, *fakeAfter, fakeBuilt, *fakeHealer) {
	update, checkout, after, built := aFactoryUpdate()
	checkout.fetchErr = errors.New(corruptPackSaid)
	healer := &fakeHealer{checkout: checkout}
	update.Heal = healer
	update.Box = box
	update.Host = box
	now := time.Date(2026, 11, 12, 18, 30, 0, 0, time.UTC)
	update.Now = func() time.Time { return now }
	return update, checkout, after, built, healer
}

func TestASelfUpdateWhoseFetchMeetsACorruptPackClonesTheCheckoutAgainAndBuilds(t *testing.T) {
	update, checkout, after, built, healer := aCorruptCheckout("cloud1")
	built["millwright"] = "027f977aaaaaaaa" // built at the old head, as a host that has been running

	notes, failure := update.Update(context.Background())

	if failure != nil {
		t.Fatalf("expected no failure, got %+v (%v)", failure, notes)
	}
	want := "/rigs/millwright -> /rigs/millwright.corrupt-2026-11-12 @main"
	if len(healer.recloned) != 1 || healer.recloned[0] != want {
		t.Errorf("expected one re-clone %q, got %v", want, healer.recloned)
	}
	if after.runs != 1 {
		t.Errorf("expected the build to run once in the new clone, got %d", after.runs)
	}
	if built["millwright"] != checkout.tip {
		t.Errorf("expected the build remembered at origin's main, got %q", built["millwright"])
	}
	if all := strings.Join(notes, "\n"); !strings.Contains(all, "corrupt-2026-11-12") || !strings.Contains(all, "built") {
		t.Errorf("expected the notes to say the checkout was moved aside and built, got:\n%s", all)
	}
}

func TestASelfUpdateNeverClonesACheckoutWithUncommittedChanges(t *testing.T) {
	update, checkout, after, _, healer := aCorruptCheckout("cloud1")
	checkout.dirty = []string{"Makefile"}

	_, failure := update.Update(context.Background())

	if len(healer.recloned) != 0 || after.runs != 0 {
		t.Errorf("expected no re-clone and no build, got %v, %d builds", healer.recloned, after.runs)
	}
	if failure == nil || !strings.Contains(failure.Said, "uncommitted") {
		t.Errorf("expected the failure to say the changes are the reason, got %+v", failure)
	}
}

func TestASelfUpdateNeverClonesACheckoutWithCommitsNotOnOrigin(t *testing.T) {
	update, _, after, _, healer := aCorruptCheckout("cloud1")
	healer.unpushed = 2

	_, failure := update.Update(context.Background())

	if len(healer.recloned) != 0 || after.runs != 0 {
		t.Errorf("expected no re-clone and no build, got %v, %d builds", healer.recloned, after.runs)
	}
	if failure == nil || !strings.Contains(failure.Said, "2 commits not on origin") {
		t.Errorf("expected the failure to say the commits are the reason, got %+v", failure)
	}
}

func TestASelfUpdateThatCannotTellWhatIsUnpushedClonesNothing(t *testing.T) {
	update, _, _, _, healer := aCorruptCheckout("cloud1")
	healer.unpushErr = errors.New("rev-list: bad object")

	_, failure := update.Update(context.Background())

	if len(healer.recloned) != 0 || failure == nil {
		t.Errorf("expected no re-clone and a failure, got %v, %+v", healer.recloned, failure)
	}
}

func TestAFetchThatFailedForTheNetworkIsNotAReclone(t *testing.T) {
	update, checkout, _, _, healer := aCorruptCheckout("cloud1")
	checkout.fetchErr = &application.TransientFault{Why: "early EOF", Err: errors.New("fatal: early EOF\nfatal: index-pack failed")}

	_, failure := update.Update(context.Background())

	if len(healer.recloned) != 0 || failure == nil || failure.Step != "fetch" {
		t.Errorf("expected a plain fetch failure, got %v, %+v", healer.recloned, failure)
	}
}

func TestACloudBoxWhoseCloneFailsTooIsNamedWithTheStepToDestroyIt(t *testing.T) {
	update, _, _, _, healer := aCorruptCheckout("cloud3")
	healer.recloneErr = errors.New("git clone: No space left on device")

	_, failure := update.Update(context.Background())

	if failure == nil || !strings.Contains(failure.Said, "mw cloud down cloud3") {
		t.Errorf("expected the alarm to name mw cloud down cloud3, got %+v", failure)
	}
}

func TestAHostThatIsNoCloudBoxIsNotToldToDestroyItself(t *testing.T) {
	update, _, _, _, healer := aCorruptCheckout("")
	update.Host = "laptop"
	healer.recloneErr = errors.New("git clone: No space left on device")

	_, failure := update.Update(context.Background())

	if failure == nil || strings.Contains(failure.Said, "mw cloud down") {
		t.Errorf("expected a failure that does not name mw cloud down, got %+v", failure)
	}
}

func TestACloudBoxThatMeetsASecondCorruptPackWithinAnHourIsNamedAndNotClonedAgain(t *testing.T) {
	update, checkout, _, _, healer := aCorruptCheckout("cloud3")
	if _, failure := update.Update(context.Background()); failure != nil {
		t.Fatalf("expected the first fault healed, got %+v", failure)
	}
	checkout.head = "027f977aaaaaaaa"
	now := time.Date(2026, 11, 12, 19, 10, 0, 0, time.UTC)
	update.Now = func() time.Time { return now }

	_, failure := update.Update(context.Background())

	if len(healer.recloned) != 1 {
		t.Errorf("expected the second fault not cloned again, got %v", healer.recloned)
	}
	if failure == nil || !strings.Contains(failure.Said, "mw cloud down cloud3") || !strings.Contains(failure.Said, "second") {
		t.Errorf("expected the alarm to name the second fault and mw cloud down cloud3, got %+v", failure)
	}
}

func TestACloudBoxThatMeetsASecondCorruptPackAfterAnHourIsClonedAgain(t *testing.T) {
	update, checkout, _, _, healer := aCorruptCheckout("cloud3")
	update.Update(context.Background())
	checkout.head = "027f977aaaaaaaa"
	now := time.Date(2026, 11, 12, 19, 31, 0, 0, time.UTC)
	update.Now = func() time.Time { return now }

	_, failure := update.Update(context.Background())

	if failure != nil || len(healer.recloned) != 2 {
		t.Errorf("expected a second re-clone, got %v, %+v", healer.recloned, failure)
	}
}
