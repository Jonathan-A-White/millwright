package application

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
)

// What mw pause and mw resume name and keep (mw-gq6.707). The pause is the
// Governor's word: the factory stops claiming and every Builder in hand checks
// its work in to its branch, then waits; the resume undoes exactly that.
const (
	// PauseProtectedSession is the tmux session the seats' windows live in. No
	// story's session is ever named it, and neither command types into it or
	// closes it: a pause stops Builders, never the Mayor or the Deputy.
	PauseProtectedSession = "mw-seats"
	// PausedFileName is the paused set, in plans/pause-<date>/ of the vault.
	PausedFileName = "paused.json"
	// PauseCurrentKey is the note that names the plans directory of the pause
	// in force, empty while the factory is not paused.
	PauseCurrentKey = "pause.current"
	// PauseOpenEnd is how long the hold note lasts when a pause names no time
	// and none can be computed: a week, the longest a limit lasts. mw resume
	// ends it sooner.
	PauseOpenEnd = 7 * 24 * time.Hour
	// PauseWait is how long a pause waits on the Builders to push their
	// branches before it commits and pushes for them.
	PauseWait = 10 * time.Minute
	// PausePoll is how often the Builders are looked at while it waits.
	PausePoll = 5 * time.Second
	// PausedComment opens the comment a Builder writes on its story once its
	// work is checked in.
	PausedComment = "PAUSED"
	// pauseSaid opens the words the hold note carries, so that a note a real
	// usage limit wrote is told from this one.
	pauseSaid = "mw pause: "
)

// PauseNightPasses are the follower's night jobs a pause turns off and a resume
// turns back on: the sleep pass and the To do rank. Each asks whether the
// factory is paused before it begins (mw-gq6.710).
var PauseNightPasses = []string{SleepJobName, TodoRankJobName}

// FactoryPausedByNote reads whether a pause is in force from the note PauseBook keeps.
func FactoryPausedByNote(notes interface {
	Note(ctx context.Context, key string) (string, error)
}) func(ctx context.Context) (bool, error) {
	return func(ctx context.Context) (bool, error) {
		dir, err := notes.Note(ctx, PauseCurrentKey)
		return strings.TrimSpace(dir) != "", err
	}
}

// pausedWhy is why a night pass is not made while the factory is paused, "" when
// it is not (or paused is nil). A pause that cannot be read stops the pass too.
func pausedWhy(ctx context.Context, paused func(ctx context.Context) (bool, error)) string {
	if paused == nil {
		return ""
	}
	held, err := paused(ctx)
	if err != nil {
		return "not made: reading whether the factory is paused failed: " + firstLine(err.Error())
	}
	if held {
		return "not made: the factory is paused (mw pause); mw resume turns the pass back on"
	}
	return ""
}

// PauseDir is where a pause begun on day keeps its paused set, from the
// vault's root.
func PauseDir(day time.Time) string { return "plans/pause-" + day.UTC().Format("2006-01-02") }

// PauseTimerUnit is the systemd user timer that resumes a pause begun on day.
func PauseTimerUnit(day time.Time) string { return "mw-resume-" + day.UTC().Format("2006-01-02") }

// PauseLine is what is typed into a running Builder's session.
func PauseLine(storyID, reason string) string {
	return fmt.Sprintf("PAUSE: the Governor has paused the factory (%s). Commit everything in your worktree, push your branch, "+
		"write one comment on %s that begins %s and names the branch and what is left, then stop and wait. "+
		"A line that begins RESUME will tell you to carry on.\n", pauseOneLine(reason), storyID, PausedComment)
}

// ResumeLine is what is typed into a Builder's session that is still alive
// when the pause ends.
func ResumeLine(storyID string) string {
	return fmt.Sprintf("RESUME: the pause is over. Carry on with %s from where you stopped.\n", storyID)
}

func pauseOneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// protectedWindow reports whether a session name is one a pause must not
// touch: the seats' tmux session, or a window of it.
func protectedWindow(name string) bool {
	return name == PauseProtectedSession || strings.HasPrefix(name, PauseProtectedSession+":") ||
		strings.HasPrefix(name, PauseProtectedSession+".")
}

