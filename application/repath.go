package application

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/domain"
)

// DefaultRepathNoRoom is how long a story's pinned host may have had no room
// before a dispatch tick that has room moves the story to host=auto
// (mw-gq6.1165, AC 2): repath_no_room_minutes in the config file. PROVISIONAL:
// the Governor has yet to confirm sixty minutes.
const DefaultRepathNoRoom = 60 * time.Minute

// HostNoRoomKey is the note a host's dispatch keeps while the host has no room
// (load or memory, ReadRoom): the time it first found none, and why, so that
// another host's tick can tell a host that is full from one that was busy a
// minute ago. The note is gone once the host has room again.
func HostNoRoomKey(host string) string { return "host." + host + ".noroom" }

// HostNoRoom is what the note says: since when the host has had no room, and
// the reason it gave.
type HostNoRoom struct {
	Since time.Time
	Why   string
}

// FormatHostNoRoom is the note's value: the time (RFC 3339), a tab, the reason.
func FormatHostNoRoom(n HostNoRoom) string {
	return n.Since.UTC().Format(time.RFC3339) + "\t" + strings.Join(strings.Fields(n.Why), " ")
}

// ParseHostNoRoom reads the note's value. A value that does not parse is the
// zero HostNoRoom, which says the host has room.
func ParseHostNoRoom(value string) HostNoRoom {
	first, why, _ := strings.Cut(strings.TrimSpace(value), "\t")
	since, err := time.Parse(time.RFC3339, first)
	if err != nil {
		return HostNoRoom{}
	}
	return HostNoRoom{Since: since, Why: why}
}

// PinWhy is why a story's host key must stay as it is, or empty when it is
// free to be moved: its own host_why, or host=laptop on a rig whose file says
// it needs the laptop (a --morpheus run, the phone). A pin that must stay is
// never re-pathed (mw-gq6.1165, AC 3), and mw status names it.
func PinWhy(path domain.Path, rules domain.EpicRequirements) string {
	if why := strings.TrimSpace(path.HostWhy); why != "" {
		return why
	}
	if path.Host == "laptop" && rules.NeedsLaptop {
		return fmt.Sprintf("the rig %s needs the laptop", orRig(path.Rig))
	}
	return ""
}

// Repathed is one story a dispatch tick moved to host=auto.
type Repathed struct {
	StoryID string
	From    string
	Why     string
}

// places says where a story may be pinned: not to a retired host, and not to a
// cloud box that the cloud book does not hold (destroyed, or never made).
type places struct {
	retired []string
	plan    CloudPlan
	// boxes are the names in the book; known is false when no book was read, and
	// then no box is called gone.
	boxes map[string]bool
	known bool
}

// readPlaces reads the cloud book once. A book that cannot be read leaves the
// boxes unknown, which is what no book is: the error is said so it can be noted.
func readPlaces(ctx context.Context, retired []string, book CloudBook, plan CloudPlan) (places, error) {
	p := places{retired: retired, plan: plan}
	if book == nil {
		return p, nil
	}
	state, err := book.Read(ctx)
	if err != nil {
		return p, fmt.Errorf("the cloud's book could not be read, so no box is called gone: %w", err)
	}
	p.known, p.boxes = true, map[string]bool{}
	for _, box := range state.Boxes {
		p.boxes[box.Name] = true
	}
	return p, nil
}

// unusable says why host is no place to pin a story, or empty when it is one.
func (p places) unusable(host string) string {
	switch {
	case IsRetired(p.retired, host):
		return fmt.Sprintf("the host %s is retired (%s = [%s] in the config file)", host, RetiredHostsKey, strings.Join(p.retired, ", "))
	case p.known && p.plan.isBox(host) && !p.boxes[host]:
		return fmt.Sprintf("%s is a cloud box that is not in the cloud book (destroyed, or never made)", host)
	}
	return ""
}

// anyToJudge says whether a pass has anything that could move a story, so that a
// tick with no retired host, no book and no host's note reads no stories for it.
func (p places) anyToJudge() bool { return len(p.retired) > 0 || p.known }

