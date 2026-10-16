package gitcmd

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	millwright "github.com/Jonathan-A-White/millwright"
)

// TrimMergeDriver is the merge driver mw carries for every file it once merged
// by union (mw-gq6.499): union keeps both sides of a hunk both changed, so a
// trim got back every line it took out whenever the other side had appended.
// CodemapDriver is the name millwright's docs/codemap.md was given for the same
// driver (mw-gq6.495). A .gitattributes or info/attributes names either one.
const (
	TrimMergeDriver = "trimsafe"
	CodemapDriver   = "codemap"
)

// IsTrimMerge reports whether value, what git check-attr says of a file's merge
// attribute, names mw's trim-safe driver.
func IsTrimMerge(value string) bool {
	return value == TrimMergeDriver || value == CodemapDriver
}

// MergeDriver is mw's trim-safe driver written to a file of its own for the git
// commands of one merge.
type MergeDriver struct{ path string }

// WriteMergeDriver writes script, millwright.TrimMergeScript when it is empty,
// to a temporary file. Remove takes it away again.
func WriteMergeDriver(script string) (*MergeDriver, error) {
	if script == "" {
		script = millwright.TrimMergeScript
	}
	file, err := os.CreateTemp("", "mw-trim-merge-*.sh")
	if err != nil {
		return nil, fmt.Errorf("writing mw's trim-safe merge driver: %w", err)
	}
	_, err = file.WriteString(script)
	if closed := file.Close(); err == nil {
		err = closed
	}
	if err != nil {
		os.Remove(file.Name())
		return nil, fmt.Errorf("writing mw's trim-safe merge driver: %w", err)
	}
	return &MergeDriver{path: file.Name()}, nil
}

// Config is the -c that defines the driver under each of its names, to go
// before the subcommand of one git command: no host's git config is edited.
func (d *MergeDriver) Config() []string {
	quoted := "'" + strings.ReplaceAll(d.path, "'", `'\''`) + "'"
	var args []string
	for _, name := range []string{TrimMergeDriver, CodemapDriver} {
		args = append(args, "-c", "merge."+name+".driver=sh "+quoted+" %O %A %B")
	}
	return args
}

// Merge runs the driver on three files as git would: ours is overwritten with
// the merge. An error is the driver's refusal, with what it said.
func (d *MergeDriver) Merge(ctx context.Context, dir, base, ours, theirs string) error {
	cmd := exec.CommandContext(ctx, "sh", d.path, base, ours, theirs)
	cmd.Dir = dir
	var said bytes.Buffer
	cmd.Stdout, cmd.Stderr = &said, &said
	if err := cmd.Run(); err != nil {
		if text := strings.TrimSpace(said.String()); text != "" {
			return fmt.Errorf("%w: %s", err, text)
		}
		return err
	}
	return nil
}

// Remove takes the driver's file away.
func (d *MergeDriver) Remove() { os.Remove(d.path) }
