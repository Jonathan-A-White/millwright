package application

import (
	"context"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/domain"
)

// The files a story leaves in its own directory under the vault's runs/, side
// by side: what its session was booted with, what it reported back, and — when
// a landing failed — the whole of what git said, which the ledger's one line
// cannot hold.
const (
	BootFileName         = "boot.md"
	ResultFileName       = "result.json"
	LandingErrorFileName = "landing-error.txt"
)

// Seat is the part of a seat a session is primed with at boot: the charter,
// always, and the seat's memory of the one rig being worked, when the seat has
// one. Nothing else the seat holds — ledger, postmortems, memories of other
// rigs — is read at boot: a fresh session pays for every line of it, and what
// is history must cost nothing until someone asks (ADR 0003).
type Seat struct {
	Name    string
	Charter string
	Rig     string
	// Memory is the seat's memory of Rig, empty when the seat has none.
	Memory string
}

// The vault's own layout, as far as anything outside the vault adapter needs to
// name it: the directory the seats are in, and the directory a seat keeps its
// memory of each rig in (ADR 0003). They are here rather than in the adapter
// because a close-out has to say, by path, which of the vault's files it may
// commit — and that is a decision, not a detail of the disk.
const (
	SeatsDir = "seats"
	RigsDir  = "rigs"
	// RunsDir is the directory a story's run is kept in, one subdirectory a story.
	RunsDir = "runs"
	// MemoryExt is what a seat's memory of one rig is written in.
	MemoryExt = ".md"
	// ArchiveSuffix ends the name of the file a seat's pruned memory of a rig is
	// moved to, before MemoryExt. It is never read at boot, so it is never fuel.
	ArchiveSuffix = "-archive"
)

// RigMemorySize is how large a seat's memory of one rig is, in bytes: what every
// session working that rig reads at boot, and so what is paid on every story.
type RigMemorySize struct {
	Rig   string
	Bytes int
}

// SeatWork is everything one story is allowed to have changed in the vault,
// by path from the vault's root: the seat's ledger, which mw itself appends the
// story's line to, and that seat's memory of the rig the story was worked in,
// which the Mayor writes from what Builders propose in their closing comments
// and a session never edits (a stray edit is committed all the same, so that it
// never blocks a sync). Nothing else in the vault is either of their business,
// and a close-out commits exactly these.
//
// A name that would reach outside the vault gives no paths at all, so that a
// bad seat or rig name commits nothing rather than something unintended. So
// does an empty seat; an empty rig only leaves the memory out, because a story
// with no rig has no memory to commit.
func SeatWork(seat, rig string) []string {
	if !safeVaultName(seat) {
		return nil
	}
	work := []string{path.Join(SeatsDir, seat, LedgerFileName)}
	if rig == "" || !safeVaultName(rig) {
		return work
	}
	return append(work, path.Join(SeatsDir, seat, RigsDir, rig+MemoryExt))
}

// RunRecord is the one file of a story's run a close-out commits, by path from
// the vault's root: the session's result, the only evidence behind the fuel a
// ledger line reports. The boot file beside it is left untracked — it is what
// the session was primed with, and the run's own host is the only place it is
// wanted. A story id that would reach outside the vault gives no path.
func RunRecord(storyID string) string {
	return RunRecordForAttempt(storyID, 1)
}

// RunRecordForAttempt is RunRecord for one of a story's attempts. A story's
// first attempt keeps the plain name RunRecord always gave — the one an
// earlier close-out may already have committed — and every attempt after it
// gets a name of its own, numbered by attempt, so that a re-dispatched story
// never writes over a path a run before it may have already committed.
func RunRecordForAttempt(storyID string, attempt int) string {
	return runFilePath(storyID, ResultFileNameForAttempt(attempt))
}

// BootFileNameForAttempt and ResultFileNameForAttempt are the names a story's
// boot file and run record are written under for one of its attempts: the
// plain BootFileName and ResultFileName for the first attempt, and a name
// numbered by attempt for every one after it.
func BootFileNameForAttempt(attempt int) string {
	return attemptRunFileName(BootFileName, attempt)
}

func ResultFileNameForAttempt(attempt int) string {
	return attemptRunFileName(ResultFileName, attempt)
}

