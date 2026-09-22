package application_test

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain"
)

// fakeVault is a vault held in memory: the seats it was given, and whatever a
// run wrote into it.
type fakeVault struct {
	seats   map[string]application.Seat // "<seat>/<rig>" -> what it reads as
	written map[string]string           // "<story>/<file>" -> its contents
	ledger  []string                    // the lines appended to a seat's ledger
	// memorySizes is what RigMemorySizes reports for any seat.
	memorySizes []application.RigMemorySize
	err         error
}

func newFakeVault() *fakeVault {
	return &fakeVault{
		seats: map[string]application.Seat{
			"builder/millwright": {
				Name: "builder", Rig: "millwright",
				Charter: "the builder's charter", Memory: "what the builder knows about millwright",
			},
			"builder/fellowship": {Name: "builder", Rig: "fellowship", Charter: "the builder's charter"},
		},
		written: map[string]string{},
	}
}

func (f *fakeVault) Seat(_ context.Context, seat, rig string) (application.Seat, error) {
	if f.err != nil {
		return application.Seat{}, f.err
	}
	read, ok := f.seats[seat+"/"+rig]
	if !ok {
		return application.Seat{}, fmt.Errorf("no %s seat", seat)
	}
	return read, nil
}

func (f *fakeVault) PutRunFile(_ context.Context, storyID, name, contents string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	f.written[storyID+"/"+name] = contents
	return "/vault/runs/" + storyID + "/" + name, nil
}

func (f *fakeVault) ReadRunFile(_ context.Context, storyID, name string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	written, ok := f.written[storyID+"/"+name]
	if !ok {
		return "", fmt.Errorf("no %s of %s: %w", name, storyID, fs.ErrNotExist)
	}
	return written, nil
}

func (f *fakeVault) StatRunFile(_ context.Context, storyID, name string) (application.RunFileInfo, error) {
	if f.err != nil {
		return application.RunFileInfo{}, f.err
	}
	written, ok := f.written[storyID+"/"+name]
	if !ok {
		return application.RunFileInfo{}, fmt.Errorf("no %s of %s: %w", name, storyID, fs.ErrNotExist)
	}
	return application.RunFileInfo{Size: int64(len(written))}, nil
}

func (f *fakeVault) RunFile(storyID, name string) string {
	return "/vault/runs/" + storyID + "/" + name
}

func (f *fakeVault) Dir() string { return "/vault" }

func (f *fakeVault) AppendToLedger(_ context.Context, seat, line string) error {
	if f.err != nil {
		return f.err
	}
	f.ledger = append(f.ledger, seat+": "+line)
	return nil
}

func (f *fakeVault) RigMemorySizes(_ context.Context, _ string) ([]application.RigMemorySize, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.memorySizes, nil
}

func (f *fakeVault) ReadLedger(_ context.Context, seat string) ([]string, error) {
	if f.err != nil {
		return nil, f.err
	}
	var lines []string
	for _, held := range f.ledger {
		if strings.TrimPrefix(held, seat+": ") != held {
			lines = append(lines, strings.TrimPrefix(held, seat+": "))
		}
	}
	return lines, nil
}

// fakeHarness records the launch it was asked to turn into a session.
type fakeHarness struct {
	launch application.Launch
	err    error
}

func (h *fakeHarness) Name() domain.Harness { return domain.HarnessClaude }

func (h *fakeHarness) Session(l application.Launch) (application.SessionSpec, error) {
	h.launch = l
	if h.err != nil {
		return application.SessionSpec{}, h.err
	}
	return application.SessionSpec{
		Name:    application.SessionName(l.StoryID),
		Dir:     l.Dir,
		Command: []string{"/bin/sh", "-c", "claude"},
	}, nil
}

// aStory is the story these tests boot, with whatever a test changes applied.
func aStory(change func(*application.StoryDetail)) application.StoryDetail {
	detail := application.StoryDetail{
		Story: domain.Story{
			ID:    "mw-gq6.6",
			Title: "Seat boot: assemble a Builder session's priming",
			Overrides: domain.Path{
				Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude,
				Model: domain.ModelOpus, Effort: domain.EffortHigh,
				Formula: "tdd-feature", Host: "vps",
			},
		},
		Description: "Given a story and the vault, assemble the boot prompt file.",
		Acceptance:  "make test passes.",
	}
	if change != nil {
		change(&detail)
	}
	return detail
}

func aSeatBoot(v application.Vault, h application.Harness) application.SeatBoot {
	return application.SeatBoot{Vault: v, Harness: h, Seat: "builder", Host: "vps"}
}

