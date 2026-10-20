package application

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/Jonathan-A-White/millwright/domain"
)

// FactoryRig is the rig the factory itself is built from, and FactoryBranch the
// branch of it every host runs its mw from.
const (
	FactoryRig    = domain.FactoryRig
	FactoryBranch = "main"
)

// FactoryCheckout is the port a host's own checkout of the factory rig is
// looked at and moved through.
type FactoryCheckout interface {
	// Fetch brings the checkout's view of its origin up to date.
	Fetch(ctx context.Context, rigDir string) error

	// Tip is the commit branch is at on the remote, as the last Fetch saw it.
	Tip(ctx context.Context, rigDir, remote, branch string) (string, error)

	// Head is the commit the checkout has checked out.
	Head(ctx context.Context, rigDir string) (string, error)

	// Uncommitted lists the paths the working tree has changed and not
	// committed; empty means clean.
	Uncommitted(ctx context.Context, dir string) ([]string, error)

	// Advance fast-forwards the checkout onto commit, as Landing.Advance does.
	Advance(ctx context.Context, rigDir, branch, commit string) (Advanced, error)
}

// BuiltMarks is where a host remembers, per rig, the commit it last built
// successfully, so that a build that failed is tried again by the next tick.
type BuiltMarks interface {
	// Built is the commit last built for rig, empty when none was.
	Built(ctx context.Context, rig string) (string, error)

	// MarkBuilt records commit as the one last built for rig.
	MarkBuilt(ctx context.Context, rig, commit string) error
}

// SelfUpdate keeps the mw a host runs level with the factory rig's main
// (mw-gq6.183). A host rebuilt its mw only when it landed a millwright story
// itself, so a fix landed on the other host never reached it. Once a tick, a
// host that names a command for the factory rig (its [after_landing] table: the
// one a landing runs, `make build` with the host's own Go) fetches the rig,
// fast-forwards a clean checkout that is behind origin's main and runs that
// command in it.
//
// A checkout that is dirty, on another branch or has commits of its own is
// left exactly as it is and the notes say why; nothing is ever merged, reset or
// forced. A build that fails leaves the old binary, is said, and is tried again
// by the next tick, because the commit last built is remembered and the
// checkout is no longer behind. A host that names no command runs nothing from
// the rig, and its checkout is not touched.
type SelfUpdate struct {
	// Rigs is where each rig is checked out on this host; the factory rig is
	// looked for in it.
	Rigs     map[string]string
	Checkout FactoryCheckout
	After    AfterLanding
	Built    BuiltMarks

	// Units restarts this host's long-running mw user units once the build has
	// succeeded, so that they run what was just built. A nil Units restarts
	// nothing.
	Units UnitRestarter

	// Formulas puts the checkout's formulas/ into the vault's .beads/formulas,
	// where bd pours them from, once the checkout is level with origin's main
	// (mw-gq6.314): one vault commit naming the revision, and none when the
	// copies already match. Only the home does it, so that Home and Host must
	// say this host is home when Home is set. A nil Formulas installs nothing.
	Formulas FormulaInstaller
	Home     HomeFile
	Host     string

	// Slot is the rig's after-landing lock, taken around the command the home
	// runs for a rig other than the factory rig (mw-gq6.562). A nil Slot takes none.
	Slot MergeSlot
}

// selfUpdateWords is how every note about an update begins.
const selfUpdateWords = "self-update: " + FactoryRig

// updatedRevisionLen is how much of a commit the notes name, the same short
// revision mw version prints.
const updatedRevisionLen = 7

// deployTriesAtACommit is how many times the home runs a rig's deploy at one
// commit when it fails: the tick's own try and one more on the next tick.
const deployTriesAtACommit = 2

func updatedRevision(commit string) string {
	if len(commit) > updatedRevisionLen {
		return commit[:updatedRevisionLen]
	}
	return commit
}

// Run does one look and says what it did, as notes for a tick's line: none for
// a host that has nothing to do, so that the line says it once. It keeps the
// factory rig level and built, and on the home it deploys the other rigs too
// (deployOthers).
func (s SelfUpdate) Run(ctx context.Context) []string {
	notes, _ := s.Update(ctx)
	return notes
}

// SelfUpdateFailure is the step that kept the factory rig's checkout from being
// brought level and built on one tick, and the first line of what it said
// (mw-gq6.654): the dispatch counts these tick after tick.
type SelfUpdateFailure struct {
	// Step is fetch, fast-forward or build, or the read of origin's main or of the
	// checkout's commit.
	Step string
	Said string
}

