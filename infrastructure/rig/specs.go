package rig

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path"
	"sort"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/gitcmd"
)

// SpecsPlaceholder is where a rig's changed-specs command takes the spec files
// the story changed, and SpecsPattern is what makes a file a spec (mw-gq6.366).
const (
	SpecsPlaceholder = application.SpecsPlaceholder
	SpecsPattern     = "tests/e2e/*.spec.ts"
)

// Checks satisfies the port a rig's changed specs are run through.
var _ application.SpecChecks = (*Checks)(nil)

// WithSpecCommands names the changed-specs command line each rig has, by rig
// name, as the [changed_specs] table of the config file has it. A rig the table
// does not name has none, and lands exactly as it does without the table.
func WithSpecCommands(commands map[string]string) CheckOption {
	return func(c *Checks) {
		for rig, command := range commands {
			if strings.TrimSpace(command) != "" {
				c.specs[rig] = command
			}
		}
	}
}

// RunSpecs implements application.SpecChecks: the spec files dir has added,
// copied, modified or renamed since its merge-base with base, put into the rig's
// changed-specs command where it says {specs} and run by the shell in dir. A
// rig with no command, and a branch that changed no spec, run nothing.
func (c *Checks) RunSpecs(ctx context.Context, rig, dir, base string) (application.SpecsChecked, error) {
	template, named := c.specs[rig]
	if !named {
		return application.SpecsChecked{}, nil
	}
	specs, err := c.changedSpecs(ctx, dir, base)
	if err != nil || len(specs) == 0 {
		return application.SpecsChecked{}, err
	}
	command := strings.ReplaceAll(template, SpecsPlaceholder, shellWords(specs))

	output, err := runLine(ctx, c.shell, command, dir, c.nice, c.commandEnv(rig))
	run := application.SpecsChecked{Checked: application.Checked{Command: command, Output: output, Passed: err == nil}, Specs: specs}
	if err == nil {
		return run, nil
	}
	var exited *exec.ExitError
	if errors.As(err, &exited) && ctx.Err() == nil {
		code := exited.ExitCode()
		run.NotRun = code == exitNotExecutable || code == exitNotFound
		return run, nil
	}
	return application.SpecsChecked{}, fmt.Errorf("running `%s` in %s: %w: %s", command, dir, err, application.RecentLines(output, 10))
}

// FilesPlaceholder is where a rig's related-tests line takes the files the
// branch changed (mw-gq6.601).
const FilesPlaceholder = "{files}"

// Checks satisfies the port a rig's related-tests line is run through.
var _ application.RelatedChecks = (*Checks)(nil)

// WithRelatedCommands names the related-tests line each rig has, by rig name,
// as the [related_tests] table of the config file has it. A rig the table does
// not name has none, and its gate runs its whole tests.
func WithRelatedCommands(commands map[string]string) CheckOption {
	return func(c *Checks) {
		for rig, command := range commands {
			if strings.TrimSpace(command) != "" {
				c.related[rig] = command
			}
		}
	}
}

// RunRelated implements application.RelatedChecks: the files dir has added,
// copied, modified or renamed since its merge-base with base, put into the
// rig's related-tests line where it says {files} and run as the rig's tests
// are. A rig with no line runs nothing, and a branch that left no file (it only
// deleted some) runs nothing and says so, for the caller to run the whole tests.
func (c *Checks) RunRelated(ctx context.Context, rig, dir string, span application.RelatedSpan) (application.RelatedRun, error) {
	template, named := c.related[rig]
	if !named {
		return application.RelatedRun{}, nil
	}
	files, err := c.changedFilesSkipping(ctx, dir, span)
	if err != nil {
		return application.RelatedRun{}, err
	}
	if len(files) == 0 {
		return application.RelatedRun{NoFiles: true}, nil
	}
	checked, err := c.RunLine(ctx, rig, dir, strings.ReplaceAll(template, FilesPlaceholder, shellWords(files)))
	if err != nil {
		return application.RelatedRun{}, err
	}
	return application.RelatedRun{Checked: checked, Files: files, Ran: true}, nil
}