// PausedStory is one story that was in hand when the factory was paused.
type PausedStory struct {
	Story  string `json:"story"`
	Host   string `json:"host"`
	Window string `json:"window"`
	Branch string `json:"branch"`
	// SHA is the commit the branch was at, and Pushed says the remote held it.
	SHA    string `json:"sha,omitempty"`
	Pushed bool   `json:"pushed"`
	// PausedComment says the story carries a comment that opens PAUSED; CommentBy
	// says who wrote it: the Builder, or mw when the Builder did not.
	PausedComment bool   `json:"paused_comment"`
	CommentBy     string `json:"comment_by,omitempty"`
	// Alive says the story's session was still running when the pause was
	// done, and Ended that the pause closed it (a cloud box's sessions).
	Alive bool `json:"alive"`
	Ended bool `json:"ended,omitempty"`
	// Note is what mw did itself, or why it could not.
	Note string `json:"note,omitempty"`
}

// HostPauseOrder is what one host is asked to do.
type HostPauseOrder struct {
	Reason string    `json:"reason"`
	Until  time.Time `json:"until"`
	// EndSessions closes each session once its branch is checked in: a cloud
	// box is about to go down.
	EndSessions bool          `json:"end_sessions,omitempty"`
	Wait        time.Duration `json:"wait"`
}

// HostPaused is what one host did.
type HostPaused struct {
	Host string `json:"host"`
	// Cap is the host's session cap as it stands, recorded and never changed.
	Cap       string        `json:"cap,omitempty"`
	HoldUntil time.Time     `json:"hold_until"`
	Stories   []PausedStory `json:"stories,omitempty"`
	Notes     []string      `json:"notes,omitempty"`
}

// HostResumeOrder is what one host is asked to undo: the stories the paused
// set holds for it.
type HostResumeOrder struct {
	Stories []PausedStory `json:"stories,omitempty"`
}

// HostResumed is what one host did.
type HostResumed struct {
	Host string `json:"host"`
	// Cleared says there was a hold note to clear.
	Cleared bool `json:"cleared"`
	// Told are the stories whose session was alive and got RESUME; Gone those
	// whose session was gone and want a retry.
	Told  []string `json:"told,omitempty"`
	Gone  []string `json:"gone,omitempty"`
	Notes []string `json:"notes,omitempty"`
}

// PauseStories is what a host's pause reads and writes of the tracker.
type PauseStories interface {
	RunningStories(ctx context.Context, host string) ([]StoryDetail, error)
	StoryComments(ctx context.Context, id string) ([]Comment, error)
	CommentOnStory(ctx context.Context, id, text string) error
}

// PauseGit is what a host's pause does to a story's worktree: the five calls
// the rig adapter already answers for landings and checkouts.
type PauseGit interface {
	// Head is the commit checked out in dir.
	Head(ctx context.Context, dir string) (string, error)
	// Tip is the commit branch is at on the remote, as the last fetch or push
	// saw it; an error when the remote has none.
	Tip(ctx context.Context, dir, remote, branch string) (string, error)
	// Uncommitted lists what dir has changed and not committed.
	Uncommitted(ctx context.Context, dir string) ([]string, error)
	// CommitLeftovers commits all of it and reports the commit.
	CommitLeftovers(ctx context.Context, dir, message string) (string, error)
	// Push publishes what dir has checked out as the remote's branch, never
	// forced.
	Push(ctx context.Context, dir, remote, branch string) error
}

// FleetHostPause is the pause and resume of one host, done on that host: it is
// what `mw pause --here` runs, on the home and over ssh on every other host.
type FleetHostPause struct {
	Host    string
	Notes   SweepNotes
	Stories PauseStories
	Runner  Runner
	Git     PauseGit
	// Rigs is where each rig is checked out here.
	Rigs map[string]string
	// Cap is the host's session cap, as recorded for the paused set.
	Cap string
	// Remote is the remote a branch is pushed to; empty reads origin.
	Remote string
	// Now and Sleep are the clock and the wait between looks; the zero values
	// are the real ones.
	Now   func() time.Time
	Sleep func(ctx context.Context, d time.Duration) error
	Poll  time.Duration
}

func (h FleetHostPause) now() time.Time {
	if h.Now != nil {
		return h.Now()
	}
	return time.Now()
}

func (h FleetHostPause) remote() string {
	if h.Remote != "" {
		return h.Remote
	}
	return DefaultRemote
}