// Update is Run that also says, as a SelfUpdateFailure, when the factory rig could
// not be brought level or built. A checkout left alone, as it should be, is no
// failure; nor is a failed deploy of another rig.
func (s SelfUpdate) Update(ctx context.Context) ([]string, *SelfUpdateFailure) {
	notes, failure := s.runFactory(ctx)
	return append(notes, s.deployOthers(ctx)...), failure
}

func (s SelfUpdate) runFactory(ctx context.Context) ([]string, *SelfUpdateFailure) {
	dir := s.Rigs[FactoryRig]
	if dir == "" || s.Checkout == nil || s.After == nil || s.Built == nil || s.After.Command(FactoryRig) == "" {
		return nil, nil
	}
	failed := func(step, doing string, err error) ([]string, *SelfUpdateFailure) {
		said := firstLine(err.Error())
		return []string{fmt.Sprintf("%s could not be brought level: %s: %s", selfUpdateWords, doing, said)},
			&SelfUpdateFailure{Step: step, Said: said}
	}

	if err := s.Checkout.Fetch(ctx, dir); err != nil {
		return failed("fetch", "fetching", err)
	}
	tip, err := s.Checkout.Tip(ctx, dir, DefaultRemote, FactoryBranch)
	if err != nil || tip == "" {
		if err == nil {
			err = fmt.Errorf("origin has no %s", FactoryBranch)
		}
		return failed("read origin's "+FactoryBranch, "reading origin's "+FactoryBranch, err)
	}
	before, err := s.Checkout.Head(ctx, dir)
	if err != nil {
		return failed("read the checkout's commit", "reading the checkout's commit", err)
	}
	advanced, err := s.Checkout.Advance(ctx, dir, FactoryBranch, tip)
	if err != nil {
		return failed("fast-forward", "fast-forwarding", err)
	}
	if advanced.Left != "" {
		return []string{selfUpdateWords + " checkout left alone: " + advanced.Left}, nil
	}
	head, err := s.Checkout.Head(ctx, dir)
	if err != nil {
		return failed("read the checkout's commit", "reading the checkout's commit", err)
	}

	formulaNotes := s.installFormulas(ctx, dir, head)
	with := func(notes []string) ([]string, *SelfUpdateFailure) { return append(formulaNotes, notes...), nil }

	// Level already: build only what an earlier tick's failed build left
	// unbuilt, and only a tree that is exactly a commit.
	if !advanced.Moved {
		if built, err := s.Built.Built(ctx, FactoryRig); err != nil || built == head {
			return with(nil)
		}
		if dirty, err := s.Checkout.Uncommitted(ctx, dir); err != nil || len(dirty) > 0 {
			return with(nil)
		}
	}

	ran, err := s.After.Run(ctx, FactoryRig, dir)
	var buildSaid string
	switch {
	case err != nil:
		buildSaid = afterLandingLine(s.After.Command(FactoryRig), "could not be run: "+firstLine(err.Error()))
	case !ran.Succeeded():
		buildSaid = ran.Line()
	}
	if buildSaid != "" {
		notes, _ := with([]string{fmt.Sprintf("%s at %s: %s; the old mw is kept, the next tick tries again",
			selfUpdateWords, updatedRevision(head), buildSaid)})
		return notes, &SelfUpdateFailure{Step: "build", Said: firstLine(buildSaid)}
	}

	notes := []string{fmt.Sprintf("%s %s → %s, built", selfUpdateWords, updatedRevision(before), updatedRevision(head))}
	if !advanced.Moved {
		notes = []string{fmt.Sprintf("%s built at %s", selfUpdateWords, updatedRevision(head))}
	}
	if err := s.Built.MarkBuilt(ctx, FactoryRig, head); err != nil {
		notes = append(notes, "the build could not be remembered: "+firstLine(err.Error()))
	}
	said, _ := RestartFactoryUnits(ctx, s.Units, FactoryRig)
	return with(append(notes, said...))
}

// leftMark is the key, in Built, under which a rig's checkout that was left
// alone is remembered with the reason it was left, so that a tick says it once
// and not every fifteen minutes.
func leftMark(rig string) string { return rig + ".left" }

// deployedMark is the key, in Built, under which a host remembers the commit a
// rig's last passing after-landing run ran at (mw-gq6.564), and deployFailedMark
// the one under which the home counts the failed tries at a commit ("<commit> <n>").
func deployedMark(rig string) string     { return rig + ".deployed" }
func deployFailedMark(rig string) string { return rig + ".deploy-failed" }