// repath moves the open stories pinned to a host that cannot take them to
// host=auto (mw-gq6.1165): a host the Governor retired, a cloud box the book no
// longer holds, or a host that has had no room for over d.RepathNoRoom while
// this host has room and passes the gates a story passes here. A pin that must
// stay (PinWhy) is never moved, nor a held story, nor a claimed one. A move is
// a metadata write and a RE-PATHED: comment on the story, never a new bead.
//
// It runs before the ready stories are read, so the tick that moves a story can
// take it. A dry run moves nothing. What cannot be read or written is a note on
// the report and the tick goes on.
func (d Dispatch) repath(ctx context.Context, noRoom string, network *NetworkReading, report *DispatchReport) {
	if d.DryRun {
		return
	}
	lister, ok := d.Tracker.(interface {
		WorkInHand(ctx context.Context) (WorkInHand, error)
	})
	if !ok {
		return
	}
	where, err := readPlaces(ctx, d.Retired, d.CloudBook, d.CloudPlan)
	if err != nil {
		report.Notes = append(report.Notes, err.Error())
	}
	full := d.hostsWithNoRoom(ctx, report)
	if !where.anyToJudge() && len(full) == 0 {
		return
	}
	work, err := lister.WorkInHand(ctx)
	if err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("the stories ready on every host could not be read, so none was re-pathed: %v", err))
		return
	}
	limit := d.RepathNoRoom
	if limit <= 0 {
		limit = DefaultRepathNoRoom
	}
	rules := map[string]domain.EpicRequirements{}
	for _, detail := range work.Ready {
		if detail.IsEpic || detail.IsParent() || detail.Hitl() || domain.HeldForLabel(detail.Labels) != "" {
			continue
		}
		path := detail.Merged()
		host := path.Host
		if host == "" || host == domain.HostAuto {
			continue
		}
		why := where.unusable(host)
		if why == "" {
			n, isFull := full[host]
			if !isFull || d.now().Sub(n.Since) <= limit {
				continue
			}
			if here := d.cannotTakeInstead(host, detail, path, noRoom, network, report); here != "" {
				continue
			}
			why = fmt.Sprintf("%s has had no room since %s (%s), more than %s", host, n.Since.UTC().Format(time.RFC3339), n.Why, elapsed(limit))
		}
		if _, read := rules[path.Rig]; !read && d.Rules != nil && path.Rig != "" {
			required, err := d.Rules.EpicRequirements(ctx, path.Rig)
			if err != nil {
				report.Notes = append(report.Notes, fmt.Sprintf("the file of the rig %s could not be read, so a pin on it that must stay is not known: %v", path.Rig, err))
			}
			rules[path.Rig] = required
		}
		if PinWhy(path, rules[path.Rig]) != "" {
			continue
		}
		if err := d.Tracker.SetStoryMetadata(ctx, detail.Story.ID, map[string]string{"host": domain.HostAuto}); err != nil {
			report.Notes = append(report.Notes, fmt.Sprintf("%s: pinned to %s, which cannot take it, and not re-pathed: %v; run mw path set %s host=auto", detail.Story.ID, host, err, detail.Story.ID))
			continue
		}
		comment := fmt.Sprintf("%s the host of %s was %s, and %s: it is host=auto now, so a host that passes its gates takes it (mw dispatch wrote this).",
			RePathedMark, detail.Story.ID, host, why)
		if err := d.Tracker.CommentOnStory(ctx, detail.Story.ID, comment); err != nil {
			report.Notes = append(report.Notes, fmt.Sprintf("%s: re-pathed from %s to host=auto, but the %s comment could not be written: %v", detail.Story.ID, host, RePathedMark, err))
		}
		report.Repathed = append(report.Repathed, Repathed{StoryID: detail.Story.ID, From: host, Why: why})
	}
}

// cannotTakeInstead is why this host cannot take a story pinned to another
// host that is full, or empty when it can: it is the one that has room now and
// passes the gates the story would meet here (the rig checked out, the network,
// the grist test key, a harness stop).
func (d Dispatch) cannotTakeInstead(pinned string, detail StoryDetail, path domain.Path, noRoom string, network *NetworkReading, report *DispatchReport) string {
	switch {
	case pinned == d.Host:
		return "this is the full host"
	case IsRetired(d.Retired, d.Host):
		return "this host is retired"
	case noRoom != "":
		return "this host has no room"
	case report.HarnessHold != nil:
		return "this host is held by a harness stop"
	}
	if _, ok := d.Rigs[path.Rig]; !ok {
		return "the rig is not checked out here"
	}
	if network != nil && network.Metered && d.HeavyNet[path.Rig] {
		return "the network here is metered"
	}
	if d.HasGristTestKey != nil && needsGristTestKey(detail) && !d.HasGristTestKey() {
		return "this host has no grist test key"
	}
	return ""
}

// hostsWithNoRoom reads every host's no-room note: host to what it says. A
// Memory that cannot list notes by prefix, or a read that fails, finds none.
func (d Dispatch) hostsWithNoRoom(ctx context.Context, report *DispatchReport) map[string]HostNoRoom {
	notes, ok := d.Memory.(DamperNotes)
	if !ok {
		return nil
	}
	full, err := noRoomNotes(ctx, notes)
	if err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("which hosts have no room could not be read, so no story was re-pathed for it: %v", err))
		return nil
	}
	return full
}

