package application

import (
	"context"
	"fmt"
	"path"
	"strings"

	"github.com/Jonathan-A-White/millwright/domain"
)

// The files a story leaves in its own directory under the vault's runs/, side
// by side: what its session was booted with, and what it reported back.
const (
	BootFileName   = "boot.md"
	ResultFileName = "result.json"
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
	// MemoryExt is what a seat's memory of one rig is written in.
	MemoryExt = ".md"
)

// SeatWork is everything one story is allowed to have changed in the vault,
// by path from the vault's root: the seat's ledger, which mw itself appends the
// story's line to, and that seat's memory of the rig the story was worked in,
// which the session may have added a line to. Nothing else in the vault is
// either of their business, and a close-out commits exactly these.
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

// safeVaultName reports whether a name can be put in a vault path as it is.
func safeVaultName(name string) bool {
	return name != "" && !strings.ContainsAny(name, `/\`) && !strings.Contains(name, "..")
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
}

// Boot assembles the session that works detail in the worktree dir. What comes
// back is ready for a Runner to start.
func (b SeatBoot) Boot(ctx context.Context, detail StoryDetail, dir string) (SessionSpec, error) {
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

	bootFile, err := b.Vault.PutRunFile(ctx, id, BootFileName, BootPrompt(seat, detail))
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
		ResultFile: b.Vault.RunFile(id, ResultFileName),
		Kickoff:    KickoffPrompt(b.Seat, id, b.Vault.Dir()),
		After:      b.after(id),
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
		"This session is headless and ends when your turn ends: run the suite in the foreground "+
		"and wait for it, never in the background, and commit before you stop.", seat, storyID, bdVault(vaultDir))
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
