package application

import (
	"context"
	"io"
	"time"
)

// WatchIdeasEndFor runs the wait's watch for the end of Ideas only, as a wait
// whose note says the mode is on until the given Unix time, whose clock of the
// mode is now, and whose release cannot be done (its inbox is not wired), for
// the tests of package application_test. It returns when ctx is done.
func WatchIdeasEndFor(ctx context.Context, until int64, now func() time.Time, errs io.Writer) {
	run := &talkWaitRun{
		TalkWait: TalkWait{
			Ideas:   &IdeasOnlyMode{Now: now, Files: noIdeasOnlyFiles{state: IdeasOnlyState{On: true, Until: until}}},
			Release: &IdeasOnlyRelease{},
			Err:     errs,
		},
		ctx:       ctx,
		ideaState: IdeasOnlyState{On: true, Until: until},
	}
	run.watchIdeasEnd(ctx, func(error) {})
}

// noIdeasOnlyFiles is the two host files of Ideas only with nothing held and a state that does not change.
type noIdeasOnlyFiles struct{ state IdeasOnlyState }

func (f noIdeasOnlyFiles) State(context.Context) (IdeasOnlyState, error) { return f.state, nil }
func (noIdeasOnlyFiles) SetState(context.Context, IdeasOnlyState) error  { return nil }
func (noIdeasOnlyFiles) Held(context.Context) ([]IdeasOnlyHeld, error)   { return nil, nil }
func (noIdeasOnlyFiles) Hold(context.Context, IdeasOnlyHeld) error       { return nil }
func (noIdeasOnlyFiles) Release(context.Context, time.Time) error        { return nil }