func (h FleetHostPause) sleep(ctx context.Context, d time.Duration) error {
	if h.Sleep != nil {
		return h.Sleep(ctx, d)
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}

// Pause sets this host's hold note, types the PAUSE line into every running
// Builder's session, waits for each branch to be pushed and the story to say
// PAUSED, then commits, pushes and comments itself for any that did not. A
// session is closed afterwards only when the order says so.
func (h FleetHostPause) Pause(ctx context.Context, order HostPauseOrder) (HostPaused, error) {
	started := h.now()
	out := HostPaused{Host: h.Host, Cap: h.Cap}

	hold := HarnessHold{Reason: ReasonUsageLimit, Said: pauseSaid + pauseOneLine(order.Reason), Until: order.Until}
	if err := keepHarnessHold(ctx, h.Notes, h.Host, hold); err != nil {
		return out, fmt.Errorf("%s: the hold note could not be set: %w", h.Host, err)
	}
	out.HoldUntil = order.Until
	if kept, ok := readHarnessHold(ctx, h.Notes, h.Host); ok {
		out.HoldUntil = kept.Until
	}

	running, err := h.Stories.RunningStories(ctx, h.Host)
	if err != nil {
		return out, fmt.Errorf("%s: the stories in hand could not be read: %w", h.Host, err)
	}
	var live []*PausedStory
	for _, detail := range running {
		id := detail.Story.ID
		ps := PausedStory{Story: id, Host: h.Host, Window: SessionName(id), Branch: StoryBranch(id)}
		if protectedWindow(ps.Window) {
			out.Notes = append(out.Notes, fmt.Sprintf("%s: its session %q is the seats' own, so it was not touched", id, ps.Window))
			continue
		}
		status, serr := h.Runner.Status(ctx, ps.Window)
		ps.Alive = serr == nil && status.Running()
		if ps.Alive {
			if err := h.Runner.Send(ctx, ps.Window, PauseLine(id, order.Reason)); err != nil {
				ps.Note = fmt.Sprintf("the PAUSE line could not be typed: %v", err)
			}
		}
		out.Stories = append(out.Stories, ps)
	}
	for i := range out.Stories {
		live = append(live, &out.Stories[i])
	}

	dirs := map[string]string{}
	for _, detail := range running {
		dirs[detail.Story.ID] = WorktreeDir(h.Rigs[detail.Merged().Rig], detail.Story.ID)
	}

	// The wait is for the sessions typed to: the rest are done at once.
	deadline := started.Add(order.Wait)
	for {
		unsettled := false
		for _, ps := range live {
			h.look(ctx, ps, dirs[ps.Story], started)
			if ps.Alive && !(ps.Pushed && ps.PausedComment) {
				unsettled = true
			}
		}
		if !unsettled || !h.now().Before(deadline) {
			break
		}
		poll := h.Poll
		if poll <= 0 {
			poll = PausePoll
		}
		if err := h.sleep(ctx, poll); err != nil {
			break
		}
	}

	for _, ps := range live {
		h.settle(ctx, ps, dirs[ps.Story], order, started)
		if order.EndSessions && ps.Alive && ps.Pushed {
			if err := h.Runner.Close(ctx, ps.Window); err != nil {
				ps.Note = joinNote(ps.Note, fmt.Sprintf("its session could not be closed: %v", err))
			} else {
				ps.Ended = true
			}
		}
	}
	sort.SliceStable(out.Stories, func(i, j int) bool { return out.Stories[i].Story < out.Stories[j].Story })
	return out, nil
}

func joinNote(a, b string) string {
	if a == "" {
		return b
	}
	return a + "; " + b
}

// look reads where a story's branch stands: its commit, whether the remote
// holds it and whether the story carries a PAUSED comment from this pause.
func (h FleetHostPause) look(ctx context.Context, ps *PausedStory, dir string, since time.Time) {
	ps.SHA, ps.Pushed = "", false
	if sha, err := h.Git.Head(ctx, dir); err == nil {
		ps.SHA = strings.TrimSpace(sha)
	}
	if ps.SHA != "" {
		if tip, err := h.Git.Tip(ctx, dir, h.remote(), ps.Branch); err == nil && strings.TrimSpace(tip) == ps.SHA {
			if dirty, err := h.Git.Uncommitted(ctx, dir); err == nil && len(dirty) == 0 {
				ps.Pushed = true
			}
		}
	}
	if !ps.PausedComment {
		comments, err := h.Stories.StoryComments(ctx, ps.Story)
		if err == nil && pausedCommentIn(comments, since) {
			ps.PausedComment, ps.CommentBy = true, "builder"
		}
	}
}

// pausedCommentIn reports whether a comment opens PAUSED and is not older than
// since; a comment with no time is taken as new.
func pausedCommentIn(comments []Comment, since time.Time) bool {
	for _, c := range comments {
		if strings.HasPrefix(strings.TrimSpace(c.Text), PausedComment) && (c.Created.IsZero() || !c.Created.Before(since)) {
			return true
		}
	}
	return false
}

// settle does what a Builder that did not, did not: commit what is left in the
// worktree, push the branch and write the PAUSED comment.
func (h FleetHostPause) settle(ctx context.Context, ps *PausedStory, dir string, order HostPauseOrder, since time.Time) {
	if !ps.Pushed {
		if ps.SHA == "" {
			ps.Note = joinNote(ps.Note, "no worktree to save: "+dir)
		} else {
			var did []string
			if dirty, err := h.Git.Uncommitted(ctx, dir); err == nil && len(dirty) > 0 {
				if _, err := h.Git.CommitLeftovers(ctx, dir, "paused: the work in hand when mw pause ran ("+ps.Story+")"); err != nil {
					ps.Note = joinNote(ps.Note, fmt.Sprintf("its leftovers could not be committed: %v", err))
				} else {
					did = append(did, "committed the leftovers")
				}
			}
			if err := h.Git.Push(ctx, dir, h.remote(), ps.Branch); err != nil {
				ps.Note = joinNote(ps.Note, fmt.Sprintf("its branch could not be pushed: %v", err))
			} else {
				did = append(did, "pushed the branch")
			}
			if len(did) > 0 {
				ps.Note = joinNote(ps.Note, "mw "+strings.Join(did, " and ")+" itself")
			}
			h.look(ctx, ps, dir, since)
		}
	}
	if !ps.PausedComment {
		text := fmt.Sprintf("%s by mw pause (%s): branch %s at %s, pushed: %v. The Builder wrote no PAUSED comment of its own.",
			PausedComment, pauseOneLine(order.Reason), ps.Branch, pauseShortSHA(ps.SHA), ps.Pushed)
		if err := h.Stories.CommentOnStory(ctx, ps.Story, text); err != nil {
			ps.Note = joinNote(ps.Note, fmt.Sprintf("the PAUSED comment could not be written: %v", err))
		} else {
			ps.PausedComment, ps.CommentBy = true, "mw"
		}
	}
}

func pauseShortSHA(sha string) string {
	if sha == "" {
		return "no commit"
	}
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}

// Resume clears this host's hold note and types RESUME into each story's
// session that is still alive; the ones whose session is gone it reports, for
// a retry.
func (h FleetHostPause) Resume(ctx context.Context, order HostResumeOrder) (HostResumed, error) {
	out := HostResumed{Host: h.Host}
	if _, held := readHarnessHold(ctx, h.Notes, h.Host); held {
		if err := h.Notes.ClearNote(ctx, HarnessHoldKey(h.Host)); err != nil {
			return out, fmt.Errorf("%s: the hold note could not be cleared: %w", h.Host, err)
		}
		out.Cleared = true
	}
	for _, ps := range order.Stories {
		if protectedWindow(ps.Window) {
			out.Notes = append(out.Notes, fmt.Sprintf("%s: its session %q is the seats' own, so it was not touched", ps.Story, ps.Window))
			continue
		}
		status, err := h.Runner.Status(ctx, ps.Window)
		if err == nil && status.Running() {
			if err := h.Runner.Send(ctx, ps.Window, ResumeLine(ps.Story)); err != nil {
				out.Notes = append(out.Notes, fmt.Sprintf("%s: the RESUME line could not be typed: %v", ps.Story, err))
				continue
			}
			out.Told = append(out.Told, ps.Story)
			continue
		}
		out.Gone = append(out.Gone, ps.Story)
	}
	return out, nil
}

// PausedSet is what a pause leaves in the vault, and what a resume undoes.
type PausedSet struct {
	Date     string    `json:"date"`
	Reason   string    `json:"reason"`
	PausedAt time.Time `json:"paused_at"`
	// Until is when the hold notes lapse by themselves; ResumeAt is when the
	// timer resumes, zero when none was set (Timer is its unit).
	Until    time.Time `json:"until"`
	ResumeAt time.Time `json:"resume_at,omitzero"`
	Timer    string    `json:"timer,omitempty"`
	// MaxBoxes is cloud.max_boxes as it stood before the pause set it to 0;
	// MaxBoxesSet says it was set at all.
	MaxBoxes    string `json:"max_boxes,omitempty"`
	MaxBoxesSet bool   `json:"max_boxes_set,omitempty"`
	// Hosts are the hosts the pause reached, with the cap each had; Unreached
	// are those it did not, with why.
	Hosts []PausedSetHost `json:"hosts"`
	// NightPasses are the follower's night jobs the pause turned off (the sleep
	// pass, the To do rank); the resume turns them back on.
	NightPasses []string      `json:"night_passes,omitempty"`
	Unreached   []string      `json:"unreached,omitempty"`
	Boxes       []string      `json:"boxes_down,omitempty"`
	Stories     []PausedStory `json:"stories"`
	Bead        string        `json:"bead,omitempty"`
	Notes       []string      `json:"notes,omitempty"`
	ResumedAt   time.Time     `json:"resumed_at,omitzero"`
}

// PausedSetHost is one host of the paused set.
type PausedSetHost struct {
	Host string `json:"host"`
	Cap  string `json:"cap,omitempty"`
	// Cloud says the host is a cloud box.
	Cloud bool `json:"cloud,omitempty"`
}

// String is the set as a person reads it.
func (s PausedSet) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "paused %s: %s\n", s.PausedAt.UTC().Format(time.RFC3339), s.Reason)
	fmt.Fprintf(&b, "hold notes until %s", s.Until.UTC().Format(time.RFC3339))
	if s.Timer != "" {
		fmt.Fprintf(&b, "; resume timer %s at %s", s.Timer, s.ResumeAt.UTC().Format(time.RFC3339))
	}
	b.WriteString("\n")
	for _, h := range s.Hosts {
		fmt.Fprintf(&b, "host %s: cap %s kept", h.Host, orUnset(h.Cap))
		if h.Cloud {
			b.WriteString(", a cloud box")
		}
		b.WriteString("\n")
	}
	if s.MaxBoxesSet {
		fmt.Fprintf(&b, "cloud.max_boxes 0 (was %s)\n", s.MaxBoxes)
	}
	if len(s.NightPasses) > 0 {
		fmt.Fprintf(&b, "night passes off: %s\n", strings.Join(s.NightPasses, ", "))
	}
	for _, box := range s.Boxes {
		fmt.Fprintf(&b, "box %s brought down\n", box)
	}
	for _, host := range s.Unreached {
		fmt.Fprintf(&b, "NOT REACHED: %s\n", host)
	}
	if len(s.Stories) == 0 {
		b.WriteString("no story was in hand\n")
	}
	for _, ps := range s.Stories {
		fmt.Fprintf(&b, "story %s on %s: window %s, branch %s at %s, pushed %v, PAUSED comment %v",
			ps.Story, ps.Host, ps.Window, ps.Branch, pauseShortSHA(ps.SHA), ps.Pushed, ps.PausedComment)
		if ps.Ended {
			b.WriteString(", session ended")
		} else if ps.Alive {
			b.WriteString(", session kept idle")
		}
		if ps.Note != "" {
			b.WriteString(" (" + ps.Note + ")")
		}
		b.WriteString("\n")
	}
	for _, n := range s.Notes {
		b.WriteString("note: " + n + "\n")
	}
	return b.String()
}

