package application

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

var _ FindingRechecker = Suite{}

// RecheckFinding reads a finding against the head of rig's origin, in a
// worktree of its own (mw-gq6.1245): it runs command there when it is not empty,
// and counts the lines of each file the finding names (a file that is not there
// has no count). A command that fails is an answer; an error is a read that
// could not be made.
func (s Suite) RecheckFinding(ctx context.Context, rig, command string, files []FindingLine) (FindingAnswer, error) {
	rigDir, known := s.Rigs[rig]
	if !known {
		return FindingAnswer{}, fmt.Errorf("%s is no rig of this host: the config file's [rigs] table does not name it", rig)
	}
	dir, head, release, err := s.cutTree(ctx, rig, rigDir, RecheckTreePrefix)
	if err != nil {
		return FindingAnswer{}, err
	}
	defer release()

	answer := FindingAnswer{Sha: head.Commit, Lines: map[string]int{}}
	for _, file := range files {
		if _, counted := answer.Lines[file.Path]; counted {
			continue
		}
		counted, err := s.Runner.RunLine(ctx, rig, dir, "[ -f "+shellWord(file.Path)+" ] && wc -l < "+shellWord(file.Path))
		if err != nil {
			return FindingAnswer{}, fmt.Errorf("counting the lines of %s in %s: %w", file.Path, dir, err)
		}
		if counted.Passed {
			if n, err := strconv.Atoi(strings.TrimSpace(counted.Output)); err == nil {
				answer.Lines[file.Path] = n
			}
		}
	}
	if command == "" {
		return answer, nil
	}
	checked, err := s.Runner.RunLine(ctx, rig, dir, command)
	if err != nil {
		return FindingAnswer{}, fmt.Errorf("running `%s` in %s: %w", command, dir, err)
	}
	if checked.NotRun {
		return FindingAnswer{}, fmt.Errorf("`%s` could not be run in %s: %s", command, dir, checked.Tail(3))
	}
	answer.Ran, answer.Passed, answer.Tail = true, checked.Passed, checked.Tail(SpecRecheckTail)
	return answer, nil
}
