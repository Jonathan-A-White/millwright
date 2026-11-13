package application

import (
	"context"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/domain"
)

// StarvedAfter is how long a rig may have ready stories and none running, held
// by something, before mw doctor and mw status call it starved (mw-gq6.580):
// lampas sat with 0 running from 18:38Z to 22:41Z under a failing grist smoke
// that also held its own fix, and nothing said so but the Governor's eyes.
const StarvedAfter = time.Hour

// DispatchTimerPeriod is how often mw-dispatch.timer runs a host's dispatch
// (contrib/systemd/mw-dispatch.timer, OnCalendar=hourly); a host with an event
// follower or a faster drop-in runs it sooner, never later.
const DispatchTimerPeriod = time.Hour

// StarvedPassStale is how long a host's last good dispatch run may be behind
// before its note of the rigs it holds back is no longer a current hold
// (mw-gq6.1049): the timer's period, the TicksNoteSlack its ticks note may lag,
// and five minutes for the run itself. The desktop at cap 0 stops before its pass
// and left a note of load 28.2 for 2h21m while it had a load of 0.1.
const StarvedPassStale = DispatchTimerPeriod + TicksNoteSlack + 5*time.Minute

// StarvedRanWindow is how far back the closed stories are read for the hosts
// that ran a rig (mw-gq6.786); the alarm states it in days when no host did.
const StarvedRanWindow = 14 * 24 * time.Hour

// StarvedPrinciple is the word a starved rig's DAMPERS line begins with.
const StarvedPrinciple = "starved"

// MailStarved starts the subject of the mail that tells the Mayor a rig is
// starved.
const MailStarved = "Starved"

// PourRefusedGate stands for the reason dispatch passes a story over that no
// gate of DispatchGates names: its formula was refused when poured.
const PourRefusedGate = "pour-refused"

// ClaimLostGate stands for the other reason no gate names: another host
// claimed the story first, between the ready list and the claim (mw-gq6.569).
const ClaimLostGate = "claim-lost"

// LeftoverRefusedGate stands for a third: an earlier attempt's worktree could
// not be cleared out of the way of the fresh cut, because a live session of the
// story is in it or its uncommitted work could not be committed (mw-gq6.669).
const LeftoverRefusedGate = "leftover-refused"

// HeldAfterClaimGate stands for a fourth: the Mayor held the story after this
// dispatch claimed it, so the claim was given back and no session was started
// (mw-gq6.878).
const HeldAfterClaimGate = "held-after-claim"

// StarvedHolds maps every reason mw dispatch passes a ready story over for, by
// the gate's name in DispatchGates (and PourRefusedGate), to the hold name the
// starved-rig alarm gives it. A gate added to Dispatch.run is mapped here too:
// a test fails until it is. "paused" has none: a paused host passes no story,
// it does nothing at all.
var StarvedHolds = map[string]string{
	"hitl":                   "Governor's own story",
	"held-for":               "held for an event",
	"parent":                 "parent with open children",
	"no-path":                "unreadable path",
	"no-host":                "no host",
	"other-host":             "another host (asleep?)",
	"harness-hold":           "harness stop",
	"no-room":                "no room",
	"queued":                 "waits for its batch",
	"landed":                 "lost close",
	"blocked":                "blocked",
	"rig-not-checked-out":    "rig not checked out",
	"smoke-hold":             "smoke hold",
	"metered-network":        "metered network",
	"no-grist-key":           "no grist key",
	"formula-not-installed":  "formula not installed",
	"no-sweep":               "no sweep",
	"tester-no-landed-story": "tester names no landed story",
	"attempts-exhausted":     "attempts exhausted",
	"handed-back":            "handed back",
	"needs-files-key":        "needs files key",
	"shared-files":           "files overlap",
	"testers-per-rig":        "damper",
	"drained":                "drained",
	"cap":                    "cap",
	PourRefusedGate:          "pour refused",
	ClaimLostGate:            "claim lost",
	LeftoverRefusedGate:      "earlier attempt in the way",
	HeldAfterClaimGate:       "held after the claim",
}

// StarvedKey is the note a host's dispatch keeps of the rigs it holds back
// every ready story of while none of the rig's runs: one line each, tab
// separated: the rig, when it was first so held (RFC 3339), the hold's name,
// and why. mw doctor and mw status read it with the work in hand.
func StarvedKey(host string) string { return "host." + host + ".starved" }