func TestBootWritesTheBootFileAndHandsTheHarnessEverything(t *testing.T) {
	v, h := newFakeVault(), &fakeHarness{}

	spec, err := aSeatBoot(v, h).Boot(context.Background(), aStory(nil), "/root/.mw-worktrees/mw-gq6.6")
	if err != nil {
		t.Fatalf("booting the story: %v", err)
	}
	if spec.Name != "mw-gq6_6" {
		t.Errorf("expected the session to be named after the story, got %q", spec.Name)
	}

	written, ok := v.written["mw-gq6.6/"+application.BootFileName]
	if !ok {
		t.Fatalf("expected the boot file to be written, got %v", v.written)
	}
	if written != application.BootPrompt(v.seats["builder/millwright"], aStory(nil)) {
		t.Error("expected the boot file to hold the boot prompt")
	}

	l := h.launch
	switch {
	case l.StoryID != "mw-gq6.6":
		t.Errorf("expected the story, got %q", l.StoryID)
	case l.Seat != "builder" || l.Host != "vps":
		t.Errorf("expected builder on vps, got %q on %q", l.Seat, l.Host)
	case l.Dir != "/root/.mw-worktrees/mw-gq6.6":
		t.Errorf("expected the worktree, got %q", l.Dir)
	case l.BootFile != "/vault/runs/mw-gq6.6/"+application.BootFileName:
		t.Errorf("expected the boot file that was written, got %q", l.BootFile)
	case l.ResultFile != "/vault/runs/mw-gq6.6/"+application.ResultFileName:
		t.Errorf("expected the result beside it, got %q", l.ResultFile)
	case l.Kickoff != application.KickoffPrompt("builder", "mw-gq6.6", "/vault"):
		t.Errorf("expected the kickoff prompt, got %q", l.Kickoff)
	case l.Path.Model != domain.ModelOpus:
		t.Errorf("expected the story's model, got %q", l.Path.Model)
	}
}

func TestBootRefusesWhatItCannotBoot(t *testing.T) {
	cases := map[string]struct {
		boot   func() application.SeatBoot
		detail application.StoryDetail
	}{
		"a story with no path": {
			func() application.SeatBoot { return aSeatBoot(newFakeVault(), &fakeHarness{}) },
			aStory(func(d *application.StoryDetail) { d.Story.Overrides.Rig = "" }),
		},
		"a story for another host": {
			func() application.SeatBoot { return aSeatBoot(newFakeVault(), &fakeHarness{}) },
			aStory(func(d *application.StoryDetail) { d.Story.Overrides.Host = "laptop" }),
		},
		"a story for another harness": {
			func() application.SeatBoot { return aSeatBoot(newFakeVault(), &fakeHarness{}) },
			aStory(func(d *application.StoryDetail) { d.Story.Overrides.Harness = domain.Harness("herdr") }),
		},
		"no seat named": {
			func() application.SeatBoot {
				b := aSeatBoot(newFakeVault(), &fakeHarness{})
				b.Seat = ""
				return b
			},
			aStory(nil),
		},
		"a seat the vault does not hold": {
			func() application.SeatBoot {
				b := aSeatBoot(newFakeVault(), &fakeHarness{})
				b.Seat = "nobody"
				return b
			},
			aStory(nil),
		},
	}
	for name, c := range cases {
		if _, err := c.boot().Boot(context.Background(), c.detail, "/worktree"); err == nil {
			t.Errorf("expected %s to be refused", name)
		}
	}
}

func TestBootSaysWhichStoryItFailedOn(t *testing.T) {
	v := newFakeVault()
	v.err = errors.New("the vault is not there")

	_, err := aSeatBoot(v, &fakeHarness{}).Boot(context.Background(), aStory(nil), "/worktree")
	if err == nil {
		t.Fatal("expected a vault that cannot be read to fail the boot")
	}
	if !strings.Contains(err.Error(), "mw-gq6.6") || !strings.Contains(err.Error(), "the vault is not there") {
		t.Errorf("expected the story and the reason, got %q", err)
	}
}

func TestBootPrimesTheSeatThenTheRigThenTheStory(t *testing.T) {
	seat := application.Seat{
		Name: "builder", Rig: "millwright",
		Charter: "CHARTER", Memory: "RIG MEMORY",
	}
	prompt := application.BootPrompt(seat, aStory(nil))

	at := 0
	for _, want := range []string{"CHARTER", "RIG MEMORY", "mw-gq6.6", "Seat boot: assemble", "make test passes."} {
		found := strings.Index(prompt[at:], want)
		if found < 0 {
			t.Fatalf("expected %q after what comes before it, got:\n%s", want, prompt)
		}
		at += found + len(want)
	}
	for _, want := range []string{"- rig: millwright", "- model: opus", "- effort: high", "- formula: tdd-feature"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("expected the boot prompt to hold the path line %q", want)
		}
	}
}

