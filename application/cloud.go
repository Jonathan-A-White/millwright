package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/domain"
	"github.com/Jonathan-A-White/millwright/domain/events"
)

// CloudProvider makes and destroys the cloud boxes: contrib/vultr-boost, or a
// fake in a scenario. A box is named for the host it becomes (cloud1, cloud2).
type CloudProvider interface {
	// Begin starts the making of the box in a process of its own, outliving
	// the caller and its context, and returns once that has started: the
	// process runs CloudMake. An error is a make that did not start.
	Begin(ctx context.Context, name string) error
	// Up makes the box and returns once it is up; an error is a box that did
	// not come up, which may still have been partly made. It is what the
	// process Begin starts calls, never the check.
	Up(ctx context.Context, name string) error
	// Down destroys the box, or what there is of it; a box not there is no
	// error.
	Down(ctx context.Context, name string) error
	// Billed is what the provider's billing says the boxes named have cost so
	// far this month, and whether it could be asked at all.
	Billed(ctx context.Context, names []string) (usd float64, known bool, err error)
	// Instances are the boxes the provider holds, whatever the book says,
	// each by the name it was labelled with.
	Instances(ctx context.Context) ([]CloudInstance, error)
	// Reap destroys one instance by its id, for a box no state of ours
	// holds, so that Down could not find it.
	Reap(ctx context.Context, instance CloudInstance) error
	// Adopt takes a box the provider holds and the book names, whose make
	// was cut off before its state was kept, into the provider's state
	// (terraform import), so that Down finds it.
	Adopt(ctx context.Context, name string, instance CloudInstance) error
	// SetCap sets how many sessions the box runs at once, in the config of
	// the box itself, over WireGuard: its next dispatch reads it, running
	// sessions are not touched. An error is a box that could not be reached
	// or written, which keeps the cap it had.
	SetCap(ctx context.Context, name string, cap int) error
	// SetPace makes sure the box's dispatch timer ticks every five minutes
	// (the timer drop-in boost-pace.conf, as boost-bootstrap.sh writes it) and
	// returns the OnCalendar the timer has after: a box made from the snapshot
	// carries the home's hourly timer, and has no event follower to spring its
	// dispatch. An error is a box that could not be reached or written.
	SetPace(ctx context.Context, name string) (pace string, err error)
	// SetHarness brings the box's Claude Code to version, the home's, when it
	// is behind it, over WireGuard as SetPace does: a box made from the
	// snapshot comes up on the snapshot's. A failed install is no error: the
	// box is left as it was and CloudHarness.Why says why, because a box that
	// cannot start a session is worse than one on an older Claude Code. An error
	// is a box that could not be reached.
	SetHarness(ctx context.Context, name, version string) (CloudHarness, error)
	// SetSessionDeny makes the box's own config hold [session] deny = rules
	// (the home's list of permission rules every story session is refused), over
	// WireGuard as SetPace does, and says whether it had to change a byte: false
	// is a box that already held exactly that list. Its next dispatch reads it;
	// sessions running are not touched. An error is a box that could not be
	// reached or written (mw-t5shinh.33).
	SetSessionDeny(ctx context.Context, name string, rules []string) (changed bool, err error)
	// CheckBox has the box check its own disk, over WireGuard as SetPace does:
	// git fsck --connectivity-only on its vault and every rig checkout, go mod
	// verify in its millwright checkout, no EXT4-fs error in its kernel log, and
	// no file older than its boot that begins with a block of zeros (a snapshot
	// restore that did not write the whole disk, as cloud6's did not).
	// What it finds wrong is CloudBoxChecked.Faults and no error: an error is a
	// box that could not be reached or asked, which is no verdict (mw-gq6.1172).
	CheckBox(ctx context.Context, name string) (CloudBoxChecked, error)
	// Address is where the box is reached from the home: its WireGuard address
	// read off the hub, and the user it dispatches as. The home resolves no
	// name for a box. An error is a box with no address to read, with why.
	Address(ctx context.Context, name string) (CloudAddress, error)
}

// CloudHarness is what bringing a box's Claude Code level found: the version
// it had and has. Now equal to Was with no Why is a box already level; with a
// Why, an install that did not happen or did not take.
type CloudHarness struct {
	Was, Now, Why string
}

// CloudAddress is where a cloud box is reached over WireGuard.
type CloudAddress struct {
	User, Host string
}

// String is the address as ssh takes it: user@host.
func (a CloudAddress) String() string { return a.User + "@" + a.Host }

// CloudInstance is one box as the provider holds it: Status is Vultr's
// status (active, pending, ...) and Server its server_status (ok, locked,
// installingbooting, none), empty when not said.
type CloudInstance struct {
	ID, Label, Status, Server string
}

// Text is what Vultr says of the instance in two words, "active locked": its
// status and its server status, the first alone when it says no server status.
func (i CloudInstance) Text() string {
	return strings.TrimSpace(i.Status + " " + i.Server)
}

// Up reports whether the provider says the instance is made and ready: active
// and its server ok. One that does not say its server status is read by its
// status alone.
func (i CloudInstance) Up() bool {
	return i.Status == "active" && (i.Server == "" || i.Server == "ok")
}

// What the provider can refuse, each a reason a retry does not mend. The
// adapter wraps one of these around the provider's own words; the text is
// what the book and the cloud events say.
var (
	ErrCloudFeeLimit = errors.New("Vultr fee limit reached: raise it at my.vultr.com or wait for the month")
	ErrCloudQuota    = errors.New("Vultr instance limit reached: raise it at my.vultr.com, or destroy a box there")
	ErrCloudSoldOut  = errors.New("Vultr has none of the plan left in the region: choose another region or plan in the tfvars")
	ErrCloudBadToken = errors.New("Vultr refused the API token: put a new vultr_api_token (mw secrets put), and check the API's access control at my.vultr.com")
	// ErrCloudLocked is Vultr's 409: it will not delete an instance it has
	// locked, as one being restored from a snapshot is. It is no refusal of a
	// make; a delete is tried again.
	ErrCloudLocked = errors.New("Vultr would not delete (locked)")
	// ErrCloudUpDeadline is a make that ran past its deadline: what became of
	// the box is Vultr's to say, not the wrapper's exit status.
	ErrCloudUpDeadline = errors.New("the make ran past its deadline")
)

// CloudKeptWorktree is one worktree a cloud box keeps: a Builder's checkout of
// a story, which a turn cap, a refused landing or a stopped session leaves on
// the box, unpushed (a box has no GitHub credentials) and so held nowhere else.
type CloudKeptWorktree struct {
	// Story is the story the worktree is for, Branch its branch, Dir where it
	// is on the box and Head the commit the branch is at.
	Story, Branch, Dir, Head string
	// Ahead counts the commits the branch has that the rig's target does not,
	// and Uncommitted lists the paths the worktree has changed or added and
	// not committed (untracked ones included).
	Ahead       int
	Uncommitted []string
}

// Holds reports whether the worktree has anything the vault could lack.
func (w CloudKeptWorktree) Holds() bool { return w.Ahead > 0 || len(w.Uncommitted) > 0 }

// CloudBoxWork is what the cloud asks of a box's kept worktrees before the box
// is destroyed, over ssh: the adapter reaches the box at the address the
// provider gives. A box destroyed on a timer and made from a snapshot is given
// no secret to push with, so the home brings the work back instead.
type CloudBoxWork interface {
	// Worktrees lists the worktrees the box keeps, each with what it holds. An
	// error is a box that could not be reached or read: it is not "none".
	Worktrees(ctx context.Context, box string) ([]CloudKeptWorktree, error)
	// Bundle writes the commits of the worktree's branch ahead of the rig's
	// target into the file dest on this host, making its directory.
	Bundle(ctx context.Context, box string, wt CloudKeptWorktree, dest string) error
	// VerifyBundle checks the bundle at dest holds what the branch has ahead.
	VerifyBundle(ctx context.Context, box string, wt CloudKeptWorktree, dest string) error
	// UncommittedDiff is a patch of everything the worktree has changed or
	// added and not committed, untracked files included, against its head.
	UncommittedDiff(ctx context.Context, box string, wt CloudKeptWorktree) (string, error)
}

// CloudNotice tells the Governor what only he can mend: a card to him.
type CloudNotice interface {
	Tell(ctx context.Context, title, text string) error
}

// CloudBook is where the cloud's state is kept between checks: in the vault,
// so that the month's spend outlives any one host.
type CloudBook interface {
	// Read is the state as last written; a book never written is the zero
	// state.
	Read(ctx context.Context) (CloudState, error)
	// Write keeps the state, why saying what changed.
	Write(ctx context.Context, state CloudState, why string) error
}

// CloudWork is what the cloud check reads of the tracker: the stories ready
// and the stories claimed, across every host.
type CloudWork interface {
	WorkInHand(ctx context.Context) (WorkInHand, error)
}

// CloudStories is what the cloud reads and writes of the tracker's stories to
// re-path the ones a destroyed box leaves pathed to it. It is satisfied by the
// beads Gateway and the fake tracker.
type CloudStories interface {
	StoriesSince(ctx context.Context, since time.Time) ([]StoryDetail, error)
	SetStoryMetadata(ctx context.Context, id string, fields map[string]string) error
	// ReleaseClaimHeldBy gives back a claim a box's dispatch made.
	ReleaseClaimHeldBy(ctx context.Context, id, holder string) error
	// ClearRunState takes a story's run state off it, for a claim given back
	// whose session is gone with its box.
	ClearRunState(ctx context.Context, id string) error
	// CommentOnStory says on a story what was done to its claim.
	CommentOnStory(ctx context.Context, id, text string) error
}

// CloudNotes is what the cloud check reads of the tracker's notes: a box's
// LastSyncKey, which its sync writes into the one database every host reads
// once the box is level, the first thing its dispatch does. A box named by
// another host's name, or whose vault is broken, writes none of its own.
type CloudNotes interface {
	Note(ctx context.Context, key string) (string, error)
}

// CloudState is what the book holds.
type CloudState struct {
	// Month is the UTC month ("2026-10") Hours counts, and Hours the box hours
	// billed that month for the boxes already destroyed, each begun hour a
	// whole one.
	Month string `json:"month,omitempty"`
	Hours int    `json:"hours,omitempty"`
	// Boxes are the boxes up, oldest first.
	Boxes []CloudBox `json:"boxes,omitempty"`
	// CapSaid is the month the cloud cap reached event was written for: it is
	// written once a month.
	CapSaid string `json:"cap_said,omitempty"`
	// HeldUntil is when a box may be made again after one failed to come up
	// twice running.
	HeldUntil time.Time `json:"held_until,omitzero"`
	// FeeLimitAt is when Vultr last refused a box for its monthly fee limit,
	// zero while it has not or a box has come up since: no box is made for
	// CloudFeeLimitPause after it, and the Governor has one card for it.
	FeeLimitAt time.Time `json:"fee_limit_at,omitzero"`
	// OrphansSaid are the Vultr ids of the orphans a check could not destroy
	// and said so for, that the next check does not say it again.
	OrphansSaid []string `json:"orphans_said,omitempty"`
	// Moves are the latest ups and downs, oldest first, at most
	// cloudMovesKept of them, for mw status.
	Moves []CloudMove `json:"moves,omitempty"`
	// Downed is when each box was last destroyed, by name, kept CloudNameBar so
	// that no box is made again under a name whose per-host notes (last sync,
	// ticks, mw commit) the old box wrote (mw-gq6.1127).
	Downed map[string]time.Time `json:"downed,omitempty"`
}