// StarvedRig is one rig a dispatch found ready stories held back, none running.
type StarvedRig struct {
	Rig   string
	Since time.Time
	// Hold is the name StarvedHolds gives the reason most of the rig's ready
	// stories were passed over for, and Reason the words dispatch printed.
	Hold   string
	Reason string
	// Host is the host whose note held the rig, set when the notes are read; the
	// note's lines do not carry it, its key does.
	Host string
	// Ran are the hosts, other than Host, a story of the rig closed within
	// StarvedRanWindow was routed to, and RanRead whether that was read at all:
	// without it "no other host" is a guess (mw-gq6.786). Set when the notes are
	// read, for a rig held by a harness stop; not kept in the note.
	Ran     []string
	RanRead bool
	// Landed is when the newest story of the rig, closed on any host within
	// StarvedRanWindow, was closed; zero when none was or the closed stories were
	// not read. A rig that landed a story since Since is not starved (mw-gq6.1049).
	// Set when the notes are read, for a note held over StarvedAfter; not kept in
	// the note.
	Landed time.Time
	// PassAt is the time of Host's last good dispatch run as its ticks note has it,
	// zero for none, and PassKnown whether the host's ticks note said anything of
	// dispatch at all. Set when the notes are read; not kept in the note.
	PassAt    time.Time
	PassKnown bool
	// GivesWay says Host gives way to the person at its screen: its sessions
	// are frozen, so a rig it holds back is not starved, and its note of it was
	// written before the freeze. Set when the notes are read; not kept in the note.
	GivesWay bool
}

// current says whether the note still holds at now: no story of the rig closed
// on any host since it was first held, and the host that wrote it is making
// passes. A host that stops before its pass (the cap at 0, a host down or
// paused, a crashed dispatch) leaves its note as it was, and a note nobody has
// rewritten is not a hold now (mw-gq6.1049). A host whose ticks note says nothing
// of dispatch is given the benefit of the doubt: it may be a host that keeps no
// log.
func (r StarvedRig) current(now time.Time) bool {
	if !r.Landed.IsZero() && !r.Landed.Before(r.Since) {
		return false
	}
	if r.PassKnown && (r.PassAt.IsZero() || now.Sub(r.PassAt) > StarvedPassStale) {
		return false
	}
	return true
}

// FormatStarved is the note's value, its lines in rig order.
func FormatStarved(rigs []StarvedRig) string {
	sorted := slices.Clone(rigs)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Rig < sorted[j].Rig })
	lines := make([]string, 0, len(sorted))
	for _, r := range sorted {
		field := func(s string) string { return strings.Join(strings.Fields(s), " ") }
		lines = append(lines, strings.Join([]string{field(r.Rig), r.Since.UTC().Format(time.RFC3339), field(r.Hold), field(r.Reason)}, "\t"))
	}
	return strings.Join(lines, "\n")
}

// ParseStarved reads the note's value. A line that does not parse is passed over.
func ParseStarved(value string) []StarvedRig {
	var rigs []StarvedRig
	for _, line := range strings.Split(value, "\n") {
		fields := strings.SplitN(strings.TrimSpace(line), "\t", 4)
		if len(fields) < 3 {
			continue
		}
		since, err := time.Parse(time.RFC3339, fields[1])
		if err != nil {
			continue
		}
		rig := StarvedRig{Rig: fields[0], Since: since, Hold: fields[2]}
		if len(fields) == 4 {
			rig.Reason = fields[3]
		}
		rigs = append(rigs, rig)
	}
	return rigs
}

