package application

import (
	"context"
	"fmt"
	"strings"
)

// ReasonHarnessMissing is a session that died at its start because the
// harness's program was not there (mw-gq6.1281): a box's Claude Code is replaced
// by the home's cloud check while the box's own dispatch ticks, and a session
// started between the old binary going and the new one coming ended with the
// shell's "nice: 'claude': No such file or directory" and no result. The story's
// code was never run, so the attempt is named tooling (not counted against it)
// and the claim is given back for the next dispatch tick, nobody asked. A second
// such session on the same story stops as any session that left no result does
// (ReasonNoResult), for the Mayor.
const ReasonHarnessMissing Reason = "harness-missing"

// HarnessMissingFileName is the run file mw next keeps the shell's words in: what
// says, to the next attempt's close-out, that the story has been given back for
// a missing harness once already.
const HarnessMissingFileName = "harness-missing.txt"

// HarnessMissingFileNameForAttempt is HarnessMissingFileName for one attempt, named
// as the result it stands beside.
func HarnessMissingFileNameForAttempt(attempt int) string {
	return attemptRunFileName(HarnessMissingFileName, attempt)
}

// harnessMissingLine is the line of a pane's tail that says the shell found no
// claude program to run ("nice: 'claude': No such file or directory",
// "bash: claude: No such file or directory"), when the pane shows it.
func harnessMissingLine(tail string) (string, bool) {
	for _, line := range strings.Split(tail, "\n") {
		line = strings.TrimSpace(line)
		if missingFileIsClaude(line) {
			return line, true
		}
	}
	return "", false
}

// missingFileIsClaude is whether a line says that the file the shell could not
// find is the claude program itself: the word before "No such file or directory"
// is claude, bare or at the end of a path, quoted or not. Another program's
// missing file whose path merely contains claude (a notes file under .claude, a
// claude-notes.sh) is not (mw-gq6.1357).
func missingFileIsClaude(line string) bool {
	const said = ": No such file or directory"
	at := strings.Index(line, said)
	if at < 0 {
		return false
	}
	missing := line[:at]
	if i := strings.LastIndex(missing, ": "); i >= 0 {
		missing = missing[i+2:]
	}
	missing = strings.Trim(missing, "'\"`")
	if i := strings.LastIndex(missing, "/"); i >= 0 {
		missing = missing[i+1:]
	}
	return missing == "claude"
}

// harnessMissing is what a close-out does with a session that left no result and
// whose pane says the shell found no claude to run (mw-gq6.1281), in place of
// stopping it as no-result with the claim kept. The attempt is named tooling and
// the claim given back, once per story; a story that was given back for it
// before is not handled here, and the close-out stops it as it always has, the
// reason in c.crashSaid. handled is whether it was stopped here.
func (n Next) harnessMissing(ctx context.Context, c *closeOut, report *NextReport) (rep NextReport, handled bool, err error) {
	if ctx.Err() != nil {
		return NextReport{}, false, nil
	}
	tail, lookErr := n.paneTail(ctx, c)
	if lookErr != nil {
		return NextReport{}, false, nil
	}
	line, missing := harnessMissingLine(tail)
	if !missing {
		return NextReport{}, false, nil
	}
	for attempt := 1; attempt < c.attempt(); attempt++ {
		if _, err := n.Vault.ReadRunFile(ctx, c.id, HarnessMissingFileNameForAttempt(attempt)); err == nil {
			c.crashSaid = "The harness was missing under an earlier attempt of this story too, which was given back once for it already, so this one is not: " + line
			return NextReport{}, false, nil
		}
	}
	if _, err := n.Vault.PutRunFile(ctx, c.id, HarnessMissingFileNameForAttempt(c.attempt()), line+"\n"); err != nil {
		c.crashSaid = fmt.Sprintf("The harness was missing (%s), but that could not be kept in the run of %s, so the claim is not given back: %v", line, c.id, err)
		return NextReport{}, false, nil
	}
	c.crashFile = HarnessMissingFileNameForAttempt(c.attempt())
	why := fmt.Sprintf("the session never ran: the shell found no claude to start (%s), as when the box's Claude Code is being replaced; the story's code was not run, so the claim is given back and the story dispatched again", line)
	said := n.nameToolingCause(ctx, c, report, why)
	rep, err = n.stop(ctx, c, report, ReasonHarnessMissing, why, said)
	return rep, true, err
}
