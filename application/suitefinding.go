package application

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

var _ FindingRechecker = Suite{}
var _ FindingRecheckerAt = Suite{}

// RecheckFindingAt runs a finding's command in a worktree of its own cut at the
// commit rev names, the finder's own (mw-gq6.1733): a command that passes there
// reproduces nothing of the finding. It counts no lines.
func (s Suite) RecheckFindingAt(ctx context.Context, rig, rev, command string) (FindingAnswer, error) {
	rigDir, known := s.Rigs[rig]
	if !known {
		return FindingAnswer{}, fmt.Errorf("%s is no rig of this host: the config file's [rigs] table does not name it", rig)
	}
	if rev == "" || command == "" {
		return FindingAnswer{}, fmt.Errorf("a finding is read at a commit only for a command and a commit: got %q and %q", command, rev)
	}
	return s.recheckFinding(ctx, rig, rigDir, rev, command, nil)
}

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
	return s.recheckFinding(ctx, rig, rigDir, "", command, files)
}

// recheckFinding is RecheckFinding in the tree cut at rev (empty: origin's head).
func (s Suite) recheckFinding(ctx context.Context, rig, rigDir, rev, command string, files []FindingLine) (FindingAnswer, error) {
	dir, head, release, err := s.cutTreeAt(ctx, rig, rigDir, RecheckTreePrefix, rev)
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
	command, err = s.findingLine(rig, command)
	if err != nil {
		return FindingAnswer{}, err
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

// findingLine is the line a finding's command is run as in the fresh worktree
// of main: a command that is not node's is itself, and `npm test` or `npx
// vitest run <files>` follows the install step of the rig's own [tests] line
// (the tree has no node_modules) and the env its unit step runs with
// (TZ=UTC). A rig whose [tests] line installs nothing, or has no npm test unit
// step for `npm test`, is an error: the finding is not read and is filed as
// before (mw-gq6.1293).
func (s Suite) findingLine(rig, command string) (string, error) {
	if command != "npm test" && !strings.HasPrefix(command, "npx vitest run ") {
		return command, nil
	}
	tests := strings.TrimSpace(s.Tests[rig])
	install := installStep(tests)
	if install == "" {
		return "", fmt.Errorf("the rig %s's [tests] line has no install step (npm ci), so a fresh worktree of main has no node_modules to run `%s` with", rig, command)
	}
	var unit string
	for _, step := range strings.Split(tests, " && ") {
		if found := unitRunners.FindStringSubmatch(strings.TrimSpace(step)); found != nil {
			unit = strings.TrimSpace(step)
			if command == "npm test" && found[1] == "" {
				return "", fmt.Errorf("the rig %s's unit step is `%s`, not npm test, so `npm test` is not read", rig, unit)
			}
			break
		}
	}
	if unit == "" && command == "npm test" {
		return "", fmt.Errorf("the rig %s's [tests] line has no npm test step, so `npm test` is not read", rig)
	}
	if command == "npm test" {
		return install + " && " + unit, nil
	}
	var env string
	for _, word := range strings.Fields(unit) {
		if !strings.Contains(word, "=") || !foundSafeWord.MatchString(word) {
			break
		}
		env += word + " "
	}
	return install + " && " + env + command, nil
}