// starvedPass reads the rigs a dispatch pass held back whole: each rig with a
// ready story passed over, other than for the Governor's own or a hold the Mayor
// put on it (held-for), and no story
// running or started in it. The hold named is the one most of its stories were
// passed over for, the first of them winning a tie; the reason is the first
// such story's own.
func starvedPass(ready []StoryDetail, passed []Passed, running []StoryDetail, started []Started) map[string]StarvedRig {
	rigOf := map[string]string{}
	for _, detail := range ready {
		rigOf[detail.Story.ID] = detail.Merged().Rig
	}
	busy := map[string]bool{}
	for _, detail := range running {
		if !detail.Hitl() {
			busy[detail.Merged().Rig] = true
		}
	}
	for _, s := range started {
		busy[s.Path.Rig] = true
	}
	type tally struct {
		count  int
		reason string
	}
	holds := map[string]map[string]*tally{}
	var order = map[string][]string{}
	for _, p := range passed {
		rig, hold := rigOf[p.StoryID], StarvedHolds[p.Gate]
		if rig == "" || hold == "" || p.Gate == "hitl" || p.Gate == "held-for" || p.Gate == "parent" || p.Waiting || busy[rig] {
			continue
		}
		if holds[rig] == nil {
			holds[rig] = map[string]*tally{}
		}
		if holds[rig][hold] == nil {
			holds[rig][hold] = &tally{reason: p.Why}
			order[rig] = append(order[rig], hold)
		}
		holds[rig][hold].count++
	}
	found := map[string]StarvedRig{}
	for rig, byHold := range holds {
		best := order[rig][0]
		for _, hold := range order[rig][1:] {
			if byHold[hold].count > byHold[best].count {
				best = hold
			}
		}
		found[rig] = StarvedRig{Rig: rig, Hold: best, Reason: byHold[best].reason}
	}
	return found
}

// keepStarved brings the host's note level with the pass: a rig found held back
// whole for the first time is held from now, one held before keeps the time it
// was first held (with the hold of this pass), and one with a story running or
// started, or nothing left ready, is taken off. The note is written only when it
// changes. A dry run writes nothing.
func (d Dispatch) keepStarved(ctx context.Context, ready []StoryDetail, running []StoryDetail, report *DispatchReport) {
	if d.DryRun || d.Memory == nil {
		return
	}
	was, _ := d.Memory.Note(ctx, StarvedKey(d.Host))
	before := map[string]StarvedRig{}
	for _, r := range ParseStarved(was) {
		before[r.Rig] = r
	}
	now := starvedPass(ready, report.Passed, running, report.Started)
	var kept []StarvedRig
	for _, rig := range slices.Sorted(maps.Keys(now)) {
		found := now[rig]
		found.Since = d.now()
		if earlier, held := before[rig]; held {
			found.Since = earlier.Since
		}
		kept = append(kept, found)
	}
	value := FormatStarved(kept)
	if value == FormatStarved(ParseStarved(was)) {
		return
	}
	var err error
	if len(kept) == 0 {
		err = d.Memory.ClearNote(ctx, StarvedKey(d.Host))
	} else {
		err = d.Memory.SetNote(ctx, StarvedKey(d.Host), value)
	}
	if err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("which rigs are held back whole could not be kept in the host's note: %v", err))
	}
}

// StarvedAlarms judges the rigs the hosts' notes say are held back whole
// against the work in hand. A note that is not current (a story of the rig
// closed on any host since it was first held, or its host has made no pass for
// StarvedPassStale) is passed over before the rest: a rig is starved when it was first so held more than
// StarvedAfter ago and it has a ready story and none running now (a story
// queued for its batch has no session, so it is not running). A story of it that
// starts clears the alarm at once. Two hosts' notes of one rig count the
// earlier. One alarm a rig, its line naming the rig, how many are ready, for how
// long none ran, and the hold with its reason.
func StarvedAlarms(rigs []StarvedRig, work WorkInHand, now time.Time) []DamperAlarm {
	oldest := map[string]StarvedRig{}
	for _, r := range rigs {
		// A host that gives way holds nothing back: it takes nothing new while the
		// person is at the screen, and what it noted before the freeze is not a
		// rig left to starve (mw-2891blf.3).
		if r.GivesWay || !r.current(now) {
			continue
		}
		if held, ok := oldest[r.Rig]; !ok || r.Since.Before(held.Since) {
			oldest[r.Rig] = r
		}
	}
	var alarms []DamperAlarm
	for _, rig := range slices.Sorted(maps.Keys(oldest)) {
		r := oldest[rig]
		if now.Sub(r.Since) <= StarvedAfter {
			continue
		}
		running := false
		for _, list := range [][]StoryDetail{work.Running, work.OpenClaimed} {
			for _, d := range list {
				running = running || !d.IsParent() && !d.Hitl() && !QueuedForBatch(d) && d.Merged().Rig == rig
			}
		}
		var ready []string
		for _, d := range work.Ready {
			if !d.IsParent() && !d.Hitl() && d.Merged().Rig == rig {
				ready = append(ready, d.Story.ID)
			}
		}
		if running || len(ready) == 0 {
			continue
		}
		text := fmt.Sprintf("%s: %d ready, none running for %s, held by %s", rig, len(ready), elapsed(now.Sub(r.Since)), r.Hold)
		if r.Reason != "" {
			text += " — " + r.Reason
		}
		if way := harnessWayOut(r, work); way != "" {
			text += ". " + way
		}
		// The rig and the stories ready are the alarm; how long it has lasted, the
		// hold and the reason's words differ from one host's reading to the next. A
		// starved rig is one fact whatever it is held by, so its identity leaves the
		// hold out: the hold flipping from no room to no grist key and back is not
		// news (mw-gq6.984, mw-gq6.1194). Whether it is a wait or a fault is Wait.
		wait := holdIsAWait(r)
		identity := strings.Join(append([]string{rig}, slices.Sorted(slices.Values(ready))...), "\t")
		alarms = append(alarms, DamperAlarm{Principle: StarvedPrinciple, Text: text, Beads: ready, Identity: identity, Wait: wait})
	}
	return alarms
}

