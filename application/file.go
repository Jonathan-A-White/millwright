package application

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"sort"
	"strings"

	"github.com/Jonathan-A-White/millwright/domain"
)

// File files one of the Mayor's plans: it reads the whole plan before it writes
// any of it, files the epic and its stories through the work tracker, prints
// the tree it filed, and releases the stories only if the plan is approved.
//
// Nothing is written until the whole plan is filable. A plan with one story
// that has no path, or stories that wait on each other, leaves the tracker
// exactly as it was found — half a plan in the tracker is worse than none,
// because the half that is there looks like work somebody meant.
//
// Every story is filed held, and only approval releases it. That is the point
// of the command: filing is cheap and reversible, dispatching is neither, and
// the gap between them is where the Governor says yes.
type File struct {
	Tracker WorkTracker

	// Rules is where the plan's rig's requirements of its epics are read from.
	// A nil Rules checks nothing.
	Rules EpicRules

	// Rigs are the rigs checked out on this host, by name, and RigFiles says
	// whether a path is in one: a story whose files are in none of its rig is
	// warned of, as filed into the wrong rig (mw-gq6.428). A nil RigFiles, or a
	// rig not in Rigs, warns of nothing. RigFiles also counts the files of a
	// directory a story's files key names, to refuse a crowded one as mw path
	// set does (mw-gq6.442).
	Rigs     map[string]string
	RigFiles RigFiles

	// Waive is the Governor's word that some of what the rig requires is
	// waived for this epic. The zero value waives nothing.
	Waive EpicWaiver

	// GuestAsk is what the owner of a guest rig said when they asked for the
	// plan, quoted. A plan that works in a guest rig is refused without it, and
	// it is written on the epic with it.
	GuestAsk string

	// Contacts, when set, is his address book: a story filed with the
	// hitl:todo label is a To do card, and a name-only 'Text: <name> | <draft>'
	// line in its description is given the name's one mobile number
	// (TextNumbers). Nil files every description as written.
	Contacts ContactBook

	// Out is where the tree is printed. A nil Out prints nothing.
	Out io.Writer

	// GristApps are the apps the config's [grist-apps] table names on this host,
	// by app, to refuse an acceptance criterion that runs mw grist eval or smoke
	// for any other (mw-ehxr4b). A nil GristApps checks nothing.
	GristApps map[string]string

	// Err is where the warnings go: a story with more test callers than its size
	// allows (mw-gq6.661). A nil Err prints none.
	Err io.Writer

	// Approve is asked, once the tree has been printed, whether the stories may
	// be released. A nil Approve leaves them held — which is what an unattended
	// run does, since nobody is there to say yes.
	Approve func(context.Context, FiledPlan) (bool, error)
}

// FiledPlan is what came of filing a plan: the ids the tracker gave the epic
// and its stories, each story's Path and what it waits on, and whether they
// were released.
type FiledPlan struct {
	EpicID   string
	Title    string
	Defaults domain.Path
	Stories  []FiledStory
	// Epics are the epics filed directly under this one, which a plan read back
	// from the tracker may have and a plan just filed never does. They are not
	// stories: nothing here releases them, and the tree names them as epics.
	Epics    []FiledEpic
	Released bool
}

// FiledEpic is an epic found under a filed epic: its id, what it is called, and
// the word the tree uses for what the tracker says it is (see FiledStory.State).
// Its own stories are not read.
type FiledEpic struct {
	ID    string
	Title string
	State string
}

// FiledStory is one story as it was filed: the key it had in the plan, the id
// it has now, the Path it is worked by, the ids it still waits on, and what the
// tracker says it is now. A story just filed is held; a story read back later
// may be anything.
type FiledStory struct {
	ID              string
	Key             string
	Title           string
	Path            domain.Path
	EstimateMinutes int
	Needs           []string
	// State is the word the tree uses for what the tracker says this story is:
	// held, open, in progress or closed. An empty State reads as held, which is
	// what a story that has only just been filed is.
	State string
	// Iced says the bead is in the icebox: a release leaves it held.
	Iced bool
	// InFlight are the stories already open or running on this story's rig whose
	// files its own overlap (or, a story that names no files, the running ones),
	// as they were when it was filed. They are told, never acted on: the
	// dependency is the Mayor's to add.
	InFlight []InFlight
}