func orUnset(s string) string {
	if s == "" {
		return "unset"
	}
	return s
}

// PauseFleet reaches each host's FleetHostPause: the home's own directly, every
// other by ssh.
type PauseFleet interface {
	// Hosts are the hosts to pause, the home first, the cloud boxes among them.
	Hosts(ctx context.Context) ([]string, error)
	Pause(ctx context.Context, host string, order HostPauseOrder) (HostPaused, error)
	Resume(ctx context.Context, host string, order HostResumeOrder) (HostResumed, error)
}

// PauseCloud is the cloud as a pause sees it.
type PauseCloud interface {
	// Boxes are the boxes up.
	Boxes(ctx context.Context) ([]string, error)
	// Down destroys one and says what it did.
	Down(ctx context.Context, box string) (string, error)
}

// PauseConfig is the home's cloud.max_boxes.
type PauseConfig interface {
	// MaxBoxes reads it; set is false when the config says none.
	MaxBoxes(ctx context.Context) (value string, set bool, err error)
	SetMaxBoxes(ctx context.Context, value string) error
}

// PauseBook keeps the paused set in the vault, and which one is in force.
type PauseBook interface {
	// Current is the plans directory of the pause in force, if any.
	Current(ctx context.Context) (dir string, ok bool, err error)
	SetCurrent(ctx context.Context, dir string) error
	ClearCurrent(ctx context.Context) error
	Read(ctx context.Context, dir string) (PausedSet, error)
	// Write keeps the set in dir/paused.json and commits it in the vault.
	Write(ctx context.Context, dir string, set PausedSet) error
}

