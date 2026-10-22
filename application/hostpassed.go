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

// NoHostTakesAfter is how long a ready story may have been passed by every host
// before mw status names it under DAMPERS as one no host can take (mw-gq6.703):
// five Kestrel stories sat unclaimed for 35 minutes, passed by the home because
// the network was metered and by the Boost because the rig was not checked out
// there, and nothing said so.
const NoHostTakesAfter = 20 * time.Minute

// DamperNoHost is the principle of the DAMPERS line for a ready story every host
// has passed for a reason of the host's own.
const DamperNoHost = "no host can take it"

// hostPassedGates are the gates of Dispatch.run a story is passed at for a
// reason of the host's own, one that holds until the host is changed and that
// another host does not share: the rig is not checked out here, the network
// here is metered. The others are the story's own (blocked, attempts-exhausted,
// no files key), the rig's or the vault's, which every host shares (smoke-hold,
// formula-not-installed), or pass in a tick or two with no change made
// (no-room, harness-hold, cap).
var hostPassedGates = map[string]bool{
	"rig-not-checked-out": true,
	"metered-network":     true,
}

// HostPassedKey is the note a host's dispatch keeps of the ready stories it
// passed for a reason of the host's own, and since when: one line each, tab
// separated: the story's id, the time it was first so passed (RFC 3339), the
// gate's name and the reason dispatch gave. mw status reads every host's.
func HostPassedKey(host string) string { return "host." + host + ".passed" }

// HostPassed is one ready story a host's dispatch passed for a reason of the
// host's own.
type HostPassed struct {
	Story string
	Since time.Time
	Gate  string
	Why   string
}

// FormatHostPassed is the note's value, its lines in story order.
func FormatHostPassed(passed []HostPassed) string {
	sorted := slices.Clone(passed)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Story < sorted[j].Story })
	lines := make([]string, 0, len(sorted))
	for _, p := range sorted {
		field := func(s string) string { return strings.Join(strings.Fields(s), " ") }
		lines = append(lines, strings.Join([]string{field(p.Story), p.Since.UTC().Format(time.RFC3339), field(p.Gate), field(p.Why)}, "\t"))
	}
	return strings.Join(lines, "\n")
}

// ParseHostPassed reads the note's value. A line that does not parse is passed over.
func ParseHostPassed(value string) []HostPassed {
	var passed []HostPassed
	for _, line := range strings.Split(value, "\n") {
		fields := strings.SplitN(strings.TrimSpace(line), "\t", 4)
		if len(fields) < 3 {
			continue
		}
		since, err := time.Parse(time.RFC3339, fields[1])
		if err != nil {
			continue
		}
		p := HostPassed{Story: fields[0], Since: since, Gate: fields[2]}
		if len(fields) == 4 {
			p.Why = fields[3]
		}
		passed = append(passed, p)
	}
	return passed
}

// keepHostPassed brings the host's note level with the pass: a story passed for
// a reason of the host's own for the first time is passed from now, one passed
// before keeps the time it was first passed, and one that was not passed so
// this time, or is no longer ready, is taken off. The note is written only when
// it changes. A dry run writes nothing.
func (d Dispatch) keepHostPassed(ctx context.Context, ready []StoryDetail, report *DispatchReport) {
	if d.DryRun || d.Memory == nil {
		return
	}
	stillReady := map[string]bool{}
	for _, detail := range ready {
		stillReady[detail.Story.ID] = true
	}
	was, _ := d.Memory.Note(ctx, HostPassedKey(d.Host))
	before := map[string]HostPassed{}
	for _, p := range ParseHostPassed(was) {
		before[p.Story] = p
	}
	var kept []HostPassed
	for _, p := range report.Passed {
		if !hostPassedGates[p.Gate] || !stillReady[p.StoryID] {
			continue
		}
		found := HostPassed{Story: p.StoryID, Since: d.now(), Gate: p.Gate, Why: p.Why}
		if earlier, ok := before[p.StoryID]; ok && earlier.Gate == p.Gate {
			found.Since = earlier.Since
		}
		kept = append(kept, found)
	}
	value := FormatHostPassed(kept)
	if value == FormatHostPassed(ParseHostPassed(was)) {
		return
	}
	var err error
	if len(kept) == 0 {
		err = d.Memory.ClearNote(ctx, HostPassedKey(d.Host))
	} else {
		err = d.Memory.SetNote(ctx, HostPassedKey(d.Host), value)
	}
	if err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("which stories were passed for a reason of this host's own could not be kept in the host's note: %v", err))
	}
}