// InFlight is a story already open or running that a planned story's files
// overlap: its id and title, and the path they share, empty when the planned
// story names no files and only the rig is shared.
type InFlight struct {
	ID    string
	Title string
	Share string
}

// line is the in-flight story as the tree says it.
func (i InFlight) line() string {
	share := "same rig, no files named"
	if i.Share != "" {
		share = "shares " + i.Share
	}
	return fmt.Sprintf("in flight: %s %s (%s)", i.ID, i.Title, share)
}

// inFlightFor finds the stories of the work in hand that a story worked by this
// path (and these files) overlaps, in the order the tracker reported them,
// running before open. A story that names files overlaps the stories on its rig
// whose files meet its own; one that names none overlaps the running stories on
// its rig, since nothing says what it touches.
func inFlightFor(work WorkInHand, path domain.Path) []InFlight {
	ours := domain.FileList(path.Files)
	var found []InFlight
	// A story queued for its batch that is open and claimed has its branch yet
	// to land, as one in progress has (mw-bo813m2).
	running := slices.Clone(work.Running)
	for _, detail := range work.OpenClaimed {
		if QueuedForBatch(detail) {
			running = append(running, detail)
		}
	}
	lists := [][]StoryDetail{running, work.Ready}
	if len(ours) == 0 {
		lists = lists[:1]
	}
	for _, list := range lists {
		for _, detail := range list {
			theirs := detail.Merged()
			if theirs.Rig != path.Rig {
				continue
			}
			flight := InFlight{ID: detail.Story.ID, Title: detail.Story.Title}
			if len(ours) > 0 {
				file, shared := domain.SharedFile(ours, domain.FileList(theirs.Files))
				if !shared {
					continue
				}
				flight.Share = file
			}
			found = append(found, flight)
		}
	}
	return found
}

// state is what the tree calls this story, defaulting to held.
func (s FiledStory) state() string {
	if s.State == "" {
		return StateHeld
	}
	return s.State
}

// The words a tree uses for what the tracker says a story is. They are the
// factory's words, not beads': a story beads has deferred is one the factory is
// holding until somebody approves it.
const (
	StateHeld       = "held"
	StateOpen       = "open"
	StateInProgress = "in progress"
	StateClosed     = "closed"
)

// StateOf is the word a tree uses for a status the tracker reported. A status
// the factory does not know is printed as it came, rather than guessed at.
func StateOf(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "":
		return StateHeld
	case StatusHeld:
		return StateHeld
	case StatusOpen:
		return StateOpen
	case StatusInProgress:
		return StateInProgress
	case StatusClosed:
		return StateClosed
	}
	return status
}