// PauseTimer schedules the one-shot resume.
type PauseTimer interface {
	// Schedule runs `mw resume` at at, as the user timer unit, and says what it
	// made.
	Schedule(ctx context.Context, at time.Time, unit string) (string, error)
	Cancel(ctx context.Context, unit string) error
}

// PauseRetry is `mw retry` for one story.
type PauseRetry interface {
	Retry(ctx context.Context, story string) (string, error)
}

// PauseRequest is what mw pause was asked.
type PauseRequest struct {
	Reason string
	// Until, when set, is when the hold notes lapse and the timer resumes.
	Until  time.Time
	Bead   string
	DryRun bool
	Wait   time.Duration
}

// Pause is mw pause.
type Pause struct {
	Fleet   PauseFleet
	Cloud   PauseCloud
	Config  PauseConfig
	Book    PauseBook
	Timer   PauseTimer
	Mail    Mailbox
	Tracker interface {
		CommentOnStory(ctx context.Context, id, text string) error
	}
	// Plan gives the resume time when the request names none; nil leaves a
	// pause open-ended.
	Plan func(ctx context.Context) (ResumePlan, error)
	Host string
	Now  func() time.Time
	Out  io.Writer
}

func (p Pause) now() time.Time {
	if p.Now != nil {
		return p.Now()
	}
	return time.Now()
}