// CloudBox is one box up.
type CloudBox struct {
	Name string    `json:"name"`
	Up   time.Time `json:"up"`
	// IdleSince is when a check first found the box with no story claimed on
	// it, the time it came up (never when its make began) until it has taken
	// one; zero while it has work or is still being made or restored.
	IdleSince time.Time `json:"idle_since,omitzero"`
	// Making is set from the time the box is asked for until its make ends,
	// Up being when it was asked for: the box is billed from then, and no
	// check destroys it or counts it idle meanwhile.
	Making bool `json:"making,omitempty"`
	// Vultr is what Vultr said of the box's instance at the last cloud check
	// that read it while the box was being made, "active locked" (its status
	// and its server status): empty before a check has read it, or when Vultr
	// holds no instance of the box yet. SlowSaid is that the check has said this
	// make ran past CloudPlan.MakeSlowLimit, once.
	Vultr    string `json:"vultr,omitempty"`
	SlowSaid bool   `json:"slow_said,omitempty"`
	// VultrID is Vultr's id of the box's instance and LockedSince when a check
	// first found it locked, at any time the box is in the book (a make, or a
	// teardown Vultr will not take): both empty when Vultr holds no instance of
	// it, or it is not locked (mw-gq6.1127).
	VultrID     string    `json:"vultr_id,omitempty"`
	LockedSince time.Time `json:"locked_since,omitzero"`
	// Restoring is a box whose make ran past its deadline while Vultr still
	// held the instance, restoring from a snapshot: InstanceID is Vultr's id
	// for it and RestoringSince when the make gave up waiting. A check adopts
	// it when Vultr says it is up, and destroys it Plan.Up after that. Like a
	// box being made it is billed, and never idle.
	Restoring      bool      `json:"restoring,omitempty"`
	InstanceID     string    `json:"instance_id,omitempty"`
	RestoringSince time.Time `json:"restoring_since,omitzero"`
	// Made is when Vultr was found to hold the box up, at the end of its make
	// or when a check adopted it; zero in a book written before it was kept,
	// read as Up. Dispatched is when its first dispatch reached the beads
	// server, at or after Up: only then is the box said up. NotDispatching is
	// why it had not CloudDispatchWait after it was made, said once.
	Made           time.Time `json:"made,omitzero"`
	Dispatched     time.Time `json:"dispatched,omitzero"`
	NotDispatching string    `json:"not_dispatching,omitempty"`
	// Cap is how many sessions the box runs at once, as the check last set it
	// on the box: the count of its free sessions. Zero is a box whose cap the
	// book has not set yet, counted at the plan's BoxCap. CapFault is why the
	// cap the plan wants has not reached the box, "" when it has; the box keeps
	// counting at Cap meanwhile.
	Cap      int    `json:"cap,omitempty"`
	CapFault string `json:"cap_fault,omitempty"`
	// Pace is the OnCalendar of the box's mw-dispatch.timer as the check last
	// read it back from the box ("" when it has not), CloudPaceFive being the
	// one a box needs; PaceFault is why the check could not set or read it.
	Pace      string `json:"pace,omitempty"`
	PaceFault string `json:"pace_fault,omitempty"`
	// Harness is the home's Claude Code version the box was last brought level
	// to ("" in a book written before it was kept, or a box never brought level),
	// so that a pass reaches a box again only when the home's version has moved;
	// HarnessFault is why the last install did not take, said as a move once.
	Harness      string `json:"harness,omitempty"`
	HarnessFault string `json:"harness_fault,omitempty"`
	// SessionDeny is the home's [session] deny list, as domain.SessionDenyLiteral
	// spells it, that the box was last brought level to ("" in a book written
	// before it was kept, or a box never brought level), so that a pass reaches a
	// box again only when the home's list has moved (mw-t5shinh.33).
	SessionDeny string `json:"session_deny,omitempty"`
	// SessionDenyFault is why the last try to set it did not reach the box, said
	// as a move once, and shown by mw status (mw-gq6.1174).
	SessionDenyFault string `json:"session_deny_fault,omitempty"`
	// Saved are the worktrees of the box whose work the vault already holds,
	// each as "<story> <head> <hash of the uncommitted patch>", so that a
	// teardown refused later does not bundle the same work again at every
	// check. Refused is what the last refusal to destroy the box said, so that
	// a refusal is a move once, not at every check.
	Saved   []string `json:"saved,omitempty"`
	Refused string   `json:"refused,omitempty"`
	// Checked is when the box last passed its own check with Vultr not
	// reporting it locked, zero while it has not: only then may it take a story
	// (MayWork). CheckFault is what its check found wrong, "" when nothing, and
	// Gate what the box's gate note was last written to say, so that it is
	// written once (mw-gq6.1172).
	Checked    time.Time `json:"checked,omitzero"`
	CheckFault string    `json:"check_fault,omitempty"`
	Gate       string    `json:"gate,omitempty"`
	// Alerted is that the Governor was told of this spell of refusals, so that
	// the checks after it do not tell him again; a saved box clears it.
	Alerted bool `json:"alerted,omitempty"`
}

// CloudPaceFive is the OnCalendar of a dispatch timer at the pace a box needs:
// every five minutes, on the clock.
const CloudPaceFive = "*:0/5"

// paceText is how a box's dispatch timer pace is said: the OnCalendar in force
// as the pace it is.
func (b CloudBox) paceText() string {
	switch b.Pace {
	case CloudPaceFive:
		return "every 5 minutes"
	case "":
		text := "pace not known"
		if b.PaceFault != "" {
			text += ": " + b.PaceFault
		}
		return text
	case "hourly":
		return "hourly, not every 5 minutes: a box has no follower, so its dispatch waits up to an hour"
	}
	return "at OnCalendar " + b.Pace + ", not every 5 minutes"
}

// boxCap is how many sessions the box is counted to run at once: the cap the
// book last set on it, else the plan's.
func (b CloudBox) boxCap(plan CloudPlan) int {
	if b.Cap > 0 {
		return b.Cap
	}
	return plan.boxCap()
}

// made is when the box was made, Up for one the book did not keep it of.
func (b CloudBox) made() time.Time {
	if b.Made.IsZero() {
		return b.Up
	}
	return b.Made
}

// CloudMove is one up, down or failure, as the cloud event said it.
type CloudMove struct {
	At   time.Time `json:"at"`
	What string    `json:"what"`
}

// cloudMovesKept is how many moves the book keeps for mw status.
const cloudMovesKept = 5

// CloudMakeStaleMargin is how long past the make's own deadline a box may be
// made before a check gives it up for lost (CloudPlan.MakeStale): the process
// that was making it did not live to say how it ended.
const CloudMakeStaleMargin = 15 * time.Minute

// DefaultCloudUp is how long a make may run when the plan does not say: a
// Vultr snapshot restore took about 50 minutes on 2026-10-15.
const DefaultCloudUp = 60 * time.Minute

// CloudDispatchWait is how long a box made may go with no dispatch of its own
// reaching the beads server before the book says it is up, not dispatching: a
// box from the snapshot runs its bootstrap and its first dispatch in minutes.
const CloudDispatchWait = 20 * time.Minute

// CloudFailHold is how long no box is made after one failed to come up twice
// running: a fault at the provider is not tried every minute.
const CloudFailHold = time.Hour

// CloudFeeLimitPause is how long no box is tried after Vultr refused one for
// its monthly fee limit: not a failure to come up, so not HeldUntil, but an
// hour all the same, as CloudFailHold is: each try is a terraform run and four
// vault commits (mw-gq6.1163). A box destroyed after the refusal ends it early,
// since it frees room under the limit.
const CloudFeeLimitPause = time.Hour

// CloudPlan is the [cloud] table, as the check reads it.
type CloudPlan struct {
	// MaxBoxes is the most boxes up at once; MonthlyCapUSD the most the boxes
	// may cost in a UTC month; HourlyUSD what one costs an hour.
	MaxBoxes      int
	MonthlyCapUSD float64
	HourlyUSD     float64
	// Idle is how long a box may go with no story claimed on it before it is
	// destroyed.
	Idle time.Duration
	// BoxCap is how many sessions a box runs at once, and HostCaps how many
	// each other host does: the free sessions are counted from them.
	BoxCap   int
	HostCaps map[string]int
	// Retired are the hosts in retired_hosts: no session runs on one, so a
	// line of HostCaps naming one counts no free sessions (mw-gq6.1111).
	Retired []string
	// Prefix names the boxes, Prefix1, Prefix2, ...; empty is "cloud".
	Prefix string
	// Up is how long a make may run before it reads the instance back from the
	// provider ([cloud] up_minutes); zero is DefaultCloudUp.
	Up time.Duration
	// MakeSlow is how long a make may run, unfinished, before it is a cloud
	// event and a line under DAMPERS ([cloud] slow_make_minutes); zero is
	// DefaultCloudMakeSlow.
	MakeSlow time.Duration
	// NoDispatch is how long after its make a box may go with no dispatch of
	// its own reaching the beads server before the check destroys it ([cloud]
	// no_dispatch_minutes); zero is DefaultCloudNoDispatch (mw-gq6.1143).
	NoDispatch time.Duration
}

// DefaultCloudNoDispatch is how long a box made may go with no dispatch before
// the check destroys it, when the plan does not say: past CloudDispatchWait,
// after which the book has already said the box is up, not dispatching.
const DefaultCloudNoDispatch = 30 * time.Minute

// NoDispatchLimit is how long after its make a box may go with no dispatch.
func (p CloudPlan) NoDispatchLimit() time.Duration {
	if p.NoDispatch <= 0 {
		return DefaultCloudNoDispatch
	}
	return p.NoDispatch
}

// DefaultCloudMakeSlow is how long a make may run before it is called slow,
// when the plan does not say.
const DefaultCloudMakeSlow = 15 * time.Minute

// CloudLockedLimit is how long Vultr may report a box locked, making or down,
// before the book names it with its instance id: a lock that long is Vultr's to
// mend, with a ticket (mw-gq6.1127).
const CloudLockedLimit = 30 * time.Minute

// CloudNameBar is how long after a box is destroyed no box is made under its name.
const CloudNameBar = 24 * time.Hour

// MakeSlowLimit is how long a make may run before it is called slow.
func (p CloudPlan) MakeSlowLimit() time.Duration {
	if p.MakeSlow <= 0 {
		return DefaultCloudMakeSlow
	}
	return p.MakeSlow
}

// UpLimit is how long a make may run.
func (p CloudPlan) UpLimit() time.Duration {
	if p.Up <= 0 {
		return DefaultCloudUp
	}
	return p.Up
}

// MakeStale is how long a box may be made before a check gives it up for
// lost: the make's deadline and CloudMakeStaleMargin.
func (p CloudPlan) MakeStale() time.Duration { return p.UpLimit() + CloudMakeStaleMargin }

// DefaultCloudPrefix is what a box's name begins with.
const DefaultCloudPrefix = "cloud"

// IsBox says name is a box's name: the prefix and a number.
func (p CloudPlan) IsBox(name string) bool { return p.isBox(name) }