// Run files the plan and reports what it filed.
func (f File) Run(ctx context.Context, plan domain.Plan) (FiledPlan, error) {
	if f.Tracker == nil {
		return FiledPlan{}, fmt.Errorf("filing a plan: there is no work tracker to file it in")
	}
	reasons, guests, rules, err := f.refusals(ctx, plan)
	if len(reasons) > 0 {
		return FiledPlan{}, &domain.PlanRefused{Reasons: reasons}
	}
	if err != nil {
		return FiledPlan{}, err
	}
	order, err := plan.Order()
	if err != nil {
		return FiledPlan{}, err
	}

	// What is in flight is read before anything is filed, so that the plan's own
	// stories are never among it. Failing to read it refuses nothing.
	work, workErr := f.Tracker.WorkInHand(ctx)

	epicID, err := f.Tracker.CreateEpic(ctx, NewEpic{
		Title:           plan.Epic.Title,
		Description:     plan.Epic.Description,
		SuccessCriteria: plan.Epic.Success,
		Priority:        plan.Epic.Priority,
		Defaults:        plan.Epic.Defaults,
	})
	if err != nil {
		return FiledPlan{}, fmt.Errorf("filing the epic %q: %w", plan.Epic.Title, err)
	}
	filed := FiledPlan{EpicID: epicID, Title: plan.Epic.Title, Defaults: plan.Epic.Defaults}

	// The stories go in the order nothing comes before what it waits on, so
	// that a story can name what it waits on by the id it already has.
	ids := make(map[string]string, len(order))
	var ownDemos []string
	for _, story := range order {
		path, err := story.PathFrom(plan.Epic.Defaults)
		if err != nil {
			return filed, fmt.Errorf("filing story %s under %s: %w", story.Key, epicID, err)
		}
		needs := make([]string, 0, len(story.Needs))
		for _, need := range story.Needs {
			needs = append(needs, ids[need])
		}

		description := story.Description
		if hasLabel(story.Labels, LabelHitlTodo) {
			description = TextNumbers{Book: f.Contacts, Say: f.Out}.Fill(ctx, description)
		}
		id, err := f.Tracker.CreateStory(ctx, NewStory{
			EpicID:          epicID,
			Title:           story.Title,
			Description:     description,
			Acceptance:      story.Acceptance,
			Priority:        story.Priority,
			EstimateMinutes: story.Estimate,
			Overrides:       story.PathOverrides(),
			Needs:           needs,
			Labels:          story.Labels,
		})
		if err != nil {
			return filed, fmt.Errorf("filing story %s under %s (the epic and %d of its stories are filed and held): %w",
				story.Key, epicID, len(filed.Stories), err)
		}
		var inFlight []InFlight
		if workErr == nil {
			inFlight = inFlightFor(work, path)
		}
		ids[story.Key] = id
		if governorsDemo(story.Labels, story.PathOverrides().Files) && !hasHitlLabel(story.Labels) {
			ownDemos = append(ownDemos, id)
		}
		filed.Stories = append(filed.Stories, FiledStory{
			ID:              id,
			Key:             story.Key,
			Title:           story.Title,
			Path:            path,
			EstimateMinutes: story.Estimate,
			Needs:           needs,
			State:           StateHeld,
			InFlight:        inFlight,
		})
	}

	if err := f.recordGuestAsk(ctx, epicID, guests); err != nil {
		return filed, err
	}
	if len(f.Waive.Names) > 0 {
		if err := f.recordWaiver(ctx, epicID, rules); err != nil {
			return filed, err
		}
	}

	f.print(filed.Tree())
	for _, story := range filed.Stories {
		if wrongRigOf(f.Rigs, f.RigFiles, story.Path) {
			f.print(WrongRig{ID: story.ID, Rig: strings.TrimSpace(story.Path.Rig)}.Line() + "\n")
		}
	}
	// Warnings only: the stories are filed, and released, all the same.
	f.print(sizeWarnings(filed.Stories, true, false))
	// Warnings only here: it is mw file --check that fails for them.
	f.testCallers(plan, func(key string) string { return ids[key] })
	f.filesNamed(plan, func(key string) string { return ids[key] })
	f.warnSmokeWithoutRef(plan, func(key string) string { return ids[key] })
	f.warnRemoteShell(plan, func(key string) string { return ids[key] })
	for _, id := range ownDemos {
		f.print(demoNoHitlLine(id) + "\n")
	}
	if workErr != nil {
		f.print(fmt.Sprintf("\nWhat is in flight on the rig could not be read, so no story above says what it overlaps: %v\n", workErr))
	}
	if len(f.Waive.Names) > 0 {
		f.print(fmt.Sprintf("\nWaived for %s: %s. The Governor's word is on the epic.\n", epicID, strings.Join(f.Waive.Names, ", ")))
	}

	if f.Approve == nil {
		f.print(filed.holdings())
		return filed, nil
	}
	approved, err := f.Approve(ctx, filed)
	if err != nil {
		f.print(filed.holdings())
		return filed, fmt.Errorf("the plan is filed and held under %s, but nobody said whether to release it: %w", epicID, err)
	}
	if !approved {
		f.print(filed.holdings())
		return filed, nil
	}

	for _, story := range filed.Stories {
		if err := f.Tracker.ReleaseStory(ctx, story.ID); err != nil {
			return filed, fmt.Errorf("releasing story %s of %s: %w", story.ID, epicID, err)
		}
	}
	filed.Released = true
	f.print(filed.releases())
	f.print(sizeWarnings(filed.Stories, false, true))
	return filed, nil
}

// Check reads a plan as Run does and files none of it: it says every reason
// the plan cannot be filed, across every story, in one *domain.PlanRefused, or
// prints how many stories would be filed. Only a reading that fails (the
// tracker or the rig's file cannot be read) stops it short of the rest.
func (f File) Check(ctx context.Context, plan domain.Plan) error {
	if f.Tracker == nil {
		return fmt.Errorf("checking a plan: there is no work tracker to check it against")
	}
	reasons, _, _, err := f.refusals(ctx, plan)
	if len(reasons) > 0 {
		return &domain.PlanRefused{Reasons: reasons}
	}
	if err != nil {
		return err
	}
	f.warnSmokeWithoutRef(plan, func(key string) string { return key })
	f.warnRemoteShell(plan, func(key string) string { return key })
	f.filesNamed(plan, func(key string) string { return key })
	if over := f.testCallers(plan, func(key string) string { return key }); len(over) > 0 {
		return fmt.Errorf("%s: more test callers than the size allows: split the story, size it up, or say why not with \"callers_ok\" (mw-gq6.661)",
			strings.Join(over, ", "))
	}
	f.print(fmt.Sprintf("ok: %d stories would be filed\n", len(plan.Stories)))
	return nil
}

