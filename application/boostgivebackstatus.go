package application

import (
	"context"
	"strings"
)

// gaveBacks reads the note of each host's give-back (GaveBackKey), by host: the
// home's own record that it gave a Boost's claims back, which mw status says in
// place of the STRANDED line (mw-6ww.185.10). A tracker that cannot list notes by
// prefix, or a read that fails, has none.
func (s Status) gaveBacks(ctx context.Context) map[string]GaveBack {
	notes, ok := s.Notes.(DamperNotes)
	if !ok {
		return nil
	}
	kept, err := notes.NotesWithPrefix(ctx, "host.")
	if err != nil {
		return nil
	}
	var gave map[string]GaveBack
	for key, value := range kept {
		host, found := strings.CutPrefix(key, "host.")
		if !found {
			continue
		}
		if host, found = strings.CutSuffix(host, ".gave_back"); !found || host == "" {
			continue
		}
		if g := ParseGaveBack(value); g.Ran() {
			if gave == nil {
				gave = map[string]GaveBack{}
			}
			if g.Host == "" {
				g.Host = host
			}
			gave[host] = g
		}
	}
	return gave
}

// givenBackLines says, for each of the stories that is open and unclaimed again,
// by story id, that a give-back gave it back, as GaveBack.Line has it: a story
// the home gave back is no longer stranded, and says why it is open. Nil when
// none.
func givenBackLines(gave map[string]GaveBack, stories []StoryDetail) map[string]string {
	var lines map[string]string
	for _, detail := range stories {
		if detail.Assignee != "" || !strings.EqualFold(strings.TrimSpace(detail.Status), StatusOpen) {
			continue
		}
		for _, g := range gave {
			if line := g.Line(detail.Story.ID); line != "" {
				if lines == nil {
					lines = map[string]string{}
				}
				lines[detail.Story.ID] = line
				break
			}
		}
	}
	return lines
}

// downSaid is notAnswering for the lines that name a cure for a host that is
// down, with the give-back said once it has run: 'desktop does not answer (silent
// 2h), the home's give-back ran at 11-21 10:00Z'.
func (w HostWork) downSaid() string {
	down := w.notAnswering()
	if down == "" || !w.GaveBack.Ran() {
		return down
	}
	return down + "; " + w.GaveBack.Said()
}