// isBox says name is a box's name: the prefix and a number.
func (p CloudPlan) isBox(name string) bool {
	number, found := strings.CutPrefix(name, p.prefix())
	if !found || number == "" {
		return false
	}
	for _, r := range number {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func (p CloudPlan) prefix() string {
	if p.Prefix == "" {
		return DefaultCloudPrefix
	}
	return p.Prefix
}

// DefaultCloudBoxCap is how many sessions a box runs at once when the plan
// does not say: boost-bootstrap.sh's own default.
const DefaultCloudBoxCap = 2

func (p CloudPlan) boxCap() int {
	if p.BoxCap <= 0 {
		return DefaultCloudBoxCap
	}
	return p.BoxCap
}

// CloudCheck is one look at the cloud (mw cloud check; the follower's cloud
// job each minute). It costs no tokens: it reads the tracker's work in hand,
// the book and the provider's billing, and calls the provider only to make or
// destroy a box. In order, it:
//
//   - asks the provider for every instance it holds, adopts each box the book
//     holds as restoring (its make ran past its deadline with Vultr still
//     restoring the instance) that Vultr now says is up, drops one Vultr no
//     longer holds, and destroys one still restoring up_minutes after the
//     make gave up waiting; then destroys each instance
//     named for a box (cloud1, cloud2, ...) that the book does not hold, an
//     orphan of a make that was killed, which is billed and which Down cannot
//     find, having no state; each is a move, and one that cannot be destroyed
//     yet (Vultr locks an instance being restored) is said once and tried
//     again at the next check;
//   - says a box up once its first dispatch has reached the beads server, and
//     up, not dispatching, with why, when none has CloudDispatchWait after it
//     was made;
//   - marks each box idle from the first check that finds no story claimed on
//     it that a session works, and busy again once one is: a story queued for
//     its rig's batch is claimed on the box but has no session, so it does not
//     keep the box busy;
//   - sets the plan's BoxCap on each box that has dispatched and runs another
//     (a changed box_cap reaches the boxes already up, raised or lowered:
//     running sessions finish, no new ones start above the cap); a box that
//     cannot be reached keeps its old cap in the count, and its CapFault says
//     why;
//   - gives up a box begun the plan's MakeStale ago and not yet up (the process
//     making it died) and destroys it;
//   - destroys every box up when one more hour of them would pass the
//     month's cap, whether or not its work could be saved: the cap wins, the
//     salvage is tried once, and what it could not save is a move and an
//     alert to the Governor;
//   - destroys each box idle for Plan.Idle;
//   - makes one box when more stories wait for any host than there are
//     sessions free across the hosts and the boxes, while there are fewer
//     than Plan.MaxBoxes, and the month can pay a first hour of it and one
//     more hour of every box with it.
//
// The month's spend is the box hours the book counts, each begun hour a
// whole one, or the provider's billing when it answers more. At the cap no
// box is made and one cloud event says so, once a month. A box that does not
// come up is destroyed and tried once more; when that fails too, no box is
// made for CloudFailHold. Every up, down and failure is a cloud event, actor
// cloud@Host, and a move the book keeps for mw status. The book is written
// before a box is asked for, so a box that costs is never one it does not
// know of.
//
// The check never waits for a box: a make takes minutes (terraform apply) and
// must outlive a caller that is stopped, so the check writes the box into the
// book as being made, asks the provider to Begin it in a process of its own
// and returns. That process is CloudMake, which says how the make ended.
type CloudCheck struct {
	Work CloudWork
	// Notes is where a box's first dispatch is seen to reach the beads server.
	// Nil sees none, and no box is said up.
	Notes CloudNotes
	// Gate is where each box's gate note is written, which the box's dispatch
	// reads before it takes a story. Nil writes none: the book alone says which
	// box may work.
	Gate CloudGate
	// Stories is where the stories pathed to a box are found and re-pathed
	// when the box is destroyed. Nil re-paths none.
	Stories  CloudStories
	Provider CloudProvider
	Book     CloudBook
	// Box, Vault and Files are what a box's kept worktrees are saved with
	// before the box is destroyed: Box brings the work back, Vault says where
	// it belongs and Files commits and pushes it. A nil Box saves none (and
	// destroys a box as before); with a Box, Vault and Files are needed.
	Box   CloudBoxWork
	Vault Vault
	Files VaultFiles
	Log   EventLog
	// Notice tells the Governor what only he can mend. Nil tells no one.
	Notice CloudNotice
	// Host is the host the check runs on: its events' actor is cloud@Host.
	Host string
	Plan CloudPlan
	Now  func() time.Time
	// HarnessVersion is the Claude Code version of the home (`mw version`), which
	// a box is brought to before its dispatch timer is set. Nil, or one that
	// says nothing, brings no box to any.
	HarnessVersion func(ctx context.Context) (string, error)
	// SessionDeny is the home's [session] deny rules (its config.toml), which a
	// box is brought level to once, as with the harness. Nil, one that errs, or
	// one that says no rules, sets no box: a box is never emptied.
	SessionDeny func(ctx context.Context) ([]string, error)
	// Out is where what the check did and could not do is said. Nil says
	// nothing.
	Out io.Writer
}

// CloudReport is what one check found and did.
type CloudReport struct {
	// Waiting are the stories that wait for any host, Free the sessions free
	// across the hosts and the boxes.
	Waiting, Free int
	// Coming are the sessions the boxes still being made, or restoring, will
	// run once they are up: not free, and counted only so that a second box is
	// not made for the stories a box on its way already serves.
	Coming   int
	Boxes    int
	SpentUSD float64
	// Moves are the cloud events the check wrote.
	Moves []string
	// Making are the boxes the check began, still being made.
	Making []string
	// Paces say, for each box up, the pace of its dispatch timer.
	Paces []string
}

// String is the report in one line.
func (r CloudReport) String() string {
	line := fmt.Sprintf("cloud: %d stories wait for any host, %d sessions free, %d boxes up, $%.2f spent this month", r.Waiting, r.Free, r.Boxes, r.SpentUSD)
	if r.Coming > 0 {
		line = fmt.Sprintf("cloud: %d stories wait for any host, %d sessions free (%d more once the boxes being made are up), %d boxes up, $%.2f spent this month", r.Waiting, r.Free, r.Coming, r.Boxes, r.SpentUSD)
	}
	for _, m := range r.Moves {
		line += "\n  " + m
	}
	for _, name := range r.Making {
		line += "\n  making " + name + " in the background; the book says when it is up"
	}
	for _, pace := range r.Paces {
		line += "\n  " + pace
	}
	return line
}

// Run does one check. A box the provider could not destroy, or a book that
// could not be written, is an error, once the rest of the check is done.
func (c CloudCheck) Run(ctx context.Context) (CloudReport, error) {
	switch {
	case c.Work == nil || c.Provider == nil || c.Book == nil:
		return CloudReport{}, fmt.Errorf("the cloud check needs the tracker, a provider and the book")
	case c.Plan.MaxBoxes < 0 || c.Plan.MonthlyCapUSD <= 0 || c.Plan.HourlyUSD <= 0 || c.Plan.Idle <= 0:
		return CloudReport{}, fmt.Errorf("the cloud check needs a cap, an hourly price and an idle time above zero")
	case c.Box != nil && (c.Vault == nil || c.Files == nil):
		return CloudReport{}, fmt.Errorf("the cloud check saves a box's worktrees into the vault: it needs the vault and its files")
	}
	now := c.now()
	state, err := c.Book.Read(ctx)
	if err != nil {
		return CloudReport{}, fmt.Errorf("reading the cloud's book: %w", err)
	}
	work, err := c.Work.WorkInHand(ctx)
	if err != nil {
		return CloudReport{}, fmt.Errorf("reading the work in hand: %w", err)
	}
	p := cloudPass{check: c, now: now, state: state}
	p.roll()
	p.billed = p.askBilled(ctx)
	p.reconcile(ctx)
	p.dropStale(ctx)
	p.seeSlowMakes(ctx)
	p.checkBoxes(ctx)
	p.seeDispatch(ctx)
	p.dropUndispatched(ctx, work)
	p.markIdle(work)
	p.setCaps(ctx)
	p.setPaces(ctx)
	p.setSessionDenies(ctx)

	if n := len(p.state.Boxes); n > 0 && p.spent()+c.Plan.HourlyUSD*float64(n) > c.Plan.MonthlyCapUSD {
		p.capReached(ctx, "destroying every box")
		for _, box := range append([]CloudBox(nil), p.state.Boxes...) {
			if !box.Making && !p.down(ctx, box.Name, cloudCapWhy) {
				break
			}
		}
	}
	for _, box := range append([]CloudBox(nil), p.state.Boxes...) {
		if !box.Making && !box.IdleSince.IsZero() && now.Sub(box.IdleSince) >= c.Plan.Idle {
			p.down(ctx, box.Name, "idle "+Clock(now.Sub(box.IdleSince)))
		}
	}
	p.repathGone(ctx, work)
	p.report.Waiting, p.report.Free, p.report.Coming = p.demand(work)
	if p.report.Waiting > p.report.Free+p.report.Coming && len(p.state.Boxes) < c.Plan.MaxBoxes {
		p.make(ctx)
	}
	if p.dirty != "" {
		p.keep(ctx, p.dirty)
	}
	p.report.Boxes, p.report.SpentUSD = len(p.state.Boxes), p.spent()
	for _, box := range p.state.Boxes {
		if !box.Making && !box.Restoring {
			p.report.Paces = append(p.report.Paces, fmt.Sprintf("%s: dispatch timer %s", box.Name, box.paceText()))
		}
	}
	return p.report, p.err
}

// Down destroys one box by hand, `mw cloud down <box>`, and says what it did.
// name must be a box's name (the plan's prefix and a number): a host that is
// not a cloud box is refused, so that no desktop's stories are re-pathed. A
// box the book does not hold is not an error, its stories are still re-pathed.
func (c CloudCheck) Down(ctx context.Context, name string) (string, error) {
	return c.down(ctx, name, false)
}

// DownForce is `mw cloud down --force <box>`: Down without the salvage. The
// box's kept worktrees are not looked at or saved, and the output and the
// move say the work was not saved. It ends a box whose teardown the salvage
// keeps refusing (mw-gq6.1043).
func (c CloudCheck) DownForce(ctx context.Context, name string) (string, error) {
	return c.down(ctx, name, true)
}

func (c CloudCheck) down(ctx context.Context, name string, force bool) (string, error) {
	if c.Provider == nil || c.Book == nil {
		return "", fmt.Errorf("mw cloud down needs a provider and the book")
	}
	if c.Box != nil && (c.Vault == nil || c.Files == nil) {
		return "", fmt.Errorf("mw cloud down saves a box's worktrees into the vault: it needs the vault and its files")
	}
	if !c.Plan.isBox(name) {
		return "", fmt.Errorf("%q is not a cloud box: a box is named %s and a number", name, c.Plan.prefix())
	}
	state, err := c.Book.Read(ctx)
	if err != nil {
		return "", fmt.Errorf("reading the cloud's book: %w", err)
	}
	p := cloudPass{check: c, now: c.now(), state: state, force: force}
	p.roll()
	why := "mw cloud down"
	if force {
		why = cloudForceWhy
	}
	ok := p.down(ctx, name, why)
	if p.dirty != "" {
		p.keep(ctx, p.dirty)
	}
	lines := append([]string(nil), p.report.Moves...)
	if !ok {
		return strings.Join(lines, "\n"), p.err
	}
	if len(lines) == 0 {
		lines = append(lines, fmt.Sprintf("%s is not in the book: nothing to destroy", name))
	}
	return strings.Join(lines, "\n"), p.err
}

func (c CloudCheck) now() time.Time {
	if c.Now == nil {
		return time.Now().UTC()
	}
	return c.Now().UTC()
}

func (c CloudCheck) say(format string, args ...any) {
	if c.Out != nil {
		fmt.Fprintf(c.Out, "mw cloud: "+format+"\n", args...)
	}
}

// cloudCapWhy is why down is told when the month's cap destroys a box, and
// cloudForceWhy when mw cloud down --force does.
const (
	cloudCapWhy   = "the month's cap"
	cloudForceWhy = "mw cloud down --force: its work was not saved"
)

// cloudPass is one check's working state.
type cloudPass struct {
	check  CloudCheck
	now    time.Time
	state  CloudState
	billed float64
	// dirty says why the state wants writing at the end, "" when it does not.
	dirty  string
	report CloudReport
	err    error
	// force destroys without salvage (mw cloud down --force).
	force bool
	// saved are the vault paths each story's work was saved to from a box this
	// pass destroyed, and unsaved the reason a story's work was left on it
	// (mw-gq6.1144): the comment on a story handed back says them.
	saved   map[string][]string
	unsaved map[string]string
	// repathed are the boxes whose stories the down pass re-pathed this check.
	repathed map[string]bool
	// unread are the boxes whose note of a dispatch could not be read this
	// check: they are not judged to have none.
	unread map[string]bool
	// listed is that Vultr's instances were read this check, so that the book's
	// word of which box is locked is this check's.
	listed bool
}

func (p *cloudPass) fail(err error) {
	p.check.say("%v", err)
	if p.err == nil {
		p.err = err
	}
}

// roll starts the month's count afresh when the book's month is past.
func (p *cloudPass) roll() {
	if month := cloudMonth(p.now); p.state.Month != month {
		p.state.Month, p.state.Hours = month, 0
		p.dirty = "a new month"
	}
	for name, at := range p.state.Downed {
		if p.now.Sub(at) >= CloudNameBar {
			delete(p.state.Downed, name)
			p.dirty = "name free again"
		}
	}
}

// askBilled is what the provider's billing says of the boxes' names this
// month, 0 when it cannot say.
func (p *cloudPass) askBilled(ctx context.Context) float64 {
	var names []string
	for i := 1; i <= max(p.check.Plan.MaxBoxes, len(p.state.Boxes)); i++ {
		names = append(names, fmt.Sprintf("%s%d", p.check.Plan.prefix(), i))
	}
	// A name skipped because its box was downed lately is billed for too, and so
	// is a box made under a name past the plan's count.
	for _, box := range p.state.Boxes {
		if !slices.Contains(names, box.Name) {
			names = append(names, box.Name)
		}
	}
	for name := range p.state.Downed {
		if !slices.Contains(names, name) {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	usd, known, err := p.check.Provider.Billed(ctx, names)
	if err != nil || !known {
		if err != nil {
			p.check.say("the provider's billing could not be read, so the book's count stands: %v", err)
		}
		return 0
	}
	return usd
}

// spent is the month's spend: the book's count or the billing's, whichever is
// more.
func (p *cloudPass) spent() float64 {
	return max(float64(p.state.hours(p.now))*p.check.Plan.HourlyUSD, p.billed)
}

// reconcile reads the provider's instances back and sets the book right by
// them, before anything is believed of the book: a restoring box is adopted
// when Vultr says it is up, given up when it is gone or has restored too long,
// and each instance named for a box that the book does not hold is destroyed.
// An orphan is billed and counts against the account's limits, and no state is
// there for Down to find it by. A box the book holds, being made, restoring or
// up, is never one.
func (p *cloudPass) reconcile(ctx context.Context) {
	instances, err := p.check.Provider.Instances(ctx)
	if err != nil {
		p.check.say("the provider's instances could not be listed, so no orphan is looked for and no restoring box is read: %v", err)
		return
	}
	p.settleRestoring(ctx, instances)
	p.readMakes(instances)
	p.listed = true
	held := map[string]bool{}
	for _, box := range p.state.Boxes {
		held[box.Name] = true
	}
	present := map[string]bool{}
	for _, inst := range instances {
		present[inst.ID] = true
		if !p.boxName(inst.Label) || held[inst.Label] {
			continue
		}
		which := fmt.Sprintf("%s (Vultr instance %s, %s)", inst.Label, inst.ID, inst.Status)
		if !p.reapInstance(ctx, inst, "orphan "+which+", no box of the book's") {
			continue
		}
		p.move(ctx, fmt.Sprintf("destroyed orphan %s: a box no state held, billed and in no book", which))
		p.dirty = "destroyed orphan " + inst.Label
	}
	// What was said of an instance that Vultr no longer holds is no more to say.
	kept := p.state.OrphansSaid[:0:0]
	for _, id := range p.state.OrphansSaid {
		if present[id] {
			kept = append(kept, id)
		}
	}
	if !slices.Equal(kept, p.state.OrphansSaid) {
		p.state.OrphansSaid = kept
		p.dirty = "orphans said"
	}
}

// readMakes keeps, on each box the book holds, what Vultr says of the instance
// labelled for it: while it is being made, its word ("active locked", a snapshot
// restore), so that mw status and DAMPERS can say a make that sits there is not a
// box up; and for every box its instance id and when it was first found locked,
// so that a lock past CloudLockedLimit is named with the id to open a ticket for.
func (p *cloudPass) readMakes(instances []CloudInstance) {
	for i := range p.state.Boxes {
		box := &p.state.Boxes[i]
		var found CloudInstance
		held := false
		for _, inst := range instances {
			if inst.Label == box.Name {
				found, held = inst, true
			}
		}
		if box.Making && box.Vultr != found.Text() {
			box.Vultr = found.Text()
			p.dirty = box.Name + " vultr"
		}
		// Vultr's word of a make is not its word of the box once it is made: the
		// book of cloud4 and cloud5 went on saying "active locked" for hours after
		// Vultr had said ok, and was read as a box locked while it worked
		// (mw-gq6.1172). LockedSince is what says a made box is locked.
		if !box.Making && !box.Restoring && box.Vultr != "" {
			box.Vultr = ""
			p.dirty = box.Name + " vultr"
		}
		id, since := "", time.Time{}
		if held {
			id = found.ID
			if found.Server == "locked" {
				since = box.LockedSince
				if since.IsZero() {
					since = p.now
				}
			}
		}
		if box.VultrID != id || !box.LockedSince.Equal(since) {
			box.VultrID, box.LockedSince = id, since
			p.dirty = box.Name + " vultr"
		}
	}
}

// settleRestoring reads each box the book holds as restoring against what
// Vultr holds. One Vultr says is up is adopted into the provider's state and
// is a box up. One Vultr no longer holds is dropped. One still restoring is
// waited for, the make's deadline again, and then destroyed.
func (p *cloudPass) settleRestoring(ctx context.Context, instances []CloudInstance) {
	for _, box := range append([]CloudBox(nil), p.state.Boxes...) {
		if !box.Restoring {
			continue
		}
		var inst CloudInstance
		found := false
		for _, i := range instances {
			if i.ID == box.InstanceID {
				inst, found = i, true
			}
		}
		since := Clock(p.now.Sub(box.RestoringSince))
		switch {
		case !found:
			p.state.Hours += billedHours(maxTime(box.Up, cloudMonthStart(p.now)), p.now)
			p.dropBox(box.Name)
			p.move(ctx, fmt.Sprintf("%s is gone from Vultr: the instance %s it was restoring is no longer held; the book drops it", box.Name, box.InstanceID))
			p.dirty = "gone " + box.Name
		case inst.Up() && p.noPeer(ctx, box):
			// Vultr finished a restore the hub has no peer for: the box would
			// sit waiting for a first dispatch it can never make, and bill
			// idle (cloud7, 2026-11-13). It is destroyed, and no made move is
			// written.
			p.down(ctx, box.Name, fmt.Sprintf("Vultr finished restoring it, but %s is not a peer of the hub: it has no address, so it can take no story", box.Name))
		case inst.Up() && p.adopt(ctx, box, inst):
			p.setBox(box.Name, func(b *CloudBox) {
				b.Restoring, b.InstanceID, b.RestoringSince, b.IdleSince, b.Made = false, "", time.Time{}, p.now, p.now
			})
			p.state.FeeLimitAt = time.Time{}
			p.move(ctx, fmt.Sprintf("made %s: Vultr finished restoring it, %s after the make gave up waiting; adopted; %s", box.Name, since, cloudUpWhen))
			p.dirty = "made " + box.Name
		case p.now.Sub(box.RestoringSince) >= p.check.Plan.UpLimit():
			if p.down(ctx, box.Name, "it did not come up") {
				p.move(ctx, fmt.Sprintf("destroyed %s: Vultr was still restoring instance %s %s after the make gave up waiting", box.Name, box.InstanceID, since))
			}
		}
	}
}

// noPeer reports whether the hub has no peer for the box, by the address the
// provider reads off it: the make asks the same of a box that came up in time
// (CloudMake.enrolled). Any other trouble reading the address leaves the box
// adopted as before.
func (p *cloudPass) noPeer(ctx context.Context, box CloudBox) bool {
	_, err := p.check.Provider.Address(ctx, box.Name)
	return noHubAddress(err)
}

// adopt has the provider take the instance of a restoring box into its state,
// and reports whether it did; a box it could not take is tried again at the
// next check, until the restore has taken too long.
func (p *cloudPass) adopt(ctx context.Context, box CloudBox, inst CloudInstance) bool {
	if err := p.check.Provider.Adopt(ctx, box.Name, inst); err != nil {
		p.check.say("Vultr says %s is up, but it could not be adopted: %v", box.Name, err)
		return false
	}
	return true
}

func (p *cloudPass) setBox(name string, change func(b *CloudBox)) {
	for i := range p.state.Boxes {
		if p.state.Boxes[i].Name == name {
			change(&p.state.Boxes[i])
		}
	}
}

func (p *cloudPass) dropBox(name string) {
	p.state.Boxes = slices.DeleteFunc(p.state.Boxes, func(b CloudBox) bool { return b.Name == name })
}

// held reads the provider back for the instance labelled name.
func (p *cloudPass) held(ctx context.Context, name string) (inst CloudInstance, found bool, err error) {
	instances, err := p.check.Provider.Instances(ctx)
	if err != nil {
		return CloudInstance{}, false, err
	}
	for _, i := range instances {
		if i.Label == name {
			return i, true, nil
		}
	}
	return CloudInstance{}, false, nil
}

// reapInstance destroys one instance Vultr holds, by its id, and reports
// whether it is gone: the provider answers well only to Vultr's 204 or 404.
// What it was refused is a move the first time, and said again, quietly, at
// each check after.
func (p *cloudPass) reapInstance(ctx context.Context, inst CloudInstance, what string) bool {
	err := p.check.Provider.Reap(ctx, inst)
	if err == nil {
		p.state.OrphansSaid = slices.DeleteFunc(p.state.OrphansSaid, func(id string) bool { return id == inst.ID })
		return true
	}
	text := fmt.Sprintf("could not destroy %s: %v; tried again at each check", what, oneLine(err.Error()))
	if slices.Contains(p.state.OrphansSaid, inst.ID) {
		p.check.say("%s", text)
		return false
	}
	p.state.OrphansSaid = append(p.state.OrphansSaid, inst.ID)
	p.dirty = "orphans said"
	p.move(ctx, text)
	return false
}

// boxName reports whether label is a name a box is given: the prefix and a
// number.
func (p *cloudPass) boxName(label string) bool {
	n, ok := strings.CutPrefix(label, p.check.Plan.prefix())
	if !ok || n == "" {
		return false
	}
	for _, r := range n {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// feeLimitUntil is when a box may be tried again after Vultr's fee-limit
// refusal: CloudFeeLimitPause after it, or zero when the book holds none or a
// box was destroyed since (markDowned keeps when; a make that did not come up
// records none, so the refused try's own destroy ends nothing).
func (s CloudState) feeLimitUntil() time.Time {
	if s.FeeLimitAt.IsZero() {
		return time.Time{}
	}
	for _, at := range s.Downed {
		if at.After(s.FeeLimitAt) {
			return time.Time{}
		}
	}
	return s.FeeLimitAt.Add(CloudFeeLimitPause)
}

// feeLimit notes that Vultr refused a box for its monthly fee limit. It is a
// money cap and no fault of a make: the first refusal is a move and one card
// to the Governor, who alone can raise the limit; the box is tried again after
// CloudFeeLimitPause, and a box that comes up ends it.
func (p *cloudPass) feeLimit(ctx context.Context) {
	first := p.state.FeeLimitAt.IsZero()
	p.state.FeeLimitAt = p.now
	p.dirty = "Vultr fee limit"
	if !first {
		p.check.say("%v", ErrCloudFeeLimit)
		return
	}
	p.move(ctx, ErrCloudFeeLimit.Error())
	if p.check.Notice == nil {
		return
	}
	if err := p.check.Notice.Tell(ctx, "Vultr will make no cloud box: its monthly fee limit is reached", ErrCloudFeeLimit.Error()); err != nil {
		p.check.say("the card to the Governor could not be sent: %v", err)
	}
}

// dropStale gives up each box begun the plan's MakeStale ago and still being made:
// destroys what there is of it, and says so.
func (p *cloudPass) dropStale(ctx context.Context) {
	for _, box := range append([]CloudBox(nil), p.state.Boxes...) {
		if !box.Making || p.now.Sub(box.Up) < p.check.Plan.MakeStale() {
			continue
		}
		if p.down(ctx, box.Name, "it did not come up") {
			p.move(ctx, fmt.Sprintf("failed %s: the make begun at %s never said how it ended; destroyed", box.Name, box.Up.Format("15:04Z")))
			p.keep(ctx, "failed "+box.Name)
		}
	}
}

// seeSlowMakes says, once for each, a box whose make has run past the plan's
// MakeSlowLimit: a cloud event naming the box and what Vultr says of it. DAMPERS
// names it for as long as the make runs (SlowMakes).
func (p *cloudPass) seeSlowMakes(ctx context.Context) {
	for i := range p.state.Boxes {
		box := &p.state.Boxes[i]
		if !box.Making || box.SlowSaid || p.now.Sub(box.Up) < p.check.Plan.MakeSlowLimit() {
			continue
		}
		box.SlowSaid = true
		p.dirty = box.Name + " slow make"
		p.move(ctx, fmt.Sprintf("%s: the make begun at %s has not finished after %s (the limit is %s); %s",
			box.Name, box.Up.UTC().Format("15:04Z"), Clock(p.now.Sub(box.Up)), Clock(p.check.Plan.MakeSlowLimit()), box.vultrText()))
	}
}

// vultrText is what Vultr said of the box at the last check, in a phrase.
func (b CloudBox) vultrText() string {
	if b.Vultr == "" {
		return "Vultr's status of it is not known"
	}
	return "Vultr says " + b.Vultr
}

// CloudMakeAlarm is one alarm the book holds of a box, for DAMPERS and the quiet
// alarm: Box names it, Text says it, Identity keeps one alarm one line.
type CloudMakeAlarm struct {
	Box, Key, Text, Identity string
}

func (a CloudMakeAlarm) damper() DamperAlarm {
	return DamperAlarm{Principle: DamperCloudMake, Text: a.Text, Identity: a.Identity}
}

// MakeAlarms are the alarms of the boxes the book holds, oldest first: a box being
// made for longer than the plan's MakeSlowLimit, with Vultr's word and the step
// that ends it (mw cloud down <box>), and a box Vultr has reported locked for
// longer than CloudLockedLimit, making or not, with the instance id to open a
// Vultr ticket for; and a box that failed its own check (mw-gq6.1172), with
// what it found and the same step.
func (s CloudState) MakeAlarms(plan CloudPlan, now time.Time) []CloudMakeAlarm {
	var alarms []CloudMakeAlarm
	for _, box := range s.Boxes {
		if box.Making && now.Sub(box.Up) >= plan.MakeSlowLimit() {
			alarms = append(alarms, CloudMakeAlarm{Box: box.Name, Key: "cloud:" + box.Name, Identity: "cloud make " + box.Name,
				Text: fmt.Sprintf("%s has been made for %s and the make has not finished; %s; to give it up: mw cloud down %s",
					box.Name, Clock(now.Sub(box.Up)), box.vultrText(), box.Name)})
		}
		if !box.LockedSince.IsZero() && now.Sub(box.LockedSince) >= CloudLockedLimit {
			id := box.VultrID
			if id == "" {
				id = "(id not known)"
			}
			alarms = append(alarms, CloudMakeAlarm{Box: box.Name, Key: "cloud.locked:" + box.Name, Identity: "cloud locked " + box.Name,
				Text: fmt.Sprintf("%s has been locked at Vultr for %s (instance %s): open a Vultr ticket for instance %s, then mw cloud down %s",
					box.Name, Clock(now.Sub(box.LockedSince)), id, id, box.Name)})
		}
		if !box.Making && !box.Restoring && box.CheckFault != "" {
			alarms = append(alarms, CloudMakeAlarm{Box: box.Name, Key: "cloud.check:" + box.Name, Identity: "cloud check " + box.Name,
				Text: fmt.Sprintf("%s failed its own check and takes no story, billed all the while (%s); to destroy it: mw cloud down %s",
					box.Name, box.CheckFault, box.Name)})
		}
	}
	return alarms
}

// cloudUpWhen is what a move of a box made says of when it is up.
const cloudUpWhen = "up once its first dispatch reaches the beads server"

// seeDispatch says each box made up once its first dispatch has reached the
// beads server: the note of when it was last level (LastSyncKey), which its
// dispatch's sync writes into the one database, at or after the box's make
// began. Vultr's word is not enough: a box made from a snapshot that named
// itself by the box it was made from, or whose vault was broken, ran and
// claimed nothing (mw-5gr3k0.8). A box with no such note CloudDispatchWait
// after it was made is said up, not dispatching, once, with what the note says.
func (p *cloudPass) seeDispatch(ctx context.Context) {
	if p.check.Notes == nil {
		return
	}
	for i := range p.state.Boxes {
		box := &p.state.Boxes[i]
		if box.Making || box.Restoring || !box.Dispatched.IsZero() {
			continue
		}
		said, err := p.check.Notes.Note(ctx, LastSyncKey(box.Name))
		if err != nil {
			p.check.say("whether %s has dispatched could not be read: %v", box.Name, err)
			if p.unread == nil {
				p.unread = map[string]bool{}
			}
			p.unread[box.Name] = true
			continue
		}
		said = strings.TrimSpace(said)
		at, perr := time.Parse(LastSyncFormat, said)
		if said != "" && perr == nil && !p.state.NoteOfAnOlderBox(p.check.Plan, box.Name, at) {
			box.Dispatched, box.NotDispatching = at, ""
			p.move(ctx, fmt.Sprintf("up %s: its first dispatch reached the beads server at %s, %s after it was made", box.Name, at.UTC().Format("15:04Z"), Clock(max(0, at.Sub(box.made())))))
			p.dirty = "up " + box.Name
			continue
		}
		// A box that failed its own check does not dispatch, and the book says why.
		if box.NotDispatching != "" || box.CheckFault != "" || p.now.Sub(box.made()) < CloudDispatchWait {
			continue
		}
		why := fmt.Sprintf("no dispatch of %s has reached the beads server in %s since it was made", box.Name, Clock(p.now.Sub(box.made())))
		switch {
		case said == "":
			why += " (the server holds no note of it ever being level)"
		case perr != nil:
			why += fmt.Sprintf(" (its note of when it was last level reads %q)", said)
		default:
			why += fmt.Sprintf(" (it was last level %s, before this box was made)", at.UTC().Format("01-02 15:04Z"))
		}
		why += "; on the box, see the host line of ~/.config/mw/config.toml, git status in the vault and journalctl --user -u mw-dispatch"
		box.NotDispatching = why
		p.move(ctx, box.Name+" up, not dispatching: "+why)
		p.dirty = box.Name + " not dispatching"
	}
}

// dropUndispatched destroys each box made Plan.NoDispatchLimit ago or more
// whose first dispatch has not reached the beads server, and says why in the
// cloud event. A box that never dispatches takes no story and is billed all the
// same: the one seen was made without an address on the hub, so the home could
// neither set its pace nor ask it anything, and a refused teardown (its
// worktrees could not be asked) kept it up and billed (mw-gq6.1143). A
// replacement may be made, the same check, when stories still wait. It needs
// Notes, as seeDispatch does: without them no box is known to have dispatched.
// A box with a story claimed on it is left alone.
func (p *cloudPass) dropUndispatched(ctx context.Context, work WorkInHand) {
	if p.check.Notes == nil {
		return
	}
	limit := p.check.Plan.NoDispatchLimit()
	for _, box := range append([]CloudBox(nil), p.state.Boxes...) {
		if box.Making || box.Restoring || !box.Dispatched.IsZero() || p.unread[box.Name] || p.now.Sub(box.made()) < limit {
			continue
		}
		// A box that failed its own check is kept from dispatching by its gate:
		// the alarm names it and mw cloud down ends it, so that a box is not made
		// in its place from the same snapshot every half hour (mw-gq6.1172).
		if box.CheckFault != "" {
			continue
		}
		// A story claimed on the box says it has dispatched, whatever the book
		// kept (a book written before it kept Dispatched): it is not the box seen.
		if claimedOnBox(work, box.Name) {
			continue
		}
		why := fmt.Sprintf("no dispatch of %s has reached the beads server in %s since it was made", box.Name, Clock(p.now.Sub(box.made())))
		if _, err := p.check.Provider.Address(ctx, box.Name); err != nil && noHubAddress(err) {
			why += ": it is not a peer of the hub, it has no address"
		}
		p.down(ctx, box.Name, why)
	}
}

// noHubAddress says err is the provider's word that the box is not a peer of the
// hub and so has no address (vultr-boost address), not a hub it could not reach.
func noHubAddress(err error) bool {
	return err != nil && strings.Contains(err.Error(), "is not a peer of the hub")
}

// nothingToSave says, when box has no address on the hub and no story claimed on
// it, that nothing on it needs saving: it has run no session, and it cannot be
// asked for its worktrees. The claims are read off the beads server, an
// in-progress or open claim held as mw@<box> or builder@<box>; a claim, a
// tracker that cannot be read, or an address that is there (a box that does not
// answer is another matter) keeps the teardown refused as before.
func (p *cloudPass) nothingToSave(ctx context.Context, box CloudBox) (string, bool) {
	if p.check.Work == nil {
		return "", false
	}
	if _, err := p.check.Provider.Address(ctx, box.Name); !noHubAddress(err) {
		return "", false
	}
	work, err := p.check.Work.WorkInHand(ctx)
	if err != nil {
		return "", false
	}
	if claimedOnBox(work, box.Name) {
		return "", false
	}
	return fmt.Sprintf("%s is not a peer of the hub (no address) and no story is claimed on it by %s or %s: nothing on it needed saving", box.Name, boxHolders(box.Name)[0], boxHolders(box.Name)[1]), true
}

// boxHolders are the assignees a claim made on a box is held under: mw@<box>,
// the dispatch's, and builder@<box>.
func boxHolders(box string) []string {
	return []string{SeatIdentity(MwSeat, box), SeatIdentity("builder", box)}
}

// claimedOnBox says a story is claimed on the box, in progress, open and still
// assigned, or held, by either of its holders.
func claimedOnBox(work WorkInHand, box string) bool {
	holders := boxHolders(box)
	for _, list := range [][]StoryDetail{work.Running, work.OpenClaimed, work.Held} {
		for _, d := range list {
			if slices.Contains(holders, d.Assignee) {
				return true
			}
		}
	}
	return false
}

func (p *cloudPass) markIdle(work WorkInHand) {
	for i := range p.state.Boxes {
		box := &p.state.Boxes[i]
		if box.Making || box.Restoring {
			continue
		}
		busy := sessions(work, box.Name, p.now) > 0
		switch {
		case busy && !box.IdleSince.IsZero():
			box.IdleSince = time.Time{}
			p.dirty = box.Name + " is busy"
		case !busy && box.IdleSince.IsZero():
			box.IdleSince = p.now
			p.dirty = box.Name + " is idle"
		}
	}
}

// setCaps sets the plan's BoxCap on each box up that runs another, once its
// first dispatch has reached the beads server (before that the box may not
// answer ssh yet, and its config is the bootstrap's own). A box that took it
// is counted at the new cap, and is a move in the book for each box and one
// cloud event for all of them; a box that could not be reached keeps its cap,
// is counted at it, and is a move and a CapFault the first time for that cap,
// said again at each check without a move.
func (p *cloudPass) setCaps(ctx context.Context) {
	want := p.check.Plan.boxCap()
	var set []string
	for i := range p.state.Boxes {
		box := &p.state.Boxes[i]
		if box.Making || box.Restoring || box.Dispatched.IsZero() {
			continue
		}
		if box.Cap == want {
			if box.CapFault != "" {
				box.CapFault = ""
				p.dirty = box.Name + " cap"
			}
			continue
		}
		if err := p.check.Provider.SetCap(ctx, box.Name, want); err != nil {
			text := fmt.Sprintf("could not set cap %d on %s: %v", want, box.Name, oneLine(err.Error()))
			if first := !strings.HasPrefix(box.CapFault, fmt.Sprintf("could not set cap %d ", want)); first {
				box.CapFault = text
				p.move(ctx, fmt.Sprintf("%s; it is counted at the cap %d it has", text, box.boxCap(p.check.Plan)))
				p.dirty = box.Name + " cap"
			} else {
				p.check.say("%s", text)
			}
			continue
		}
		was := box.boxCap(p.check.Plan)
		known := box.Cap > 0
		box.Cap, box.CapFault = want, ""
		p.dirty = box.Name + " cap"
		set = append(set, box.Name)
		what := fmt.Sprintf("%s: cap set to %d", box.Name, want)
		if known {
			what = fmt.Sprintf("%s: cap %d to %d", box.Name, was, want)
		}
		p.check.say("%s", what)
		p.report.Moves = append(p.report.Moves, what)
		p.state.Moves = p.addMove(CloudMove{At: p.now, What: what})
	}
	if len(set) > 0 {
		p.emit(ctx, fmt.Sprintf("box_cap %d set on %s: sessions a box runs at once", want, strings.Join(set, ", ")))
	}
}

// setPaces makes each box up run its dispatch timer every five minutes. A box
// made from the snapshot carries the hourly timer of the home, which an event
// follower springs; a box has none, so it would wait up to an hour for each
// dispatch (mw-gq6.1004). Unlike the cap it does not wait for the box's first
// dispatch, which an hourly box may be a long time making. A box known to be at
// the pace does not have its timer set again, but its Claude Code is brought
// level whenever the home's version is not the one it was last brought level to
// (mw-gq6.1062); one that could not be reached for its pace is a move the first
// time for that fault, and tried at the next check. The home's version is read
// once a pass.
func (p *cloudPass) setPaces(ctx context.Context) {
	home, read := "", false
	for i := range p.state.Boxes {
		box := &p.state.Boxes[i]
		if box.Making || box.Restoring {
			continue
		}
		if !read && p.check.HarnessVersion != nil {
			read = true
			version, err := p.check.HarnessVersion(ctx)
			if home = HarnessNumber(version); home == "" {
				home = "-"
				p.check.say("the home's Claude Code version is not known (%q, %v): no box is brought level", version, err)
			}
		}
		if home != "" && home != "-" && box.Harness != home {
			p.setHarness(ctx, box, home)
		}
		if box.Pace == CloudPaceFive {
			continue
		}
		pace, err := p.check.Provider.SetPace(ctx, box.Name)
		if err != nil {
			text := fmt.Sprintf("could not set the dispatch timer of %s to every 5 minutes: %v", box.Name, oneLine(err.Error()))
			if box.PaceFault != text {
				box.PaceFault = text
				p.move(ctx, text)
				p.dirty = box.Name + " pace"
			} else {
				p.check.say("%s", text)
			}
			continue
		}
		pace = strings.TrimSpace(pace)
		if pace == box.Pace && box.PaceFault == "" {
			continue
		}
		was := box.Pace
		box.Pace, box.PaceFault = pace, ""
		p.dirty = box.Name + " pace"
		what := fmt.Sprintf("%s: dispatch timer set, now %s", box.Name, box.paceText())
		if was != "" {
			what += " (was " + was + ")"
		}
		p.check.say("%s", what)
		p.report.Moves = append(p.report.Moves, what)
		p.state.Moves = p.addMove(CloudMove{At: p.now, What: what})
	}
}

// setHarness brings a box's Claude Code to the home's version, and keeps the
// version the box was brought level to, saying the version before and after in
// a move. A box already level is said nothing of; one whose install failed
// stays as it was, in one line that is a move only the first time, and is tried
// again at the next pass.
func (p *cloudPass) setHarness(ctx context.Context, box *CloudBox, version string) {
	got, err := p.check.Provider.SetHarness(ctx, box.Name, version)
	if err != nil {
		// A box that cannot be reached is a fault in the book, a move the first
		// time for that fault, shown by mw status, and tried at the next pass
		// (mw-gq6.1174).
		text := fmt.Sprintf("could not read the Claude Code of %s: %v", box.Name, oneLine(err.Error()))
		if box.HarnessFault != text {
			box.HarnessFault = text
			p.move(ctx, text)
			p.dirty = box.Name + " harness"
		} else {
			p.check.say("%s", text)
		}
		return
	}
	var what string
	switch {
	case got.Why != "":
		what = fmt.Sprintf("%s: claude stays at %s, not %s: %s", box.Name, got.Was, version, oneLine(got.Why))
		if box.HarnessFault == what {
			p.check.say("%s", what)
			return
		}
		box.HarnessFault = what
	case got.Now != got.Was:
		what = fmt.Sprintf("%s: claude %s to %s (the home's)", box.Name, got.Was, got.Now)
	default:
		p.check.say("%s: claude %s is level with the home's %s", box.Name, got.Was, version)
	}
	if got.Why == "" {
		box.Harness, box.HarnessFault = version, ""
	}
	p.dirty = box.Name + " harness"
	if what == "" {
		return
	}
	p.check.say("%s", what)
	p.report.Moves = append(p.report.Moves, what)
	p.state.Moves = p.addMove(CloudMove{At: p.now, What: what})
}

// setSessionDenies brings each box up to the home's [session] deny list
// (mw-t5shinh.33), as setHarness does for Claude Code: a box whose kept list is
// the home's is not reached; one that is not is set over ssh, once, a move when
// the box's file had to change, and kept so that the next pass leaves it alone.
// A box that could not be reached is said and tried at the next pass. The home's
// list is read once a pass; an unreadable or empty one sets no box.
func (p *cloudPass) setSessionDenies(ctx context.Context) {
	if p.check.SessionDeny == nil {
		return
	}
	var rules []string
	var home string
	read := false
	for i := range p.state.Boxes {
		box := &p.state.Boxes[i]
		if box.Making || box.Restoring {
			continue
		}
		if !read {
			read = true
			var err error
			rules, err = p.check.SessionDeny(ctx)
			if err != nil {
				p.check.say("the home's [session] deny list cannot be read (%v): no box is brought level to it", oneLine(err.Error()))
				return
			}
			if len(rules) == 0 {
				return
			}
			home = domain.SessionDenyLiteral(rules)
		}
		if box.SessionDeny == home {
			continue
		}
		changed, err := p.check.Provider.SetSessionDeny(ctx, box.Name, rules)
		if err != nil {
			text := fmt.Sprintf("could not set session.deny on %s: %v", box.Name, oneLine(err.Error()))
			if box.SessionDenyFault != text {
				box.SessionDenyFault = text
				p.move(ctx, text)
				p.dirty = box.Name + " session deny"
			} else {
				p.check.say("%s", text)
			}
			continue
		}
		box.SessionDeny, box.SessionDenyFault = home, ""
		p.dirty = box.Name + " session deny"
		if !changed {
			p.check.say("%s: session.deny is level with the home's", box.Name)
			continue
		}
		what := fmt.Sprintf("%s: session.deny set to the home's %d rules", box.Name, len(rules))
		p.check.say("%s", what)
		p.report.Moves = append(p.report.Moves, what)
		p.state.Moves = p.addMove(CloudMove{At: p.now, What: what})
	}
}

// demand counts the stories that wait for any host, and the sessions free
// across the hosts the plan names and the boxes.
//
// A box still being made, or restoring, has no session: it counts none free
// (mw-gq6.1113). Its sessions are coming, so that the check does not make a
// second box for a story the first already serves. So are those of a box the
// home has not cleared yet (CloudBox.MayWork); a box that failed its own check
// counts neither.
func (p *cloudPass) demand(work WorkInHand) (waiting, free, coming int) {
	for _, d := range work.Ready {
		if d.Merged().Host == domain.HostAuto && !d.Hitl() {
			waiting++
		}
	}
	for host, limit := range p.check.Plan.HostCaps {
		if IsRetired(p.check.Plan.Retired, host) {
			continue
		}
		free += max(0, limit-sessions(work, host, p.now))
	}
	for _, box := range p.state.Boxes {
		if box.Making || box.Restoring {
			coming += box.boxCap(p.check.Plan)
			continue
		}
		if !box.MayWork() {
			// A box that failed its own check takes no story until it is destroyed:
			// it counts nothing. One Vultr holds locked, or whose check has not
			// run yet, is on its way as a box being made is (mw-gq6.1172).
			if box.CheckFault == "" {
				coming += box.boxCap(p.check.Plan)
			}
			continue
		}
		free += max(0, box.boxCap(p.check.Plan)-sessions(work, box.Name, p.now))
	}
	return waiting, free, coming
}

// sessions counts the stories claimed on host that a session works. A story
// queued for its batch keeps its claim, worktree and branch but its session is
// gone by design, so it is not one (mw-gq6.859). One set back to open, or held,
// under a session that still heartbeats is (mw-gq6.879).
func sessions(work WorkInHand, host string, now time.Time) int {
	n := 0
	for _, d := range work.RunningOnLive(host, now) {
		if !d.Hitl() && !QueuedForBatch(d) {
			n++
		}
	}
	return n
}

// make begins one box: it is written into the book as being made, and the
// provider is asked to make it in a process of its own, which CloudMake is.
func (p *cloudPass) make(ctx context.Context) {
	plan := p.check.Plan
	if p.now.Before(p.state.HeldUntil) {
		p.check.say("%d stories wait, but no box is made before %s: one failed to come up twice", p.report.Waiting, p.state.HeldUntil.Format("15:04Z"))
		return
	}
	if until := p.state.feeLimitUntil(); p.now.Before(until) {
		p.check.say("%d stories wait, but no box is made before %s: %v", p.report.Waiting, until.Format("15:04Z"), ErrCloudFeeLimit)
		return
	}
	for _, box := range p.state.Boxes {
		if (box.Making || box.Restoring) && p.now.Sub(box.Up) >= plan.MakeSlowLimit() {
			p.check.say("%d stories wait, but no box is made: %s has been made for %s and is stuck (%s); mw cloud down %s ends it",
				p.report.Waiting, box.Name, Clock(p.now.Sub(box.Up)), box.vultrText(), box.Name)
			return
		}
	}
	name := p.nextName()
	if p.spent()+plan.HourlyUSD*float64(len(p.state.Boxes)+2) > plan.MonthlyCapUSD {
		p.capReached(ctx, "no box is made")
		return
	}
	p.state.Boxes = append(p.state.Boxes, CloudBox{Name: name, Up: p.now, Making: true})
	if !p.keep(ctx, "making "+name) {
		p.state.Boxes = p.state.Boxes[:len(p.state.Boxes)-1]
		return
	}
	// The gate of the name is shut before the box is asked for: the new box
	// takes no story on the word a destroyed box of the name was cleared by.
	if err := p.shutGate(ctx, name, "it is being made"); err != nil {
		p.state.Boxes = p.state.Boxes[:len(p.state.Boxes)-1]
		p.keep(ctx, "not making "+name)
		p.fail(fmt.Errorf("not making %s: its gate note could not be shut, so a box of that name might take stories unchecked: %w", name, err))
		return
	}
	p.move(ctx, fmt.Sprintf("making %s: %d stories wait for any host, %d sessions free", name, p.report.Waiting, p.report.Free))
	p.dirty = "making " + name
	if err := p.check.Provider.Begin(ctx, name); err != nil {
		// Nothing is making it, so nothing is billed: it is not in the book.
		p.state.Boxes = p.state.Boxes[:len(p.state.Boxes)-1]
		p.dirty = "failed " + name
		p.fail(fmt.Errorf("beginning to make %s: %w", name, err))
		return
	}
	p.report.Making = append(p.report.Making, name)
}

// down destroys the box, counting its hours into the month's, and reports
// whether it is gone. It is gone when Vultr says it is, read back after the
// provider's Down: a wrapper that holds no state for a box (its make was
// killed, or its state lost) exits well and destroys nothing. A box still held
// is then destroyed by its id, and the book keeps it, saying what Vultr said,
// when Vultr will not. A box restoring has no state to find it by: it is
// destroyed by its id at once.
func (p *cloudPass) down(ctx context.Context, name, why string) bool {
	at := -1
	for i, box := range p.state.Boxes {
		if box.Name == name {
			at = i
		}
	}
	if at < 0 {
		p.repath(ctx, name, why)
		return true
	}
	box := p.state.Boxes[at]
	if box.Restoring && box.InstanceID != "" {
		if !p.reapInstance(ctx, CloudInstance{ID: box.InstanceID, Label: name}, fmt.Sprintf("%s (Vultr instance %s, restoring)", name, box.InstanceID)) {
			return false
		}
	} else {
		if !p.salvage(ctx, box, why) {
			return false
		}
		if err := p.check.Provider.Down(ctx, name); err != nil {
			p.move(ctx, fmt.Sprintf("failed to destroy %s (%s): %v", name, why, oneLine(err.Error())))
			p.fail(fmt.Errorf("destroying %s: %w", name, err))
			return false
		}
		inst, found, err := p.held(ctx, name)
		if err != nil {
			p.move(ctx, fmt.Sprintf("could not tell whether %s (%s) is destroyed: Vultr's instances could not be listed: %v", name, why, oneLine(err.Error())))
			p.fail(fmt.Errorf("reading back the destroying of %s: %w", name, err))
			return false
		}
		if found && !p.reapInstance(ctx, inst, fmt.Sprintf("%s (Vultr instance %s, %s), still held after the wrapper was done with it", name, inst.ID, inst.Status)) {
			return false
		}
	}
	p.state.Hours += billedHours(maxTime(p.state.Boxes[at].Up, cloudMonthStart(p.now)), p.now)
	p.state.Boxes = append(p.state.Boxes[:at], p.state.Boxes[at+1:]...)
	// A box that never came up wrote no notes under its name: only one that was
	// up leaves a name to rest.
	if !box.Making && !box.Restoring && why != "it did not come up" {
		p.markDowned(name)
	}
	if why != "it did not come up" {
		p.move(ctx, fmt.Sprintf("down %s: %s", name, why))
	}
	if !box.Checked.IsZero() || box.Gate != "" {
		// Best effort: a make shuts the gate of its name again before it begins.
		if err := p.shutGate(ctx, name, "it is destroyed"); err != nil {
			p.check.say("the gate note of %s could not be shut after it was destroyed: %v", name, oneLine(err.Error()))
		}
	}
	p.keep(ctx, "down "+name)
	p.repath(ctx, name, why)
	return true
}

// markDowned notes that the box named is destroyed, so that nextName leaves its
// name alone for CloudNameBar.
func (p *cloudPass) markDowned(name string) {
	if p.state.Downed == nil {
		p.state.Downed = map[string]time.Time{}
	}
	p.state.Downed[name] = p.now
}

// salvage saves what a box's kept worktrees hold before the box is destroyed,
// and reports whether the box may be: a box has no GitHub credentials, so a
// Builder's unpushed commits and uncommitted changes are held nowhere else.
// For each worktree with a branch ahead of its rig's target it bundles the
// commits into the vault's runs/<story>/ and verifies the bundle, and it keeps
// the uncommitted changes as a patch beside it, under the names mw retry uses
// (BundleFileName, PatchFileName), at the first attempt number no file of the
// story's holds, so that earlier evidence is never written over. A bundle that
// fails, one that does not verify, a vault that will not take it, or a box that
// cannot be reached refuses the teardown: the box stays up, billed, and the
// refusal names the story and why, and tells the Governor once, with the
// command that ends it (mw cloud down --force). At the month's cap it does not
// refuse: see cloudCapWhy. A box with no worktree, or whose worktrees
// have nothing ahead and nothing uncommitted, is destroyed as before.
func (p *cloudPass) salvage(ctx context.Context, box CloudBox, why string) bool {
	// A box being made or restored has run nothing, and one that did not come
	// up has no worktree to ask for. --force looks at nothing.
	if p.force || p.check.Box == nil || box.Making || box.Restoring || why == "it did not come up" {
		return true
	}
	atCap := why == cloudCapWhy
	var lost []string
	worktrees, err := p.check.Box.Worktrees(ctx, box.Name)
	if err != nil {
		if said, none := p.nothingToSave(ctx, box); none {
			p.move(ctx, said)
			worktrees, err = nil, nil
		}
	}
	if err != nil {
		err = fmt.Errorf("it could not be asked what worktrees it keeps: %w", err)
		if !atCap {
			return p.refuse(ctx, box, why, "", err)
		}
		lost = append(lost, "whatever its worktrees hold: "+oneLine(err.Error()))
	}
	for _, wt := range worktrees {
		if !wt.Holds() {
			continue
		}
		if err := p.saveWorktree(ctx, box, wt); err != nil {
			if !atCap {
				return p.refuse(ctx, box, why, wt.Story, err)
			}
			// The month's cap wins: one try, then the box is destroyed.
			if p.unsaved == nil {
				p.unsaved = map[string]string{}
			}
			p.unsaved[wt.Story] = oneLine(err.Error())
			lost = append(lost, fmt.Sprintf("story %s, branch %s at %s (%d commits never pushed, %d uncommitted paths): %s",
				wt.Story, wt.Branch, wt.Head, wt.Ahead, len(wt.Uncommitted), oneLine(err.Error())))
		}
	}
	if len(lost) > 0 {
		text := fmt.Sprintf("destroying %s anyway: the month's cap is reached and its work could not be saved; not saved: %s", box.Name, strings.Join(lost, "; "))
		p.move(ctx, text)
		p.alert(ctx, box, "Cloud box "+box.Name+" destroyed at the month's cap with work not saved", text)
		return true
	}
	if box.Refused != "" {
		p.setBox(box.Name, func(b *CloudBox) { b.Refused, b.Alerted = "", false })
		p.dirty = "saved " + box.Name
	}
	return true
}

// alert tells the Governor what only he can mend, when the check has a notice
// to tell him by, and reports whether it did. A notice that fails is said, so
// that the caller can try again at the next check.
func (p *cloudPass) alert(ctx context.Context, box CloudBox, title, text string) bool {
	if p.check.Notice == nil {
		return false
	}
	if err := p.check.Notice.Tell(ctx, title, text); err != nil {
		p.check.say("the Governor could not be told of %s: %v", box.Name, err)
		return false
	}
	return true
}

// saveWorktree brings one worktree's work into the vault, committed and
// pushed, unless the box's book says the vault already holds it.
func (p *cloudPass) saveWorktree(ctx context.Context, box CloudBox, wt CloudKeptWorktree) error {
	if runFilePath(wt.Story, "x") == "" {
		return fmt.Errorf("the worktree %s names no story the vault can keep a run for", wt.Dir)
	}
	var patch string
	if len(wt.Uncommitted) > 0 {
		var err error
		if patch, err = p.check.Box.UncommittedDiff(ctx, box.Name, wt); err != nil {
			return fmt.Errorf("reading what %s left uncommitted: %w", wt.Dir, err)
		}
	}
	key := wt.Story + " " + wt.Head
	if patch != "" {
		sum := sha256.Sum256([]byte(patch))
		key += " " + hex.EncodeToString(sum[:6])
	}
	if slices.Contains(box.Saved, key) {
		return nil
	}
	attempt, err := p.freeAttempt(ctx, wt.Story, wt.Ahead > 0, patch != "")
	if err != nil {
		return err
	}
	var paths []string
	if wt.Ahead > 0 {
		dest := p.check.Vault.RunFile(wt.Story, BundleFileName(attempt))
		if err := p.check.Box.Bundle(ctx, box.Name, wt, dest); err != nil {
			return fmt.Errorf("bundling %s into %s: %w", wt.Branch, dest, err)
		}
		if err := p.check.Box.VerifyBundle(ctx, box.Name, wt, dest); err != nil {
			return fmt.Errorf("the bundle %s does not verify: %w", dest, err)
		}
		paths = append(paths, BundleRecord(wt.Story, attempt))
	}
	if patch != "" {
		if _, err := p.check.Vault.PutRunFile(ctx, wt.Story, PatchFileName(attempt), patch); err != nil {
			return fmt.Errorf("keeping the uncommitted changes of %s in the vault: %w", wt.Dir, err)
		}
		paths = append(paths, PatchRecord(wt.Story, attempt))
	}
	message := fmt.Sprintf("%s: attempt %d work of %s kept from cloud box %s before it was destroyed", wt.Story, attempt, wt.Branch, box.Name)
	if _, err := commitWritten(ctx, p.check.Files, message, paths); err != nil {
		return fmt.Errorf("committing %s in the vault: %w", strings.Join(paths, ", "), err)
	}
	if _, err := retryPastASiblingsWrite(ctx, sleepFor, p.check.Files.Push); err != nil {
		return fmt.Errorf("%s is committed in the vault but could not be pushed: %w", strings.Join(paths, ", "), err)
	}
	p.setBox(box.Name, func(b *CloudBox) { b.Saved = append(b.Saved, key) })
	p.dirty = "saved " + box.Name
	if p.saved == nil {
		p.saved = map[string][]string{}
	}
	p.saved[wt.Story] = append(p.saved[wt.Story], paths...)
	p.move(ctx, fmt.Sprintf("saved %s from %s before it is destroyed: %s", wt.Story, box.Name, strings.Join(paths, ", ")))
	return nil
}

// freeAttempt is the first attempt number, from the story's own attempts on,
// that has no bundle (when one is wanted) and no patch (likewise) in the
// story's run directory: a story saved before keeps every earlier file.
func (p *cloudPass) freeAttempt(ctx context.Context, story string, bundle, patch bool) (int, error) {
	attempt := 1
	if p.check.Stories != nil {
		if stories, err := p.check.Stories.StoriesSince(ctx, time.Time{}); err == nil {
			for _, s := range stories {
				if s.Story.ID == story && s.Attempts > attempt {
					attempt = s.Attempts
				}
			}
		}
	}
	for ; attempt < 1000; attempt++ {
		taken := false
		for name, wanted := range map[string]bool{BundleFileName(attempt): bundle, PatchFileName(attempt): patch} {
			if !wanted {
				continue
			}
			_, err := p.check.Vault.StatRunFile(ctx, story, name)
			switch {
			case err == nil:
				taken = true
			case !errors.Is(err, fs.ErrNotExist):
				return 0, fmt.Errorf("looking for %s in the vault: %w", name, err)
			}
		}
		if !taken {
			return attempt, nil
		}
	}
	return 0, fmt.Errorf("every attempt number of %s up to 999 has a file in the vault already", story)
}

// refuse leaves the box up because its work could not be saved, says so (a
// move the first time, a quiet line at the checks after), and fails the check.
func (p *cloudPass) refuse(ctx context.Context, box CloudBox, why, story string, err error) bool {
	of := ""
	if story != "" {
		of = " (story " + story + ")"
	}
	text := fmt.Sprintf("not destroying %s (%s)%s: its work could not be saved into the vault, so it stays up and billed: %v", box.Name, why, of, oneLine(err.Error()))
	// The reason is what is compared, not the text: an idle box's why grows by
	// a minute at each check.
	said := of + ": " + oneLine(err.Error())
	if box.Refused == said && why != "mw cloud down" {
		p.check.say("%s", text)
	} else {
		p.setBox(box.Name, func(b *CloudBox) { b.Refused = said })
		p.dirty = "refused " + box.Name
		p.move(ctx, text)
	}
	// The Governor is told once for a spell of refusals, not at each check; a
	// refusal of mw cloud down is already in front of the one who ran it.
	if !box.Alerted && why != "mw cloud down" {
		alert := fmt.Sprintf("%s stays up and billed at $%.2f an hour%s: its work could not be saved into the vault: %s. To destroy it without saving the work: mw cloud down --force %s",
			box.Name, p.check.Plan.HourlyUSD, of, oneLine(err.Error()), box.Name)
		if p.alert(ctx, box, "Cloud box "+box.Name+" cannot be destroyed: its work cannot be saved", alert) {
			p.setBox(box.Name, func(b *CloudBox) { b.Alerted = true })
			p.dirty = "refused " + box.Name
		}
	}
	p.fail(fmt.Errorf("not destroying %s%s: %w", box.Name, of, err))
	return false
}

// repath sets host=auto on every open and deferred story whose path names the
// box, a box that is gone: a story pathed to a host that is not there is never
// dispatched, so it would wait for ever. A claimed story is left to its
// claim. It says each story moved, as a cloud event; a story that cannot be
// moved fails the check with its id, for the Mayor to re-path by hand.
func (p *cloudPass) repath(ctx context.Context, name, why string) {
	if p.repathed == nil {
		p.repathed = map[string]bool{}
	}
	p.repathed[name] = true
	stories, ok := p.pathedStories(ctx, name)
	if !ok {
		return
	}
	p.handBack(ctx, stories, name, why)
	p.repathFrom(ctx, stories, name, false)
}

// handBack gives back every claim on a box that is gone, however its session
// ended (mw-gq6.1127): the box and its sessions are destroyed, and a claim left
// on it holds the story from every dispatch until the Mayor frees it by hand.
// Each is released and re-pathed to host=auto, after the box's work was saved
// into the vault. A story queued for its rig's batch, whose branch is pushed
// and whose batch lands it, keeps its queued run state, so that mw land takes it
// and no dispatch works it again; any other loses its run state with the claim,
// so that the next dispatch reads it as a fresh story. It says each story
// handed back, as a cloud event; one that cannot be handed back fails the check
// with its id.
func (p *cloudPass) handBack(ctx context.Context, stories []StoryDetail, name, why string) {
	var queued, others, stuck []string
	for _, story := range stories {
		status := strings.ToLower(strings.TrimSpace(story.Status))
		// A claim is the dispatch's, mw@<box>, or a Builder session's,
		// builder@<box>: both end with the box (mw-gq6.1144).
		if story.IsEpic || !slices.Contains(boxHolders(name), story.Assignee) || status != StatusInProgress && status != StatusOpen {
			continue
		}
		id := story.Story.ID
		isQueued := QueuedForBatch(story)
		if !isQueued {
			if err := p.check.Stories.ClearRunState(ctx, id); err != nil {
				stuck = append(stuck, id)
				continue
			}
		}
		if err := p.check.Stories.ReleaseClaimHeldBy(ctx, id, story.Assignee); err != nil {
			stuck = append(stuck, id)
			continue
		}
		if story.Merged().Host != domain.HostAuto {
			if err := p.check.Stories.SetStoryMetadata(ctx, id, map[string]string{"host": domain.HostAuto}); err != nil {
				stuck = append(stuck, id)
				continue
			}
		}
		if isQueued {
			queued = append(queued, id)
		} else {
			others = append(others, id)
		}
		p.commentHandedBack(ctx, story, name, why, isQueued)
	}
	if len(queued) > 0 {
		p.move(ctx, fmt.Sprintf("handed back %s from %s, queued for its batch with no session on the box, and re-pathed it to host=auto", strings.Join(queued, ", "), name))
	}
	if len(others) > 0 {
		p.move(ctx, fmt.Sprintf("handed back %s from %s: the box is gone, so the claim is released, its run state cleared, and the story re-pathed to host=auto", strings.Join(others, ", "), name))
	}
	if len(stuck) > 0 {
		p.fail(fmt.Errorf("could not hand back %s from %s: release the claim by hand and set host=auto with %sauto", strings.Join(stuck, ", "), name, RepathHint))
	}
}

// commentHandedBack says on a story handed back that its box was destroyed, by
// what, and what was saved of its work, as mw retry's comment does for a claim
// it gives back; a story that was not queued for its batch also has its
// attempt named tooling (mw cause), the box's loss being no fault of its own,
// so that it is not counted against its attempts. A part that cannot be
// written is a line in the log, not a failure: the claim is already given back.
func (p *cloudPass) commentHandedBack(ctx context.Context, story StoryDetail, box, why string, queued bool) {
	id := story.Story.ID
	attempt := max(1, story.Attempts)
	named := ""
	if !queued {
		causes := map[int]string{}
		for at, class := range story.AttemptCauses {
			causes[at] = class
		}
		causes[attempt] = CauseTooling
		fields := map[string]string{
			CauseField:         CauseTooling,
			CauseWhyField:      fmt.Sprintf("cloud box %s was destroyed (%s) while its session worked the story", box, why),
			AttemptCausesField: attemptCausesText(causes),
		}
		if err := p.check.Stories.SetStoryMetadata(ctx, id, fields); err != nil {
			p.check.say("%s could not be named %s (mw cause): %v", id, CauseTooling, oneLine(err.Error()))
			named = fmt.Sprintf(" Attempt %d could not be named %s (mw cause): %v.", attempt, CauseTooling, oneLine(err.Error()))
		} else {
			named = fmt.Sprintf(" Attempt %d is named %s (mw cause), not the story's own: it is not counted against its attempts.", attempt, CauseTooling)
		}
	}
	saved := " Nothing further on the box needed saving: what the vault already holds of its work is all there is."
	switch {
	case p.force:
		saved = " The box's work was not saved (mw cloud down --force looks at nothing on it)."
	case p.unsaved[id] != "":
		saved = " Its work was not saved: " + p.unsaved[id] + "."
	case len(p.saved[id]) > 0:
		saved = " Its work was saved into the vault before the box was destroyed: " + strings.Join(p.saved[id], ", ") + "."
	}
	kept := "its run state cleared, and the story re-pathed to host=auto"
	if queued {
		kept = "its queued run state kept for the batch to land, and the story re-pathed to host=auto"
	}
	text := fmt.Sprintf("mw cloud down: cloud box %s was destroyed (%s), and the session working this story with it. The claim held by %s is released, %s.%s%s",
		box, why, story.Assignee, kept, named, saved)
	if err := p.check.Stories.CommentOnStory(ctx, id, text); err != nil {
		p.check.say("the hand-back of %s could not be commented on: %v", id, oneLine(err.Error()))
	}
}

// pathedStories is every story the tracker holds, to find those pathed to a
// box; ok is false (and the check failed) when it cannot be read, and when the
// check has no tracker to re-path with.
func (p *cloudPass) pathedStories(ctx context.Context, name string) (stories []StoryDetail, ok bool) {
	if p.check.Stories == nil {
		return nil, false
	}
	stories, err := p.check.Stories.StoriesSince(ctx, time.Time{})
	if err != nil {
		p.fail(fmt.Errorf("reading the stories pathed to %s to re-path them: %w", name, err))
		return nil, false
	}
	return stories, true
}

// repathFrom is repath over stories already read. A box that was destroyed now
// re-paths the Mayor's held stories with the rest; keepHeld leaves them as
// they are, for a box that was gone long ago: a dispatch tick re-paths such a
// story once it is released (mw-gq6.1205).
func (p *cloudPass) repathFrom(ctx context.Context, stories []StoryDetail, name string, keepHeld bool) {
	var moved, stuck []string
	for _, story := range stories {
		status := strings.ToLower(strings.TrimSpace(story.Status))
		if story.IsEpic || story.Merged().Host != name || (status != StatusOpen && (status != StatusHeld || keepHeld)) {
			continue
		}
		if err := p.check.Stories.SetStoryMetadata(ctx, story.Story.ID, map[string]string{"host": domain.HostAuto}); err != nil {
			stuck = append(stuck, story.Story.ID)
			continue
		}
		moved = append(moved, story.Story.ID)
	}
	if len(moved) > 0 {
		p.move(ctx, fmt.Sprintf("re-pathed %s from %s to host=auto", strings.Join(moved, ", "), name))
	}
	if len(stuck) > 0 {
		p.fail(fmt.Errorf("could not re-path %s from %s to host=auto: set it with %sauto", strings.Join(stuck, ", "), name, RepathHint))
	}
}

// repathGone re-paths every open story whose path names a box the book does
// not hold, however the story came to be pathed there: the down pass re-paths
// once, and a story filed or re-pathed to the box after it was destroyed would
// otherwise wait for ever. A deferred story is the Mayor's and is left alone
// (mw-gq6.1165, AC 4); once he releases it, a dispatch tick re-paths it. It reads the work in hand the check
// already has, so it starts no read of its own. A host that is no box's name is
// never touched, and a claimed story is left to its claim.
func (p *cloudPass) repathGone(ctx context.Context, work WorkInHand) {
	if p.check.Stories == nil {
		return
	}
	stories := slices.Concat(work.Ready, work.Held)
	held := map[string]bool{}
	for _, box := range p.state.Boxes {
		held[box.Name] = true
	}
	var gone []string
	for _, story := range stories {
		if host := story.Merged().Host; !story.IsEpic && p.check.Plan.isBox(host) && !held[host] && !p.repathed[host] && !slices.Contains(gone, host) {
			gone = append(gone, host)
		}
	}
	slices.Sort(gone)
	for _, name := range gone {
		p.repathFrom(ctx, stories, name, true)
	}
}

// capReached says the month's cap is reached, once a month.
func (p *cloudPass) capReached(ctx context.Context, what string) {
	line := fmt.Sprintf("cap reached: $%.2f spent of $%.2f this month; %s", p.spent(), p.check.Plan.MonthlyCapUSD, what)
	month := cloudMonth(p.now)
	if p.state.CapSaid == month {
		p.check.say("%s", line)
		return
	}
	p.state.CapSaid = month
	p.move(ctx, line)
	p.dirty = "cap reached"
}

// move is one cloud event, and a line the book keeps for mw status.
func (p *cloudPass) move(ctx context.Context, what string) {
	p.check.say("%s", what)
	p.report.Moves = append(p.report.Moves, what)
	p.state.Moves = p.addMove(CloudMove{At: p.now, What: what})
	p.emit(ctx, what)
}

// addMove is the book's moves with m after them, the latest cloudMovesKept.
func (p *cloudPass) addMove(m CloudMove) []CloudMove {
	moves := append(p.state.Moves, m)
	if n := len(moves); n > cloudMovesKept {
		moves = append([]CloudMove(nil), moves[n-cloudMovesKept:]...)
	}
	return moves
}

// emit writes one cloud event, and no move of the book's.
func (p *cloudPass) emit(ctx context.Context, what string) {
	if p.check.Log == nil {
		return
	}
	_, err := EventEmit{
		Log:   p.check.Log,
		Now:   func() time.Time { return p.now },
		Event: events.Event{Kind: events.KindCloud, Actor: "cloud@" + p.check.Host, Detail: what},
	}.Run(ctx)
	if err != nil {
		p.check.say("the cloud event %q could not be written: %v", what, err)
	}
}

// keep writes the book, and reports whether it was written.
func (p *cloudPass) keep(ctx context.Context, why string) bool {
	if err := p.check.Book.Write(ctx, p.state, why); err != nil {
		p.fail(fmt.Errorf("writing the cloud's book (%s): %w", why, err))
		return false
	}
	p.dirty = ""
	return true
}

func (p *cloudPass) nextName() string {
	taken := map[string]bool{}
	for _, box := range p.state.Boxes {
		taken[box.Name] = true
	}
	// A box destroyed lately leaves its name unused for CloudNameBar: the notes
	// it wrote under the name (last sync, ticks, mw commit) are not the new box's.
	for name, at := range p.state.Downed {
		if p.now.Sub(at) < CloudNameBar {
			taken[name] = true
		}
	}
	for i := 1; ; i++ {
		if name := fmt.Sprintf("%s%d", p.check.Plan.prefix(), i); !taken[name] {
			return name
		}
	}
}

// hours are the box hours billed this month: the book's count of the boxes
// destroyed, and the hours begun of each box up.
func (s CloudState) hours(now time.Time) int {
	h := 0
	if s.Month == cloudMonth(now) {
		h = s.Hours
	}
	for _, box := range s.Boxes {
		h += billedHours(maxTime(box.Up, cloudMonthStart(now)), now)
	}
	return h
}

// billedHours are the hours begun from from to to, each a whole one, and at
// least one: a box is billed its first hour as soon as it is made.
func billedHours(from, to time.Time) int {
	return max(1, int(math.Ceil(to.Sub(from).Hours())))
}

func cloudMonth(t time.Time) string { return t.UTC().Format("2006-01") }

func cloudMonthStart(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
}

func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

// CloudReading is the cloud as mw status shows it.
type CloudReading struct {
	Boxes    []CloudBox
	Moves    []CloudMove
	SpentUSD float64
	CapUSD   float64
	MaxBoxes int
	// BoxCap is the box_cap the plan wants, which a box with a CapFault has
	// not got.
	BoxCap int
	Now    time.Time
	// FeeLimitAt is when Vultr last refused a box for its fee limit, zero when
	// it is not holding one back.
	FeeLimitAt time.Time
	// FeeLimitUntil is when a box is tried again after it, zero when a box
	// destroyed since has freed room.
	FeeLimitUntil time.Time
}

// ReadCloud reads the book for mw status: the boxes, the latest moves and the
// month's spend counted from the box hours. It asks the provider nothing.
func ReadCloud(ctx context.Context, book CloudBook, plan CloudPlan, now time.Time) (*CloudReading, error) {
	state, err := book.Read(ctx)
	if err != nil {
		return nil, err
	}
	return &CloudReading{
		Boxes:    state.Boxes,
		Moves:    state.Moves,
		SpentUSD: float64(state.hours(now)) * plan.HourlyUSD,
		CapUSD:   plan.MonthlyCapUSD,
		MaxBoxes: plan.MaxBoxes,
		BoxCap:   plan.boxCap(),
		Now:      now,

		FeeLimitAt:    state.FeeLimitAt,
		FeeLimitUntil: state.feeLimitUntil(),
	}, nil
}

// write is the CLOUD section: the month's spend against the cap, each box up
// with its age, and the latest moves.
func (r CloudReading) write(b *strings.Builder) {
	clip(b, fmt.Sprintf("CLOUD: $%.2f spent of $%.2f this month, %d of %d boxes", r.SpentUSD, r.CapUSD, len(r.Boxes), r.MaxBoxes))
	for _, box := range r.Boxes {
		if box.Restoring {
			clip(b, fmt.Sprintf("  %s  restoring %s (Vultr instance %s), up %s", box.Name, Clock(r.Now.Sub(box.RestoringSince)), box.InstanceID, Clock(r.Now.Sub(box.Up))))
			continue
		}
		if box.Making {
			// Its make has not finished: it has taken no story, and Vultr's word
			// is shown once a check has read it (mw-gq6.1113).
			line := fmt.Sprintf("  %s  making %s", box.Name, Clock(r.Now.Sub(box.Up)))
			if box.Vultr != "" {
				line += ", Vultr " + box.Vultr
			}
			clip(b, line)
			continue
		}
		line := fmt.Sprintf("  %s  up %s", box.Name, Clock(r.Now.Sub(box.Up)))
		// What the check could not set on the box is shown under whichever line
		// the box gets (mw-gq6.1174).
		faults := func() {
			for _, fault := range []string{box.HarnessFault, box.SessionDenyFault} {
				if fault != "" {
					clip(b, "    "+fault)
				}
			}
		}
		// A box the home has not cleared takes no story, whatever it has dispatched.
		if why := box.heldWhy(); why != "" && (box.CheckFault != "" || !box.LockedSince.IsZero() || !box.Dispatched.IsZero()) {
			clip(b, line+", takes no story: "+why)
			faults()
			continue
		}
		// A box a book kept no Made of was said up by its make, as before.
		switch {
		case !box.Dispatched.IsZero():
		case box.NotDispatching != "":
			clip(b, line+", not dispatching: "+box.NotDispatching)
			faults()
			continue
		case !box.Made.IsZero():
			clip(b, line+", waiting for its first dispatch")
			faults()
			continue
		}
		if !box.IdleSince.IsZero() {
			line += ", idle " + Clock(r.Now.Sub(box.IdleSince))
		} else {
			line += ", working"
		}
		clip(b, line)
		if box.CapFault != "" {
			clip(b, fmt.Sprintf("    cap %d, not the %d box_cap says: %s", box.boxCap(CloudPlan{BoxCap: r.BoxCap}), r.BoxCap, box.CapFault))
		}
		faults()
	}
	if !r.FeeLimitAt.IsZero() {
		clip(b, "  Vultr fee limit reached since "+r.FeeLimitAt.UTC().Format("01-02 15:04Z"))
		clip(b, "    raise it at my.vultr.com, or wait for the month")
		if r.Now.Before(r.FeeLimitUntil) {
			clip(b, "    no box is tried before "+r.FeeLimitUntil.UTC().Format("15:04Z"))
		}
	}
	for _, m := range r.Moves {
		clip(b, "  "+m.At.UTC().Format("01-02 15:04Z")+" "+m.What)
	}
}

// CloudBoxCapMax is the most sessions a box may be told to run at once.
const CloudBoxCapMax = 8

// CloudCapFiles is where box_cap is kept: the [cloud] table of the home's
// ~/.config/mw/config.toml, its copy in the vault (hosts/vultr/cloud-table.toml)
// and the cap the next box is made with (hosts/vultr/vultr-boost.tfvars).
type CloudCapFiles interface {
	// BoxCap is the box_cap the home's [cloud] table says, 0 when it says none.
	BoxCap(ctx context.Context) (int, error)
	// SetBoxCap writes n in all three places, the home's config last, and
	// commits the vault's two; an error names what was written before it.
	SetBoxCap(ctx context.Context, n int) error
}

// CloudCap is mw cloud cap: it changes box_cap in the places the cloud keeps
// it. It starts no box and reaches no box: the next cloud check sets the cap
// on each box up. It is for the home only, the one host that runs the check.
type CloudCap struct {
	Files CloudCapFiles
	Home  HomeFile
	Host  string
}

// CloudCapReport is what mw cloud cap changed.
type CloudCapReport struct{ Was, Now int }

// String is the report, with the way back.
func (r CloudCapReport) String() string {
	was := fmt.Sprintf("was %d", r.Was)
	if r.Was == 0 {
		was = "it said none, so the default 2 was in force"
	}
	return fmt.Sprintf("box_cap is %d (%s): written in the home's [cloud] table, the vault's hosts/vultr/cloud-table.toml and cap in hosts/vultr/vultr-boost.tfvars.\n"+
		"The next cloud check, within a minute, sets it on each box up; running sessions finish, and a box over the cap starts no new one.\n"+
		"Way back: mw cloud cap %d", r.Now, was, max(r.Was, 1))
}

// Run sets box_cap to n, 1 to CloudBoxCapMax; on a host that is not home it
// changes nothing.
func (c CloudCap) Run(ctx context.Context, n int) (CloudCapReport, error) {
	if n < 1 || n > CloudBoxCapMax {
		return CloudCapReport{}, fmt.Errorf("a box runs 1 to %d sessions at once, not %d", CloudBoxCapMax, n)
	}
	home, err := IsHome(ctx, c.Home, c.Host)
	if err != nil {
		return CloudCapReport{}, err
	}
	if !home {
		record, _ := WhereIsHome(ctx, c.Home)
		return CloudCapReport{}, &HomeElsewhere{Home: record.Host, Host: c.Host}
	}
	was, err := c.Files.BoxCap(ctx)
	if err != nil {
		return CloudCapReport{}, err
	}
	if err := c.Files.SetBoxCap(ctx, n); err != nil {
		return CloudCapReport{}, err
	}
	return CloudCapReport{Was: was, Now: n}, nil
}
