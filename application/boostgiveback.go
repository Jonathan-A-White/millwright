package application

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/domain"
)

// BoostSilentAfter is how long the home's Boost must have failed to answer ssh,
// straight, before boost-reach alarms and the home gives that Boost's stories
// back (mw-6ww.185.10). PROVISIONAL: the Governor has yet to confirm thirty
// minutes.
const BoostSilentAfter = 30 * time.Minute

// CauseHost is the class a give-back names the attempt it ended: the host the
// story was worked on stopped answering, which is no fault of the story's own
// code, so OwnAttempts does not count it. It is written straight to the story's
// metadata, as CauseTooling is; mw cause names only the four classes of
// CauseClasses.
const CauseHost = "host"

// GaveBackKey is the note the home keeps when it gave a Boost's claims back:
// host.<h>.gave_back, a GaveBack as JSON, so that mw status can say the give-back
// ran (and what it left) where it would say STRANDED.
func GaveBackKey(host string) string { return "host." + host + ".gave_back" }

// GaveBack is what one give-back did, as the note keeps it.
type GaveBack struct {
	Host string
	// At is when the give-back ran, and After how long the host had not
	// answered by then.
	At    time.Time
	After time.Duration
	// Gave are the stories whose claim was given back; Left the stories whose
	// claim was left on the host, each with why.
	Gave []string
	Left map[string]string
}

// gaveBackNote is GaveBack as the note holds it.
type gaveBackNote struct {
	Host    string            `json:"host"`
	At      time.Time         `json:"at"`
	Minutes int               `json:"minutes"`
	Gave    []string          `json:"gave,omitempty"`
	Left    map[string]string `json:"left,omitempty"`
}

// String is the note's value.
func (g GaveBack) String() string {
	raw, _ := json.Marshal(gaveBackNote{Host: g.Host, At: g.At.UTC(), Minutes: int(g.After / time.Minute), Gave: g.Gave, Left: g.Left})
	return string(raw)
}

// ParseGaveBack reads the note's value. A value that is empty or cannot be read
// is the zero GaveBack, which says nothing was given back.
func ParseGaveBack(value string) GaveBack {
	var n gaveBackNote
	if strings.TrimSpace(value) == "" || json.Unmarshal([]byte(value), &n) != nil || n.At.IsZero() {
		return GaveBack{}
	}
	return GaveBack{Host: n.Host, At: n.At, After: time.Duration(n.Minutes) * time.Minute, Gave: n.Gave, Left: n.Left}
}

// Ran says a give-back is noted.
func (g GaveBack) Ran() bool { return !g.At.IsZero() }

// when is the time as mw status says it.
func (g GaveBack) when() string { return g.At.UTC().Format(giveWayShort) }

// Line is what mw status says of a story the give-back gave back, "" for any
// other: 'given back at 11-21 10:00Z: desktop did not answer for 30 min'.
func (g GaveBack) Line(id string) string {
	for _, gave := range g.Gave {
		if gave == id {
			after := g.After
			if after <= 0 {
				after = BoostSilentAfter
			}
			return fmt.Sprintf("given back at %s: %s did not answer for %d min", g.when(), g.Host, int(after/time.Minute))
		}
	}
	return ""
}

// LeftLine is what mw status says of a claim the give-back left on the host, ""
// for any other.
func (g GaveBack) LeftLine(id string) string {
	why := g.Left[id]
	if why == "" {
		return ""
	}
	return fmt.Sprintf("the home's give-back at %s left it: %s", g.when(), why)
}

// Said is a short clause for a line that tells the Mayor what to run for a host
// that is down, when the give-back has run: 'the home's give-back ran at ...'.
func (g GaveBack) Said() string {
	if !g.Ran() {
		return ""
	}
	return fmt.Sprintf("the home's give-back ran at %s", g.when())
}

// BoostGiveBackNotes is where the give-back reads the host's give-way and pause
// notes and keeps its own: the tracker's notes.
type BoostGiveBackNotes interface {
	Note(ctx context.Context, key string) (string, error)
	SetNote(ctx context.Context, key, value string) error
}