// GristFirstWords end the reason dispatch gives for the cap while a tutor is in
// use: the host's sessions are cut to leave room for it (grist first).
const GristFirstWords = " while a tutor is in use (grist first)"

// holdIsAWait says a rig's hold is the host's room and nothing else: no room (the
// load is over the cores) or the cap on its sessions, which eases by itself when a
// running session ends. A cap is a wait while the host may run some sessions at
// all (a limit over 0, read from the reason's "of the N sessions") or a tutor in
// use cut them; a drained host (cap 0) is held by "drained", not the cap, and stays
// a fault as every other hold does (mw-gq6.984, mw-gq6.1050).
func holdIsAWait(r StarvedRig) bool {
	if r.Hold == StarvedHolds["no-room"] {
		return true
	}
	if r.Hold != StarvedHolds["cap"] {
		return false
	}
	reason := strings.TrimSpace(r.Reason)
	if strings.HasSuffix(reason, strings.TrimSpace(GristFirstWords)) {
		return true
	}
	m := capLimitWords.FindStringSubmatch(reason)
	return m != nil && strings.Trim(m[1], "0") != ""
}

// capLimitWords picks the limit out of the reason dispatch gives for the cap:
// "<host> has taken N of the L sessions it may run at once".
var capLimitWords = regexp.MustCompile(`\bof the (\d+) sessions\b`)

// harnessWayOut is the sentence a rig held by a host's harness stop carries
// (mw-gq6.785): each ready story of the rig routed to that host, with the one
// mw path set command that sends it to the first other host that runs the rig,
// or that no other host has run it in the last StarvedRanWindow. The hosts that
// run the rig are those a story of it in hand (ready, claimed or running) is
// routed to, and those a story of it closed within the window was (r.Ran). With
// no host named and the closed stories unread, it makes no claim of "no other
// host" (mw-gq6.786). It re-routes nothing: a story can be on a host for a
// reason, so the alarm only names the command. A rig held for any other reason,
// or whose note names no host, has none.
func harnessWayOut(r StarvedRig, work WorkInHand) string {
	if r.Hold != StarvedHolds["harness-hold"] || r.Host == "" {
		return ""
	}
	others := map[string]bool{}
	for _, host := range r.Ran {
		if host != r.Host {
			others[host] = true
		}
	}
	for _, list := range [][]StoryDetail{work.Ready, work.Running, work.OpenClaimed} {
		for _, d := range list {
			if host := d.Merged().Host; !d.IsParent() && d.Merged().Rig == r.Rig && host != "" && host != domain.HostAuto && host != r.Host {
				others[host] = true
			}
		}
	}
	var routed []string
	for _, d := range work.Ready {
		if !d.IsParent() && !d.Hitl() && d.Merged().Rig == r.Rig && d.Merged().Host == r.Host {
			routed = append(routed, d.Story.ID)
		}
	}
	if len(routed) == 0 {
		return ""
	}
	if len(others) == 0 {
		if !r.RanRead {
			return fmt.Sprintf("Way out: the hosts that ran %s in the last %s could not be read, so none is named", r.Rig, StarvedRanDays())
		}
		return fmt.Sprintf("Way out: no other host has run %s in the last %s", r.Rig, StarvedRanDays())
	}
	hosts := slices.Sorted(maps.Keys(others))
	commands := make([]string, len(routed))
	for i, id := range routed {
		commands[i] = fmt.Sprintf("`mw path set %s host=%s`", id, hosts[0])
	}
	return fmt.Sprintf("Way out: other hosts that run %s: %s; to route a story there: %s", r.Rig, strings.Join(hosts, ", "), strings.Join(commands, " · "))
}

