package application

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/domain"
)

// HostWhyHelp is the one line on what a host_why key does, in mw path set's help
// and its list of keys (mw-gq6.1702).
const HostWhyHelp = "why the host pin must stay: a pin with a host_why is never moved by mw dispatch"

// pinNeedsWhy reports whether path pins the story to a named host with nothing
// that says the pin must stay (PinWhy), so a dispatch tick moves it to host=auto
// once that host has had no room for repath_no_room_minutes (mw-gq6.1702).
func pinNeedsWhy(path domain.Path, rules domain.EpicRequirements) bool {
	host := strings.TrimSpace(path.Host)
	return host != "" && host != domain.HostAuto && PinWhy(path, rules) == ""
}

// pinRules reads the rig's file for a path that is a pin without a host_why of
// its own, to know whether the rig needs the laptop. A rig that cannot be read
// says nothing: no requirements, as the re-path itself judges.
func pinRules(ctx context.Context, rules EpicRules, path domain.Path) domain.EpicRequirements {
	if rules == nil || strings.TrimSpace(path.Rig) == "" || strings.TrimSpace(path.HostWhy) != "" {
		return domain.EpicRequirements{}
	}
	required, _ := rules.EpicRequirements(ctx, path.Rig)
	return required
}

// pinNoWhyWarning is the one warning, empty for none, of a story pinned to a
// named host with no host_why: a dispatch tick moves it to host=auto after the
// host has had no room for limit (zero is DefaultRepathNoRoom), so the pin's
// reason has to be written to keep it. A warning only; nothing is refused.
func pinNoWhyWarning(ctx context.Context, rules EpicRules, limit time.Duration, id string, path domain.Path) string {
	if !pinNeedsWhy(path, pinRules(ctx, rules, path)) {
		return ""
	}
	if limit <= 0 {
		limit = DefaultRepathNoRoom
	}
	return fmt.Sprintf("WARN: %s: pinned to host=%s with no host_why, so mw dispatch moves it to host=auto once %s has had no room for %s (repath_no_room_minutes); "+
		"add host_why=\"<why>\" to keep it there: mw path set %s host_why=\"<why>\"", id, path.Host, path.Host, elapsed(limit), id)
}

// warnPinNoWhy prints pinNoWhyWarning for each story of details a release lets
// go. A warning only; nothing is held back for it.
func warnPinNoWhy(ctx context.Context, out io.Writer, rules EpicRules, limit time.Duration, details ...StoryDetail) {
	if out == nil {
		return
	}
	for _, d := range details {
		if line := pinNoWhyWarning(ctx, rules, limit, d.Story.ID, d.Merged()); line != "" {
			fmt.Fprintln(out, line)
		}
	}
}

// pinsNoWhy names each open or held story of work, on any host, that is pinned
// to a named host with no host_why: the sweep of mw status (mw-gq6.1702). A
// parent, an epic and a Governor's bead are no story a dispatch moves.
func (s Status) pinsNoWhy(ctx context.Context, work WorkInHand) []string {
	var ids []string
	rules := map[string]domain.EpicRequirements{}
	for _, list := range [][]StoryDetail{work.Ready, work.Held} {
		for _, d := range list {
			if d.IsEpic || d.IsParent() || d.Hitl() {
				continue
			}
			path := d.Merged()
			if _, read := rules[path.Rig]; !read {
				rules[path.Rig] = pinRules(ctx, s.Rules, path)
			}
			if pinNeedsWhy(path, rules[path.Rig]) {
				ids = append(ids, d.Story.ID)
			}
		}
	}
	sort.Strings(ids)
	return ids
}

// pinsNoWhyLine is the note of mw status for the open stories pinned to a named
// host with no host_why, or empty for none. A note, not a warning: the pin is
// the Mayor's to explain, and nothing is wrong until its host is full for an hour.
func (r StatusReport) pinsNoWhyLine() string {
	n := len(r.PinsNoWhy)
	if n == 0 {
		return ""
	}
	noun := "stories are"
	if n == 1 {
		noun = "story is"
	}
	return fmt.Sprintf("note: %d open %s pinned to a host with no host_why, so mw dispatch moves %s to host=auto once the host has had no room for repath_no_room_minutes",
		n, noun, map[bool]string{true: "it", false: "each"}[n == 1])
}