// BoostGiveBack is what the home does when its Boost has not answered for
// BoostSilentAfter (boost-reach's alarm): every claim the Boost holds whose lease
// has lapsed is given back as mw retry <id> --force gives one back, the attempt
// named host so that it is not counted against the story, and the Mayor is
// mailed once, story by story, what was kept and where each may go now
// (mw-6ww.185.10). A story queued for its rig's batch or already landed is left
// alone, and so is one pinned to the Boost by a host_why; the mail names each.
//
// It acts for a Boost that gives way, is paused or is retired, and for the home
// itself, never: the first two are the person's own choice, the claims of the
// others are not stranded, and the home's are not a Boost's to give back.
type BoostGiveBack struct {
	// Retry is the retry each story is given back through: built for the home
	// (its Host, tracker, worktrees, landing, rigs), with no Reach, since the
	// Boost is not answering. Run forces it.
	Retry Retry

	Notes   BoostGiveBackNotes
	Mailbox Mailbox

	// Home is this host's name; Retired the hosts the Governor retired.
	Home    string
	Retired []string

	// GiveWayStale is giveway_stale, for the note of a host that gives way; zero
	// reads DefaultGiveWayStale. After is how long the host had not answered, as
	// the mail and the story's comment say it; zero reads BoostSilentAfter.
	GiveWayStale time.Duration
	After        time.Duration

	// Now is the clock; nil reads the real one.
	Now func() time.Time
}

// BoostGave is one story given back.
type BoostGave struct {
	StoryID string
	// Kept says what the Boost pushed that the next attempt continues.
	Kept string
	// Where says where the story may go now.
	Where string
}

// BoostLeft is one claim left on the Boost, and why.
type BoostLeft struct {
	StoryID string
	Why     string
}

// BoostGiveBackReport is what one give-back did.
type BoostGiveBackReport struct {
	Host string
	// Skipped says why nothing was done, when the host is one never given back.
	Skipped string
	Gave    []BoostGave
	Left    []BoostLeft
	// Notes are what could not be written or sent, and went on all the same.
	Notes []string
}

func (g BoostGiveBack) now() time.Time {
	if g.Now == nil {
		return time.Now() //mw:realclock the default of the injected Now, used where none is set
	}
	return g.Now()
}

func (g BoostGiveBack) after() time.Duration {
	if g.After <= 0 {
		return BoostSilentAfter
	}
	return g.After
}

func (g BoostGiveBack) afterSaid() string { return fmt.Sprintf("%d min", int(g.after()/time.Minute)) }

// skip says why host is never given back, "" when it may be. An error is a note
// that could not be read: the claims are kept.
func (g BoostGiveBack) skip(ctx context.Context, host string) (string, error) {
	switch {
	case host == g.Home:
		return host + " is the home, whose claims are never given back by itself", nil
	case IsRetired(g.Retired, host):
		return host + " is retired", nil
	}
	if standing := (GiveWayReader{Notes: g.Notes, Stale: g.GiveWayStale}).Of(ctx, host, g.now()); standing.Stands {
		return host + " gives way to the person at its screen: its claims are frozen, not stranded", nil
	}
	pause, paused, err := PausedByNote(ctx, g.Notes, host)
	if err != nil {
		return "", fmt.Errorf("giving back the claims of %s: whether it is paused could not be read, so none was given back: %w", host, err)
	}
	if paused {
		return fmt.Sprintf("%s is paused (by %s): its claims are the person's to settle", host, pause.Actor), nil
	}
	return "", nil
}

// lapsed are the stories host holds whose lease has run out, by id.
func (g BoostGiveBack) lapsed(ctx context.Context, host string) ([]StoryDetail, error) {
	lister, ok := g.Retry.Tracker.(RunningEverywhere)
	if !ok {
		return nil, fmt.Errorf("giving back the claims of %s: the tracker cannot list what is running on it", host)
	}
	here, away, err := lister.RunningEverywhere(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("giving back the claims of %s: reading what is running: %w", host, err)
	}
	seen := map[string]bool{}
	var held []StoryDetail
	for _, detail := range append(here, away...) {
		id := detail.Story.ID
		if seen[id] || detail.IsEpic || ClaimHost(detail.Assignee) != host || !strings.EqualFold(strings.TrimSpace(detail.Status), StatusInProgress) {
			continue
		}
		if detail.LeaseExpires.IsZero() || !g.now().After(detail.LeaseExpires) {
			continue
		}
		seen[id] = true
		held = append(held, detail)
	}
	sort.Slice(held, func(i, j int) bool { return held[i].Story.ID < held[j].Story.ID })
	return held, nil
}