// StarvedRanDays is StarvedRanWindow in days, as the alarm states it.
func StarvedRanDays() string { return fmt.Sprintf("%d days", int(StarvedRanWindow/(24*time.Hour))) }

// StarvedRanLimit is how many of a held rig's newest closed stories are read for
// the hosts that ran it (mw-gq6.790): the read costs one list of that many rows,
// never the 23,000 stories a fortnight closes across every rig.
const StarvedRanLimit = 200

// rigClosedLister is what reads a rig's newest closed stories since a time, each
// with the Path it carries; the beads gateway is one
// (Gateway.ClosedStoriesOfRig), a tracker without it makes the survey claim
// nothing of closed work.
type rigClosedLister interface {
	ClosedStoriesOfRig(ctx context.Context, rig string, since time.Time, limit int) ([]ClosedStory, error)
}

// closedOfRig reads the newest StarvedRanLimit stories of rig closed within
// StarvedRanWindow of now: one list filtered by rig and bounded, whose rows carry
// the host, so no story is shown. ok is false when the read is not to be had.
func (s DamperSurvey) closedOfRig(ctx context.Context, rig string, now time.Time) (closed []ClosedStory, ok bool) {
	lister, can := s.Tracker.(rigClosedLister)
	if !can {
		return nil, false
	}
	closed, err := lister.ClosedStoriesOfRig(ctx, rig, now.Add(-StarvedRanWindow), StarvedRanLimit)
	if err != nil {
		return nil, false
	}
	return closed, true
}

// ranHosts is the hosts the closed stories of rig were routed to, in the order
// they first appear.
func ranHosts(closed []ClosedStory, rig string) (ran []string) {
	seen := map[string]bool{}
	for _, c := range closed {
		if c.Rig != rig || c.Host == "" || c.Host == domain.HostAuto || seen[c.Host] {
			continue
		}
		seen[c.Host] = true
		ran = append(ran, c.Host)
	}
	return ran
}

// landedOfRig is when the newest of the closed stories of rig that landed work
// was closed: a story another one superseded landed nothing.
func landedOfRig(closed []ClosedStory, rig string) time.Time {
	var newest time.Time
	for _, c := range closed {
		if c.Rig == rig && !c.Superseded && c.ClosedAt.After(newest) {
			newest = c.ClosedAt
		}
	}
	return newest
}

