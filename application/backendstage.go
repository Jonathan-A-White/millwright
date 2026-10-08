package application

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/domain"
)

// BackendPendingPrefix is the start of the note a landing that changed a rig's
// backend is kept under until the home has staged it: one note per landed
// commit, BackendPendingPrefix + rig + "." + commit. A note, not a state, so
// that it is never an event of its own (see PosternNotes).
const BackendPendingPrefix = "backend.pending."

// BackendSwapPrefix is the start of the note a staged swap is kept under until
// the home's tick has run it, one note per staged commit, BackendSwapPrefix +
// rig + "." + commit. A note, not a state, as BackendPendingPrefix is.
const BackendSwapPrefix = "backend.swap."

// The two ways a rig's staged swap is taken: BackendSwapAuto, the default, has
// the home run it itself when no Talk is open (mw-gq6.270), BackendSwapHands
// leaves it as a hands step for the Governor to tap.
const (
	BackendSwapAuto  = "auto"
	BackendSwapHands = "hands"
)

// BackendTries is how many times a backend is built and filed before the
// attempts stop and the landed story says why: a build that fails three times
// running does not mend by itself, and a tick that rebuilt it forever would
// spend the home's minutes on nothing.
const BackendTries = 3

// BackendOutPlaceholder is where a rig's build command says the binary goes; the
// adapter puts the path of the file it is to leave there.
const BackendOutPlaceholder = "{out}"

// BackendRig is how one rig's own backend is built, staged and swapped on the
// home, as the host's [backend.<rig>] table says. A landing that changed
// anything under Dir is one whose backend half is not live until the home's
// binary is swapped.
type BackendRig struct {
	// Dir is the directory of the rig the backend is built in, and the one whose
	// changes count: "server" for postern.
	Dir string
	// Build is the command line that builds the backend, run in Dir of a
	// throwaway worktree at the landed commit, with BackendOutPlaceholder where
	// the binary goes.
	Build string
	// Stage is the directory built binaries wait in, named <live's name>-<short
	// commit>. Live is the binary the service runs, which nothing here touches.
	Stage string
	Live  string
	// Service is the user unit that runs Live, Health a URL that answers once it
	// is up, and Check an optional command that must also succeed after the swap.
	Service string
	Health  string
	Check   string
	// Swap is BackendSwapAuto or BackendSwapHands; empty is auto.
	Swap string

	// The VPS standby runs the same backend, behind the front door, and drifts when
	// a landing leaves it on the old build (mw-gq6.189). VPSHost, the host's name in
	// [hands_hosts], turns its staging on: the built binary is copied to VPSStage on
	// that host and its swap written as a root hands step for VPSLive and the system
	// unit VPSService, which VPSHealth, a URL that answers once it is up, checks. A
	// root step needs the Governor's approval, so the standby's swap is always a tap.
	VPSHost, VPSStage, VPSLive, VPSService, VPSHealth string
}

// Standby reports whether this rig's backend also runs on the VPS standby.
func (c BackendRig) Standby() bool { return c.VPSHost != "" }

// standbyRig is the VPS standby's side of c as a BackendRig, which the swap step
// is written from.
func (c BackendRig) standbyRig() BackendRig {
	return BackendRig{Stage: c.VPSStage, Live: c.VPSLive, Service: c.VPSService, Health: c.VPSHealth}
}

// Automatic reports whether the home swaps this rig's staged backend itself.
func (c BackendRig) Automatic() bool { return c.Swap != BackendSwapHands }

// BackendBuilds is the port the backend is read and built through: git and the
// rig's own build command, and nothing else. It has no way to install a binary,
// restart a unit or reach a live path, and that is on purpose: the swap is a
// hands step, run by the HandsRunner once the Governor approves it, or by the
// home's tick itself when the rig's swap is automatic.
type BackendBuilds interface {
	// Changed reports whether the commits branch has beyond base change anything
	// under subdir of the rig at rigDir: the story's own changes, not whatever the
	// target branch gained meanwhile.
	Changed(ctx context.Context, rigDir, base, branch, subdir string) (bool, error)

	// Build builds the rig at commit in a throwaway worktree, running command in
	// subdir of it with BackendOutPlaceholder replaced by a file beside out, which
	// it then moves to out; the worktree is removed whatever happens. A binary
	// already at out is left, so that building twice is building once.
	Build(ctx context.Context, rigDir, commit, subdir, command, out string) error
}