// PassedByHost is what one host's dispatch noted of a story.
type PassedByHost struct {
	Host string
	HostPassed
}

// NoHostTakes is a ready story every host that could take it has passed for a
// reason of the host's own.
type NoHostTakes struct {
	Story string
	Rig   string
	// Passed are the hosts' reasons, in host order.
	Passed []PassedByHost
	// Since is when the last of the hosts first passed it: the story has waited
	// for no host at all since then.
	Since time.Time
}

// String is the line under the story on the READY line.
func (n NoHostTakes) String() string {
	reasons := make([]string, len(n.Passed))
	for i, p := range n.Passed {
		reasons[i] = p.Host + ": " + p.Why
	}
	return "passed by every host, so none takes it: " + strings.Join(reasons, "; ")
}

// fix is what a person does about a host's reason, the cure the line names.
func (p PassedByHost) fix(rig string) string {
	switch p.Gate {
	case "rig-not-checked-out":
		return fmt.Sprintf("clone %s on %s and mw config set rigs.%s", rig, p.Host, rig)
	case "metered-network":
		return fmt.Sprintf("wait for an unmetered network on %s, or set the story's host to one the rig is checked out on", p.Host)
	}
	return ""
}

// Alarm is the DAMPERS line for a story that has waited more than NoHostTakesAfter,
// and false for one that has not.
func (n NoHostTakes) Alarm(now time.Time) (DamperAlarm, bool) {
	if n.Since.IsZero() || now.Sub(n.Since) <= NoHostTakesAfter {
		return DamperAlarm{}, false
	}
	reasons := make([]string, len(n.Passed))
	for i, p := range n.Passed {
		reasons[i] = fmt.Sprintf("%s: %s (fix: %s)", p.Host, p.Why, p.fix(n.Rig))
	}
	return DamperAlarm{
		Principle: DamperNoHost,
		Text:      fmt.Sprintf("passed by every host for %s. %s", elapsed(now.Sub(n.Since)), strings.Join(reasons, "; ")),
		Beads:     []string{n.Story},
	}, true
}

// noHostTakes finds, among the ready stories, each one every host that could
// take it has passed for a reason of the host's own, by id; nil when none has
// been, or there are no notes to read. A story pinned to a host can be taken by
// that host alone; one that may run on any host by the home hosts, this host and
// any host that has noted a pass, so a host that has noted nothing of the story
// can take it and the story is not called passed by all.
func (s Status) noHostTakes(ctx context.Context, ready []StoryDetail) map[string]NoHostTakes {
	notes, ok := s.Notes.(DamperNotes)
	if !ok {
		return nil
	}
	kept, err := notes.NotesWithPrefix(ctx, "host.")
	if err != nil {
		s.print("mw status: what each host passed for its own reason could not be read: " + err.Error() + "\n")
		return nil
	}
	byHost := map[string]map[string]HostPassed{}
	for key, value := range kept {
		host, found := strings.CutPrefix(key, "host.")
		if !found {
			continue
		}
		host, found = strings.CutSuffix(host, ".passed")
		if !found || host == "" {
			continue
		}
		byHost[host] = map[string]HostPassed{}
		for _, p := range ParseHostPassed(value) {
			byHost[host][p.Story] = p
		}
	}
	anyHost := map[string]bool{s.Host: true}
	for _, host := range domain.HomeHosts {
		anyHost[host] = true
	}
	for host := range byHost {
		anyHost[host] = true
	}
	var found map[string]NoHostTakes
	for _, detail := range ready {
		if detail.IsEpic {
			continue
		}
		merged := detail.Merged()
		hosts := slices.Sorted(maps.Keys(anyHost))
		if merged.Host != "" && merged.Host != domain.HostAuto {
			hosts = []string{merged.Host}
		}
		takes := NoHostTakes{Story: detail.Story.ID, Rig: merged.Rig}
		for _, host := range hosts {
			p, passed := byHost[host][detail.Story.ID]
			if !passed {
				takes = NoHostTakes{}
				break
			}
			takes.Passed = append(takes.Passed, PassedByHost{Host: host, HostPassed: p})
			if p.Since.After(takes.Since) {
				takes.Since = p.Since
			}
		}
		if takes.Story == "" {
			continue
		}
		if found == nil {
			found = map[string]NoHostTakes{}
		}
		found[takes.Story] = takes
	}
	return found
}