// readStarved reads every host's note of the rigs it holds back whole.
func (s DamperSurvey) readStarved(ctx context.Context, now time.Time) ([]StarvedRig, error) {
	if s.Notes == nil {
		return nil, nil
	}
	notes, err := s.Notes.NotesWithPrefix(ctx, "host.")
	if err != nil {
		return nil, fmt.Errorf("reading the hosts' notes of the rigs held back: %w", err)
	}
	var rigs []StarvedRig
	book := readBoxBook(ctx, s.Cloud, s.CloudPlan)
	type closedRead struct {
		closed []ClosedStory
		ok     bool
	}
	readClosed := map[string]closedRead{}
	for _, key := range slices.Sorted(maps.Keys(notes)) {
		if strings.HasPrefix(key, "host.") && strings.HasSuffix(key, ".starved") {
			host := strings.TrimSuffix(strings.TrimPrefix(key, "host."), ".starved")
			// A box the cloud destroyed, or is making again, holds back no rig:
			// its note is the old box's (mw-gq6.1127).
			if down, err := (CloudBoxesDown{Book: s.Cloud, Plan: s.CloudPlan}).Down(ctx, host); err == nil && down {
				continue
			}
			// Its ticks and give-way notes are the old box's when it has not synced
			// since it was made (mw-gq6.1171).
			older := book.older(host, lastSyncOf(notes[LastSyncKey(host)]))
			var dispatch TickCount
			if !older {
				dispatch = ParseHostTicks(notes[TicksKey(host)]).Dispatch
			}
			for _, r := range ParseStarved(notes[key]) {
				r.Host = host
				r.PassKnown, r.PassAt = dispatch.Known, dispatch.LastGood
				r.GivesWay = !older && readGiveWay(notes[GiveWayKey(host)], now, s.GiveWayStale).Stands
				// The closed stories are read for a note old enough to be an alarm
				// (the landing that clears it) and for a harness stop (the hosts
				// that ran the rig), once a rig.
				young := now.Sub(r.Since) <= StarvedAfter
				if s.Tracker != nil && (!young || r.Hold == StarvedHolds["harness-hold"]) {
					got, tried := readClosed[r.Rig]
					if !tried {
						got.closed, got.ok = s.closedOfRig(ctx, r.Rig, now)
						readClosed[r.Rig] = got
					}
					if got.ok {
						r.Landed = landedOfRig(got.closed, r.Rig)
					}
					if r.Hold == StarvedHolds["harness-hold"] {
						r.Ran, r.RanRead = ranHosts(got.closed, r.Rig), got.ok
					}
				}
				rigs = append(rigs, r)
			}
		}
	}
	return rigs, nil
}

// StarvedMailedKey is the note, shared by every host, that holds what the Mayor
// was last mailed of the starved rigs (mw-gq6.868): the alarms' identities. It
// is not under "host.", which is where each host keeps its own.
const StarvedMailedKey = "starved.mailed"

// StarvedMailNotes is the part of the tracker's notes StarvedRigCheck keeps what
// was mailed in.
type StarvedMailNotes interface {
	Note(ctx context.Context, key string) (string, error)
	SetNote(ctx context.Context, key, value string) error
	ClearNote(ctx context.Context, key string) error
}

// starvedMailed is the value StarvedMailedKey holds for these alarms: their
// identities, in order, one a line.
func starvedMailed(alarms []DamperAlarm) string {
	identities := make([]string, len(alarms))
	for i, a := range alarms {
		identities[i] = a.Identity
	}
	slices.Sort(identities)
	return strings.Join(identities, "\n")
}

// Starved reads the starved-rig alarms alone, at now: what mw doctor judges.
func (s DamperSurvey) Starved(ctx context.Context, now time.Time) ([]DamperAlarm, error) {
	rigs, err := s.readStarved(ctx, now)
	if err != nil || len(rigs) == 0 {
		return nil, err
	}
	work, err := s.Tracker.WorkInHand(ctx)
	if err != nil {
		return nil, fmt.Errorf("reading what is in hand: %w", err)
	}
	return StarvedAlarms(rigs, work, now), nil
}

// StarvedRigName is what the check is called: in the log, and on the command
// line as `mw doctor starved-rig`.
const StarvedRigName = "starved-rig"

// StarvedRigCheck is the mw doctor check that says a rig is starved: it has
// ready stories, none running, for over StarvedAfter, held by something dispatch
// named (mw-gq6.580). Its cure is the mail to the Mayor, once a fault episode: the
// episode ends when the probe next finds nothing starved, as when a story of
// the rig starts, and a later starving is mailed again. It changes nothing else.
// A rig held only by load, or by the cap a tutor in use puts on the host, is a
// wait for the host's room, not a fault: it is mailed once for a starving,
// whichever of the two holds it, and the check stays ok (mw-gq6.870, mw-gq6.984), so it never reaches the
// Governor as a faulty verdict, a note or a failed unit.
type StarvedRigCheck struct {
	// Host is this host, named in the mail.
	Host string
	// Alarms reads the starved rigs, one alarm each (DamperSurvey.Starved).
	Alarms func(ctx context.Context) ([]DamperAlarm, error)
	// Refused, when set, reads the open stories a run state keeps from dispatch
	// (DamperSurvey.Refused); the Mayor is mailed of each once, in this mail
	// (mw-gq6.1166). Nil mails none.
	Refused func(ctx context.Context) ([]DamperAlarm, error)
	// Mail sends the Mayor the message.
	Mail func(ctx context.Context, message NewMessage) error
	// Mailed, shared by every host, keeps what the Mayor was last mailed so that
	// two hosts raising one alarm mail it once (mw-gq6.868); nil mails every time.
	Mailed StarvedMailNotes

	found []DamperAlarm
}