// markDeployed records that the rig's after-landing command passed at the commit
// its checkout is at, and forgets any failed tries. It returns a note when the
// mark could not be kept, empty otherwise. A nil marks or checkout keeps nothing.
func markDeployed(ctx context.Context, marks BuiltMarks, checkout FactoryCheckout, rig, dir string) string {
	if marks == nil || checkout == nil {
		return ""
	}
	head, err := checkout.Head(ctx, dir)
	if err != nil || head == "" {
		return fmt.Sprintf("the deployed mark of %s could not be set: the checkout's commit could not be read", rig)
	}
	return markDeployedAt(ctx, marks, rig, head)
}

func markDeployedAt(ctx context.Context, marks BuiltMarks, rig, commit string) string {
	if marks == nil || commit == "" {
		return ""
	}
	if err := marks.MarkBuilt(ctx, deployedMark(rig), commit); err != nil {
		return fmt.Sprintf("the deployed mark of %s could not be set: %s", rig, firstLine(err.Error()))
	}
	if held, _ := marks.Built(ctx, deployFailedMark(rig)); held != "" {
		_ = marks.MarkBuilt(ctx, deployFailedMark(rig), "")
	}
	return ""
}

// deployTries is how many times a deploy of rig at commit has already failed on
// the home's ticks.
func (s SelfUpdate) deployTries(ctx context.Context, rig, commit string) int {
	held, _ := s.Built.Built(ctx, deployFailedMark(rig))
	at, count, ok := strings.Cut(held, " ")
	if !ok || at != commit {
		return 0
	}
	n, _ := strconv.Atoi(count)
	return n
}