// BackendShip is the port a built binary is copied to another host's stage
// through: the standby's, and nothing else. Like BackendBuilds it cannot install a
// binary or restart a unit; what it leaves at dest is a staged file for the swap
// step to install once the Governor approves it.
type BackendShip interface {
	// Ship copies the file at src to dest on host, named as [hands_hosts] names it,
	// leaving nothing half-written at dest.
	Ship(ctx context.Context, host, src, dest string) error
}

// BackendHands is HandsAdd, narrowed to the one call a use case that writes a
// step needs.
type BackendHands interface {
	Run(ctx context.Context, req HandsAddRequest) (HandsStepRecord, error)
}

// SwapLock is the lock a swap holds while it runs, taken without waiting: the
// inbox lock, which mw postern inbox --apply holds for as long as it runs, so that
// a swap never restarts the backend under a pass that reads it, nor runs beside
// another swap.
type SwapLock interface {
	TryTake(ctx context.Context) (release func(), taken bool, err error)
}

// BackendLanding is a landed story whose backend half is to be staged.
type BackendLanding struct {
	Rig    string `json:"rig"`
	Story  string `json:"story"`
	Title  string `json:"title"`
	Epic   string `json:"epic"`
	Commit string `json:"commit"`

	// Bead, Tries and Why are what an unfinished staging remembers of itself: the
	// bead already filed for it, how many times it has been tried and how the last
	// try failed.
	Bead  string `json:"bead,omitempty"`
	Tries int    `json:"tries,omitempty"`
	Why   string `json:"why,omitempty"`

	// StandbyBead is the bead already filed for the VPS standby's swap, as Bead is
	// for the home's.
	StandbyBead string `json:"standby_bead,omitempty"`
}

// BackendStage keeps a rig's backend level with its landings (mw-gq6.185). A
// landing of a story that changed the rig's backend used to ship the app and
// leave the live backend as it was, so the app's new half could not work until
// the Mayor built the backend by hand, staged it and wrote the swap as a hands
// step. This does that clerical half: it builds the backend at the landed commit
// on the home, and files a new hitl bead under the landed story's epic (none, for a standalone story) whose one
// hands step swaps the live binary — the already-live check, one backup, the
// install, the restart, four tries at the health URL, and the way
// back — and sends the Governor one message on that bead's channel.
//
// It never restarts a service, installs a binary or touches the live one itself:
// the swap is the step's, run when the Governor approves it, or by the home's
// tick (Pending, swapStaged) once no Talk is open, unless the rig's swap is
// "hands" (mw-gq6.270). On a host that is not home, a landing leaves a note and the
// home's next tick does the rest. A zero BackendStage, or a rig with no settings,
// does nothing.
type BackendStage struct {
	// Rigs is where each rig is checked out on this host, Settings how each rig's
	// backend is built and swapped.
	Rigs     map[string]string
	Settings map[string]BackendRig

	Builds  BackendBuilds
	Ship    BackendShip
	Home    HomeFile
	Host    string
	Tracker WorkTracker
	Notes   PosternNotes
	Hands   BackendHands

	// Runner, Say, Lock and Now are what the home's tick swaps a staged backend with
	// (mw-gq6.270): the runner that runs the step, the sender that says its result on
	// the swap bead's channel, the inbox lock and the clock the Talk's quiet spell is
	// read by. Without a Runner or a Lock every swap is left as a hands step for a tap.
	Runner HandsRunner
	Say    PosternSender
	Lock   SwapLock
	Now    func() time.Time
}

// Wants reports whether this host names a backend for rig.
func (b BackendStage) Wants(rig string) bool {
	_, ok := b.Settings[rig]
	return ok && b.Builds != nil
}