// Run gives back the lapsed claims of host.
func (g BoostGiveBack) Run(ctx context.Context, host string) (BoostGiveBackReport, error) {
	report := BoostGiveBackReport{Host: host}
	switch {
	case strings.TrimSpace(host) == "":
		return report, fmt.Errorf("giving back the claims of a Boost: which host?")
	case g.Retry.Tracker == nil || g.Notes == nil:
		return report, fmt.Errorf("giving back the claims of %s: there is no work tracker or notes to do it with", host)
	}
	why, err := g.skip(ctx, host)
	if err != nil {
		return report, err
	}
	if why != "" {
		report.Skipped = why
		return report, nil
	}
	held, err := g.lapsed(ctx, host)
	if err != nil {
		return report, err
	}
	fetched := map[string]bool{}
	for _, detail := range held {
		if left := g.leftAlone(detail, host); left != "" {
			report.Left = append(report.Left, BoostLeft{StoryID: detail.Story.ID, Why: left})
			continue
		}
		kept := g.pushed(ctx, detail, fetched)
		gave, why := g.giveBack(ctx, detail, host, kept)
		if why != "" {
			report.Left = append(report.Left, BoostLeft{StoryID: detail.Story.ID, Why: why})
			continue
		}
		report.Gave = append(report.Gave, gave.BoostGave)
		report.Notes = append(report.Notes, gave.notes...)
	}
	if len(report.Gave)+len(report.Left) == 0 {
		return report, nil
	}
	return report, g.tell(ctx, &report)
}

// leftAlone is why a lapsed claim is not given back, "" when it is: queued for
// its batch, which lands it from origin; landed, which mw next closes; pinned to
// the host by a host_why, which the Mayor moves on purpose.
func (g BoostGiveBack) leftAlone(detail StoryDetail, host string) string {
	id := detail.Story.ID
	path := detail.Merged()
	switch {
	case QueuedForBatch(detail) && !detail.Held():
		return fmt.Sprintf("queued for its batch (%s:%s): the batch of %s lands it from origin, so it is left claimed", RunState, RunQueued, orRig(path.Rig))
	case LandedRun(detail):
		return fmt.Sprintf("its last run landed (%s:%s): mw next %s closes it, and a give-back would work it a second time", RunState, RunLanded, id)
	}
	if why := strings.TrimSpace(path.HostWhy); why != "" {
		return fmt.Sprintf("pinned to %s by its host_why (%q): left claimed; to move it on purpose: %s", path.Host, why, fromElsewhereCure(id))
	}
	return ""
}

// pushed is what the Boost pushed of the story's branch that the next attempt
// continues: the branch and its newest commit at the remote, or that nothing was
// pushed. A rig the home does not have checked out cannot say.
func (g BoostGiveBack) pushed(ctx context.Context, detail StoryDetail, fetched map[string]bool) string {
	id := detail.Story.ID
	branch := StoryBranch(id)
	rig := detail.Merged().Rig
	dir, ok := g.Retry.Rigs[rig]
	if !ok || g.Retry.Landing == nil {
		return fmt.Sprintf("%s: the rig %s is not checked out on %s, so what was pushed could not be read", branch, orRig(rig), g.Home)
	}
	if !fetched[dir] && g.Retry.Worktrees != nil {
		fetched[dir] = true
		_ = g.Retry.Worktrees.Fetch(ctx, dir)
	}
	remote := g.Retry.remote()
	tip, err := g.Retry.Landing.RefHead(ctx, dir, StartPoint(remote, branch))
	if err != nil || tip == "" {
		return "nothing was pushed"
	}
	return fmt.Sprintf("%s at %s, pushed to %s", branch, shortCommit(tip), remote)
}

// gaveStory is BoostGave with the notes its writes left.
type gaveStory struct {
	BoostGave
	notes []string
}