// noRoomNotes is the hosts whose no-room note says they have none, by host.
func noRoomNotes(ctx context.Context, notes DamperNotes) (map[string]HostNoRoom, error) {
	kept, err := notes.NotesWithPrefix(ctx, "host.")
	if err != nil {
		return nil, err
	}
	var full map[string]HostNoRoom
	for key, value := range kept {
		host, found := strings.CutPrefix(key, "host.")
		if !found {
			continue
		}
		if host, found = strings.CutSuffix(host, ".noroom"); !found || host == "" {
			continue
		}
		if n := ParseHostNoRoom(value); !n.Since.IsZero() {
			if full == nil {
				full = map[string]HostNoRoom{}
			}
			full[host] = n
		}
	}
	return full, nil
}

// keepNoRoom keeps this host's no-room note level with the room just read: a
// host with no room notes the time it first found none and leaves the note
// alone while it stays full, and a host with room again takes the note off. A
// dry run writes nothing, nor does a Memory that is nil.
func (d Dispatch) keepNoRoom(ctx context.Context, noRoom string, report *DispatchReport) {
	if d.DryRun || d.Memory == nil {
		return
	}
	was, err := d.Memory.Note(ctx, HostNoRoomKey(d.Host))
	if err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("this host's no-room note could not be read: %v", err))
		return
	}
	switch kept := !ParseHostNoRoom(was).Since.IsZero(); {
	case noRoom != "" && !kept:
		err = d.Memory.SetNote(ctx, HostNoRoomKey(d.Host), FormatHostNoRoom(HostNoRoom{Since: d.now(), Why: noRoom}))
	case noRoom == "" && kept:
		err = d.Memory.ClearNote(ctx, HostNoRoomKey(d.Host))
	}
	if err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("this host's no-room note could not be written: %v", err))
	}
}

// repathedLines are the lines of the report for the stories the tick moved.
func (r DispatchReport) repathedLines() []string {
	var lines []string
	for _, moved := range r.Repathed {
		lines = append(lines, fmt.Sprintf("re-pathed %s from %s to host=auto: %s", moved.StoryID, moved.From, moved.Why))
	}
	return lines
}

// HostRefusedWhy is why a host is no place to write into a path, or empty when
// it is one: a retired host, or a cloud box the book does not hold. mw path set
// and the filers write host=auto for such a host and say why (mw-gq6.1165, AC 5).
func HostRefusedWhy(ctx context.Context, host string, retired []string, book CloudBook, plan CloudPlan) string {
	if host == "" || host == domain.HostAuto {
		return ""
	}
	if why := (places{retired: retired}).unusable(host); why != "" {
		return why
	}
	where, err := readPlaces(ctx, nil, book, plan)
	if err != nil {
		return ""
	}
	return where.unusable(host)
}

// pinsOf names each unclaimed story in a host's block that the host cannot take
// (retired, a box the book no longer holds, or full for over the re-path limit)
// and that a dispatch tick leaves there because its pin says why it must stay:
// story id to "held by its pin: <why>". Nil when none.
func (s Status) pinsOf(ctx context.Context, w HostWork) map[string]string {
	var ready []StoryDetail
	for _, d := range w.Stories {
		if strings.ToLower(strings.TrimSpace(d.Status)) == StatusOpen && d.Assignee == "" {
			ready = append(ready, d)
		}
	}
	return s.heldByPin(ctx, ready)
}

// heldByPin names each ready story a dispatch tick would re-path but for its
// pin, by id: its host is retired, a box the book no longer holds, or full for
// over the re-path limit, and PinWhy says it must stay there. Nil when none.
func (s Status) heldByPin(ctx context.Context, ready []StoryDetail) map[string]string {
	where, _ := readPlaces(ctx, s.Retired, s.Cloud, s.CloudPlan)
	var full map[string]HostNoRoom
	if notes, ok := s.Notes.(DamperNotes); ok {
		full, _ = noRoomNotes(ctx, notes)
	}
	var lines map[string]string
	rules := map[string]domain.EpicRequirements{}
	for _, detail := range ready {
		path := detail.Merged()
		if detail.IsEpic || path.Host == "" || path.Host == domain.HostAuto {
			continue
		}
		cannot := where.unusable(path.Host)
		if n, isFull := full[path.Host]; cannot == "" && isFull && s.now().Sub(n.Since) > DefaultRepathNoRoom {
			cannot = fmt.Sprintf("%s has had no room since %s", path.Host, n.Since.UTC().Format(time.RFC3339))
		}
		if cannot == "" {
			continue
		}
		if _, read := rules[path.Rig]; !read && s.Rules != nil && path.Rig != "" {
			rules[path.Rig], _ = s.Rules.EpicRequirements(ctx, path.Rig)
		}
		why := PinWhy(path, rules[path.Rig])
		if why == "" {
			continue
		}
		if lines == nil {
			lines = map[string]string{}
		}
		lines[detail.Story.ID] = "held by its pin: " + why
	}
	return lines
}