// Touches reports whether the story branch, measured against base, changed
// anything the rig's backend is built from. A rig with no backend is not asked.
func (b BackendStage) Touches(ctx context.Context, rig, base, branch string) (bool, error) {
	if !b.Wants(rig) {
		return false, nil
	}
	return b.Builds.Changed(ctx, b.Rigs[rig], base, branch, b.Settings[rig].Dir)
}

// Landed stages the backend of a landing that changed it, and says what it did
// as notes for the close-out's report. On the home it builds and files at once; a
// failure is kept as a pending note for the next tick to try again. Anywhere else
// it keeps the pending note and names the home.
func (b BackendStage) Landed(ctx context.Context, l BackendLanding) []string {
	if !b.Wants(l.Rig) || b.Notes == nil || b.Tracker == nil || b.Hands == nil {
		return nil
	}
	home, err := b.homeHost(ctx)
	if err != nil {
		return b.keep(ctx, l, "the backend of "+l.Rig+" landed at "+updatedRevision(l.Commit)+" and this host cannot tell which host is home ("+firstLine(err.Error())+"): left for the home's next tick")
	}
	if home != b.Host {
		return b.keep(ctx, l, fmt.Sprintf("the backend of %s changed in %s: the home, %s, stages it on its next tick", l.Rig, l.Story, home))
	}
	return b.attempt(ctx, l, home)
}