// Run pauses the factory, or says it already is.
func (p Pause) Run(ctx context.Context, req PauseRequest) (PausedSet, error) {
	if strings.TrimSpace(req.Reason) == "" && !req.DryRun {
		return PausedSet{}, errors.New("a pause needs --reason: the Governor's words for it")
	}
	now := p.now().UTC()
	dir, paused, err := p.Book.Current(ctx)
	if err != nil {
		return PausedSet{}, fmt.Errorf("reading whether the factory is paused: %w", err)
	}
	var set PausedSet
	if paused {
		set, err = p.Book.Read(ctx, dir)
		if err != nil {
			return PausedSet{}, fmt.Errorf("the pause in force (%s) could not be read: %w", dir, err)
		}
		if len(set.Unreached) == 0 {
			fmt.Fprintf(p.Out, "already paused since %s: nothing changed\n%s", set.PausedAt.UTC().Format(time.RFC3339), set)
			return set, nil
		}
	}

	until, plan := req.Until, ResumePlan{}
	if paused {
		until = set.Until
	} else if until.IsZero() && p.Plan != nil {
		if got, err := p.Plan(ctx); err == nil && !got.At.IsZero() {
			plan, until = got, got.At
		} else if err != nil {
			fmt.Fprintf(p.Out, "no resume time could be computed (%v): the pause is open-ended\n", err)
		}
	}
	timed := !until.IsZero()
	if until.IsZero() {
		until = now.Add(PauseOpenEnd)
	}
	if !plan.At.IsZero() {
		fmt.Fprintln(p.Out, plan.Sum)
	}
	if !until.After(now) {
		if !plan.At.IsZero() {
			return PausedSet{}, errors.New("the resume time computed is already now: the quota left would not outlast the time to the reset at full throttle, so nothing was paused")
		}
		return PausedSet{}, fmt.Errorf("--until %s is not in the future", until.UTC().Format(time.RFC3339))
	}
	hosts, err := p.Fleet.Hosts(ctx)
	if err != nil {
		return PausedSet{}, fmt.Errorf("the hosts could not be listed: %w", err)
	}
	boxes, err := p.Cloud.Boxes(ctx)
	if err != nil {
		return PausedSet{}, fmt.Errorf("the cloud boxes could not be listed: %w", err)
	}
	isBox := map[string]bool{}
	for _, b := range boxes {
		isBox[b] = true
		if !contains(hosts, b) {
			hosts = append(hosts, b)
		}
	}
	if req.DryRun {
		fmt.Fprintf(p.Out, "dry run: would pause %s until %s, nothing was changed\n", strings.Join(hosts, ", "), until.UTC().Format(time.RFC3339))
		return PausedSet{}, nil
	}
	if paused {
		hosts = set.Unreached
	} else {
		set = PausedSet{Date: now.Format("2006-01-02"), Reason: req.Reason, PausedAt: now, Until: until, Bead: req.Bead,
			NightPasses: slices.Clone(PauseNightPasses)}
		value, isSet, err := p.Config.MaxBoxes(ctx)
		if err != nil {
			return PausedSet{}, fmt.Errorf("cloud.max_boxes could not be read: %w", err)
		}
		set.MaxBoxes, set.MaxBoxesSet = value, isSet
		dir = PauseDir(now)
	}
	set.Unreached = nil

	wait := req.Wait
	results := make([]HostPaused, len(hosts))
	errs := make([]error, len(hosts))
	var group sync.WaitGroup
	for i, host := range hosts {
		group.Add(1)
		go func() {
			defer group.Done()
			results[i], errs[i] = p.Fleet.Pause(ctx, host, HostPauseOrder{Reason: req.Reason, Until: until, EndSessions: isBox[host], Wait: wait})
		}()
	}
	group.Wait()

	if !paused && set.MaxBoxesSet {
		if err := p.Config.SetMaxBoxes(ctx, "0"); err != nil {
			set.Notes = append(set.Notes, fmt.Sprintf("cloud.max_boxes could not be set to 0: %v", err))
		}
	}
	for i, host := range hosts {
		if errs[i] != nil {
			set.Unreached = append(set.Unreached, host)
			set.Notes = append(set.Notes, fmt.Sprintf("%s: %v", host, errs[i]))
			continue
		}
		r := results[i]
		set.Hosts = append(set.Hosts, PausedSetHost{Host: host, Cap: r.Cap, Cloud: isBox[host]})
		set.Stories = append(set.Stories, r.Stories...)
		set.Notes = append(set.Notes, r.Notes...)
		if isBox[host] {
			p.bringDown(ctx, host, r, &set)
		}
	}

	if timed && set.Timer == "" && p.Timer != nil {
		unit := PauseTimerUnit(now)
		made, err := p.Timer.Schedule(ctx, until, unit)
		if err != nil {
			set.Notes = append(set.Notes, fmt.Sprintf("the resume timer could not be scheduled: %v", err))
		} else {
			set.Timer, set.ResumeAt = made, until
		}
	}

	if err := p.Book.Write(ctx, dir, set); err != nil {
		return set, fmt.Errorf("the paused set could not be kept in the vault: %w", err)
	}
	if err := p.Book.SetCurrent(ctx, dir); err != nil {
		return set, fmt.Errorf("the pause could not be marked in force: %w", err)
	}
	fmt.Fprint(p.Out, set)
	p.record(ctx, set, "The factory is paused", fmt.Sprintf("The factory is paused (%s).\n\n%s\nmw resume undoes it.", req.Reason, set))
	return set, nil
}

