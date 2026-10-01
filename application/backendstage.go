package application

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Jonathan-A-White/millwright/domain"
)

// BackendPendingPrefix is the start of the note a landing that changed a rig's
// backend is kept under until the home has staged it: one note per landed
// commit, BackendPendingPrefix + rig + "." + commit. A note, not a state, so
// that it is never an event of its own (see PosternNotes).
const BackendPendingPrefix = "backend.pending."

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
}

// BackendBuilds is the port the backend is read and built through: git and the
// rig's own build command, and nothing else. It has no way to install a binary,
// restart a unit or reach a live path, and that is on purpose: the swap is a
// hands step, run only once the Governor approves it.
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

// BackendHands is HandsAdd, narrowed to the one call a use case that writes a
// step needs.
type BackendHands interface {
	Run(ctx context.Context, req HandsAddRequest) (HandsStepRecord, error)
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
}

// BackendStage keeps a rig's backend level with its landings (mw-gq6.185). A
// landing of a story that changed the rig's backend used to ship the app and
// leave the live backend as it was, so the app's new half could not work until
// the Mayor built the backend by hand, staged it and wrote the swap as a hands
// step. This does that clerical half: it builds the backend at the landed commit
// on the home, and files a new hitl bead under the landed story's epic whose one
// hands step swaps the live binary — the already-live check, one backup, the
// install, the restart, four tries at the health and inbox checks, and the way
// back — and sends the Governor one message on that bead's channel.
//
// It never restarts a service, installs a binary or touches the live one: the
// swap runs when the Governor approves the step, and only then. On a host that is
// not home, a landing leaves a note and the home's next tick (Pending) does the
// rest. A zero BackendStage, or a rig with no settings, does nothing.
type BackendStage struct {
	// Rigs is where each rig is checked out on this host, Settings how each rig's
	// backend is built and swapped.
	Rigs     map[string]string
	Settings map[string]BackendRig

	Builds  BackendBuilds
	Home    HomeFile
	Host    string
	Tracker WorkTracker
	Notes   PosternNotes
	Hands   BackendHands
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
// built and filed, as a landing on the home does. A host that is not home leaves
// the notes where they are, and so does one that cannot tell.
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
	return notes
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
			return []string{staged, "backend: the pending note of " + l.Rig + " could not be cleared: " + firstLine(err.Error())}
		}
		return []string{staged}
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
func (b BackendStage) stage(ctx context.Context, l *BackendLanding, home string) (string, error) {
	cfg := b.Settings[l.Rig]
	rigDir := b.Rigs[l.Rig]
	switch {
	case rigDir == "":
		return "", fmt.Errorf("this host has no checkout of %s", l.Rig)
	case l.Epic == "":
		return "", fmt.Errorf("%s has no epic to file the swap under", l.Story)
	}
	short := updatedRevision(l.Commit)
	out := filepath.Join(cfg.Stage, filepath.Base(cfg.Live)+"-"+short)

	if err := b.Builds.Build(ctx, rigDir, l.Commit, cfg.Dir, cfg.Build, out); err != nil {
		return "", fmt.Errorf("building %s: %w", l.Rig, err)
	}
	step := BackendSwap(cfg, home, short, out)

	if l.Bead == "" {
		bead, err := b.Tracker.CreateStory(ctx, NewStory{
			EpicID:      l.Epic,
			Title:       fmt.Sprintf("Swap the home's %s backend to %s so %s's backend half is live", l.Rig, short, l.Story),
			Description: backendBeadText(*l, cfg, short, out),
			Acceptance:  fmt.Sprintf("cmp %s %s is equal; %s answers.", cfg.Live, out, cfg.Health),
			Priority:    1,
			Labels:      []string{LabelHitl},
		})
		if err != nil {
			return "", fmt.Errorf("filing the swap under %s: %w", l.Epic, err)
		}
		l.Bead = bead
		// Held stories are not shown to anybody: the swap is for the Governor now.
		if err := b.Tracker.ReleaseStory(ctx, bead); err != nil {
			return "", fmt.Errorf("releasing %s: %w", bead, err)
		}
	}
	if _, err := b.Hands.Run(ctx, HandsAddRequest{Bead: l.Bead, Step: step, Replace: true}); err != nil {
		return "", fmt.Errorf("writing the swap on %s: %w", l.Bead, err)
	}
	return fmt.Sprintf("backend: %s at %s staged at %s; the swap is a hands step on %s, for the Governor to approve", l.Rig, short, out, l.Bead), nil
}

func backendBeadText(l BackendLanding, cfg BackendRig, short, out string) string {
	return fmt.Sprintf("%s landed (%s) with a change under %s/ of %s, but the landing ships the app only: the live backend, %s, "+
		"is still the one built before it, so what the story added there is not live.\n\n"+
		"mw built %s at %s and staged it at %s. One hands step on this bead swaps it in: it does nothing if the staged binary is already live, "+
		"keeps one backup of the old one, installs, restarts %s, and reads %s and the Mayor's inbox up to four times; "+
		"if it cannot, it puts the old binary back by itself. The step runs only when the Governor approves it.",
		l.Story, l.Title, cfg.Dir, l.Rig, cfg.Live, l.Rig, short, out, cfg.Service, cfg.Health)
}

// BackendSwap is the hands step that swaps staged binary out in for the live one:
// exactly what the Mayor wrote by hand for postern's presence mark (mw-j0f2d.35).
// It is text for a person to approve and the factory to run once they do; nothing
// in the factory runs it unapproved.
func BackendSwap(cfg BackendRig, host, short, out string) domain.HandsStep {
	live, service := shQuote(cfg.Live), shQuote(cfg.Service)
	check := ""
	if strings.TrimSpace(cfg.Check) != "" {
		check = " && " + strings.TrimSpace(cfg.Check)
	}
	run := fmt.Sprintf(`set -e; l=%s; n=%s; b=%s; [ -x "$n" ]; `+
		`if cmp -s "$l" "$n"; then echo backend %s is already live: nothing was changed by this tap; exit 0; fi; `+
		`[ -e "$b" ] || cp -p "$l" "$b"; install -m 755 "$n" "$l"; systemctl --user restart %s; sleep 8; ok=0; `+
		`for i in 1 2 3 4; do if curl -fsS -m 20 %s%s; then ok=1; break; fi; echo try $i failed, waiting 15 s; sleep 15; done; `+
		`if [ $ok = 1 ]; then echo backend %s is live and answering; `+
		`else echo FAILED after 4 tries: putting the old backend back; install -m 755 "$b" "$l"; systemctl --user restart %s; exit 1; fi`,
		live, shQuote(out), shQuote(cfg.Live+".prev-before-"+short), short, service, shQuote(cfg.Health), check, short, service)
	wayBack := fmt.Sprintf("install -m 755 %s %s; systemctl --user restart %s", shQuote(cfg.Live+".prev-before-"+short), live, service)
	return domain.HandsStep{ID: "backend-" + short, Host: host, As: domain.HandsAsUser, Run: run, WayBack: wayBack}
}

// shQuote is s as one word of a shell command line.
func shQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }
