package application

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

// AcceptanceAtClaimField is the metadata a story carries to say what its
// acceptance read when the claim it is held under was made (mw-gq6.509): a
// short hash of each line, comma-joined, in order. A Builder reads its story
// once, at boot, so a criterion the Mayor adds afterwards never reaches it;
// mw next reads the bead's acceptance again at close-out and compares. A story
// claimed before this was written has no record, and is not compared.
const AcceptanceAtClaimField = "acceptance_at_claim"

// acceptanceLines are the lines of an acceptance text that say something, each
// with its whitespace collapsed, so that re-spacing or a blank line is not a
// change.
func acceptanceLines(text string) []string {
	var lines []string
	for _, line := range strings.Split(text, "\n") {
		if line = strings.Join(strings.Fields(line), " "); line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

// lineHash is the short hash of one line of acceptance.
func lineHash(line string) string {
	sum := sha256.Sum256([]byte(line))
	return hex.EncodeToString(sum[:4])
}

// AcceptanceAtClaim is the value of AcceptanceAtClaimField for an acceptance
// text. An empty acceptance is recorded as "-", so that it is told from a story
// with no record.
func AcceptanceAtClaim(acceptance string) string {
	var hashes []string
	for _, line := range acceptanceLines(acceptance) {
		hashes = append(hashes, lineHash(line))
	}
	if len(hashes) == 0 {
		return "-"
	}
	return strings.Join(hashes, ",")
}

// AcceptanceChange is how a story's acceptance differs from what it read at the
// claim.
type AcceptanceChange struct {
	// Added are the lines of the acceptance now that were not in it at the claim.
	Added []string
	// Dropped is how many lines of it at the claim are gone or moved.
	Dropped int
}

// AcceptanceChangedSince compares the acceptance a story holds now with the
// record its claim made. It is false when nothing was recorded or nothing
// changed.
func AcceptanceChangedSince(recorded, acceptance string) (AcceptanceChange, bool) {
	if recorded == "" || recorded == AcceptanceAtClaim(acceptance) {
		return AcceptanceChange{}, false
	}
	had := map[string]bool{}
	for _, hash := range strings.Split(recorded, ",") {
		had[hash] = true
	}
	var change AcceptanceChange
	now := map[string]bool{}
	for _, line := range acceptanceLines(acceptance) {
		now[lineHash(line)] = true
		if !had[lineHash(line)] {
			change.Added = append(change.Added, line)
		}
	}
	for hash := range had {
		if hash != "-" && !now[hash] {
			change.Dropped++
		}
	}
	return change, true
}

// acceptanceChangedRefusal is the refusal of a story whose acceptance is not
// what its claim recorded (mw-gq6.509): the Builder worked the text it read at
// boot, so the story is handed back for a session to work the whole of it.
func acceptanceChangedRefusal(c *closeOut) (Refusal, bool) {
	change, changed := AcceptanceChangedSince(c.detail.AcceptanceAtClaim, c.detail.Acceptance)
	if !changed {
		return Refusal{}, false
	}
	said := "The acceptance of " + c.id + " is not what it read when the claim was made, so the session worked a text that is not the story's now."
	if len(change.Added) > 0 {
		said += "\nLines added since the claim:\n" + bulletList(change.Added)
	}
	if change.Dropped > 0 {
		said += fmt.Sprintf("\n%d line(s) the claim had were taken out or changed.", change.Dropped)
	}
	said += "\nThe claim is given back and the branch kept: the next session works the whole acceptance."
	return Refusal{ReasonAcceptanceChanged, "the acceptance changed after the claim of " + c.id, said}, true
}