// testCallers counts the test callers of each story of the plan that names files
// and a size, says it and warns of the ones over their size's allowance (see
// TestCallers), and returns the labels of the ones over that do not say
// "callers_ok". label names a story by its plan key.
func (f File) testCallers(plan domain.Plan, label func(key string) string) []string {
	var over []string
	for _, story := range plan.Stories {
		path, err := story.PathFrom(plan.Epic.Defaults)
		if err != nil {
			continue
		}
		callers, ok := testCallersOf(f.Rigs, f.RigFiles, label(story.Key), path)
		if !ok {
			continue
		}
		if callerNotes(f.Out, f.Err, callers) {
			over = append(over, callers.Label)
		}
	}
	return over
}

// filesNamed counts the files each story of the plan that names files and a size
// names, and warns of the ones over their size's allowance (see FilesNamed). A
// warning only, in Run and in Check. label names a story by its plan key.
func (f File) filesNamed(plan domain.Plan, label func(key string) string) {
	for _, story := range plan.Stories {
		path, err := story.PathFrom(plan.Epic.Defaults)
		if err != nil {
			continue
		}
		if named, ok := filesNamedOf(f.Rigs, f.RigFiles, label(story.Key), path); ok {
			filesNamedNote(f.Err, named)
		}
	}
}

// refusals is every reason the plan cannot be filed, one check after another and
// none stopping the rest: what Run and Check both ask, so that a plan Check
// passes is a plan Run does not refuse. It also returns the guest rigs and the
// rig's requirements Run goes on to record. The error is a reading that failed;
// the checks that did not need it have still been made.
func (f File) refusals(ctx context.Context, plan domain.Plan) ([]string, []guestRig, domain.EpicRequirements, error) {
	var reasons []string
	var readErr error
	note := func(err error) {
		var refused *domain.PlanRefused
		switch {
		case err == nil:
		case errors.As(err, &refused):
			reasons = append(reasons, refused.Reasons...)
		case readErr == nil:
			readErr = err
		}
	}

	note(plan.Validate())
	reasons = append(reasons, f.validateFormulasOf(ctx, plan, &readErr)...)
	reasons = append(reasons, f.validateFiles(plan.Stories, plan.Epic.Defaults)...)
	reasons = append(reasons, validateTesterStories(plan.Stories, plan.Epic.Defaults)...)
	reasons = append(reasons, f.validateGristApps(plan.Stories)...)

	guests, err := f.guestRigsOf(ctx, plan)
	note(err)
	note(f.checkGuests(guests))
	rules, err := f.requirementsOf(ctx, plan)
	note(err)
	return reasons, guests, rules, readErr
}

// FormulaTitles is the port by which a tracker says what a formula's step
// titles are, as the formula's file words them: {{story}} and {{title}} still
// in them. A tracker that is not one cannot be asked, and the title check that
// uses it is skipped (mw-gq6.244).
type FormulaTitles interface {
	FormulaStepTitles(ctx context.Context, formula string) ([]string, error)
}

