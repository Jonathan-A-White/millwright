package application

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
)

// readResultPulled reads a story's run file from the vault, and when it is not
// there pulls the vault once (git pull --rebase, as the vault's Pull does) and
// reads it again (mw-gq6.1620). The session may have run on another host whose
// runs/<id>/result.json was pushed to the vault and not yet pulled here: stopping
// 'no-result' then wrote a false ledger row, mail and note for a result that was
// one pull away. A file still missing after the pull is missing, and any other
// read error is the error. A pull that fails is told in pullNote and the first
// read stands; a Next with no Files pulls nothing.
func (n Next) readResultPulled(ctx context.Context, storyID, name string) (printed string, err error, pullNote string) {
	printed, err = n.Vault.ReadRunFile(ctx, storyID, name)
	if !errors.Is(err, fs.ErrNotExist) || n.Files == nil {
		return printed, err, ""
	}
	if _, pullErr := retryPastASiblingsWrite(ctx, sleepFor, n.Files.Pull); pullErr != nil {
		return printed, err, fmt.Sprintf("%s has no %s in this host's vault, and the vault could not be pulled to look for it: %v", storyID, name, pullErr)
	}
	printed, err = n.Vault.ReadRunFile(ctx, storyID, name)
	return printed, err, ""
}
