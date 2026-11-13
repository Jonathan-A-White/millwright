package rig

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/gitcmd"
)

// Shape reads a rig's checkout for mw report's SHAPE: the files each commit
// changed from git's log, how long a file is today, and its test callers. It
// writes nothing.
type Shape struct {
	// Program is the git command; empty is Program.
	Program string
}

var _ application.ShapeSource = Shape{}

// commitMark begins each commit in the log Shape reads, before its time.
const commitMark = "\x01"

// Changes implements application.ShapeSource: git log --numstat of the commits
// since the time, merges left out, a renamed file under its new name. A file
// git counts as binary has no lines.
func (s Shape) Changes(ctx context.Context, rigDir string, since time.Time) ([]application.ShapeChange, error) {
	out, err := s.log(ctx, rigDir, since, "--numstat", "-M")
	if err != nil {
		return nil, err
	}
	var changes []application.ShapeChange
	for _, commit := range strings.Split(out, commitMark)[1:] {
		at, tokens, err := commitOf(commit)
		if err != nil {
			return nil, err
		}
		for i := 0; i < len(tokens); i++ {
			added, rest, ok := strings.Cut(strings.TrimLeft(tokens[i], "\n"), "\t")
			if !ok {
				continue
			}
			removed, file, ok := strings.Cut(rest, "\t")
			if !ok {
				continue
			}
			if file == "" && i+2 < len(tokens) {
				// A rename is the numbers, then the old name and the new one.
				file = tokens[i+2]
				i += 2
			}
			changes = append(changes, application.ShapeChange{At: at, Path: file, Added: linesOf(added), Removed: linesOf(removed)})
		}
	}
	return changes, nil
}

// Deleted implements application.ShapeSource: git log --diff-filter=D.
func (s Shape) Deleted(ctx context.Context, rigDir string, since time.Time) ([]application.ShapeChange, error) {
	out, err := s.log(ctx, rigDir, since, "--diff-filter=D", "--name-only")
	if err != nil {
		return nil, err
	}
	var deleted []application.ShapeChange
	for _, commit := range strings.Split(out, commitMark)[1:] {
		at, tokens, err := commitOf(commit)
		if err != nil {
			return nil, err
		}
		for _, file := range tokens {
			if file = strings.TrimLeft(file, "\n"); file != "" {
				deleted = append(deleted, application.ShapeChange{At: at, Path: file})
			}
		}
	}
	return deleted, nil
}

// Lines implements application.ShapeSource: the lines of the file that hold
// something besides white space.
func (Shape) Lines(rigDir, file string) (int, error) {
	text, err := os.ReadFile(filepath.Join(rigDir, filepath.FromSlash(file)))
	if err != nil {
		return 0, err
	}
	var n int
	for _, line := range bytes.Split(text, []byte("\n")) {
		if len(bytes.TrimSpace(line)) > 0 {
			n++
		}
	}
	return n, nil
}

// TestCallersOf implements application.ShapeSource: the count Files.TestCallers
// gives for each file named alone, with the tracked test files read once and
// the files counted side by side, because SHAPE asks for over a hundred and
// one call each would read every test again.
func (Shape) TestCallersOf(rigDir string, files []string) (map[string]int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), callerSearchLimit)
	defer cancel()
	args := []string{"-C", rigDir, "ls-files", "-z", "--", "features", "tests", ":(glob)**/*_test.go"}
	out, err := exec.CommandContext(ctx, "git", args...).Output()
	if err != nil {
		return nil, fmt.Errorf("counting the test callers in %s: %w", rigDir, err)
	}
	type testFile struct {
		name string
		text []byte
	}
	var tests []testFile
	for _, name := range strings.Split(string(out), "\x00") {
		if name == "" {
			continue
		}
		if text, err := os.ReadFile(filepath.Join(rigDir, filepath.FromSlash(name))); err == nil {
			tests = append(tests, testFile{name, text})
		}
	}
	counts := make([]int, len(files))
	next := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < runtime.NumCPU(); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range next {
				matcher := newCallerMatcher(rigDir, []string{files[i]})
				own := path.Clean(files[i])
				for _, test := range tests {
					if test.name != own && matcher.calls(path.Dir(test.name), test.text) {
						counts[i]++
					}
				}
			}
		}()
	}
	for i := range files {
		next <- i
	}
	close(next)
	wg.Wait()
	byFile := make(map[string]int, len(files))
	for i, file := range files {
		byFile[file] = counts[i]
	}
	return byFile, nil
}

// log runs git log for the commits since the time, each headed by commitMark
// and its committer time, the rest of the record split on NUL.
func (s Shape) log(ctx context.Context, rigDir string, since time.Time, args ...string) (string, error) {
	program := s.Program
	if program == "" {
		program = Program
	}
	args = append([]string{"log", "-z", "--no-merges", "--since=" + since.UTC().Format(time.RFC3339), "--format=" + commitMark + "%cI"}, args...)
	cmd := gitcmd.Command(ctx, program, rigDir, args...)
	var out, errs bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errs
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("reading the history of %s with git: %w: %s", rigDir, err, strings.TrimSpace(errs.String()))
	}
	return out.String(), nil
}

// commitOf splits one commit's record into its time and its NUL-separated rest.
func commitOf(record string) (time.Time, []string, error) {
	tokens := strings.Split(record, "\x00")
	at, err := time.Parse(time.RFC3339, strings.TrimSpace(tokens[0]))
	if err != nil {
		return time.Time{}, nil, fmt.Errorf("reading when a commit was made: %w", err)
	}
	return at, tokens[1:], nil
}

// linesOf reads a numstat count; "-" is a binary file's, and no lines.
func linesOf(count string) int {
	n, err := strconv.Atoi(count)
	if err != nil {
		return 0
	}
	return n
}
