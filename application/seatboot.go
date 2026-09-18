package application

import (
	"context"
	"fmt"
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

	// RunFile is where one file of a story's run belongs, whether or not
	// anything has been written to it.
	RunFile(storyID, name string) string
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
		Kickoff:    KickoffPrompt(b.Seat, id),
	})
	if err != nil {
		return SessionSpec{}, fmt.Errorf("booting %s: %w", id, err)
	}
	if err := spec.Validate(); err != nil {
		return SessionSpec{}, fmt.Errorf("booting %s: %w", id, err)
	}
	return spec, nil
}

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
func KickoffPrompt(seat, storyID string) string {
	return fmt.Sprintf("You are booted into the %s seat of millwright, and your story is %s. "+
		"Your charter, your memory of this rig and the story itself are in the system prompt you were given. "+
		"Your worktree is the directory you are in: work only there. "+
		"Follow the story's formula steps in order, leave the build and the tests green, "+
		"and write one truthful closing comment on the story when you are done. "+
		"Do not push, do not merge, do not close the story.", seat, storyID)
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
