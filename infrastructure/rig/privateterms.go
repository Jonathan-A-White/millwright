package rig

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"
)

// privateTermsCheck is the script a rig that is public tracks to hold that no
// commit it publishes carries a term of the private list (mw-t5shinh.19).
const privateTermsCheck = "scripts/check-private-terms.sh"

// checkPrivateTerms is what Push does before it sends anything (mw-gq6.1543): a
// push sends every commit of the branch the remote lacks, so each of them is
// read for the lines it adds, not only the tip a later commit may have cleaned.
// A rig that tracks no such script has no list to hold to and is not checked;
// with no list on the host the script passes, saying skipped. The script comes
// from the landing's own checkout. A refusal names the commit and file the
// script found, never the term nor the line.
func (w *Worktrees) checkPrivateTerms(ctx context.Context, landingDir, remote, branch string) error {
	script := filepath.Join(landingDir, filepath.FromSlash(privateTermsCheck))
	if info, err := os.Stat(script); err != nil || info.IsDir() {
		return nil
	}
	// what the branch holds beyond the remote's copy of it, or, for a branch the
	// remote lacks, beyond anything the remote holds.
	span := "HEAD --not --remotes=" + remote
	if _, err := w.git(ctx, landingDir, "rev-parse", "--verify", "-q", "refs/remotes/"+remote+"/"+branch); err == nil {
		span = "refs/remotes/" + remote + "/" + branch + "..HEAD"
	}
	cmd := exec.CommandContext(ctx, "sh", script, "--commits="+span, landingDir)
	cmd.Dir = landingDir
	said, err := cmd.CombinedOutput()
	if err == nil {
		return nil
	}
	var exit *exec.ExitError
	text := strings.TrimSpace(string(said))
	if errors.As(err, &exit) && exit.ExitCode() == 1 {
		return fmt.Errorf("%w: %s", application.ErrPushPrivateTerm, text)
	}
	return fmt.Errorf("checking the commits to push to %s for private terms in %s: %w: %s", branch, landingDir, err, text)
}
