package rig

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain"
)

// RegenerateLimit is the longest one generator of a rig's regenerated_files may
// run in a landing before it is stopped and the merge undone.
const RegenerateLimit = 10 * time.Minute

// regenerateTail is how many of the last lines of a generator's output a
// refusal says.
const regenerateTail = 6

// Regenerator runs one rig's generator command in a landing worktree and says
// what it wrote, both streams together. A command that does not exit zero is
// an error.
type Regenerator func(ctx context.Context, dir, command string) (string, error)

// WithRegenerator replaces the shell the rig's regenerated_files commands are
// run by, for a test that needs a stand-in for a rig's generator.
func WithRegenerator(run Regenerator) Option {
	return func(w *Worktrees) { w.regenerator = run }
}

// runShell is the Regenerator of a host: `sh -c <command>` in dir.
func runShell(ctx context.Context, dir, command string) (string, error) {
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Dir = dir
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	return out.String(), err
}

// regeneratedBy are the entries of the rig's regenerated_files that list file.
func regeneratedBy(entries []domain.RegeneratedFiles, file string) []domain.RegeneratedFiles {
	var by []domain.RegeneratedFiles
	for _, entry := range entries {
		if entry.Matches(file) {
			by = append(by, entry)
		}
	}
	return by
}

// regenerate settles the files a merge conflicted in that the rig lists as
// regenerated: each takes the target's side (the landing worktree's HEAD is the
// target), each entry that lists one of them runs its command once in dir, what
// the commands rewrote is staged, and a file left holding a conflict marker is
// refused. It says each file with the command that wrote it again, in the order
// of the entries. An error is a refusal that leaves the merge as it stopped,
// for the caller to undo; stopped is git's own words, which it wraps. Its
// "CONFLICT" lines are the ones a batch names when it sets the story aside.
func (w *Worktrees) regenerate(ctx context.Context, dir string, entries []domain.RegeneratedFiles, generated []string, stopped error) ([]application.Regeneration, error) {
	if len(generated) == 0 {
		return nil, nil
	}
	for _, file := range generated {
		if _, err := w.git(ctx, dir, "checkout", "--ours", "--", file); err != nil {
			return nil, fmt.Errorf("%w (CONFLICT (regenerated): %s could not take the target's side: %s)", stopped, file, oneLine(err.Error()))
		}
		if _, err := w.git(ctx, dir, "add", "--", file); err != nil {
			return nil, fmt.Errorf("%w (%v)", stopped, err)
		}
	}
	run := w.regenerator
	if run == nil {
		run = runShell
	}
	var done []application.Regeneration
	ran := map[string]bool{}
	for _, entry := range entries {
		var mine []string
		for _, file := range generated {
			if len(regeneratedBy([]domain.RegeneratedFiles{entry}, file)) > 0 {
				mine = append(mine, file)
			}
		}
		if len(mine) == 0 {
			continue
		}
		if !ran[entry.Run] {
			ran[entry.Run] = true
			limited, cancel := context.WithTimeout(ctx, RegenerateLimit)
			said, err := run(limited, dir, entry.Run)
			cancel()
			if err != nil {
				return nil, fmt.Errorf("%w (CONFLICT (regenerated): %s by `%s` failed: %v; its last lines: %s)",
					stopped, strings.Join(mine, ", "), entry.Run, err, lastLines(said))
			}
		}
		for _, file := range mine {
			done = append(done, application.Regeneration{Path: file, Run: entry.Run})
		}
	}
	// What the commands rewrote: the tracked files they changed and the files
	// that were in conflict, never what else they left lying about.
	if _, err := w.git(ctx, dir, "add", "-u"); err != nil {
		return nil, fmt.Errorf("%w (%v)", stopped, err)
	}
	for _, file := range generated {
		if marked, err := hasConflictMarker(filepath.Join(dir, filepath.FromSlash(file))); err != nil {
			return nil, fmt.Errorf("%w (CONFLICT (regenerated): %s could not be read after its generator ran: %v)", stopped, file, err)
		} else if marked {
			return nil, fmt.Errorf("%w (CONFLICT (regenerated): %s still holds a conflict marker after its generator ran)", stopped, file)
		}
		if _, err := w.git(ctx, dir, "add", "--", file); err != nil {
			return nil, fmt.Errorf("%w (CONFLICT (regenerated): %s was not left by its generator: %s)", stopped, file, oneLine(err.Error()))
		}
	}
	return done, nil
}

// hasConflictMarker reports whether the file at path holds a line git writes
// round a conflict: one that starts with seven < or seven >.
func hasConflictMarker(path string) (bool, error) {
	text, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	for _, line := range strings.Split(string(text), "\n") {
		if strings.HasPrefix(line, "<<<<<<<") || strings.HasPrefix(line, ">>>>>>>") {
			return true, nil
		}
	}
	return false, nil
}

// lastLines are the last few non-empty lines of said, on one line.
func lastLines(said string) string {
	var lines []string
	for _, line := range strings.Split(said, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	if len(lines) > regenerateTail {
		lines = lines[len(lines)-regenerateTail:]
	}
	if len(lines) == 0 {
		return "(it said nothing)"
	}
	return strings.Join(lines, " | ")
}

// oneLine is said with its lines joined, so that it reads as one.
func oneLine(said string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(said, "\n", " ")), " ")
}