// afterLandingRigs are the rigs other than the factory rig that this host names
// an [after_landing] command for and has checked out, by name.
func (s SelfUpdate) afterLandingRigs() []string {
	var names []string
	for name, dir := range s.Rigs {
		if name != FactoryRig && dir != "" && s.After.Command(name) != "" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// deployOthers is the home's share (mw-gq6.562). An app story that landed on a
// cloud box moved only that box's checkout, and the box names no command for the
// rig, so nothing deployed it: the live site stayed on the old version until a
// later landing of the same rig happened on a host that does. So, on the home,
// for each rig its [after_landing] table names (the factory rig has its own
// pass), a clean checkout on main that is behind origin is fast-forwarded and
// the rig's command run in it exactly as `mw after-landing <rig>` runs it: under
// the rig's after-landing lock and limit, and once more after a passing network
// fault. A checkout that is dirty, on another branch or diverged is left alone
// and said once.
//
// A host that is not home, or whose home cannot be told, does none of it.
//
// What is deployed is decided by a per-rig deployed mark (mw-gq6.564): the
// commit the last passing after-landing run on this host ran at. A checkout whose
// HEAD differs from it is deployed even when it did not move this tick — a hand
// fast-forward, a deploy that failed or was cut off. A rig with no mark whose
// checkout is level gets the mark set to HEAD without a deploy, and the line says
// so once. A deploy that fails is said with how to run it by hand, is tried once
// more by the next tick and then left, said, until HEAD moves.
func (s SelfUpdate) deployOthers(ctx context.Context) []string {
	if s.Home == nil || s.Checkout == nil || s.After == nil || s.Built == nil {
		return nil
	}
	rigs := s.afterLandingRigs()
	if len(rigs) == 0 {
		return nil
	}
	if home, err := IsHome(ctx, s.Home, s.Host); err != nil || !home {
		return nil
	}
	var notes []string
	for _, rig := range rigs {
		notes = append(notes, s.deployRig(ctx, rig, s.Rigs[rig])...)
	}
	return notes
}

func (s SelfUpdate) deployRig(ctx context.Context, rig, dir string) []string {
	words := "self-update: " + rig
	failed := func(doing string, err error) []string {
		return []string{fmt.Sprintf("%s could not be brought level: %s: %s", words, doing, firstLine(err.Error()))}
	}
	if err := s.Checkout.Fetch(ctx, dir); err != nil {
		return failed("fetching", err)
	}
	tip, err := s.Checkout.Tip(ctx, dir, DefaultRemote, FactoryBranch)
	if err != nil || tip == "" {
		if err == nil {
			err = fmt.Errorf("origin has no %s", FactoryBranch)
		}
		return failed("reading origin's "+FactoryBranch, err)
	}
	before, err := s.Checkout.Head(ctx, dir)
	if err != nil {
		return failed("reading the checkout's commit", err)
	}
	advanced, err := s.Checkout.Advance(ctx, dir, FactoryBranch, tip)
	if err != nil {
		return failed("fast-forwarding", err)
	}
	remembered, _ := s.Built.Built(ctx, leftMark(rig))
	if advanced.Left != "" {
		if remembered == advanced.Left {
			return nil
		}
		note := words + " checkout left alone: " + advanced.Left
		if err := s.Built.MarkBuilt(ctx, leftMark(rig), advanced.Left); err != nil {
			note += " (it could not be remembered, so it will be said again: " + firstLine(err.Error()) + ")"
		}
		return []string{note}
	}
	if remembered != "" {
		_ = s.Built.MarkBuilt(ctx, leftMark(rig), "")
	}
	head, err := s.Checkout.Head(ctx, dir)
	if err != nil {
		return failed("reading the checkout's commit", err)
	}
	deployed, _ := s.Built.Built(ctx, deployedMark(rig))
	if deployed == head {
		return nil
	}
	tries := s.deployTries(ctx, rig, head)
	if deployed == "" && !advanced.Moved && tries == 0 {
		// First look at a rig that is level: whether its live site is behind is
		// not known, and a deploy held or run by hand is his, so the mark is set
		// to what is here and nothing is run.
		note := fmt.Sprintf("%s is level at %s with no deployed mark: the mark is set to it without deploying; run mw after-landing %s if the live site is behind", words, updatedRevision(head), rig)
		if err := s.Built.MarkBuilt(ctx, deployedMark(rig), head); err != nil {
			note += " (it could not be remembered, so this will be said again: " + firstLine(err.Error()) + ")"
		}
		return []string{note}
	}
	if tries >= deployTriesAtACommit {
		return nil
	}

	var out strings.Builder
	run := AfterLandingRun{AfterLanding: s.After, Slot: s.Slot, Rigs: s.Rigs, Host: s.Host, Out: &out, Built: s.Built, Checkout: s.Checkout}
	runErr := run.Run(ctx, rig)
	moved := fmt.Sprintf("%s %s → %s", words, updatedRevision(before), updatedRevision(head))
	if !advanced.Moved {
		moved = fmt.Sprintf("%s at %s, not yet deployed", words, updatedRevision(head))
	}
	said := strings.TrimSpace(out.String())
	if runErr != nil {
		tries++
		kept := ""
		if err := s.Built.MarkBuilt(ctx, deployFailedMark(rig), fmt.Sprintf("%s %d", head, tries)); err != nil {
			kept = " (the try could not be remembered, so it may be tried again: " + firstLine(err.Error()) + ")"
		}
		failedWords, next := "failed", "the next tick tries once more"
		if tries >= deployTriesAtACommit {
			failedWords, next = "failed again", "it is not tried again until the checkout moves"
		}
		return []string{fmt.Sprintf("%s, the home's deploy %s: %s; %s; run mw after-landing %s by hand%s",
			moved, failedWords, firstLine(runErr.Error()), next, rig, kept)}
	}
	return []string{fmt.Sprintf("%s, deployed by the home: %s", moved, strings.ReplaceAll(said, "\n", " / "))}
}

// Look is the dry form of a tick's update: it moves, fetches and runs nothing,
// and says for each rig this host names an [after_landing] command for — the
// factory rig included — whether its checkout is level with origin's main, as the
// last fetch saw it. A rig other than the factory rig on a host that is not home
// is said to be the home's to deploy.
func (s SelfUpdate) Look(ctx context.Context) []string {
	if s.Checkout == nil || s.After == nil {
		return nil
	}
	names := s.afterLandingRigs()
	if s.Rigs[FactoryRig] != "" && s.After.Command(FactoryRig) != "" {
		names = append([]string{FactoryRig}, names...)
	}
	home := true
	if s.Home != nil {
		if is, err := IsHome(ctx, s.Home, s.Host); err == nil {
			home = is
		}
	}
	var notes []string
	for _, rig := range names {
		dir := s.Rigs[rig]
		head, herr := s.Checkout.Head(ctx, dir)
		tip, terr := s.Checkout.Tip(ctx, dir, DefaultRemote, FactoryBranch)
		var note string
		switch {
		case herr != nil || terr != nil || tip == "":
			note = "could not be compared with origin's " + FactoryBranch
		case head == tip:
			note = fmt.Sprintf("is level with origin's %s at %s", FactoryBranch, updatedRevision(head))
		default:
			note = fmt.Sprintf("is behind origin's %s (or has commits of its own): %s → %s", FactoryBranch, updatedRevision(head), updatedRevision(tip))
		}
		note = fmt.Sprintf("self-update: %s %s (as last fetched)", rig, note)
		if rig != FactoryRig && !home {
			note += "; this host is not home, so the home deploys it"
		}
		notes = append(notes, note)
	}
	return notes
}