// giveBack gives one claim back through mw retry --force and names the attempt
// host. why is not empty when the retry refused or failed.
func (g BoostGiveBack) giveBack(ctx context.Context, detail StoryDetail, host, kept string) (gaveStory, string) {
	id := detail.Story.ID
	path := detail.Merged()
	retry := g.Retry
	retry.Force, retry.Hold, retry.FromMain, retry.RePathed, retry.Out, retry.Err = true, false, false, "", nil, nil
	if _, err := retry.Run(ctx, id); err != nil {
		return gaveStory{}, fmt.Sprintf("could not be given back: %s; from elsewhere, run: %s", oneLine(err.Error()), fromElsewhereCure(id))
	}
	gave := gaveStory{BoostGave: BoostGave{StoryID: id, Kept: kept}}
	attempt := max(1, detail.Attempts)

	causes := map[int]string{}
	for at, class := range detail.AttemptCauses {
		causes[at] = class
	}
	causes[attempt] = CauseHost
	fields := map[string]string{
		CauseField:         CauseHost,
		CauseWhyField:      fmt.Sprintf("%s did not answer for %s while it held the story", host, g.afterSaid()),
		AttemptCausesField: attemptCausesText(causes),
	}
	named := fmt.Sprintf("Attempt %d is named %s (mw cause), not the story's own: it is not counted against its attempts.", attempt, CauseHost)
	if err := g.Retry.Tracker.SetStoryMetadata(ctx, id, fields); err != nil {
		named = fmt.Sprintf("Attempt %d could not be named %s (mw cause): %s.", attempt, CauseHost, oneLine(err.Error()))
		gave.notes = append(gave.notes, fmt.Sprintf("%s: %s", id, named))
	}

	switch {
	case path.Host == host:
		if err := g.Retry.Tracker.SetStoryMetadata(ctx, id, map[string]string{"host": domain.HostAuto}); err != nil {
			gave.Where = fmt.Sprintf("still pinned to %s, which could not be changed (%s): run mw path set %s host=auto", host, oneLine(err.Error()), id)
			gave.notes = append(gave.notes, fmt.Sprintf("%s: host=auto could not be set: %v", id, err))
		} else {
			gave.Where = "host=auto now: any host that passes its gates may take it"
		}
	case path.Host == domain.HostAuto || path.Host == "":
		gave.Where = "host=auto: any host that passes its gates may take it"
	default:
		gave.Where = fmt.Sprintf("host=%s, as its path says: %s takes it", path.Host, path.Host)
	}
	comment := fmt.Sprintf("mw boost-reach: %s did not answer for %s, so the home (%s) gave this claim back as mw retry %s --force does; "+
		"the branch %s is kept for the next attempt (%s). %s Where it goes now: %s.",
		host, g.afterSaid(), g.Home, id, StoryBranch(id), kept, named, gave.Where)
	if err := g.Retry.Tracker.CommentOnStory(ctx, id, comment); err != nil {
		gave.notes = append(gave.notes, fmt.Sprintf("%s: the comment saying why could not be written: %v", id, err))
	}
	return gave, ""
}

// tell keeps the note mw status reads and mails the Mayor once: each story, what
// was kept, and where it may go now. A failure of either is a note, and the
// error returned for the mail.
func (g BoostGiveBack) tell(ctx context.Context, report *BoostGiveBackReport) error {
	gaveBack := GaveBack{Host: report.Host, At: g.now(), After: g.after(), Left: map[string]string{}}
	for _, gave := range report.Gave {
		gaveBack.Gave = append(gaveBack.Gave, gave.StoryID)
	}
	for _, left := range report.Left {
		gaveBack.Left[left.StoryID] = left.Why
	}
	if err := g.Notes.SetNote(ctx, GaveBackKey(report.Host), gaveBack.String()); err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("the note mw status reads of this give-back could not be written: %v", err))
	}
	if g.Mailbox == nil {
		return nil
	}
	if _, err := g.Mailbox.Send(ctx, NewMessage{
		From:    SeatIdentity(MwSeat, g.Home),
		To:      MayorMailbox,
		Subject: fmt.Sprintf("%s did not answer for %s: %d given back, %d left", report.Host, g.afterSaid(), len(report.Gave), len(report.Left)),
		Body:    g.mailBody(*report),
	}); err != nil {
		return fmt.Errorf("giving back the claims of %s: the mail to %s could not be sent: %w", report.Host, MayorMailbox, err)
	}
	return nil
}

// mailBody is the one mail of the outage.
func (g BoostGiveBack) mailBody(report BoostGiveBackReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "The Boost %s has not answered for %s, so the home (%s) gave back its claims whose lease had lapsed, as mw retry <id> --force does. "+
		"Each attempt is named %s and not counted against its story.\n", report.Host, g.afterSaid(), g.Home, CauseHost)
	if len(report.Gave) > 0 {
		b.WriteString("\nGiven back:\n")
		for _, gave := range report.Gave {
			fmt.Fprintf(&b, "- %s: kept %s. Where it may go now: %s.\n", gave.StoryID, gave.Kept, gave.Where)
		}
	}
	if len(report.Left) > 0 {
		b.WriteString("\nLeft claimed on " + report.Host + ":\n")
		for _, left := range report.Left {
			fmt.Fprintf(&b, "- %s: %s\n", left.StoryID, left.Why)
		}
	}
	for _, note := range report.Notes {
		fmt.Fprintf(&b, "\nNote: %s\n", note)
	}
	return b.String()
}
