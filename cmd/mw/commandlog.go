package main

import (
	"context"
	"sort"
	"time"

	"github.com/spf13/cobra"

	"github.com/Jonathan-A-White/millwright/application"
)

// executeRecorded runs the command line args under root and then appends one
// line to the log: the command's path (mw version), the exit status and how
// long it took. The arguments are never kept, since they can hold private text.
// A log that cannot be written is left alone: no run fails for want of a line.
func executeRecorded(ctx context.Context, root *cobra.Command, args []string, log application.CommandLog, now func() time.Time) error {
	command := root.Name()
	if found, _, err := root.Find(args); err == nil && found != nil {
		command = found.CommandPath()
	}
	root.SetArgs(args)
	start := now()
	err := root.ExecuteContext(ctx)
	if log != nil {
		_ = log.AppendCommandRun(context.WithoutCancel(ctx), application.CommandRun{
			At: start.UTC(), Command: command, Status: statusOf(err), DurationMs: now().Sub(start).Milliseconds(),
		})
	}
	return err
}

func statusOf(err error) int {
	if err == nil {
		return 0
	}
	return exitStatus(err)
}

// commandNames are the paths of every command of mw that can be run, as
// executeRecorded records them: help and completion, which cobra adds, left out.
func commandNames(root *cobra.Command) []string {
	var names []string
	var walk func(*cobra.Command)
	walk = func(c *cobra.Command) {
		for _, child := range c.Commands() {
			if child.Hidden || child.Name() == "help" || child.Name() == "completion" {
				continue
			}
			if child.Runnable() {
				names = append(names, child.CommandPath())
			}
			walk(child)
		}
	}
	walk(root)
	sort.Strings(names)
	return names
}
