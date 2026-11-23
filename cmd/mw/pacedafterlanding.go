package main

import (
	"context"
	"fmt"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
)

// pacedAfterLanding is what a landing runs, once it has closed its stories, to
// release the next stories of the paced lists they were on: the lists of the
// vault (vaultPacedLists), at the pace mw release-paced keeps (mw-gq6.1682). A
// [pace] table that cannot be read releases none and says so in a note.
func pacedAfterLanding(dir string, gateway interface {
	application.WorkTracker
	application.SweepNotes
}) application.PacedReleaser {
	line, err := config.PaceUsageLine()
	if err != nil {
		return pacedUnreadable{err: err}
	}
	maxAtOnce, err := config.PaceMaxAtOnce()
	if err != nil {
		return pacedUnreadable{err: err}
	}
	return application.PacedAfterLanding{
		Lists: vaultPacedLists{dir: dir},
		Release: application.ReleasePaced{
			Tracker:   gateway,
			Readings:  application.UsageReadings{Notes: gateway},
			UsageLine: line,
			MaxAtOnce: maxAtOnce,
			Rules:     rigRulesIfKnown(),
		},
	}
}

// pacedUnreadable is the paced release of a landing whose pace could not be read.
type pacedUnreadable struct{ err error }

func (p pacedUnreadable) ReleaseAfter(context.Context, []string) []string {
	return []string{fmt.Sprintf("release-paced: no list was run again, the pace could not be read: %v", p.err)}
}
