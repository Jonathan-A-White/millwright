package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
)

// newLintCheckItCmd builds `mw lint-checkit`: whether the labels a bead's HOW TO
// CHECK IT quotes are in the rig's src/.
func newLintCheckItCmd() *cobra.Command {
	var dir string
	cmd := &cobra.Command{
		Use:   "lint-checkit <bead> [--dir <checkout>]",
		Short: "Check that the labels a bead's HOW TO CHECK IT quotes are in the rig's src/",
		Long: "mw lint-checkit <bead> reads the newest HOW TO CHECK IT of the bead's comments, takes each label it quotes\n" +
			"(between double quotes, straight or curly, or between backticks) and looks for it, as a fixed string and\n" +
			"case as written, in the files under the src/ of the bead's rig on this host. It writes nothing.\n\n" +
			"It prints 'lint-checkit <bead>: N quoted labels, M found in src/' and, for labels the source lacks,\n" +
			"'not found in src/: <labels>', and exits non-zero when any label is not found. A bead with no HOW TO CHECK IT,\n" +
			"or one that quotes no label, has nothing to check and passes. A rig with no src/ cannot say a label is on the\n" +
			"screen: that is an error, and exits non-zero.\n\n" +
			"--dir searches <dir>/src instead of the rig's checkout: a Builder passes its worktree (--dir .), which holds the\n" +
			"labels its story adds.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			gateway, _, err := posternGateway()
			if err != nil {
				return err
			}
			rigs, _ := config.Rigs()
			result, err := application.LintCheckIt{
				Beads: gateway, Src: sourceFiles{}, Rigs: rigs, Dir: dir, Out: cmd.OutOrStdout(),
			}.Run(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			if result.Failed() {
				cmd.SilenceErrors = true
				cmd.SilenceUsage = true
				return fmt.Errorf("lint-checkit %s: %d quoted labels not found in src/", args[0], len(result.Missing))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "", "search <dir>/src instead of the bead's rig checkout")
	return cmd
}

// sourceFileMax bounds what is read of one file; a larger one is no screen text.
const sourceFileMax = 4 << 20

// sourceFiles is the application.SourceText that walks a directory on disk.
type sourceFiles struct{}

func (s sourceFiles) Contains(dir, text string) (bool, error) {
	return s.ContainsInOrder(dir, []string{text})
}

func (sourceFiles) ContainsInOrder(dir string, pieces []string) (bool, error) {
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return false, fmt.Errorf("%w: %s", application.ErrNoSource, dir)
	}
	found := errors.New("found")
	err = filepath.WalkDir(dir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if entry.Name() == "node_modules" || entry.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		if info, err := entry.Info(); err != nil || info.Size() > sourceFileMax {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rest := string(body)
		for _, piece := range pieces {
			at := strings.Index(rest, piece)
			if at < 0 {
				return nil
			}
			rest = rest[at+len(piece):]
		}
		return found
	})
	if errors.Is(err, found) {
		return true, nil
	}
	return false, err
}
