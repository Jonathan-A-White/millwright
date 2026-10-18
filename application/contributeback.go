package application

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// ContributeBackJobName is the name of the follower's job that files the
// monthly contribute-back review: its events' actor is contribute-back@<host>.
const ContributeBackJobName = "contribute-back"

// DefaultContributeBackDay is the day of the month, UTC, from which the review
// is filed when nothing says otherwise.
const DefaultContributeBackDay = 1

// ContributeBackEpic is the epic the review is filed under.
const ContributeBackEpic = "mw-gq6"

// ContributeBackLabel is the label the review is filed with: a research
// ticket of the wayfinder.
const ContributeBackLabel = "wayfinder:research"

// ContributeBackTitle is the title of the review of a month ("2026-11").
func ContributeBackTitle(month string) string {
	return "Contribute-back review " + month
}

// ContributeBackKey is the tracker note that keeps the review of a month as
// filed, so that it is filed once.
func ContributeBackKey(month string) string {
	return "contribute-back." + month
}

// contributeBackQuestion is the body of the review.
const contributeBackQuestion = `## Question

Across every rig: which code we wrote to fill a gap in an open-source project (or that duplicates one) would its maintainers likely welcome as a pull request? For each candidate: project, its contribution guide, open issues asking for it, our code, effort; at most three, only real fits.

## How the answer is used

Each pick goes to the Governor as a decision card. A pull request is public: nothing is sent upstream without his word.`

// ContributeBackFiler is the part of the tracker the review is filed through.
type ContributeBackFiler interface {
	CreateStory(ctx context.Context, story NewStory) (string, error)
	ReleaseStory(ctx context.Context, id string) error
}

// ContributeBackNotes is the part of the tracker's key-value store that
// remembers which months are filed.
type ContributeBackNotes interface {
	Note(ctx context.Context, key string) (string, error)
	SetNote(ctx context.Context, key, value string) error
}

// ContributeBack is the follower's job that files, once a month and on the home
// only, a research bead asking which of the code we built to fill a gap in an
// open-source project its maintainers would welcome as a pull request. The
// picks of the review go to the Governor as decision cards; the job sends
// nothing upstream.
//
// From Day of the month on, a month whose review is not yet filed is due, so a
// follower that was down on the day files it when it is back.
type ContributeBack struct {
	// Day is the day of the month, UTC, 1 to 28, from which the review is due.
	Day int
	// Epic is the epic the review is filed under.
	Epic string
	// Tracker is where the review is filed.
	Tracker ContributeBackFiler
	// Notes remembers the months already filed.
	Notes ContributeBackNotes
	// Home says whether this host is the home: only the home files the review.
	Home func(ctx context.Context) (bool, error)
	// Now is the clock; nil is time.Now.
	Now func() time.Time
}

func (c *ContributeBack) now() time.Time {
	if c.Now != nil {
		return c.Now().UTC()
	}
	return time.Now().UTC()
}

// month is the month asked about now, "2026-11", and whether its day has come.
func (c *ContributeBack) month() (string, bool) {
	now := c.now()
	return now.Format("2006-01"), now.Day() >= c.Day
}

// Job is the follower's job: no event springs it; every pass asks whether the
// month's review is due. It is safe to run on what was waiting at the first
// pass, for filing is once a month.
func (c *ContributeBack) Job() SpringJob {
	return SpringJob{
		Name: ContributeBackJobName,
		// A failed month's review is told to the Mayor (mw-gq6.531).
		TellFailure: true,
		Probe: func(ctx context.Context) (string, bool) {
			month, due, _ := c.due(ctx)
			if !due {
				return "", false
			}
			return "contribute-back review of " + month, true
		},
		ProbeFirst: true,
		Run:        c.run,
	}
}

// due says whether the review of this month is to be filed now. A note or home
// that cannot be read leaves it not due.
func (c *ContributeBack) due(ctx context.Context) (month string, due bool, err error) {
	month, arrived := c.month()
	if !arrived {
		return month, false, nil
	}
	filed, err := c.Notes.Note(ctx, ContributeBackKey(month))
	if err != nil || filed != "" {
		return month, false, err
	}
	// Home last, for it reads a file in the vault: asked only when the review is due.
	if at, err := c.Home(ctx); err != nil || !at {
		return month, false, err
	}
	return month, true, nil
}

// run files the review of this month, if it is due, and notes it filed.
func (c *ContributeBack) run(ctx context.Context) error {
	month, due, err := c.due(ctx)
	if err != nil {
		return err
	}
	if !due {
		return SkippedPass("not filed: the review of " + month + " is not due, or already filed, or this host is not the home")
	}
	title := ContributeBackTitle(month)
	bead, err := c.Tracker.CreateStory(ctx, NewStory{
		EpicID:      c.Epic,
		Title:       title,
		Description: contributeBackQuestion,
		Labels:      []string{ContributeBackLabel},
	})
	if err != nil {
		return fmt.Errorf("filing %q under %s: %w", title, c.Epic, err)
	}
	// Noted before the release, so that a release that fails is not a second bead.
	if err := c.Notes.SetNote(ctx, ContributeBackKey(month), bead); err != nil {
		return fmt.Errorf("noting that %s is filed as %s: %w", month, bead, err)
	}
	if err := c.Tracker.ReleaseStory(ctx, bead); err != nil {
		return fmt.Errorf("releasing %s: %w", strings.TrimSpace(bead), err)
	}
	return nil
}
