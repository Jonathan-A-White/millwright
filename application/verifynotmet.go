package application

import (
	"context"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"
)

// NotMetWord is what a comment opens with when a landing's own report from the
// Governor shows it does not work (mw verify notmet). The Needs you view reads it
// where it reads VerifiedWord: the landing leaves his line and waits on its fix.
const NotMetWord = "NOT MET"

// NotMetComment is the one place the NOT MET form is written:
//
//	NOT MET by the Governor's report, txid <txid> (<time>), fix <bead>
//	NOT MET by the Governor's report, txid <txid> (<time>), no fix named
func NotMetComment(txid, at, fix string) string {
	tail := "no fix named"
	if fix != "" {
		tail = "fix " + fix
	}
	return fmt.Sprintf("%s by the Governor's report, txid %s (%s), %s", NotMetWord, txid, at, tail)
}

// notMetFixPattern reads the fix a NOT MET comment names, if it names one.
var notMetFixPattern = regexp.MustCompile(`, fix (\S+)\s*$`)

// commentMarksNotMet reports whether text opens with NOT MET: after any leading
// white space, the two words, then a word break.
func commentMarksNotMet(text string) bool {
	rest, ok := strings.CutPrefix(strings.TrimSpace(text), NotMetWord)
	return ok && (rest == "" || strings.ContainsRune(" \t\r\n:,.-", rune(rest[0])))
}

// notMetFix is the fix bead a NOT MET comment names, or "".
func notMetFix(text string) string {
	if m := notMetFixPattern.FindStringSubmatch(strings.TrimSpace(text)); m != nil {
		return m[1]
	}
	return ""
}

// landingVerdict is where a landing stands after its comments: the newest
// comment opening VERIFIED or NOT MET decides.
type landingVerdict struct {
	// Verified is true when the newest verdict opens VERIFIED.
	Verified bool
	// NotMet is true when the newest verdict opens NOT MET; Fix is the bead it
	// names, or "".
	NotMet bool
	Fix    string
}

// newestVerdict reads comments (oldest first) for the newest one that opens
// VERIFIED or NOT MET. No such comment is the zero verdict.
func newestVerdict(comments []Comment) landingVerdict {
	var out landingVerdict
	for _, c := range comments {
		switch {
		case commentMarksVerified(c.Text):
			out = landingVerdict{Verified: true}
		case commentMarksNotMet(c.Text):
			out = landingVerdict{NotMet: true, Fix: notMetFix(c.Text)}
		}
	}
	return out
}

// VerifyNotMetRequest is what mw verify notmet is asked.
type VerifyNotMetRequest struct {
	Bead string
	// Txid is his report that shows it does not work.
	Txid string
	// Fix is the bead filed to fix it; empty names none.
	Fix string
}

// VerifyNotMet is mw verify notmet: it writes the NOT MET comment on a closed
// landing his own report shows does not work, so that the Needs you view takes
// the card off his line until the fix lands.
type VerifyNotMet struct {
	Beads VerifyConfirmBeads
	// InboxDir is where the inbox pass saved his messages and pictures; it dates
	// the report. Empty, or a txid it does not hold, dates it by Now.
	InboxDir string
	// Now is the clock; nil is time.Now.
	Now func() time.Time
	// Out is where the act is said. A nil Out says nothing.
	Out io.Writer
}

// Run writes the comment. It is refused, writing nothing, when the bead is not
// known or is not a closed landing, when its newest verdict is VERIFIED, and
// when --fix names no bead (or the landing itself).
func (n VerifyNotMet) Run(ctx context.Context, req VerifyNotMetRequest) error {
	req.Bead, req.Txid, req.Fix = strings.TrimSpace(req.Bead), strings.TrimSpace(req.Txid), strings.TrimSpace(req.Fix)
	if req.Bead == "" || req.Txid == "" {
		return fmt.Errorf("mw verify notmet: say the bead and his report: mw verify notmet <bead> --txid <txid> [--fix <bead>]")
	}
	found, err := n.Beads.ShowBeads(ctx, []string{req.Bead})
	if err != nil {
		return fmt.Errorf("mw verify notmet: reading %s: %w", req.Bead, err)
	}
	if len(found) != 1 {
		return fmt.Errorf("mw verify notmet: the tracker has no bead %s", req.Bead)
	}
	if bead := found[0]; !bead.Closed() || !hasLanded(bead) {
		return fmt.Errorf("mw verify notmet: %s is not a closed landing (status %s), so there is no landing to mark not met", req.Bead, bead.Status)
	}
	comments, err := n.Beads.StoryComments(ctx, req.Bead)
	if err != nil {
		return fmt.Errorf("mw verify notmet: reading %s's comments: %w", req.Bead, err)
	}
	if newestVerdict(comments).Verified {
		return fmt.Errorf("mw verify notmet: the newest verdict on %s opens %s, so it is not marked not met", req.Bead, VerifiedWord)
	}
	if req.Fix != "" {
		if req.Fix == req.Bead {
			return fmt.Errorf("mw verify notmet: --fix %s is the landing itself; name the bead that fixes it", req.Fix)
		}
		fixes, err := n.Beads.ShowBeads(ctx, []string{req.Fix})
		if err != nil {
			return fmt.Errorf("mw verify notmet: reading %s: %w", req.Fix, err)
		}
		if len(fixes) != 1 {
			return fmt.Errorf("mw verify notmet: --fix %s is not a bead the tracker has", req.Fix)
		}
	}
	now := time.Now()
	if n.Now != nil {
		now = n.Now()
	}
	at := now
	if n.InboxDir != "" {
		if evidence, err := ReadVerifyEvidence(n.InboxDir, time.Time{}, []string{req.Txid}); err == nil && len(evidence) == 1 && !evidence[0].At.IsZero() {
			at = evidence[0].At
		}
	}
	text := NotMetComment(req.Txid, at.UTC().Format(verifyTimeFormat), req.Fix)
	if err := n.Beads.CommentOnStory(ctx, req.Bead, text); err != nil {
		return fmt.Errorf("mw verify notmet: commenting on %s: %w", req.Bead, err)
	}
	if n.Out != nil {
		fmt.Fprintf(n.Out, "commented on %s: %s\n", req.Bead, text)
	}
	return nil
}