// changedFilesSkipping lists the files HEAD of dir changed as span says: all
// that changed since the merge-base with span.Base, or, when span.Skip names a
// commit, what changed before it and after it, and not what it changed itself.
func (c *Checks) changedFilesSkipping(ctx context.Context, dir string, span application.RelatedSpan) ([]string, error) {
	if span.Skip == "" {
		return c.changedFiles(ctx, dir, span.Base+"...HEAD")
	}
	before, err := c.changedFiles(ctx, dir, span.Base+"..."+span.Skip+"^")
	if err != nil {
		return nil, err
	}
	after, err := c.changedFiles(ctx, dir, span.Skip+"..HEAD")
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var files []string
	for _, file := range append(before, after...) {
		if !seen[file] {
			seen[file] = true
			files = append(files, file)
		}
	}
	sort.Strings(files)
	return files, nil
}

// changedFiles lists the files added, copied, modified or renamed in the range
// of revisions given, in dir, sorted.
func (c *Checks) changedFiles(ctx context.Context, dir, revisions string) ([]string, error) {
	cmd := gitcmd.Command(ctx, c.git, dir, "diff", "--name-only", "--diff-filter=ACMR", revisions)
	var out, errs bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errs
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("which files changed in %s in %s: %w: %s", revisions, dir, err, strings.TrimSpace(errs.String()))
	}
	var files []string
	for _, file := range strings.Split(out.String(), "\n") {
		if file != "" {
			files = append(files, file)
		}
	}
	sort.Strings(files)
	return files, nil
}

// changedSpecs lists the files matching SpecsPattern that HEAD of dir changed
// since its merge-base with base (three dots), sorted. Deleted files are not
// specs to run.
func (c *Checks) changedSpecs(ctx context.Context, dir, base string) ([]string, error) {
	files, err := c.changedFiles(ctx, dir, base+"...HEAD")
	if err != nil {
		return nil, err
	}
	var specs []string
	for _, file := range files {
		if ok, _ := path.Match(SpecsPattern, file); ok {
			specs = append(specs, file)
		}
	}
	return specs, nil
}

// shellWords is words as a shell reads them back as the same words, each
// single-quoted unless it is plain.
func shellWords(words []string) string {
	quoted := make([]string, len(words))
	for i, word := range words {
		if strings.Trim(word, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789._/-") == "" {
			quoted[i] = word
			continue
		}
		quoted[i] = "'" + strings.ReplaceAll(word, "'", `'\''`) + "'"
	}
	return strings.Join(quoted, " ")
}

// TestsPlaceholder is where a rig's named-tests line takes the names of the
// tests that failed (mw-gq6.645).
const TestsPlaceholder = "{tests}"

// Checks satisfies the port a rig's named-tests line is run through.
var _ application.NamedChecks = (*Checks)(nil)

// WithNamedCommands names the line each rig runs just the failed tests by name
// with, by rig name, as the [named_tests] table of the config file has it. A rig
// the table does not name is never asked, and a failure of its tests is never
// found to be main's own.
func WithNamedCommands(commands map[string]string) CheckOption {
	return func(c *Checks) {
		for rig, command := range commands {
			if strings.TrimSpace(command) != "" {
				c.named[rig] = command
			}
		}
	}
}

// RunNamed implements application.NamedChecks: the names put into the rig's
// named-tests line where it says {tests} and run in dir as the rig's tests are.
// A rig with no line, or no names, runs nothing.
func (c *Checks) RunNamed(ctx context.Context, rig, dir string, tests []string) (application.NamedRun, error) {
	template, named := c.named[rig]
	if !named || len(tests) == 0 {
		return application.NamedRun{}, nil
	}
	checked, err := c.RunLine(ctx, rig, dir, strings.ReplaceAll(template, TestsPlaceholder, shellWords(tests)))
	if err != nil {
		return application.NamedRun{}, err
	}
	return application.NamedRun{Checked: checked, Ran: true}, nil
}