// validateFormulasOf says, naming the story key, each story whose path names a
// formula this tracker has not installed, as plan.Validate does for the rest of
// a path. It also says each story whose title, with what its formula's step
// titles add to it at the pour, is over the tracker's limit: such a story is
// filed fine and then can never be dispatched (mw-gq6.244). The tracker is asked
// only when some story in the plan names a formula at all, so a plan that never
// mentions one costs nothing extra. A tracker that cannot be asked puts its
// error in readErr.
func (f File) validateFormulasOf(ctx context.Context, plan domain.Plan, readErr *error) []string {
	var needsCheck bool
	for _, story := range plan.Stories {
		path, err := story.PathFrom(plan.Epic.Defaults)
		if err == nil && path.Formula != "" {
			needsCheck = true
			break
		}
	}
	if !needsCheck {
		return nil
	}

	installed, err := f.Tracker.Formulas(ctx)
	if err != nil {
		if *readErr == nil {
			*readErr = fmt.Errorf("checking which formulas are installed: %w", err)
		}
		return nil
	}
	var reasons []string
	overheads := map[string]int{}
	for _, story := range plan.Stories {
		path, err := story.PathFrom(plan.Epic.Defaults)
		if err != nil {
			continue
		}
		if err := path.ValidateFormula(installed); err != nil {
			reasons = append(reasons, fmt.Sprintf("filing story %s: %s", story.Key, err))
			continue
		}
		reason, err := f.validateStepTitles(ctx, story, path.Formula, overheads)
		if err != nil {
			if *readErr == nil {
				*readErr = err
			}
			continue
		}
		if reason != "" {
			reasons = append(reasons, reason)
		}
	}
	return reasons
}

// validateTesterStories says, naming the story key, each Tester story in the
// plan (labelled tester, or worked by the tester formula) that has no 'Landed
// story:' line: such a story runs its whole session and then cannot file a
// finding (mw-gq6.594). A story titled as the Tester's are (TesterTitlePrefix)
// is one too, whatever its label and formula say: mw-5r3p30.300 reached its
// close-out that way (mw-gq6.797).
func validateTesterStories(stories []domain.PlanStory, defaults domain.Path) []string {
	var reasons []string
	for _, story := range stories {
		path, err := story.PathFrom(defaults)
		if err != nil {
			continue
		}
		detail := StoryDetail{Description: story.Description, Labels: story.Labels}
		detail.Story.Overrides = path
		titled := strings.HasPrefix(strings.TrimSpace(story.Title), strings.TrimSpace(TesterTitlePrefix)+" ")
		if NamesNoLandedStory(detail) || titled && TesterLanded(detail) == "" {
			reasons = append(reasons, fmt.Sprintf("filing story %s: it is a Tester story and its description has no %q line, so its session could not file a finding; add the line",
				story.Key, TesterLandedLine+"<id>"))
		}
	}
	return reasons
}

// gristRun is one `mw grist eval|smoke <app>` found in an acceptance criterion:
// the verb, the app it names and whether the same line passes --ref.
type gristRun struct {
	Verb, App string
	Ref       bool
}

// proseWords are English words that follow "mw grist eval" or "smoke" in prose
// ("eval or smoke passes", "smoke the app"): no app is named so, and an app name
// is never one of them (mw-snbavi).
var proseWords = map[string]bool{
	"or": true, "and": true, "on": true, "the": true, "a": true, "an": true,
	"to": true, "for": true, "in": true, "of": true, "with": true, "is": true,
	"are": true, "passes": true, "pass": true, "runs": true, "run": true,
}

// gristRunsIn finds each `mw grist eval <app>` and `mw grist smoke <app>` in the
// text, line by line. An app is the word after the verb, unless that is a flag,
// a placeholder or a word of prose (proseWords): then the line names no app.
func gristRunsIn(text string) []gristRun {
	var runs []gristRun
	for _, line := range strings.Split(text, "\n") {
		words := strings.Fields(line)
		for i := 0; i+3 < len(words); i++ {
			if strings.Trim(words[i], "`'\"") != "mw" || words[i+1] != "grist" {
				continue
			}
			verb := words[i+2]
			if verb != "eval" && verb != "smoke" {
				continue
			}
			app := strings.Trim(words[i+3], "`'\".,;:()")
			if app == "" || strings.HasPrefix(app, "-") || strings.HasPrefix(app, "<") || proseWords[strings.ToLower(app)] {
				continue
			}
			run := gristRun{Verb: verb, App: app}
			for _, w := range words[i+3:] {
				if w == "--ref" || strings.HasPrefix(w, "--ref=") {
					run.Ref = true
				}
			}
			runs = append(runs, run)
		}
	}
	return runs
}

