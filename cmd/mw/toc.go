package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain"
)

// newTocCmd builds `mw toc check|fix [paths...]`: the table of contents of
// markdown between <!-- toc --> markers.
func newTocCmd() *cobra.Command {
	var min int
	cmd := &cobra.Command{
		Use:   "toc",
		Short: "Check or write the table of contents of markdown docs",
		Long: "toc keeps a table of contents between the lines " + domain.TocOpen + " and " + domain.TocClose + " of a markdown\n" +
			"document, built from its ## and ### headings (a ### nested under its ##) with GitHub's anchors. The H1 and\n" +
			"headings inside fenced code are not listed. A document wants a table at --min or more ## headings (default 4);\n" +
			"one with the markers keeps a current table whatever its count; one holding the line " + domain.TocNone + "\n" +
			"is left alone, as are CHANGELOG*.md files, anything under node_modules and untracked files.\n\n" +
			"With no paths the files are the tracked *.md of the git repository at the working directory.",
	}
	cmd.PersistentFlags().IntVar(&min, "min", domain.TocMinHeadings, "the number of ## headings that make a document want a table of contents")

	run := func(fix bool) func(cmd *cobra.Command, args []string) error {
		return func(cmd *cobra.Command, args []string) error {
			toc := application.Toc{Files: diskTocFiles{}, Min: min, Out: cmd.OutOrStdout()}
			var report application.TocReport
			var err error
			if fix {
				report, err = toc.Fix(cmd.Context(), args...)
			} else {
				report, err = toc.Check(cmd.Context(), args...)
			}
			if err != nil {
				return err
			}
			if report.Failed() {
				cmd.SilenceErrors = true
				cmd.SilenceUsage = true
				return fmt.Errorf("toc: %d documents need their table of contents", len(report.Findings))
			}
			return nil
		}
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "check [paths...]",
		Short: "List the docs whose table of contents is stale or missing, and the command that cures them",
		Long: "check prints '<path>: TOC stale' or '<path>: TOC missing (N ## headings)' for each document whose table is\n" +
			"not the one its headings give, then one line, 'mw toc fix <those paths>', and exits 1. When every document is\n" +
			"current it prints nothing and exits 0. It writes nothing.",
		RunE: run(false),
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "fix [paths...]",
		Short: "Write the table of contents of each doc that needs one",
		Long: "fix writes each document whose table is stale or missing (before its first ## heading, when it has none yet)\n" +
			"and prints '<path>: TOC written' for each. A second run changes nothing.",
		RunE: run(true),
	})
	return cmd
}

// diskTocFiles is the application.TocFiles of a directory, the working
// directory when dir is empty.
type diskTocFiles struct{ dir string }

func (d diskTocFiles) Tracked() ([]string, error) {
	list := exec.Command("git", "ls-files", "-z", "--", "*.md")
	list.Dir = d.dir
	out, err := list.Output()
	if err != nil {
		return nil, fmt.Errorf("git ls-files: %w", err)
	}
	var files []string
	for _, file := range strings.Split(string(out), "\x00") {
		if file != "" {
			files = append(files, file)
		}
	}
	return files, nil
}

func (d diskTocFiles) path(file string) string { return filepath.Join(d.dir, file) }

func (d diskTocFiles) Read(file string) (string, error) {
	body, err := os.ReadFile(d.path(file))
	return string(body), err
}

func (d diskTocFiles) Write(file, text string) error {
	mode := os.FileMode(0o644)
	if info, err := os.Stat(d.path(file)); err == nil {
		mode = info.Mode().Perm()
	}
	return os.WriteFile(d.path(file), []byte(text), mode)
}