var _ DoctorCheck = (*StarvedRigCheck)(nil)

// Name implements DoctorCheck.
func (c *StarvedRigCheck) Name() string { return StarvedRigName }

// StarvedWaitMailedKey is the note, shared by every host, that holds what the
// Mayor was last mailed of the rigs that only wait for room (mw-gq6.870), as
// StarvedMailedKey does for the faults.
const StarvedWaitMailedKey = "starved.wait-mailed"

// Probe implements DoctorCheck: faulty, one clause for each rig starved by a
// fault, when any is; cannot-tell when the notes or the work cannot be read; ok
// otherwise. A rig held only by load (no room on the host) is a wait, not a
// fault, and is the Mayor's alone: the probe mails him of it once, as a dry run
// does not, and the check stays ok, so no faulty verdict, note or exit status
// carries it further (mw-gq6.870). With no Mailed to keep what was mailed, a
// wait is not mailed, rather than mailed on every tick.
func (c *StarvedRigCheck) Probe(ctx context.Context) (Verdict, string) {
	c.found = nil
	alarms, err := c.Alarms(ctx)
	if err != nil {
		return DoctorCannotTell, err.Error()
	}
	var faults, waits []DamperAlarm
	for _, a := range alarms {
		if a.Wait {
			waits = append(waits, a)
		} else {
			faults = append(faults, a)
		}
	}
	c.pruneMailed(ctx, alarms)
	waitErr := c.tellOfWaits(ctx, waits)
	if refusedErr := c.tellOfRefused(ctx); waitErr == nil {
		waitErr = refusedErr
	}
	if len(faults) == 0 {
		switch {
		case waitErr != nil:
			return DoctorCannotTell, waitErr.Error()
		case len(waits) > 0:
			return DoctorOK, fmt.Sprintf("%s waits for room on the host, not a fault; the Mayor is told", strings.Join(rigNames(waits), ", "))
		}
		return DoctorOK, ""
	}
	c.found = faults
	lines := make([]string, len(faults))
	for i, a := range faults {
		lines[i] = a.Text
	}
	return DoctorFaulty, strings.Join(lines, "; ")
}

// rigNames is the rig each alarm's text begins with.
func rigNames(alarms []DamperAlarm) []string {
	rigs := make([]string, len(alarms))
	for i, a := range alarms {
		rigs[i] = strings.SplitN(a.Text, ":", 2)[0]
	}
	return rigs
}

// mailedLines are the identities a note holds, one a line.
func mailedLines(note string) []string {
	if note == "" {
		return nil
	}
	return strings.Split(note, "\n")
}

// mailedSoFar is every identity either mailed-note holds: a starved rig is
// mailed once whether it was a wait or a fault when the Mayor was told, so a hold
// flipping between the two is not news (mw-gq6.1194).
func (c *StarvedRigCheck) mailedSoFar(ctx context.Context) map[string]bool {
	told := map[string]bool{}
	for _, key := range []string{StarvedMailedKey, StarvedWaitMailedKey} {
		kept, _ := c.Mailed.Note(ctx, key)
		for _, line := range mailedLines(kept) {
			told[line] = true
		}
	}
	return told
}

// pruneMailed drops from both mailed-notes the identities of rigs no longer
// starved, so a rig that starves again after its alarm cleared is news again,
// whichever host sees it first. An identity still starved stays, whichever note
// holds it. A dry run, or no Mailed, changes nothing.
func (c *StarvedRigCheck) pruneMailed(ctx context.Context, alarms []DamperAlarm) {
	if c.Mailed == nil || DoctorDryRun(ctx) {
		return
	}
	current := map[string]bool{}
	for _, a := range alarms {
		current[a.Identity] = true
	}
	for _, key := range []string{StarvedMailedKey, StarvedWaitMailedKey} {
		kept, _ := c.Mailed.Note(ctx, key)
		if kept == "" {
			continue
		}
		var still []string
		for _, line := range mailedLines(kept) {
			if current[line] {
				still = append(still, line)
			}
		}
		if len(still) == len(mailedLines(kept)) {
			continue
		}
		if len(still) == 0 {
			_ = c.Mailed.ClearNote(ctx, key)
		} else {
			_ = c.Mailed.SetNote(ctx, key, strings.Join(still, "\n"))
		}
	}
}

