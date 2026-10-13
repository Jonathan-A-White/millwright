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

	output, err := runLine(ctx, c.shell, command, dir, c.nice)
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

// changedSpecs lists the files matching SpecsPattern that HEAD of dir changed
// since its merge-base with base (three dots), sorted. Deleted files are not
// specs to run.
func (c *Checks) changedSpecs(ctx context.Context, dir, base string) ([]string, error) {
	cmd := gitcmd.Command(ctx, c.git, dir, "diff", "--name-only", "--diff-filter=ACMR", base+"...HEAD")
	var out, errs bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errs
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("which specs changed since %s in %s: %w: %s", base, dir, err, strings.TrimSpace(errs.String()))
	}
	var specs []string
	for _, file := range strings.Split(out.String(), "\n") {
		if ok, _ := path.Match(SpecsPattern, file); ok {
			specs = append(specs, file)
		}
	}
	sort.Strings(specs)
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
