package application

import (
	"context"
	"fmt"
	"maps"
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

// StarvedHolds maps every reason mw dispatch passes a ready story over for, by
// the gate's name in DispatchGates (and PourRefusedGate), to the hold name the
// starved-rig alarm gives it. A gate added to Dispatch.run is mapped here too:
// a test fails until it is. "paused" has none: a paused host passes no story,
// it does nothing at all.
var StarvedHolds = map[string]string{
	"hitl":                   "Governor's own story",
	"no-path":                "unreadable path",
	"no-host":                "no host",
	"other-host":             "another host (asleep?)",
	"harness-hold":           "harness stop",
	"no-room":                "no room",
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
	"cap":                    "cap",
	PourRefusedGate:          "pour refused",
	ClaimLostGate:            "claim lost",
	LeftoverRefusedGate:      "earlier attempt in the way",
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
// ready story passed over, other than for the Governor's own, and no story
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
		if rig == "" || hold == "" || p.Gate == "hitl" || p.Waiting || busy[rig] {
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
// against the work in hand: a rig is starved when it was first so held more than
// StarvedAfter ago and it has a ready story and none running now. A story of it
// that starts clears the alarm at once. Two hosts' notes of one rig count the
// earlier. One alarm a rig, its line naming the rig, how many are ready, for how
// long none ran, and the hold with its reason.
func StarvedAlarms(rigs []StarvedRig, work WorkInHand, now time.Time) []DamperAlarm {
	oldest := map[string]StarvedRig{}
	for _, r := range rigs {
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
				running = running || !d.IsEpic && !d.Hitl() && d.Merged().Rig == rig
			}
		}
		var ready []string
		for _, d := range work.Ready {
			if !d.IsEpic && !d.Hitl() && d.Merged().Rig == rig {
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
		alarms = append(alarms, DamperAlarm{Principle: StarvedPrinciple, Text: text, Beads: ready})
	}
	return alarms
}

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
			if host := d.Merged().Host; !d.IsEpic && d.Merged().Rig == r.Rig && host != "" && host != domain.HostAuto && host != r.Host {
				others[host] = true
			}
		}
	}
	var routed []string
	for _, d := range work.Ready {
		if !d.IsEpic && !d.Hitl() && d.Merged().Rig == r.Rig && d.Merged().Host == r.Host {
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

// closedLister is what reads the stories closed since a time; the beads gateway
// is one (VerifyBeads.ClosedStories), a tracker without it makes the survey
// claim nothing of closed work.
type closedLister interface {
	ClosedStories(ctx context.Context, since time.Time) ([]ClosedStory, error)
}

// ranHosts reads the hosts a story closed within StarvedRanWindow of now was
// routed to, per rig: one bounded list of the closed stories and one read of
// them, and only when a harness-held rig needs it. ok is false when either read
// is not to be had.
func (s DamperSurvey) ranHosts(ctx context.Context, now time.Time) (ran map[string][]string, ok bool) {
	lister, can := s.Tracker.(closedLister)
	if !can {
		return nil, false
	}
	closed, err := lister.ClosedStories(ctx, now.Add(-StarvedRanWindow))
	if err != nil {
		return nil, false
	}
	ids := make([]string, len(closed))
	for i, c := range closed {
		ids[i] = c.ID
	}
	details, err := s.Tracker.ShowBeads(ctx, ids)
	if err != nil {
		return nil, false
	}
	seen := map[[2]string]bool{}
	ran = map[string][]string{}
	for _, d := range details {
		rig, host := d.Merged().Rig, d.Merged().Host
		if d.IsEpic || rig == "" || host == "" || host == domain.HostAuto || seen[[2]string{rig, host}] {
			continue
		}
		seen[[2]string{rig, host}] = true
		ran[rig] = append(ran[rig], host)
	}
	return ran, true
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
	var ran map[string][]string
	var ranRead, ranTried bool
	for _, key := range slices.Sorted(maps.Keys(notes)) {
		if strings.HasPrefix(key, "host.") && strings.HasSuffix(key, ".starved") {
			host := strings.TrimSuffix(strings.TrimPrefix(key, "host."), ".starved")
			for _, r := range ParseStarved(notes[key]) {
				r.Host = host
				if r.Hold == StarvedHolds["harness-hold"] && s.Tracker != nil {
					if !ranTried {
						ran, ranRead = s.ranHosts(ctx, now)
						ranTried = true
					}
					r.Ran, r.RanRead = ran[r.Rig], ranRead
				}
				rigs = append(rigs, r)
			}
		}
	}
	return rigs, nil
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
type StarvedRigCheck struct {
	// Host is this host, named in the mail.
	Host string
	// Alarms reads the starved rigs, one alarm each (DamperSurvey.Starved).
	Alarms func(ctx context.Context) ([]DamperAlarm, error)
	// Mail sends the Mayor the message.
	Mail func(ctx context.Context, message NewMessage) error

	found []DamperAlarm
}

var _ DoctorCheck = (*StarvedRigCheck)(nil)

// Name implements DoctorCheck.
func (c *StarvedRigCheck) Name() string { return StarvedRigName }

// Probe implements DoctorCheck: faulty, one clause for each starved rig, when any
// is; cannot-tell when the notes or the work cannot be read; ok otherwise.
func (c *StarvedRigCheck) Probe(ctx context.Context) (Verdict, string) {
	c.found = nil
	alarms, err := c.Alarms(ctx)
	if err != nil {
		return DoctorCannotTell, err.Error()
	}
	if len(alarms) == 0 {
		return DoctorOK, ""
	}
	c.found = alarms
	lines := make([]string, len(alarms))
	for i, a := range alarms {
		lines[i] = a.Text
	}
	return DoctorFaulty, strings.Join(lines, "; ")
}

// Cure implements DoctorCheck: it mails the Mayor, the starved rigs one line each.
func (c *StarvedRigCheck) Cure(ctx context.Context) error {
	if len(c.found) == 0 {
		return fmt.Errorf("no starved rig was found to tell the Mayor of")
	}
	var rigs, body []string
	for _, a := range c.found {
		rigs = append(rigs, strings.SplitN(a.Text, ":", 2)[0])
		body = append(body, a.Line())
	}
	body = append(body, "", "`mw dispatch --dry-run` on a host says why it passes each ready story over, and `mw status` shows the same lines under DAMPERS. The alarm clears when a story of the rig starts.")
	return c.Mail(ctx, NewMessage{
		From:    SeatIdentity(MwSeat, c.Host),
		To:      MayorMailbox,
		Subject: fmt.Sprintf("%s: %s has ready stories and none running for over %s", MailStarved, strings.Join(rigs, ", "), elapsed(StarvedAfter)),
		Body:    strings.Join(body, "\n"),
	})
}

// Damper implements DoctorCheck: one mail an episode.
func (c *StarvedRigCheck) Damper() (time.Duration, int) { return 0, 1 }

// WayBack implements DoctorCheck: nothing is changed but the mail.
func (c *StarvedRigCheck) WayBack() string {
	return "none: the cure only mails the Mayor; release what holds the rig (mw dispatch --dry-run says what), and the alarm clears when a story of it starts"
}