// bringDown destroys a cloud box once every story it held is pushed.
func (p Pause) bringDown(ctx context.Context, box string, r HostPaused, set *PausedSet) {
	for _, ps := range r.Stories {
		if !ps.Pushed {
			set.Notes = append(set.Notes, fmt.Sprintf("box %s kept up: %s is not pushed", box, ps.Story))
			return
		}
	}
	if _, err := p.Cloud.Down(ctx, box); err != nil {
		set.Notes = append(set.Notes, fmt.Sprintf("box %s could not be brought down: %v", box, err))
		return
	}
	set.Boxes = append(set.Boxes, box)
}

// record writes the set on its bead, if one was named, and mails the Mayor.
func (p Pause) record(ctx context.Context, set PausedSet, subject, body string) {
	if set.Bead != "" && p.Tracker != nil {
		if err := p.Tracker.CommentOnStory(ctx, set.Bead, body); err != nil {
			fmt.Fprintf(p.Out, "note: the paused set could not be written on %s: %v\n", set.Bead, err)
		}
	}
	if p.Mail != nil {
		if _, err := p.Mail.Send(ctx, NewMessage{From: SeatIdentity(MwSeat, p.Host), To: MayorMailbox, Subject: subject, Body: body}); err != nil {
			fmt.Fprintf(p.Out, "note: the mail to %s could not be sent: %v\n", MayorMailbox, err)
		}
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// Resume is mw resume.
type Resume struct {
	Fleet   PauseFleet
	Config  PauseConfig
	Book    PauseBook
	Timer   PauseTimer
	Retry   PauseRetry
	Mail    Mailbox
	Tracker interface {
		CommentOnStory(ctx context.Context, id, text string) error
	}
	Host string
	Now  func() time.Time
	Out  io.Writer
}

// Run resumes the factory, or says it is not paused.
func (r Resume) Run(ctx context.Context, dryRun bool) error {
	dir, paused, err := r.Book.Current(ctx)
	if err != nil {
		return fmt.Errorf("reading whether the factory is paused: %w", err)
	}
	if !paused {
		fmt.Fprintln(r.Out, "not paused: nothing changed")
		return nil
	}
	set, err := r.Book.Read(ctx, dir)
	if err != nil {
		return fmt.Errorf("the pause in force (%s) could not be read: %w", dir, err)
	}
	if dryRun {
		fmt.Fprintf(r.Out, "dry run: would resume the pause of %s, nothing was changed\n%s", set.PausedAt.UTC().Format(time.RFC3339), set)
		return nil
	}
	now := time.Now()
	if r.Now != nil {
		now = r.Now()
	}

	byHost := map[string][]PausedStory{}
	for _, ps := range set.Stories {
		byHost[ps.Host] = append(byHost[ps.Host], ps)
	}
	var notes []string
	gone := map[string]bool{}
	for _, ps := range set.Stories {
		if ps.Ended {
			gone[ps.Story] = true
		}
	}
	for _, h := range set.Hosts {
		if contains(set.Boxes, h.Host) {
			// The box is down: its hold note went with it.
			continue
		}
		got, err := r.Fleet.Resume(ctx, h.Host, HostResumeOrder{Stories: byHost[h.Host]})
		if err != nil {
			notes = append(notes, fmt.Sprintf("%s: %v", h.Host, err))
			continue
		}
		notes = append(notes, got.Notes...)
		if got.Cleared {
			notes = append(notes, fmt.Sprintf("%s: hold note cleared", h.Host))
		} else {
			notes = append(notes, fmt.Sprintf("%s: no hold note there", h.Host))
		}
		for _, id := range got.Told {
			notes = append(notes, fmt.Sprintf("%s: RESUME typed into %s", h.Host, id))
		}
		for _, id := range got.Gone {
			gone[id] = true
		}
	}
	for _, host := range set.Unreached {
		notes = append(notes, fmt.Sprintf("%s was not reached by the pause, so it was not reached now", host))
	}
	if set.MaxBoxesSet && r.Config != nil {
		if err := r.Config.SetMaxBoxes(ctx, set.MaxBoxes); err != nil {
			notes = append(notes, fmt.Sprintf("cloud.max_boxes could not be put back to %s: %v", set.MaxBoxes, err))
		} else {
			notes = append(notes, "cloud.max_boxes put back to "+set.MaxBoxes)
		}
	}
	if len(set.NightPasses) > 0 {
		notes = append(notes, "night passes back on: "+strings.Join(set.NightPasses, ", "))
	}
	var retry []string
	for _, ps := range set.Stories {
		if gone[ps.Story] {
			retry = append(retry, ps.Story)
		}
	}
	for _, id := range retry {
		said, err := r.Retry.Retry(ctx, id)
		if err != nil {
			notes = append(notes, fmt.Sprintf("mw retry %s: %v", id, err))
			continue
		}
		notes = append(notes, "mw retry "+id+": "+pauseOneLine(said))
	}
	if set.Timer != "" && r.Timer != nil {
		if err := r.Timer.Cancel(ctx, set.Timer); err != nil {
			notes = append(notes, fmt.Sprintf("timer %s could not be cancelled: %v", set.Timer, err))
		}
	}

	set.ResumedAt = now.UTC()
	set.Notes = append(set.Notes, notes...)
	if err := r.Book.Write(ctx, dir, set); err != nil {
		return fmt.Errorf("the resumed set could not be kept in the vault: %w", err)
	}
	if err := r.Book.ClearCurrent(ctx); err != nil {
		return fmt.Errorf("the pause could not be marked over: %w", err)
	}
	body := "The factory is resumed.\n\n" + strings.Join(notes, "\n") + "\n"
	fmt.Fprint(r.Out, "resumed the pause of "+set.PausedAt.UTC().Format(time.RFC3339)+"\n"+strings.Join(notes, "\n")+"\n")
	Pause{Mail: r.Mail, Tracker: r.Tracker, Host: r.Host, Out: r.Out}.record(ctx, set, "The factory is resumed", body)
	return nil
}