// attemptRunFileName is the run file name base is written under for attempt.
// The first attempt of a story is not numbered at all, so that nothing about
// the common case — a story worked once — changes, and so that a run record or
// boot file an earlier version of mw already committed keeps the name mw next
// still knows to read.
func attemptRunFileName(base string, attempt int) string {
	if attempt <= 1 {
		return base
	}
	ext := path.Ext(base)
	return fmt.Sprintf("%s-%d%s", strings.TrimSuffix(base, ext), attempt, ext)
}

// LandingErrorRecord is the file a failed landing's whole error is kept in, by
// path from the vault's root, committed with the run record. A story id that
// would reach outside the vault gives no path.
func LandingErrorRecord(storyID string) string {
	return runFilePath(storyID, LandingErrorFileName)
}

func runFilePath(storyID, name string) string {
	if !safeVaultName(storyID) {
		return ""
	}
	return path.Join(RunsDir, storyID, name)
}

// safeVaultName reports whether a name can be put in a vault path as it is.
func safeVaultName(name string) bool {
	return name != "" && !strings.ContainsAny(name, `/\`) && !strings.Contains(name, "..")
}

// RunFileInfo is what StatRunFile reports about one file of a story's run,
// without reading its contents.
type RunFileInfo struct {
	Size    int64
	ModTime time.Time
}

// Vault is the port the factory reads seats from and writes a story's run
// into. One adapter is the vault directory on disk.
type Vault interface {
	// Seat reads a seat's charter and its memory of one rig. A seat with no
	// memory of that rig is not an error — the Seat comes back with an empty
	// Memory — but a seat with no charter is: there is nothing to boot into.
	Seat(ctx context.Context, seat, rig string) (Seat, error)

	// PutRunFile writes one file of a story's run and reports where it landed,
	// making the story's run directory if it is not there yet.
	PutRunFile(ctx context.Context, storyID, name, contents string) (string, error)

	// ReadRunFile reads one file of a story's run back — the result a session
	// left behind. A file that was never written comes back as an error
	// satisfying errors.Is(err, fs.ErrNotExist), because a session that wrote
	// nothing and a session that wrote a failure are not the same thing.
	ReadRunFile(ctx context.Context, storyID, name string) (string, error)

	// StatRunFile reports the size and last-modified time of one file of a
	// story's run, without reading it — what a close-out has left to say about
	// a result that is there but empty, when the content itself says nothing
	// (mw-gq6.89). A file that was never written comes back as an error
	// satisfying errors.Is(err, fs.ErrNotExist), the same rule ReadRunFile
	// follows.
	StatRunFile(ctx context.Context, storyID, name string) (RunFileInfo, error)

	// RunFile is where one file of a story's run belongs, whether or not
	// anything has been written to it.
	RunFile(storyID, name string) string

	// Dir is the directory the vault is held in on this host. It differs from
	// host to host, so it is asked of the vault rather than written anywhere a
	// session reads.
	Dir() string

	// AppendToLedger adds one line to the end of a seat's ledger, making the
	// ledger if the seat has none yet. It only ever appends: the file is opened
	// for append and never read, so that no version of mw can rewrite a line a
	// seat has already written.
	AppendToLedger(ctx context.Context, seat, line string) error

	// RigMemorySizes reads how large each of a seat's memories of a rig is, in
	// rig order. The archive a memory is pruned into (<rig>-archive.md) is not a
	// memory and is not counted. A seat with no memory of any rig comes back
	// with none and no error. Only sizes are read, never the memory itself.
	RigMemorySizes(ctx context.Context, seat string) ([]RigMemorySize, error)

	// ReadLedger reads every line of a seat's ledger back, in the order it
	// holds them. A seat with no ledger yet comes back with no lines and no
	// error: there is nothing written to sum fuel from. It is the only way the
	// factory reads a ledger, and it is read for two things — a report of what
	// a seat burned, and a close-out run again asking whether a story's line is
	// in it already. Nothing here is ever rewritten.
	ReadLedger(ctx context.Context, seat string) ([]string, error)
}

// SeatBoot assembles the session that works one story: it writes the boot file
// the session is primed with and asks the harness for the command line and
// environment that start it. It launches nothing and spends no fuel.
//
// Seat is which seat the session boots into and Host is the host it boots on;
// together they are the session's identity to beads and to mail.
type SeatBoot struct {
	Vault   Vault
	Harness Harness
	Seat    string
	Host    string

	// After is what runs when the session's harness exits: the program and the
	// arguments before the story's id, which is appended to them. It is how the
	// baton is carried on without the session having to know anything about
	// what comes after it — `mw next` closing the story out. An empty After
	// leaves the session ending with nothing after it.
	After []string

	// Heartbeat is what runs beside the session's harness, from the moment it
	// starts to the moment it exits: the program and the arguments before the
	// story's id, the same shape as After. It is how the story's claim's lease
	// is kept renewed for as long as the harness is really running — `mw next
	// --heartbeat`. An empty Heartbeat starts nothing beside the harness.
	Heartbeat []string
}

// Boot assembles the session that works detail in the worktree dir. What comes
// back is ready for a Runner to start.
func (b SeatBoot) Boot(ctx context.Context, detail StoryDetail, dir string) (SessionSpec, error) {
	return b.boot(ctx, detail, dir, func(vaultDir string) string {
		return KickoffPrompt(b.Seat, detail.Story.ID, vaultDir)
	})
}

// Rebase assembles a fresh session for a story whose branch would not merge
// into its target branch without conflicts: booted into the same seat, in the
// same worktree, with the same story, and told to rebase the branch onto onto —
// the target branch as the remote has it — rather than to work the story again.
func (b SeatBoot) Rebase(ctx context.Context, detail StoryDetail, dir, onto string) (SessionSpec, error) {
	return b.boot(ctx, detail, dir, func(vaultDir string) string {
		return RebaseKickoffPrompt(b.Seat, detail.Story.ID, vaultDir, onto)
	})
}

// MergeFix assembles a fresh session for a story whose branch merges into its
// target branch without conflicts but whose merged result fails the rig's
// tests: booted into the same seat, in the same worktree — which already has
// onto merged into it — and told to fix the failing tests rather than to work
// the story again.
func (b SeatBoot) MergeFix(ctx context.Context, detail StoryDetail, dir, onto string) (SessionSpec, error) {
	return b.boot(ctx, detail, dir, func(vaultDir string) string {
		return MergeFixKickoffPrompt(b.Seat, detail.Story.ID, vaultDir, onto)
	})
}

// boot is Boot with the first thing the session is told left to the caller,
// given the vault's directory on this host.
func (b SeatBoot) boot(ctx context.Context, detail StoryDetail, dir string, kickoff func(vaultDir string) string) (SessionSpec, error) {
	id := detail.Story.ID
	switch {
	case b.Vault == nil || b.Harness == nil:
		return SessionSpec{}, fmt.Errorf("booting %s: a seat boot needs a vault and a harness", id)
	case b.Seat == "":
		return SessionSpec{}, fmt.Errorf("booting %s: which seat is this session?", id)
	case id == "":
		return SessionSpec{}, fmt.Errorf("booting a seat: which story?")
	}

	path, err := detail.Path()
	if err != nil {
		return SessionSpec{}, fmt.Errorf("booting %s: %w", id, err)
	}
	if path.Host != "" && b.Host != "" && path.Host != b.Host {
		return SessionSpec{}, fmt.Errorf("booting %s: the story is worked on %s, this is %s", id, path.Host, b.Host)
	}
	if path.Harness != b.Harness.Name() {
		return SessionSpec{}, fmt.Errorf("booting %s: the story wants the %s harness, this is %s", id, path.Harness, b.Harness.Name())
	}

	seat, err := b.Vault.Seat(ctx, b.Seat, path.Rig)
	if err != nil {
		return SessionSpec{}, fmt.Errorf("booting %s: %w", id, err)
	}

	// The attempt this session is: the first for a story dispatched fresh, one
	// more for a story sent back to rebase or dispatched again after an earlier
	// attempt ended without closing the story out. It is what keeps this
	// session's boot file and result from ever landing on a path an earlier
	// attempt may already have committed to the vault (mw-gq6.87) — a
	// re-dispatch that reused the first attempt's names once truncated a
	// committed result out from under a sync in progress, and left the vault
	// refusing every dispatch and sync until somebody committed the empty file
	// by hand.
	attempt := detail.Attempts + 1
	bootFile, err := b.Vault.PutRunFile(ctx, id, BootFileNameForAttempt(attempt), BootPrompt(seat, detail))
	if err != nil {
		return SessionSpec{}, fmt.Errorf("booting %s: %w", id, err)
	}

	spec, err := b.Harness.Session(Launch{
		StoryID: id,
		Path:    path,
		Seat:    b.Seat,
		Host:    b.Host,
		Dir:     dir,
		// The result lands beside the boot file, in a directory writing the
		// boot file has already made: the session's own shell only redirects
		// into it.
		BootFile:   bootFile,
		ResultFile: b.Vault.RunFile(id, ResultFileNameForAttempt(attempt)),
		Kickoff:    kickoff(b.Vault.Dir()),
		After:      b.after(id),
		Heartbeat:  b.heartbeat(id),
	})
	if err != nil {
		return SessionSpec{}, fmt.Errorf("booting %s: %w", id, err)
	}
	if err := spec.Validate(); err != nil {
		return SessionSpec{}, fmt.Errorf("booting %s: %w", id, err)
	}
	return spec, nil
}

// after is the command that runs when this story's session exits: what the
// factory was told to run, with the story it worked after it.
func (b SeatBoot) after(storyID string) []string {
	if len(b.After) == 0 {
		return nil
	}
	return append(append([]string(nil), b.After...), storyID)
}

// heartbeat is the command that runs beside this story's session, from the
// moment its harness starts to the moment it exits: what the factory was told
// to run, with the story it is heartbeating after it.
func (b SeatBoot) heartbeat(storyID string) []string {
	if len(b.Heartbeat) == 0 {
		return nil
	}
	return append(append([]string(nil), b.Heartbeat...), storyID)
}

// MwSeat is the name mw itself acts under when it writes to the tracker: the
// claims `mw dispatch` makes, the closes and run states `mw next` writes, the
// comments either of them leaves. It is not the Mayor and it is not the
// Builder, because neither of them decided any of it — the machinery did — and
// a factory whose history cannot tell a seat's act from the tooling's act
// cannot be read afterwards. With SeatIdentity it reads as `mw@<host>`, the
// same shape as every other name in the factory, and it says plainly which
// machine wrote the line. A session's own writes stay signed by its seat: the
// harness still sets BEADS_ACTOR to <seat>@<host>.
const MwSeat = "mw"

// SeatIdentity is who a session is when it writes anything down: the seat it
// occupies and the host it runs on. Sessions are disposable and the seat is
// what persists, so it is the seat that signs the work, never the session.
func SeatIdentity(seat, host string) string {
	if host == "" {
		return seat
	}
	return seat + "@" + host
}

// KickoffPrompt is the first thing a session is told. It is short on purpose:
// who it is and what it is working are in the boot file it was primed with,
// and everything else it needs is in the story's own formula steps.
func KickoffPrompt(seat, storyID, vaultDir string) string {
	return fmt.Sprintf("You are booted into the %s seat of millwright, and your story is %s. "+
		"Your charter, your memory of this rig and the story itself are in the system prompt you were given. "+
		"Your worktree is the directory you are in: work only there. "+
		"Follow the story's formula steps in order, leave the build and the tests green, "+
		"and write one truthful closing comment on the story when you are done. "+
		// The Mayor places what he keeps of the notes; the session only proposes.
		"Your memory of this rig is read-only to you: propose notes under \"For the rig memory:\" in your closing comment. "+
		"Do not push, do not merge, do not close the story. "+
		// The harness is told the same thing by its settings (the claude
		// adapter's SessionSettings), and mw next refuses a branch that carries
		// one anyway; this is the third place, because a session that is told
		// plainly does not have to be refused.
		"Sign nothing you commit: no Co-Authored-By trailer, no Generated with line, "+
		"no AI attribution of any kind — the seat signs the work, never the model, "+
		"and mw next refuses to land a branch whose commits carry one. "+
		// Both are learned the hard way and are the same for every rig: an
		// allow rule for bd must match every part of a compound command, so a
		// chained one goes to the classifier and can be refused whole; and a
		// headless session is over when its turn is, with anything uncommitted.
		"bd runs without asking, but as its own Bash call, never chained with another command "+
		"by ;, | or &&. "+
		"%s"+
		// mw next refuses a branch only once the session has ended and its fuel is
		// spent; mw check asks the same questions now, changes nothing, and fails
		// when mw next would.
		"Once your work is committed, run `mw check %s` and fix whatever it refuses: "+
		"it makes the checks mw next makes before landing (commits on the branch, none signed, "+
		"every formula step closed, the rig's tests passing), prints what mw next would print for each "+
		"refusal, writes nothing anywhere and exits non-zero when any check fails. "+
		"A formula step you have not closed yet is one it will name. "+
		"This session is headless and ends when your turn ends: run the suite in the foreground "+
		"and wait for it, never in the background, and commit before you stop.", seat, storyID, bdVault(vaultDir), storyID)
}

// RebaseKickoffPrompt is the first thing a session sent back to rebase is told.
// The story's work is done and its formula's steps are closed; what is left is
// only to bring the branch onto the target branch as it is now, so that mw next
// can land it. The branch has never been pushed, so rewriting it forces nothing.
func RebaseKickoffPrompt(seat, storyID, vaultDir, onto string) string {
	return fmt.Sprintf("You are booted into the %s seat of millwright, and your story is %s. "+
		"Your charter, your memory of this rig and the story itself are in the system prompt you were given. "+
		"Your worktree is the directory you are in: work only there. "+
		"The story has been worked and its formula steps are closed, but its branch does not merge into %s "+
		"without conflicts: the target branch moved on while the story was worked. "+
		"You are sent back once, to rebase: run `git rebase %s` in your worktree, resolve every conflict "+
		"so that both the story's work and what landed meanwhile are kept, run the rig's suite in the foreground "+
		"until it is green, and commit. The branch was never pushed, so the rebase forces nothing. "+
		"Do not push, do not merge, do not close the story, and work nothing else of it. "+
		"Your memory of this rig is read-only to you: propose notes under \"For the rig memory:\" in your closing comment. "+
		"Sign nothing you commit: no Co-Authored-By trailer, no Generated with line, "+
		"no AI attribution of any kind. "+
		"bd runs without asking, but as its own Bash call, never chained with another command "+
		"by ;, | or &&. "+
		"%s"+
		"Say on the story, in one comment, what the conflicts were and how you resolved them. "+
		"Once the rebase is committed, run `mw check %s` and fix whatever it refuses. "+
		"When you end, mw next lands the branch; if it still conflicts it stops, and nobody is sent back again. "+
		"This session is headless and ends when your turn ends: run the suite in the foreground "+
		"and wait for it, never in the background, and commit before you stop.", seat, storyID, onto, onto, bdVault(vaultDir), storyID)
}

// MergeFixKickoffPrompt is the first thing a session sent back to fix the
// merged tests is told. The story's work is done, its formula's steps are
// closed and its branch already has onto merged into it, without conflicts;
// what is left is why the merged result fails the rig's tests.
func MergeFixKickoffPrompt(seat, storyID, vaultDir, onto string) string {
	return fmt.Sprintf("You are booted into the %s seat of millwright, and your story is %s. "+
		"Your charter, your memory of this rig and the story itself are in the system prompt you were given. "+
		"Your worktree is the directory you are in: work only there. "+
		"The story has been worked and its formula steps are closed, and its branch already has %s merged into it, "+
		"without conflicts — but the merged result fails the rig's tests. "+
		"You are sent back once, to fix them: find why the tests fail on the merged result, fix it, "+
		"run the rig's suite in the foreground until it is green, and commit. "+
		"Do not push, do not merge again, do not close the story, and work nothing else of it. "+
		"Your memory of this rig is read-only to you: propose notes under \"For the rig memory:\" in your closing comment. "+
		"Sign nothing you commit: no Co-Authored-By trailer, no Generated with line, "+
		"no AI attribution of any kind. "+
		"bd runs without asking, but as its own Bash call, never chained with another command "+
		"by ;, | or &&. "+
		"%s"+
		"Say on the story, in one comment, what failed and how you fixed it. "+
		"Once the fix is committed, run `mw check %s` and fix whatever it refuses. "+
		"When you end, mw next lands the branch; if the merged tests still fail it stops, and nobody is sent back again. "+
		"This session is headless and ends when your turn ends: run the suite in the foreground "+
		"and wait for it, never in the background, and commit before you stop.", seat, storyID, onto, bdVault(vaultDir), storyID)
}

// bdVault is the sentence that gives a session the exact bd command for this
// host. From a worktree bd finds no beads database of its own; the vault holds
// the one, and where it is differs from host to host, so a session left to work
// the path out either guesses another host's or composes a change of directory
// ahead of bd — a chained command, refused whole. It is the literal command
// instead, taken from the vault this host is configured with.
func bdVault(vaultDir string) string {
	if vaultDir == "" {
		return ""
	}
	return fmt.Sprintf("From your worktree bd finds no beads database, so point every bd call at this host's vault "+
		"with -C, as in: bd -C %[1]s close <step>. The same goes for every other bd subcommand: bd -C %[1]s <subcommand> ... "+
		"Never change directory first. ", vaultDir)
}

// BootPrompt is the boot file a session is primed with: the seat's charter,
// then the seat's memory of this rig if it has one, then the story. Nothing
// else the vault holds belongs here.
func BootPrompt(seat Seat, detail StoryDetail) string {
	var b strings.Builder

	fmt.Fprintf(&b, "# The %s seat\n\n", seat.Name)
	section(&b, seat.Charter)

	if seat.Memory != "" {
		fmt.Fprintf(&b, "# The %s seat's memory of the rig %s\n\n", seat.Name, seat.Rig)
		section(&b, seat.Memory)
	}

	fmt.Fprintf(&b, "# Your story: %s\n\n", detail.Story.ID)
	if title := strings.TrimSpace(detail.Story.Title); title != "" {
		b.WriteString("## Title\n\n")
		section(&b, title)
	}

	b.WriteString("## Path\n\n")
	path := detail.Merged()
	for _, field := range domain.Fields {
		value, err := path.Field(field)
		if err != nil || value == "" {
			continue
		}
		fmt.Fprintf(&b, "- %s: %s\n", field, value)
	}
	b.WriteString("\n")

	if description := strings.TrimSpace(detail.Description); description != "" {
		b.WriteString("## Description\n\n")
		section(&b, description)
	}

	b.WriteString("## Acceptance criteria\n\n")
	if acceptance := strings.TrimSpace(detail.Acceptance); acceptance != "" {
		section(&b, acceptance)
	} else {
		section(&b, "(none recorded on the story)")
	}

	formulaSteps(&b, detail)
	return b.String()
}

// formulaSteps writes the story's formula into the boot file: the step beads it
// was poured into, in the order they are worked, each with the id the session
// closes when the step is done. A session that has to ask the tracker what its
// steps are pays for the asking; a session primed with them does not.
func formulaSteps(b *strings.Builder, detail StoryDetail) {
	molecule := detail.Molecule
	formula := detail.Merged().Formula
	if formula == "" && !molecule.Poured() {
		return
	}

	b.WriteString("## Your formula\n\n")
	if !molecule.Poured() {
		section(b, fmt.Sprintf("Your formula is %s. It has not been poured into step beads — it is not installed "+
			"where this factory pours formulas — so follow it from the rig's own docs.", formula))
		return
	}

	fmt.Fprintf(b, "Work these steps in order, and close each one as you finish it (`bd close <step>`); "+
		"closing a step is what makes the next one ready. They hang from %s.\n\n", molecule.RootID)
	for _, step := range molecule.Steps {
		fmt.Fprintf(b, "- %s · %s", step.ID, strings.TrimSpace(step.Title))
		if description := strings.TrimSpace(step.Description); description != "" {
			fmt.Fprintf(b, " — %s", description)
		}
		b.WriteString("\n")
	}
	b.WriteString("\n")
}

// section writes one block of the boot file, ending it with a blank line
// however the text it was given ended.
func section(b *strings.Builder, text string) {
	b.WriteString(strings.TrimRight(text, "\n"))
	b.WriteString("\n\n")
}