// tellOfNew mails the Mayor of those of these alarms he was not told of yet, in
// either note, and keeps their identities in the note under key first (a host a
// few seconds behind sees the claim); the claim is dropped again if the mail
// fails. lead starts the mail. Nothing is sent when every alarm was told of.
func (c *StarvedRigCheck) tellOfNew(ctx context.Context, key string, alarms []DamperAlarm, lead string) error {
	if c.Mailed == nil {
		return c.mailMayor(ctx, alarms, lead)
	}
	told := c.mailedSoFar(ctx)
	var fresh []DamperAlarm
	for _, a := range alarms {
		if !told[a.Identity] {
			fresh = append(fresh, a)
		}
	}
	if len(fresh) == 0 {
		return nil
	}
	before, _ := c.Mailed.Note(ctx, key)
	claimed := append(mailedLines(before), mailedLines(starvedMailed(fresh))...)
	slices.Sort(claimed)
	if err := c.Mailed.SetNote(ctx, key, strings.Join(claimed, "\n")); err != nil {
		return fmt.Errorf("what the Mayor is mailed of the starved rigs could not be kept: %w", err)
	}
	err := c.mailMayor(ctx, fresh, lead)
	if err != nil {
		if before == "" {
			_ = c.Mailed.ClearNote(ctx, key)
		} else {
			_ = c.Mailed.SetNote(ctx, key, before)
		}
	}
	return err
}

// tellOfWaits mails the Mayor of the rigs waiting for room, once a starving
// whichever host sees them first. A dry run, or no Mailed, mails nothing and
// changes nothing.
func (c *StarvedRigCheck) tellOfWaits(ctx context.Context, waits []DamperAlarm) error {
	if c.Mailed == nil || DoctorDryRun(ctx) || len(waits) == 0 {
		return nil
	}
	return c.tellOfNew(ctx, StarvedWaitMailedKey, waits, "Waiting for room on the host, not a fault: the host has no room for a Builder (its load is over its cores, or its sessions are all taken by running Builders or cut while a tutor is in use), and a story starts when that eases.")
}

// mailMayor sends the Mayor one mail of these alarms, one line each, after lead.
func (c *StarvedRigCheck) mailMayor(ctx context.Context, alarms []DamperAlarm, lead string) error {
	body := []string{}
	if lead != "" {
		body = append(body, lead, "")
	}
	for _, a := range alarms {
		body = append(body, a.Line())
	}
	body = append(body, "", "`mw dispatch --dry-run` on a host says why it passes each ready story over, and `mw status` shows the same lines under DAMPERS. The alarm clears when a story of the rig starts.")
	return c.Mail(ctx, NewMessage{
		From:    SeatIdentity(MwSeat, c.Host),
		To:      MayorMailbox,
		Subject: fmt.Sprintf("%s: %s has ready stories and none running for over %s", MailStarved, strings.Join(rigNames(alarms), ", "), elapsed(StarvedAfter)),
		Body:    strings.Join(body, "\n"),
	})
}

// Cure implements DoctorCheck: it mails the Mayor, the starved rigs one line each.
func (c *StarvedRigCheck) Cure(ctx context.Context) error {
	if len(c.found) == 0 {
		return fmt.Errorf("no starved rig was found to tell the Mayor of")
	}
	// Another host that raised this very alarm may have mailed it already; the
	// claim is written before the mail so that one a few seconds behind sees it.
	return c.tellOfNew(ctx, StarvedMailedKey, c.found, "")
}

// Damper implements DoctorCheck: one mail an episode.
func (c *StarvedRigCheck) Damper() (time.Duration, int) { return 0, 1 }

// WayBack implements DoctorCheck: nothing is changed but the mail.
func (c *StarvedRigCheck) WayBack() string {
	return "none: the cure only mails the Mayor; release what holds the rig (mw dispatch --dry-run says what), and the alarm clears when a story of it starts"
}