// validateGristApps says, naming the story key, each story whose acceptance
// runs mw grist eval or smoke for an app the config's [grist-apps] does not name
// (mw-ehxr4b): that criterion could not be run on this host.
func (f File) validateGristApps(stories []domain.PlanStory) []string {
	if f.GristApps == nil {
		return nil
	}
	var have []string
	for app := range f.GristApps {
		have = append(have, app)
	}
	sort.Strings(have)
	var reasons []string
	for _, story := range stories {
		seen := map[string]bool{}
		for _, run := range gristRunsIn(story.Acceptance) {
			if _, ok := f.GristApps[run.App]; ok || seen[run.App] {
				continue
			}
			seen[run.App] = true
			reasons = append(reasons, fmt.Sprintf("filing story %s: its acceptance runs mw grist %s %s, and %s is not in the [grist-apps] of the config; the apps it has are %s",
				story.Key, run.Verb, run.App, run.App, strings.Join(have, ", ")))
		}
	}
	return reasons
}

// SmokeReadsMainWarning is the warning for an acceptance that runs mw grist
// smoke with no --ref: smoke reads the rig's main, so it cannot prove a branch.
const SmokeReadsMainWarning = "smoke reads main; to prove a branch, use mw grist eval <app> <kind> --example <name> --ref <worktree>"

// warnSmokeWithoutRef warns of each story whose acceptance runs mw grist smoke
// with no --ref. It refuses nothing. label names a story by its plan key.
func (f File) warnSmokeWithoutRef(plan domain.Plan, label func(key string) string) {
	if f.Err == nil {
		return
	}
	for _, story := range plan.Stories {
		for _, run := range gristRunsIn(story.Acceptance) {
			if run.Verb == "smoke" && !run.Ref {
				fmt.Fprintf(f.Err, "WARN: %s: its acceptance runs mw grist smoke %s with no --ref: %s\n", label(story.Key), run.App, SmokeReadsMainWarning)
				break
			}
		}
	}
}

// RemoteShellWarning is the warning for a story whose acceptance or description
// needs a shell on another host: a Builder's remote shell is refused, so that
// half is a hands step (mw-gq6.796).
const RemoteShellWarning = "split that half into a hands step (mw hands): a Builder cannot take a shell on another host"

// sshFlagsWithArg are the ssh options that take the next word as their value.
var sshFlagsWithArg = map[string]bool{"-i": true, "-p": true, "-o": true, "-l": true, "-F": true, "-J": true}

// remoteShellIn names what in the text needs a shell on another host: ssh to a
// user@host, a dotted host name or the vps, certbot, or /etc/nginx. It is ""
// for text that has none.
func remoteShellIn(text string) string {
	if strings.Contains(text, "/etc/nginx") {
		return "/etc/nginx"
	}
	for _, line := range strings.Split(text, "\n") {
		words := strings.Fields(line)
		for i, w := range words {
			switch strings.ToLower(strings.Trim(w, "`'\"*.,;:()")) {
			case "certbot":
				return "certbot"
			case "ssh":
			default:
				continue
			}
			for j := i + 1; j < len(words); j++ {
				arg := strings.Trim(words[j], "`'\"*,;:()")
				if strings.HasPrefix(arg, "-") {
					if sshFlagsWithArg[arg] {
						j++
					}
					continue
				}
				if strings.Contains(arg, "@") || strings.Contains(strings.TrimRight(arg, "."), ".") || strings.EqualFold(arg, "vps") {
					return "ssh " + arg
				}
				break
			}
		}
	}
	return ""
}

// warnRemoteShell warns of each story whose acceptance or description needs a
// shell on another host (see remoteShellIn). It refuses nothing. label names a
// story by its plan key.
func (f File) warnRemoteShell(plan domain.Plan, label func(key string) string) {
	if f.Err == nil {
		return
	}
	for _, story := range plan.Stories {
		for _, text := range []string{story.Acceptance, story.Description} {
			if what := remoteShellIn(text); what != "" {
				fmt.Fprintf(f.Err, "WARN: %s: it names %s, which needs a shell on another host: %s\n", label(story.Key), what, RemoteShellWarning)
				break
			}
		}
	}
}

// validateFiles says, naming the story key, each story whose files key names a
// crowded directory, as mw path set refuses it (mw-gq6.442).
func (f File) validateFiles(stories []domain.PlanStory, defaults domain.Path) []string {
	var reasons []string
	for _, story := range stories {
		path, err := story.PathFrom(defaults)
		if err != nil {
			continue
		}
		if err := crowdedFiles(f.Rigs, f.RigFiles, path); err != nil {
			reasons = append(reasons, fmt.Sprintf("filing story %s: %s", story.Key, err))
		}
	}
	return reasons
}