func TestBootPromptWithoutARigMemoryHoldsTheRest(t *testing.T) {
	seat := application.Seat{Name: "builder", Rig: "fellowship", Charter: "CHARTER"}
	prompt := application.BootPrompt(seat, aStory(nil))

	if !strings.Contains(prompt, "CHARTER") || !strings.Contains(prompt, "make test passes.") {
		t.Errorf("expected the charter and the story anyway, got:\n%s", prompt)
	}
	if strings.Contains(prompt, "memory of the rig") {
		t.Errorf("expected no memory section at all, got:\n%s", prompt)
	}
}

func TestBootPromptSaysWhenAStoryHasNoAcceptanceCriteria(t *testing.T) {
	prompt := application.BootPrompt(
		application.Seat{Name: "builder", Charter: "CHARTER"},
		aStory(func(d *application.StoryDetail) { d.Acceptance = "" }),
	)
	if !strings.Contains(prompt, "## Acceptance criteria") || !strings.Contains(prompt, "none recorded") {
		t.Errorf("expected the boot prompt to say there are none, got:\n%s", prompt)
	}
}

func TestSeatIdentityIsTheSeatOnItsHost(t *testing.T) {
	if got := application.SeatIdentity("builder", "vps"); got != "builder@vps" {
		t.Errorf("expected builder@vps, got %q", got)
	}
	if got := application.SeatIdentity("builder", ""); got != "builder" {
		t.Errorf("expected a seat with no host to be just the seat, got %q", got)
	}
}

func TestBootChainsTheCloseOutOntoTheSessionWithTheStoryAfterIt(t *testing.T) {
	v, h := newFakeVault(), &fakeHarness{}
	boot := aSeatBoot(v, h)
	boot.After = []string{"/root/millwright/bin/mw", "next"}

	if _, err := boot.Boot(context.Background(), aStory(nil), "/worktree"); err != nil {
		t.Fatalf("booting the story: %v", err)
	}
	after := h.launch.After
	if len(after) != 3 || after[0] != "/root/millwright/bin/mw" || after[1] != "next" || after[2] != "mw-gq6.6" {
		t.Errorf("expected the close-out of this story to be chained on, got %q", after)
	}

	// A seat told to run nothing afterwards chains nothing: the story ends when
	// the session does.
	h.launch = application.Launch{}
	if _, err := aSeatBoot(v, h).Boot(context.Background(), aStory(nil), "/worktree"); err != nil {
		t.Fatalf("booting the story: %v", err)
	}
	if len(h.launch.After) != 0 {
		t.Errorf("expected nothing chained on, got %q", h.launch.After)
	}
}

func TestKickoffPromptGivesTheLiteralBdCommandForTheVault(t *testing.T) {
	prompt := application.KickoffPrompt("builder", "mw-gq6.6", "/home/jwhite/100%-vault")

	for _, want := range []string{
		"bd -C /home/jwhite/100%-vault close <step>",
		"bd -C /home/jwhite/100%-vault <subcommand>",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("expected the kickoff prompt to hold %q, got %q", want, prompt)
		}
	}
	if strings.Contains(prompt, "%!") {
		t.Errorf("expected the vault's path to be written as it is, got %q", prompt)
	}
	if strings.Contains(prompt, "cd ") {
		t.Errorf("expected the kickoff prompt to say nothing of a change of directory, got %q", prompt)
	}
}

func TestKickoffPromptWithNoVaultPathSaysNothingOfBdC(t *testing.T) {
	if prompt := application.KickoffPrompt("builder", "mw-gq6.6", ""); strings.Contains(prompt, "bd -C") {
		t.Errorf("expected no bd -C without a vault path, got %q", prompt)
	}
}

func TestRebaseKickoffPromptSaysWhatToRebaseOntoAndNothingMore(t *testing.T) {
	prompt := application.RebaseKickoffPrompt("builder", "mw-gq6.50", "/home/jwhite/vault", "origin/main")

	for _, want := range []string{
		"mw-gq6.50",
		"git rebase origin/main",
		"bd -C /home/jwhite/vault close <step>",
		"mw check mw-gq6.50",
		"Do not push",
		"Sign nothing",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("expected the rebase kickoff to hold %q, got %q", want, prompt)
		}
	}
	if strings.Contains(prompt, "%!") {
		t.Errorf("expected every value written as it is, got %q", prompt)
	}
}