// Pending is the home's tick: every landed backend still waiting to be staged is
// built and filed, as a landing on the home does, and every staged swap of a rig
// whose swap is automatic is run (swapStaged). A host that is not home leaves the
// notes where they are, and so does one that cannot tell.
func (b BackendStage) Pending(ctx context.Context) []string {
	if len(b.Settings) == 0 || b.Notes == nil || b.Home == nil {
		return nil
	}
	home, err := b.homeHost(ctx)
	if err != nil || home != b.Host {
		return nil
	}
	kept, err := b.Notes.NotesWithPrefix(ctx, BackendPendingPrefix)
	if err != nil {
		return []string{"backend: the pending landings could not be read: " + firstLine(err.Error())}
	}
	keys := make([]string, 0, len(kept))
	for key := range kept {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var notes []string
	for _, key := range keys {
		var l BackendLanding
		if err := json.Unmarshal([]byte(kept[key]), &l); err != nil || !b.Wants(l.Rig) {
			continue
		}
		if l.Tries >= BackendTries {
			continue
		}
		notes = append(notes, b.attempt(ctx, l, home)...)
	}
	return append(notes, b.swapStaged(ctx)...)
}

func (b BackendStage) homeHost(ctx context.Context) (string, error) {
	if b.Home == nil {
		return "", fmt.Errorf("this host has no home file to read")
	}
	record, err := WhereIsHome(ctx, b.Home)
	if err != nil {
		return "", err
	}
	return record.Host, nil
}

func backendKey(l BackendLanding) string { return BackendPendingPrefix + l.Rig + "." + l.Commit }

// keep writes l down as waiting for the home and returns the line saying so.
func (b BackendStage) keep(ctx context.Context, l BackendLanding, line string) []string {
	if err := b.save(ctx, l); err != nil {
		return []string{line, "backend: the landing could not be kept for the home: " + firstLine(err.Error())}
	}
	return []string{line}
}

func (b BackendStage) save(ctx context.Context, l BackendLanding) error {
	text, err := json.Marshal(l)
	if err != nil {
		return err
	}
	return b.Notes.SetNote(ctx, backendKey(l), string(text))
}

// attempt is one try at staging l on the home, and what became of it: done and
// the note cleared, or the failure counted, kept and, on the last try, written
// on the landed story.
func (b BackendStage) attempt(ctx context.Context, l BackendLanding, home string) []string {
	words := fmt.Sprintf("backend: %s at %s", l.Rig, updatedRevision(l.Commit))
	staged, err := b.stage(ctx, &l, home)
	if err == nil {
		if err := b.Notes.ClearNote(ctx, backendKey(l)); err != nil {
			return append(staged, "backend: the pending note of "+l.Rig+" could not be cleared: "+firstLine(err.Error()))
		}
		return staged
	}

	l.Tries++
	l.Why = firstLine(err.Error())
	lines := []string{fmt.Sprintf("%s was not staged (try %d of %d): %s", words, l.Tries, BackendTries, l.Why)}
	if err := b.save(ctx, l); err != nil {
		lines = append(lines, "backend: the failure could not be kept for the next tick: "+firstLine(err.Error()))
	}
	if l.Tries >= BackendTries {
		comment := fmt.Sprintf("The backend of %s landed with this story (commit %s) and could not be staged for the home after %d tries, so it is NOT live: %s\n\n"+
			"Nothing more will be tried; it is a person's to build and stage.", l.Rig, l.Commit, BackendTries, l.Why)
		if err := b.Tracker.CommentOnStory(ctx, l.Story, comment); err != nil {
			lines = append(lines, "backend: giving up could not be written on "+l.Story+": "+firstLine(err.Error()))
		}
	}
	return lines
}

// stage builds the backend, files the bead if it is not already filed and writes
// the swap on it. l carries the bead across a retry, so a step that failed to be
// written is written on the bead already filed rather than on a second.
func (b BackendStage) stage(ctx context.Context, l *BackendLanding, home string) ([]string, error) {
	cfg := b.Settings[l.Rig]
	rigDir := b.Rigs[l.Rig]
	switch {
	case rigDir == "":
		return nil, fmt.Errorf("this host has no checkout of %s", l.Rig)
	}
	short := updatedRevision(l.Commit)
	out := filepath.Join(cfg.Stage, filepath.Base(cfg.Live)+"-"+short)

	if err := b.Builds.Build(ctx, rigDir, l.Commit, cfg.Dir, cfg.Build, out); err != nil {
		return nil, fmt.Errorf("building %s: %w", l.Rig, err)
	}
	step := BackendSwap(cfg, home, short, out)

	if l.Bead == "" {
		l.Bead = b.openSwapOf(ctx, cfg, step.ID)
	}
	if l.Bead == "" {
		bead, err := b.fileSwap(ctx, l.Epic, NewStory{
			Title:       fmt.Sprintf("Swap the home's %s backend to %s so %s's backend half is live", l.Rig, short, l.Story),
			Description: backendBeadText(*l, cfg, short, out, b.swapsItself(cfg)),
			Acceptance:  fmt.Sprintf("cmp %s %s is equal; %s answers.", cfg.Live, out, cfg.Health),
		})
		l.Bead = bead
		if err != nil {
			return nil, err
		}
	}
	if err := b.writeSwap(ctx, cfg, l.Bead, step); err != nil {
		return nil, err
	}
	staged := fmt.Sprintf("backend: %s at %s staged at %s; the swap is a hands step on %s, for the Governor to approve", l.Rig, short, out, l.Bead)
	if b.swapsItself(cfg) {
		text, err := json.Marshal(stagedSwap{Rig: l.Rig, Story: l.Story, Bead: l.Bead, Step: step.ID, Commit: l.Commit})
		if err == nil {
			err = b.Notes.SetNote(ctx, swapKey(l.Rig, l.Commit), string(text))
		}
		if err != nil {
			return nil, fmt.Errorf("keeping the swap of %s for the tick: %w", l.Rig, err)
		}
		staged = fmt.Sprintf("backend: %s at %s staged at %s; the swap is a hands step on %s, which the home's tick runs itself once no Talk is open", l.Rig, short, out, l.Bead)
	}
	if !cfg.Standby() {
		return []string{staged}, nil
	}
	standby, err := b.stageStandby(ctx, l, cfg, short, out)
	if err != nil {
		return nil, err
	}
	return []string{staged, standby}, nil
}

// fileSwap files a hitl bead under epic for a swap and releases it: held stories
// are not shown to anybody, and the swap is for the Governor now. A landing whose
// story had no epic (a bug fix is filed standalone) has its swap filed with no
// parent. It returns the bead's id, set even when releasing it failed.
func (b BackendStage) fileSwap(ctx context.Context, epic string, story NewStory) (string, error) {
	story.EpicID = epic
	story.Standalone = epic == ""
	story.Priority = 1
	story.Labels = []string{LabelHitl}
	bead, err := b.Tracker.CreateStory(ctx, story)
	if err != nil {
		if epic == "" {
			return "", fmt.Errorf("filing the swap: %w", err)
		}
		return "", fmt.Errorf("filing the swap under %s: %w", epic, err)
	}
	if err := b.Tracker.ReleaseStory(ctx, bead); err != nil {
		// The bead is filed: a retry writes the step on it rather than on a second.
		return bead, fmt.Errorf("releasing %s: %w", bead, err)
	}
	return bead, nil
}

// writeSwap writes step on bead and marks the older swaps of cfg's live binary as
// superseded by it.
func (b BackendStage) writeSwap(ctx context.Context, cfg BackendRig, bead string, step domain.HandsStep) error {
	if _, err := b.Hands.Run(ctx, HandsAddRequest{Bead: bead, Step: step, Replace: true}); err != nil {
		return fmt.Errorf("writing the swap on %s: %w", bead, err)
	}
	if err := b.supersedeOlder(ctx, cfg, bead, step.ID); err != nil {
		return fmt.Errorf("superseding the older swaps of %s: %w", cfg.Live, err)
	}
	return nil
}

// stageStandby is the VPS half of a staging (mw-gq6.189): the binary just built is
// copied to the standby's stage, and its swap written as a root hands step on a
// bead of its own, so that the front door's backend is level with the home's once
// the Governor taps it. A host with no way to copy leaves it out and says so.
func (b BackendStage) stageStandby(ctx context.Context, l *BackendLanding, cfg BackendRig, short, built string) (string, error) {
	if b.Ship == nil {
		return "backend: " + l.Rig + "'s VPS standby is not staged: this host has no way to copy the binary there", nil
	}
	standby := cfg.standbyRig()
	dest := filepath.Join(cfg.VPSStage, filepath.Base(cfg.VPSLive)+"-"+short)
	if err := b.Ship.Ship(ctx, cfg.VPSHost, built, dest); err != nil {
		return "", fmt.Errorf("copying %s to %s on %s: %w", built, dest, cfg.VPSHost, err)
	}
	step := backendSwapAs(standby, cfg.VPSHost, short, dest, domain.HandsAsRoot)

	if l.StandbyBead == "" {
		l.StandbyBead = b.openSwapOf(ctx, standby, step.ID)
	}
	if l.StandbyBead == "" {
		bead, err := b.fileSwap(ctx, l.Epic, NewStory{
			Title:       fmt.Sprintf("Swap the VPS standby's %s backend to %s so the front door is level with the home", l.Rig, short),
			Description: standbyBeadText(*l, cfg, short, dest),
			Acceptance:  fmt.Sprintf("cmp %s %s is equal on %s; %s answers.", standby.Live, dest, cfg.VPSHost, standby.Health),
		})
		l.StandbyBead = bead
		if err != nil {
			return "", err
		}
	}
	if err := b.writeSwap(ctx, standby, l.StandbyBead, step); err != nil {
		return "", err
	}
	return fmt.Sprintf("backend: %s at %s staged on %s at %s; the standby's swap is a hands step on %s, for the Governor to approve", l.Rig, short, cfg.VPSHost, dest, l.StandbyBead), nil
}

func standbyBeadText(l BackendLanding, cfg BackendRig, short, dest string) string {
	return fmt.Sprintf("%s landed (%s) with a change under %s/ of %s, and the VPS standby behind the front door runs the backend built before it: "+
		"a phone served by the standby then lacks what the story added there.\n\n"+
		"mw copied the backend built at %s to %s on %s. One root hands step on this bead swaps it in: it does nothing if the staged binary is already live, "+
		"keeps one backup of the old one, installs, restarts %s, and reads %s up to four times; "+
		"if it never answers, it puts the old binary back by itself. The step runs when the Governor approves it; nothing runs it before.",
		l.Story, l.Title, cfg.Dir, l.Rig, short, dest, cfg.VPSHost, cfg.VPSService, cfg.VPSHealth)
}

// swapsItself reports whether a swap of cfg's rig is run by the home's tick: the
// rig has not kept the tap, and this stage has what running it takes.
func (b BackendStage) swapsItself(cfg BackendRig) bool {
	return cfg.Automatic() && b.Runner != nil && b.Lock != nil
}

// swapSteps is every swap step of cfg's live binary that has not run clean, in
// the order of the beads that hold them: a swap is a step named backend-<commit>
// whose text swaps cfg.Live; a step of any other kind or rig is not one.
func (b BackendStage) swapSteps(ctx context.Context, cfg BackendRig) ([]heldSwap, error) {
	notes, err := b.Notes.NotesWithPrefix(ctx, "hands.")
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(notes))
	for key := range notes {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	swaps := "l=" + shQuote(cfg.Live) + ";"
	var held []heldSwap
	for _, key := range keys {
		holder := strings.TrimPrefix(key, "hands.")
		if strings.HasPrefix(holder, "ran.") || strings.HasPrefix(holder, "approval.") || strings.HasPrefix(holder, "superseded.") {
			continue
		}
		steps, err := parseHandsSteps(notes[key])
		if err != nil {
			continue
		}
		for _, step := range steps {
			if !strings.HasPrefix(step.ID, "backend-") || !strings.Contains(step.Run, swaps) {
				continue
			}
			if ran, ok := parseHandsRan(notes[HandsRanKey(holder, step.ID)]); ok && ran.Exit == 0 {
				continue
			}
			held = append(held, heldSwap{Bead: holder, ID: step.ID})
		}
	}
	return held, nil
}

type heldSwap struct{ Bead, ID string }

// openSwapOf is the bead that already holds the swap step id of cfg's live binary,
// not run clean and not closed, or "" when there is none: a staging of a commit
// whose swap is already filed writes on that bead rather than filing a second
// (mw-gq6.292).
func (b BackendStage) openSwapOf(ctx context.Context, cfg BackendRig, id string) string {
	held, err := b.swapSteps(ctx, cfg)
	if err != nil {
		return ""
	}
	for _, h := range held {
		if h.ID != id {
			continue
		}
		if detail, err := b.Tracker.ShowStory(ctx, h.Bead); err == nil && detail.Status != StatusClosed {
			return h.Bead
		}
	}
	return ""
}

// supersedeOlder marks every other swap step of cfg's live binary that has not run
// clean as superseded by bead, now that bead holds a newer one: a swap step that
// ran after a newer one put the older backend back on the home (mw-gq6.190). A
// swap of the same commit (the same step id, on another bead) is a duplicate, not
// an older one: it is never marked, and a mark that two stagings put on each other
// is cleared (mw-gq6.292).
func (b BackendStage) supersedeOlder(ctx context.Context, cfg BackendRig, bead, id string) error {
	held, err := b.swapSteps(ctx, cfg)
	if err != nil {
		return err
	}
	for _, h := range held {
		if h.ID == id {
			if err := b.Notes.ClearNote(ctx, HandsSupersededKey(h.Bead, h.ID)); err != nil {
				return err
			}
			continue
		}
		if err := b.Notes.SetNote(ctx, HandsSupersededKey(h.Bead, h.ID), bead); err != nil {
			return err
		}
	}
	return nil
}

func backendBeadText(l BackendLanding, cfg BackendRig, short, out string, automatic bool) string {
	itself := ""
	if automatic {
		itself = ", or by the home's tick on its own once no Talk is open, and the result is said on this bead's channel"
	}
	return fmt.Sprintf("%s landed (%s) with a change under %s/ of %s, but the landing ships the app only: the live backend, %s, "+
		"is still the one built before it, so what the story added there is not live.\n\n"+
		"mw built %s at %s and staged it at %s. One hands step on this bead swaps it in: it does nothing if the staged binary is already live, "+
		"keeps one backup of the old one, installs, restarts %s, and reads %s up to four times; "+
		"if it never answers, it puts the old binary back by itself. The step runs when the Governor approves it%s.",
		l.Story, l.Title, cfg.Dir, l.Rig, cfg.Live, l.Rig, short, out, cfg.Service, cfg.Health, itself)
}

// BackendSwap is the hands step that swaps staged binary out in for the live one:
// exactly what the Mayor wrote by hand for postern's presence mark (mw-j0f2d.35).
// It is text for a person to approve and the factory to run once they do; the one
// thing that runs it unapproved is the home's tick, for a rig whose swap is "auto".
//
// The step is run by mw postern inbox --apply, which holds the inbox's lock for
// as long as it runs, and the backend it restarts is the one that pass reads, so
// the step reads neither: a configured check that reads the inbox is left out
// (checkReadsInbox). What puts the old backend back is the backend not answering
// its health URL, and only that: a new backend that answers is not replaced
// because a check of the host's own failed or hung, which only ends the step in
// failure for the Governor to see (mw-gq6.209).
func BackendSwap(cfg BackendRig, host, short, out string) domain.HandsStep {
	return backendSwapAs(cfg, host, short, out, domain.HandsAsUser)
}

// backendSwapAs is BackendSwap run as who: the host's own user, whose unit is a
// user unit, or root, whose unit is a system unit, as the VPS standby's is.
func backendSwapAs(cfg BackendRig, host, short, out, who string) domain.HandsStep {
	live, service := shQuote(cfg.Live), shQuote(cfg.Service)
	systemctl := "systemctl --user"
	if who == domain.HandsAsRoot {
		systemctl = "systemctl"
	}
	check := ""
	if command := strings.TrimSpace(cfg.Check); command != "" && !checkReadsInbox(command) {
		check = fmt.Sprintf(`if ! timeout %d sh -c %s; then echo backend %s answers %s but its check failed: leaving it live; exit 1; fi; `,
			BackendCheckSeconds, shQuote(command), short, shQuote(cfg.Health))
	}
	run := fmt.Sprintf(`set -e; l=%s; n=%s; b=%s; [ -x "$n" ]; `+
		`for f in %s/"$(basename "$l")"-*; do if [ -f "$f" ] && [ "$f" -nt "$n" ] && cmp -s "$f" "$l"; then echo live backend "${f##*-}" is newer than %s: nothing was changed by this tap; exit 0; fi; done; `+
		`if cmp -s "$l" "$n"; then echo backend %s is already live: nothing was changed by this tap; exit 0; fi; `+
		`[ -e "$b" ] || cp -p "$l" "$b"; install -m 755 "$n" "$l"; %s restart %s; sleep 8; ok=0; `+
		`for i in 1 2 3 4; do if curl -fsS -m 20 %s; then ok=1; break; fi; echo try $i failed, waiting 15 s; sleep 15; done; `+
		`if [ $ok = 1 ]; then %secho backend %s is live and answering; `+
		`else echo FAILED after 4 tries: putting the old backend back; install -m 755 "$b" "$l"; %s restart %s; exit 1; fi`,
		live, shQuote(out), shQuote(cfg.Live+".prev-before-"+short), shQuote(cfg.Stage), short, short, systemctl, service, shQuote(cfg.Health), check, short, systemctl, service)
	wayBack := fmt.Sprintf("install -m 755 %s %s; %s restart %s", shQuote(cfg.Live+".prev-before-"+short), live, systemctl, service)
	return domain.HandsStep{ID: "backend-" + short, Host: host, As: who, Run: run, WayBack: wayBack}
}

// BackendCheckSeconds is how long a rig's own check may run before the swap step
// cuts it off and counts it failed.
const BackendCheckSeconds = 60

// checkReadsInbox reports whether a check command reads the postern inbox, which
// the swap step is run from and may not wait on.
func checkReadsInbox(command string) bool { return strings.Contains(command, "postern inbox") }

// shQuote is s as one word of a shell command line.
func shQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }
