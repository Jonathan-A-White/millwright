package application

import (
	"context"
	"fmt"
)

// closeOutAgain runs the close-out alone again of a story its last close-out
// handed back at the merge slot's overall most (RunCloseOutAgain, mw-gq6.467):
// its claim, worktree and committed branch were kept, so what runs is `mw next`
// in its worktree, in a session of its own name, never a fresh Builder session
// and never a new attempt. The dead window the last close-out ran in is closed
// first; run=running is recorded and the lapsed lease renewed, so that the
// story is judged as any running close-out is from here on. It reports whether
// the story holds a session now: one already running, or the one started.
// Anything that fails is a note, and the story is left handed back for the
// next tick to try again.
func (d Dispatch) closeOutAgain(ctx context.Context, detail StoryDetail, report *DispatchReport) bool {
	id := detail.Story.ID
	name := SessionName(id)
	// A story held on main's own red waits for the bead that carries the fix
	// (mw-gq6.645): it holds no session and counts against no cap until then.
	wait, over, err := mainRedWaitOver(ctx, d.Tracker, id)
	if err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("%s: whether the bead %s it waits on for main's fix is closed could not be read, so its close-out was left for the next tick: %v", id, wait, err))
		return false
	}
	if !over {
		return false
	}
	mainFixed := wait != "" && wait != MainRedFixed
	status, err := d.Runner.Status(ctx, name)
	if err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("%s: handed back for its close-out alone, but whether its session %s is running could not be read, so it was left for the next tick: %v", id, name, err))
		return true
	}
	if status.Running() {
		return true
	}
	path, err := detail.Path()
	if err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("%s: handed back for its close-out alone, but it has no path: %v", id, err))
		return false
	}
	rigDir, checkedOut := d.Rigs[path.Rig]
	if !checkedOut {
		report.Notes = append(report.Notes, fmt.Sprintf("%s: handed back for its close-out alone, but the rig %s is not checked out on %s", id, path.Rig, d.Host))
		return false
	}
	spec, err := d.Boot.CloseOut(ctx, detail, WorktreeDir(rigDir, id))
	if err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("%s: %v", id, err))
		return false
	}
	if status.State != StateGone {
		if err := d.Runner.Close(ctx, name); err != nil {
			report.Notes = append(report.Notes, fmt.Sprintf("%s: the window %s its last close-out ran in could not be closed, so its close-out was not run again: %v", id, name, err))
			return false
		}
	}
	if err := d.Runner.Start(ctx, spec); err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("%s: its close-out could not be run again in %s, so it stays handed back for the next tick: %v", id, name, err))
		return false
	}
	why := fmt.Sprintf("mw dispatch on %s ran the close-out of %s again, alone, in %s from its committed branch %s: its last close-out handed it back at the merge slot's overall most. No new session and no new attempt.",
		d.Host, id, name, StoryBranch(id))
	if mainFixed {
		why = fmt.Sprintf("mw dispatch on %s ran the close-out of %s again, alone, in %s from its committed branch %s: %s, which carried main's red, is closed. No new session and no new attempt.",
			d.Host, id, name, StoryBranch(id), wait)
		if err := d.Tracker.SetStoryState(ctx, id, MainRedState, MainRedFixed, why); err != nil {
			report.Notes = append(report.Notes, fmt.Sprintf("%s: %s is closed, but it could not be recorded as %s=%s: %v", id, wait, MainRedState, MainRedFixed, err))
		}
	}
	if err := d.Tracker.SetStoryState(ctx, id, RunState, RunRunning, why); err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("%s: its close-out runs again, but it could not be recorded as %s=%s: %v", id, RunState, RunRunning, err))
	}
	if err := d.Tracker.HeartbeatClaim(ctx, id); err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("%s: its close-out runs again, but its claim's lease could not be renewed: %v", id, err))
	}
	if err := d.Tracker.CommentOnStory(ctx, id, why); err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("%s: running its close-out again could not be commented on: %v", id, err))
	}
	report.CloseOutsAgain = append(report.CloseOutsAgain, id)
	return true
}
