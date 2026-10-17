package application_test

import (
	"context"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// dirCheckouts is one fake checkout for each rig directory.
type dirCheckouts map[string]*fakeCheckout

func (d dirCheckouts) Fetch(ctx context.Context, dir string) error { return d[dir].Fetch(ctx, dir) }
func (d dirCheckouts) Tip(ctx context.Context, dir, remote, branch string) (string, error) {
	return d[dir].Tip(ctx, dir, remote, branch)
}
func (d dirCheckouts) Head(ctx context.Context, dir string) (string, error) {
	return d[dir].Head(ctx, dir)
}
func (d dirCheckouts) Uncommitted(ctx context.Context, dir string) ([]string, error) {
	return d[dir].Uncommitted(ctx, dir)
}
func (d dirCheckouts) Advance(ctx context.Context, dir, branch, commit string) (application.Advanced, error) {
	return d[dir].Advance(ctx, dir, branch, commit)
}

// rigCommands is the host's [after_landing] table, counting the runs of each.
type rigCommands struct {
	commands map[string]string
	ran      application.Ran
	runs     map[string]int
}

func (r *rigCommands) Command(rig string) string { return r.commands[rig] }
func (r *rigCommands) Run(_ context.Context, rig, _ string) (application.Ran, error) {
	if r.runs == nil {
		r.runs = map[string]int{}
	}
	r.runs[rig]++
	ran := r.ran
	ran.Command = r.commands[rig]
	return ran, nil
}

const desktopHome = "desktop 2026-10-01T05:00:00Z mayor\n"

// aHomeWithPostern is a host that is home, with the factory rig named no command
// and postern checked out one commit behind origin's main.
func aHomeWithPostern(t *testing.T, host string) (application.SelfUpdate, *fakeCheckout, *rigCommands, fakeBuilt) {
	t.Helper()
	dir := t.TempDir()
	postern := &fakeCheckout{head: "027f977aaaaaaaa", tip: "bfd385cbbbbbbbb"}
	after := &rigCommands{commands: map[string]string{"postern": "make deploy"}}
	built := fakeBuilt{}
	return application.SelfUpdate{
		Rigs:     map[string]string{"millwright": "/rigs/millwright", "postern": dir},
		Checkout: dirCheckouts{dir: postern},
		After:    after,
		Built:    built,
		Home:     &apptest.FakeHomeFile{Text: desktopHome},
		Host:     host,
	}, postern, after, built
}

// mw-gq6.562: an app story landed on a cloud box was never deployed; the home
// fast-forwards a rig whose [after_landing] it names and runs the command.
func TestTheHomeDeploysARigItsCheckoutIsBehindOnceAndOnlyOnce(t *testing.T) {
	update, postern, after, _ := aHomeWithPostern(t, "desktop")

	notes := update.Run(context.Background())
	joined := strings.Join(notes, "\n")
	if after.runs["postern"] != 1 || postern.head != "bfd385cbbbbbbbb" {
		t.Fatalf("expected postern fast-forwarded and its command run once, ran %d, head %s; notes %q", after.runs["postern"], postern.head, joined)
	}
	if want := "self-update: postern 027f977 → bfd385c, deployed"; !strings.Contains(joined, want) {
		t.Errorf("expected %q in %q", want, joined)
	}

	if again := update.Run(context.Background()); len(again) != 0 || after.runs["postern"] != 1 {
		t.Errorf("expected a second tick with nothing new to say and run nothing, got %q after %d runs", again, after.runs["postern"])
	}
}

func TestTheHomeLeavesADirtyRigCheckoutAloneAndSaysSoOnce(t *testing.T) {
	update, postern, after, _ := aHomeWithPostern(t, "desktop")
	postern.left = "it has 1 uncommitted path(s): notes.md"

	notes := update.Run(context.Background())
	if joined := strings.Join(notes, "\n"); !strings.Contains(joined, "self-update: postern checkout left alone: it has 1 uncommitted path(s): notes.md") {
		t.Fatalf("expected the checkout said left alone, got %q", joined)
	}
	if after.runs["postern"] != 0 || postern.head != "027f977aaaaaaaa" {
		t.Errorf("expected the checkout untouched, ran %d, head %s", after.runs["postern"], postern.head)
	}
	if again := update.Run(context.Background()); len(again) != 0 {
		t.Errorf("expected the second tick not to say it again, got %q", again)
	}

	// Cleaned, then dirtied again: that is news once more.
	postern.left = ""
	postern.tip = "027f977aaaaaaaa"
	update.Run(context.Background())
	postern.left = "it has 1 uncommitted path(s): notes.md"
	postern.tip = "bfd385cbbbbbbbb"
	if later := update.Run(context.Background()); len(later) != 1 {
		t.Errorf("expected a checkout dirtied again said again, got %q", later)
	}
}

func TestADeployThatFailsIsSaidWithHowToRunItByHand(t *testing.T) {
	update, _, after, _ := aHomeWithPostern(t, "desktop")
	after.ran = application.Ran{Status: 2, Output: "rsync: connection unexpectedly closed\n"}

	notes := strings.Join(update.Run(context.Background()), "\n")
	for _, want := range []string{"self-update: postern 027f977 → bfd385c", "deploy failed", "mw after-landing postern"} {
		if !strings.Contains(notes, want) {
			t.Errorf("expected %q in %q", want, notes)
		}
	}
}

func TestAHostThatIsNotHomeDeploysNoOtherRig(t *testing.T) {
	update, postern, after, _ := aHomeWithPostern(t, "laptop")

	if notes := update.Run(context.Background()); len(notes) != 0 || after.runs["postern"] != 0 || postern.head != "027f977aaaaaaaa" {
		t.Fatalf("expected a host that is not home to leave postern alone, got %q, ran %d, head %s", notes, after.runs["postern"], postern.head)
	}
}

// mw-gq6.562: the dry tick shows each [after_landing] rig and whether it is
// level with origin, as the last fetch saw it, and moves nothing.
func TestLookSaysWhichAfterLandingRigsAreLevelWithOrigin(t *testing.T) {
	update, postern, after, _ := aHomeWithPostern(t, "laptop")

	notes := strings.Join(update.Look(context.Background()), "\n")
	for _, want := range []string{"postern", "behind origin's main", "027f977", "bfd385c", "not home"} {
		if !strings.Contains(notes, want) {
			t.Errorf("expected %q in %q", want, notes)
		}
	}
	if postern.fetched != 0 || postern.head != "027f977aaaaaaaa" || after.runs["postern"] != 0 {
		t.Errorf("expected the look to touch nothing, fetched %d, head %s, ran %d", postern.fetched, postern.head, after.runs["postern"])
	}

	postern.head = postern.tip
	if level := strings.Join(update.Look(context.Background()), "\n"); !strings.Contains(level, "postern is level with origin's main at bfd385c") {
		t.Errorf("expected a level rig said level, got %q", level)
	}
}
