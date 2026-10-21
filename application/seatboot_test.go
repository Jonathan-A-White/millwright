package application_test

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	case l.Kickoff != application.KickoffPrompt("builder", "mw-gq6.6", "/vault", &application.RigMemoryAccess{Rig: "millwright"}):
		t.Errorf("expected the kickoff prompt, got %q", l.Kickoff)
	case l.Path.Model != domain.ModelOpus:
		t.Errorf("expected the story's model, got %q", l.Path.Model)
	}
}

// A Tester story (mw-it6qk5.5) commits nothing, so its session is never told
// to commit before it stops, as every other session is.
func TestBootTellsATesterToCommitNothing(t *testing.T) {
	v, h := newFakeVault(), &fakeHarness{}
	tester := aStory(func(d *application.StoryDetail) {
		d.Story.Overrides.Formula = application.TesterFormula
		d.Labels = []string{application.LabelTester}
	})
	if _, err := aSeatBoot(v, h).Boot(context.Background(), tester, "/root/.mw-worktrees/mw-gq6.6"); err != nil {
		t.Fatalf("booting the Tester story: %v", err)
	}
	kickoff := h.launch.Kickoff
	if kickoff != application.TesterKickoffPrompt("builder", "mw-gq6.6", "/vault") {
		t.Errorf("expected the Tester kickoff, got %q", kickoff)
	}
	for _, want := range []string{"commit nothing", "FINDINGS", "mw check mw-gq6.6", "Do not push"} {
		if !strings.Contains(kickoff, want) {
			t.Errorf("expected the Tester kickoff to say %q, got %q", want, kickoff)
		}
	}
	for _, never := range []string{"commit before you stop", "Once your work is committed"} {
		if strings.Contains(kickoff, never) {
			t.Errorf("expected the Tester kickoff never to say %q, got %q", never, kickoff)
		}
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

func TestBootChainsTheHeartbeatOntoTheSessionWithTheStoryAfterIt(t *testing.T) {
	v, h := newFakeVault(), &fakeHarness{}
	boot := aSeatBoot(v, h)
	boot.Heartbeat = []string{"/root/millwright/bin/mw", "next", "--heartbeat"}

	if _, err := boot.Boot(context.Background(), aStory(nil), "/worktree"); err != nil {
		t.Fatalf("booting the story: %v", err)
	}
	beat := h.launch.Heartbeat
	if len(beat) != 4 || beat[0] != "/root/millwright/bin/mw" || beat[1] != "next" || beat[2] != "--heartbeat" || beat[3] != "mw-gq6.6" {
		t.Errorf("expected the heartbeat of this story to be chained on, got %q", beat)
	}

	// A seat told to heartbeat nothing starts nothing beside the harness.
	h.launch = application.Launch{}
	if _, err := aSeatBoot(v, h).Boot(context.Background(), aStory(nil), "/worktree"); err != nil {
		t.Fatalf("booting the story: %v", err)
	}
	if len(h.launch.Heartbeat) != 0 {
		t.Errorf("expected nothing heartbeated, got %q", h.launch.Heartbeat)
	}
}

func TestKickoffPromptGivesTheLiteralBdCommandForTheVault(t *testing.T) {
	prompt := application.KickoffPrompt("builder", "mw-gq6.6", "/home/jwhite/100%-vault", nil)

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
	if prompt := application.KickoffPrompt("builder", "mw-gq6.6", "", nil); strings.Contains(prompt, "bd -C") {
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

// A bug's Builder is asked for the class of its fault, beside its rig memory
// proposals (mw-gq6.420); a story that is not a bug is not.
func TestBootAsksABugsBuilderForItsCommonError(t *testing.T) {
	v, h := newFakeVault(), &fakeHarness{}
	bug := aStory(func(d *application.StoryDetail) { d.Story.Title = "[bug] the gloss is the neighbour's" })
	if _, err := aSeatBoot(v, h).Boot(context.Background(), bug, "/worktree"); err != nil {
		t.Fatalf("booting the bug: %v", err)
	}
	if !strings.Contains(h.launch.Kickoff, application.CommonErrorsMarker) || !strings.Contains(h.launch.Kickoff, "mw errors list") {
		t.Errorf("a bug's kickoff should ask for %q, got %q", application.CommonErrorsMarker, h.launch.Kickoff)
	}

	if _, err := aSeatBoot(v, h).Boot(context.Background(), aStory(nil), "/worktree"); err != nil {
		t.Fatalf("booting the story: %v", err)
	}
	if strings.Contains(h.launch.Kickoff, application.CommonErrorsMarker) {
		t.Errorf("a story that is not a bug is not asked, got %q", h.launch.Kickoff)
	}
}

// A Builder's story session has no run_in_background (mw-gq6.90), and a Builder
// that polled its own mw check log with `sleep 570` lost minutes per check
// (mw-gq6.425): every Builder kickoff says how to wait on a long command, and
// says never to sleep-poll.
func TestEveryBuilderKickoffTeachesTheBackgroundAndTailWait(t *testing.T) {
	kickoffs := map[string]string{
		"story":    application.KickoffPrompt("builder", "mw-gq6.6", "/home/jwhite/vault", nil),
		"rebase":   application.RebaseKickoffPrompt("builder", "mw-gq6.6", "/home/jwhite/vault", "origin/main"),
		"mergefix": application.MergeFixKickoffPrompt("builder", "mw-gq6.6", "/home/jwhite/vault", "origin/main"),
	}
	for name, prompt := range kickoffs {
		for _, want := range []string{
			"no run_in_background",
			"nohup <cmd> > /tmp/<name>-<story>.log 2>&1 & echo $!",
			"timeout 550 tail --pid=<pid> -f /dev/null",
			"until the pid is gone",
			"never a fixed `sleep N`",
			"never end the turn while it runs",
		} {
			if !strings.Contains(prompt, want) {
				t.Errorf("the %s kickoff should hold %q, got %q", name, want, prompt)
			}
		}
		if strings.Contains(prompt, "never in the background") {
			t.Errorf("the %s kickoff still says never in the background: %q", name, prompt)
		}
		if strings.Contains(prompt, "%!") {
			t.Errorf("the %s kickoff has a format slip: %q", name, prompt)
		}
	}
}

// The wait form the Builders are taught really returns when the command ends,
// and really gives up its turn at the limit.
func TestTheTailWaitReturnsWhenTheCommandEndsAndAtItsLimit(t *testing.T) {
	run := func(t *testing.T, wait, command string) time.Duration {
		t.Helper()
		log := filepath.Join(t.TempDir(), "long.log")
		script := fmt.Sprintf("nohup %s > %s 2>&1 & pid=$!; %s; kill $pid 2>/dev/null; true",
			command, log, strings.ReplaceAll(wait, "<pid>", "$pid"))
		start := time.Now()
		if out, err := exec.Command("sh", "-c", script).CombinedOutput(); err != nil {
			t.Fatalf("running the wait form: %v: %s", err, out)
		}
		return time.Since(start)
	}

	if elapsed := run(t, application.LongCommandWait, "sleep 3"); elapsed < 3*time.Second || elapsed > 8*time.Second {
		t.Errorf("a 3-second command should be waited on for 3 to 8 seconds (ending +5 s), took %v", elapsed)
	}

	// 550 s cannot be waited out in a test: the same form with a 2 s limit shows
	// the limit ends the wait while the command runs on.
	short := strings.Replace(application.LongCommandWait, "timeout 550", "timeout 2", 1)
	if short == application.LongCommandWait {
		t.Fatalf("the wait form should start with timeout 550, got %q", application.LongCommandWait)
	}
	if elapsed := run(t, short, "sleep 600"); elapsed < 2*time.Second || elapsed > 5*time.Second {
		t.Errorf("a 600-second command should be left after the limit (2 s here), took %v", elapsed)
	}
}

// A Builder's session is capped (mw-1ljpo9.6): at the host's next.max_turns, or
// the story's own max_turns, and it is told at 80% of them to commit what passes
// and write its closing comment. 0 is no cap and says nothing of one.
func TestBootCapsTheSessionAndToldItToWrapUpAt80Percent(t *testing.T) {
	for _, tc := range []struct {
		name     string
		host     int
		path     string
		turns    int
		wrapUpAt string
	}{
		{"the host's cap", 120, "", 120, "by turn 96 "},
		{"the story's own cap", 120, "60", 60, "by turn 48 "},
		{"a story's own 0 is no cap", 120, "0", 0, ""},
		{"a host's 0 is no cap", 0, "", 0, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v, h := newFakeVault(), &fakeHarness{}
			boot := aSeatBoot(v, h)
			boot.MaxTurns = tc.host
			story := aStory(func(d *application.StoryDetail) { d.Story.Overrides.MaxTurns = tc.path })
			if _, err := boot.Boot(context.Background(), story, "/root/.mw-worktrees/mw-gq6.6"); err != nil {
				t.Fatalf("booting the story: %v", err)
			}
			if h.launch.MaxTurns != tc.turns {
				t.Fatalf("expected the session capped at %d turns, got %d", tc.turns, h.launch.MaxTurns)
			}
			kickoff := h.launch.Kickoff
			if tc.turns == 0 {
				if kickoff != application.KickoffPrompt("builder", "mw-gq6.6", "/vault", &application.RigMemoryAccess{Rig: "millwright"}) || strings.Contains(kickoff, "capped") {
					t.Fatalf("expected an uncapped session's kickoff to say nothing of a cap, got %q", kickoff)
				}
				return
			}
			for _, want := range []string{
				fmt.Sprintf("capped at %d turns", tc.turns), tc.wrapUpAt,
				"commit what passes", "closing comment", "given back",
			} {
				if !strings.Contains(kickoff, want) {
					t.Errorf("expected the kickoff to say %q, got %q", want, kickoff)
				}
			}
		})
	}
}

// A Tester commits nothing, so its cap tells it to write its FINDINGS instead.
func TestBootTellsACappedTesterToWriteItsFindingsNotToCommit(t *testing.T) {
	v, h := newFakeVault(), &fakeHarness{}
	boot := aSeatBoot(v, h)
	boot.MaxTurns = 120
	tester := aStory(func(d *application.StoryDetail) {
		d.Story.Overrides.Formula = application.TesterFormula
		d.Labels = []string{application.LabelTester}
	})
	if _, err := boot.Boot(context.Background(), tester, "/root/.mw-worktrees/mw-gq6.6"); err != nil {
		t.Fatalf("booting the Tester story: %v", err)
	}
	kickoff := h.launch.Kickoff
	if !strings.Contains(kickoff, "capped at 120 turns: by turn 96 close the steps you finished and write your FINDINGS") {
		t.Errorf("expected the Tester to be told to write its FINDINGS by turn 96, got %q", kickoff)
	}
	if strings.Contains(kickoff, "commit what passes") {
		t.Errorf("expected a Tester never to be told to commit, got %q", kickoff)
	}
}

func TestKickoffPromptNamesMemoryQueryOnlyOnAFactsRigAndWithTheRigsOwnName(t *testing.T) {
	facts := application.KickoffPrompt("builder", "mw-gq6.6", "/vault", &application.RigMemoryAccess{Rig: "lampas", HasFacts: true})
	if !strings.Contains(facts, "mw memory query lampas <term>") {
		t.Errorf("expected a facts rig's kickoff to name mw memory query with the rig's own name, got %q", facts)
	}
	if strings.Contains(facts, "<rig>") {
		t.Errorf("expected no placeholder rig in a facts rig's kickoff, got %q", facts)
	}

	flat := application.KickoffPrompt("builder", "mw-gq6.6", "/vault", &application.RigMemoryAccess{Rig: "postern"})
	if strings.Contains(flat, "mw memory query") {
		t.Errorf("expected a flat-file rig's kickoff to name no mw memory query, got %q", flat)
	}
	if !strings.Contains(flat, "whole rig memory is in the boot file") {
		t.Errorf("expected a flat-file rig's kickoff to say the whole rig memory is in the boot file, got %q", flat)
	}
}

func TestKickoffPromptWithNoAnswerAboutTheRigKeepsTheGenericQueryLine(t *testing.T) {
	prompt := application.KickoffPrompt("builder", "mw-gq6.6", "/vault", nil)
	if !strings.Contains(prompt, "run mw memory query <rig> <term>") {
		t.Errorf("expected the generic line when the rig is not known, got %q", prompt)
	}
}

func TestSeatBootNamesMemoryQueryOnlyForTheFactsRigItBootsInto(t *testing.T) {
	for _, tc := range []struct {
		name     string
		hasFacts bool
		want     string
		notWant  string
	}{
		{"facts rig", true, "mw memory query millwright <term>", ""},
		{"flat-file rig", false, "", "mw memory query"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v, h := newFakeVault(), &fakeHarness{}
			seat := v.seats["builder/millwright"]
			seat.HasFacts = tc.hasFacts
			v.seats["builder/millwright"] = seat
			if _, err := aSeatBoot(v, h).Boot(context.Background(), aStory(nil), "/wt"); err != nil {
				t.Fatal(err)
			}
			kickoff := h.launch.Kickoff
			if tc.want != "" && !strings.Contains(kickoff, tc.want) {
				t.Errorf("expected %q in %q", tc.want, kickoff)
			}
			if tc.notWant != "" && strings.Contains(kickoff, tc.notWant) {
				t.Errorf("expected no %q in %q", tc.notWant, kickoff)
			}
		})
	}
}

// registerWithKinds is a common-errors register held in memory: class files by
// slug and the text of kinds.toml.
type registerWithKinds struct {
	classes map[string]string
	kinds   string
}

func (r registerWithKinds) ReadErrorRegister(context.Context, string) (map[string]string, string, error) {
	return r.classes, r.kinds, nil
}
func (r registerWithKinds) WriteErrorClass(context.Context, string, string, string) (string, error) {
	return "", nil
}
func (r registerWithKinds) WriteErrorKinds(context.Context, string, string) (string, error) {
	return "", nil
}

func aClassFile(slug, symptom, since string, kinds ...string) string {
	return application.ErrorClass{Slug: slug, Symptom: symptom, Kinds: kinds, Since: since}.String()
}

func bootABug(t *testing.T, register application.ErrorFiles) string {
	t.Helper()
	h := &fakeHarness{}
	boot := aSeatBoot(newFakeVault(), h)
	boot.Errors = register
	bug := aStory(func(d *application.StoryDetail) { d.Story.Title = "[bug] the gloss is the neighbour's" })
	if _, err := boot.Boot(context.Background(), bug, "/worktree"); err != nil {
		t.Fatalf("booting the bug: %v", err)
	}
	return h.launch.Kickoff
}

// A bug's Builder is shown the classes of the register whose kinds meet its
// rig's, one slug: symptom line each, so that it names one (mw-gq6.683).
func TestBootShowsABugsBuilderTheClassesOfItsRigsKinds(t *testing.T) {
	kickoff := bootABug(t, registerWithKinds{
		classes: map[string]string{
			"go-class":   aClassFile("go-class", "a go-cli fault", "2026-10-01", "go-cli"),
			"both-class": aClassFile("both-class", "a fault of both", "2026-10-02", "pwa", "go-cli"),
			"pwa-class":  aClassFile("pwa-class", "a pwa-only fault", "2026-10-03", "pwa"),
		},
		kinds: "millwright = [\"go-cli\"]\nlegend = [\"pwa\"]\n",
	})
	i := strings.Index(kickoff, application.CommonErrorsAsk)
	if i < 0 {
		t.Fatalf("expected the ask in the kickoff, got %q", kickoff)
	}
	after := kickoff[i+len(application.CommonErrorsAsk):]
	for _, want := range []string{application.CommonErrorsHeld, "go-class: a go-cli fault", "both-class: a fault of both"} {
		if !strings.Contains(after, want) {
			t.Errorf("expected %q after the ask, got %q", want, after)
		}
	}
	if strings.Contains(kickoff, "pwa-class") {
		t.Errorf("a class of other kinds only should not be listed, got %q", kickoff)
	}
}

// A rig with no kinds, or an empty register, gets the ask as it was.
func TestBootAddsNoClassListWithoutKindsOrClasses(t *testing.T) {
	for name, register := range map[string]application.ErrorFiles{
		"no kinds for the rig": registerWithKinds{
			classes: map[string]string{"pwa-class": aClassFile("pwa-class", "x", "2026-10-03", "pwa")},
			kinds:   "legend = [\"pwa\"]\n",
		},
		"empty register": registerWithKinds{kinds: "millwright = [\"go-cli\"]\n"},
		"no register":    nil,
	} {
		kickoff := bootABug(t, register)
		if strings.Contains(kickoff, application.CommonErrorsHeld) || !strings.Contains(kickoff, application.CommonErrorsAsk) {
			t.Errorf("%s: expected the ask and no list, got %q", name, kickoff)
		}
	}
	// Exactly as before: the kickoff ends with the ask.
	if kickoff := bootABug(t, nil); !strings.HasSuffix(strings.TrimSpace(kickoff), strings.TrimSpace(application.CommonErrorsAsk)) {
		t.Errorf("expected the kickoff to end with the ask, got %q", kickoff)
	}
}

// A long register is cut near 3000 bytes, the classes most recently found
// first, and ends with the count left out and where to read them.
func TestBootCapsTheClassListAndSaysHowManyMore(t *testing.T) {
	classes := map[string]string{}
	for i := 0; i < 60; i++ {
		slug := fmt.Sprintf("class-%02d", i)
		classes[slug] = aClassFile(slug, strings.Repeat("a long symptom ", 20), fmt.Sprintf("2026-09-%02d", i%28+1), "go-cli")
	}
	// The newest by far: it must be kept.
	classes["the-newest"] = aClassFile("the-newest", "found today", "2026-10-21", "go-cli")
	kickoff := bootABug(t, registerWithKinds{classes: classes, kinds: "millwright = [\"go-cli\"]\n"})
	list := kickoff[strings.Index(kickoff, application.CommonErrorsHeld):]
	if len(list) > 3300 {
		t.Errorf("expected the list near 3000 bytes, got %d", len(list))
	}
	if !strings.Contains(list, "the-newest: found today") {
		t.Errorf("expected the newest class first, got %q", list)
	}
	lines := strings.Split(strings.TrimSpace(list), " | ")
	last := lines[len(lines)-1]
	if !strings.HasPrefix(last, "... and ") || !strings.HasSuffix(last, " more: mw errors list") {
		t.Errorf("expected a closing '... and N more: mw errors list', got %q", last)
	}
	for _, line := range lines {
		if strings.Contains(line, "a long symptom") && len(line) > len("class-00: ")+145 {
			t.Errorf("expected a symptom cut to about 140 characters, got %d: %q", len(line), line)
		}
	}
}