// validateStepTitles says why a story is refused whose title, plus the most its
// formula's step titles put before it, is over domain.MaxTitleLength, or "" when
// it fits. The titles of a formula are read once however many stories name it.
func (f File) validateStepTitles(ctx context.Context, story domain.PlanStory, formula string, overheads map[string]int) (string, error) {
	reader, ok := f.Tracker.(FormulaTitles)
	if !ok || formula == "" {
		return "", nil
	}
	overhead, known := overheads[formula]
	if !known {
		titles, err := reader.FormulaStepTitles(ctx, formula)
		if err != nil {
			return "", fmt.Errorf("reading the step titles of the formula %s: %w", formula, err)
		}
		overhead = domain.StepTitleOverhead(titles)
		overheads[formula] = overhead
	}
	if n := len(story.Title); n+overhead > domain.MaxTitleLength {
		return fmt.Sprintf("filing story %s: its title is %d bytes, and pouring the formula %s adds up to %d to it in a step title, "+
			"%d in all, and the tracker takes at most %d: shorten the title by %d",
			story.Key, n, formula, overhead, n+overhead, domain.MaxTitleLength, n+overhead-domain.MaxTitleLength), nil
	}
	return "", nil
}

// print writes one block of the report, when there is somewhere to write it.
func (f File) print(block string) {
	if f.Out == nil {
		return
	}
	fmt.Fprint(f.Out, block)
}

// Tree is the plan as a person reads it: the epic and the default Path its
// stories inherit, then every story with the Path it is worked by, what it
// still waits on, and what the tracker says it is. It is printed by the command
// that files a plan and by the one that releases it later, so that the two show
// the same thing.
func (p FiledPlan) Tree() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s · %s\n", p.EpicID, p.Title)
	fmt.Fprintf(&b, "    default path %s\n\n", p.Defaults.Summary())

	for _, story := range p.Stories {
		b.WriteString(story.tree())
	}
	for _, epic := range p.Epics {
		fmt.Fprintf(&b, "\n  %s · %s\n", epic.ID, epic.Title)
		fmt.Fprintf(&b, "      an epic, %s: its stories are not shown here, mw show %s prints them\n", epic.State, epic.ID)
	}
	return b.String()
}

// tree is the story's entry in a plan's tree: its title, its path, and what it
// is, how long it is thought to take and what it waits on.
func (s FiledStory) tree() string {
	var b strings.Builder
	fmt.Fprintf(&b, "  %s · %s\n", s.ID, s.Title)
	fmt.Fprintf(&b, "      path %s\n", s.Path.Summary())

	state := []string{s.state()}
	if s.EstimateMinutes > 0 {
		state = append(state, fmt.Sprintf("%dm", s.EstimateMinutes))
	}
	if len(s.Needs) == 0 {
		state = append(state, "waits on nothing")
	} else {
		state = append(state, "waits on "+strings.Join(s.Needs, ", "))
	}
	fmt.Fprintf(&b, "      %s\n", strings.Join(state, " · "))
	for _, flight := range s.InFlight {
		fmt.Fprintf(&b, "      %s\n", flight.line())
		fmt.Fprintf(&b, "        bd dep add %s %s\n", s.ID, flight.ID)
	}
	return b.String()
}

// Unblocked reports the ids of the stories that wait on nothing: the ones a
// release makes ready straight away.
func (p FiledPlan) Unblocked() []string {
	var free []string
	for _, story := range p.Stories {
		if len(story.Needs) == 0 {
			free = append(free, story.ID)
		}
	}
	return free
}

// holdings is what a person is told about a plan nobody released: that none of
// it can be dispatched, and how to release it without filing it twice.
func (p FiledPlan) holdings() string {
	return fmt.Sprintf("\nAll %d stories of %s are held: nothing here can be dispatched.\n"+
		"Filing this plan again would file a second copy of it, so when it is approved, release this one:\n"+
		"  mw release %s\n", len(p.Stories), p.EpicID, p.EpicID)
}

// releases is what a person is told about a plan that was approved: what was
// released, and what a dispatcher can take right now.
func (p FiledPlan) releases() string {
	ready := p.Unblocked()
	if len(ready) == 0 {
		return fmt.Sprintf("\nReleased all %d stories of %s. None of them is ready: they all wait on another.\n",
			len(p.Stories), p.EpicID)
	}
	return fmt.Sprintf("\nReleased all %d stories of %s. Ready now: %s.\n",
		len(p.Stories), p.EpicID, strings.Join(ready, ", "))
}
