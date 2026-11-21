package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain"
	"github.com/Jonathan-A-White/millwright/infrastructure/beads"
)

// doctorTimingEnv, set to anything, makes mw doctor print to stderr one line for
// each read of the tracker the damper survey makes, with the seconds it took, so
// the time the dampers line costs is seen by call (mw-gq6.1323).
const doctorTimingEnv = "MW_DOCTOR_TIMING"

// timedTracker is the survey's gateway with a timing line printed by each read
// the damper survey makes of it.
type timedTracker struct {
	*beads.Gateway
	out io.Writer
}

func (t timedTracker) took(call string, started time.Time) {
	fmt.Fprintf(t.out, "doctor timing: %-22s %6.2fs\n", call, time.Since(started).Seconds())
}

func (t timedTracker) WorkInHand(ctx context.Context) (application.WorkInHand, error) {
	defer t.took("WorkInHand", time.Now())
	return t.Gateway.WorkInHand(ctx)
}

func (t timedTracker) BeadGraph(ctx context.Context) ([]domain.GraphBead, error) {
	defer t.took("BeadGraph", time.Now())
	return t.Gateway.BeadGraph(ctx)
}

func (t timedTracker) NotesWithPrefix(ctx context.Context, prefix string) (map[string]string, error) {
	defer t.took("NotesWithPrefix "+prefix, time.Now())
	return t.Gateway.NotesWithPrefix(ctx, prefix)
}

func (t timedTracker) StoriesComments(ctx context.Context, ids []string) (map[string][]application.Comment, error) {
	defer t.took(fmt.Sprintf("StoriesComments(%d)", len(ids)), time.Now())
	return t.Gateway.StoriesComments(ctx, ids)
}

func (t timedTracker) StoriesProposalComments(ctx context.Context, ids []string) (map[string][]application.Comment, error) {
	defer t.took(fmt.Sprintf("ProposalComments(%d)", len(ids)), time.Now())
	return t.Gateway.StoriesProposalComments(ctx, ids)
}

func (t timedTracker) ClosedStories(ctx context.Context, since time.Time) ([]application.ClosedStory, error) {
	defer t.took("ClosedStories", time.Now())
	return t.Gateway.ClosedStories(ctx, since)
}

func (t timedTracker) ClosedStoriesOfRig(ctx context.Context, rig string, since time.Time, limit int) ([]application.ClosedStory, error) {
	defer t.took("ClosedStoriesOfRig "+rig, time.Now())
	return t.Gateway.ClosedStoriesOfRig(ctx, rig, since, limit)
}

// withDoctorTiming swaps the survey's readers for timed ones when
// MW_DOCTOR_TIMING is set; otherwise the survey is returned as it came.
func withDoctorTiming(survey application.DamperSurvey, gateway *beads.Gateway, out io.Writer) application.DamperSurvey {
	if os.Getenv(doctorTimingEnv) == "" {
		return survey
	}
	timed := timedTracker{Gateway: gateway, out: out}
	survey.Tracker, survey.Notes, survey.Graph, survey.Proposals = timed, timed, timed, timed
	survey.Stuck = application.StuckSurvey{Graph: timed, Comments: timed, Claims: timed, GiveWay: application.GiveWayReader{Notes: timed, Stale: giveWayStaleSetting()}}
	return survey
}
