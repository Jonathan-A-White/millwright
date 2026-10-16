package application

import (
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/Jonathan-A-White/millwright/domain"
)

// EscapeDays is how long after a landing a bug that names it counts as one the
// landing let through: the landing's day and the EscapeDays days after it.
const EscapeDays = 14

// escape is one bug story that names landings of its own rig filed in the
// EscapeDays after them: the bug, when it was filed, and those landings.
type escape struct {
	bug      string
	created  time.Time
	landings []reportEntry
}

// firstLandings are the landings among entries, each story's first.
func firstLandings(all []reportEntry) map[string]reportEntry {
	first := map[string]reportEntry{}
	for _, e := range all {
		if !e.landed || e.bug || e.escaped || e.id == "" {
			continue
		}
		if was, seen := first[e.id]; !seen || e.at.Before(was.at) {
			first[e.id] = e
		}
	}
	return first
}

// escapedBugs finds the escaped bugs among stories: a bug story created on a
// landing's day or in the EscapeDays after it, on the landing's rig, whose
// title or description names the landing's story id. Each bug comes once, with
// every landing it names, oldest first.
func escapedBugs(stories []StoryDetail, first map[string]reportEntry) []escape {
	var escapes []escape
	for _, d := range stories {
		if d.IsEpic || d.Created.IsZero() || !domain.IsBugStory(d.Type, d.Story.Title) {
			continue
		}
		rig := strings.TrimSpace(d.Merged().Rig)
		var named []reportEntry
		for id := range storyIDsNamed(d.Story.Title + "\n" + d.Description) {
			landing, ok := first[id]
			if !ok || id == d.Story.ID || rig == "" || landing.rig != rig || !withinEscape(landing.at, d.Created) {
				continue
			}
			named = append(named, landing)
		}
		if len(named) == 0 {
			continue
		}
		sort.Slice(named, func(i, j int) bool {
			if !named[i].at.Equal(named[j].at) {
				return named[i].at.Before(named[j].at)
			}
			return named[i].id < named[j].id
		})
		escapes = append(escapes, escape{bug: d.Story.ID, created: d.Created, landings: named})
	}
	return escapes
}

// withinEscape says a bug filed at created falls on the day landed or in the
// EscapeDays days after it.
func withinEscape(landed, created time.Time) bool {
	return !created.Before(landed) && created.Before(landed.AddDate(0, 0, EscapeDays+1))
}

// escapeOver says the EscapeDays after a landing's day are over by today.
func escapeOver(landed, today time.Time) bool {
	return today.After(landed.AddDate(0, 0, EscapeDays))
}

// storyIDsNamed are the words of text that may be story ids: runs of letters,
// digits, dots, hyphens and underscores, a full stop or other punctuation at
// their end left off, so "since mw-x." names mw-x and "mw-x.1" does not.
func storyIDsNamed(text string) map[string]bool {
	words := strings.FieldsFunc(text, func(r rune) bool {
		return !(r < unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r) || r == '.' || r == '-' || r == '_'))
	})
	named := map[string]bool{}
	for _, w := range words {
		if w = strings.TrimRight(w, ".-_"); w != "" {
			named[w] = true
		}
	}
	return named
}

// escapedEntries are the escaped bugs as the report counts them: each once, on
// the day it was filed, under the rig, host and model of the newest landing it
// names.
func (r FactoryReportResult) escapedEntries(escapes []escape) []reportEntry {
	var entries []reportEntry
	for _, x := range escapes {
		landing := x.landings[len(x.landings)-1]
		previous, inside := r.windowOfTime(x.created)
		entries = append(entries, reportEntry{id: x.bug, escaped: true, previous: previous, inWindow: inside,
			at: reportDay(x.created), rig: landing.rig, host: landing.host, model: landing.model})
	}
	return entries
}

// escapedBy are the bugs that escaped each landing, by its story, sorted.
func escapedBy(escapes []escape) map[string][]string {
	by := map[string][]string{}
	for _, x := range escapes {
		for _, landing := range x.landings {
			by[landing.id] = append(by[landing.id], x.bug)
		}
	}
	for _, bugs := range by {
		sort.Strings(bugs)
	}
	return by
}
