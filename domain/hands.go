package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// HandsStep is one step only the Governor's hands could take — a sudo line,
// a unit to enable, a file to move between hosts — written down by the Mayor
// on a hitl bead, exactly as it will run, and run by the factory only once
// he approves it with his key (postern's docs/protocol.md §17). The JSON tags
// are §17's field names.
type HandsStep struct {
	ID      string `json:"id"`
	Host    string `json:"host"`
	As      string `json:"as"`
	Run     string `json:"run"`
	WayBack string `json:"way_back"`
}

// Who a step runs as: the host's own user, or root through mw-hands-root.
const (
	HandsAsUser = "user"
	HandsAsRoot = "root"
)

// HandsApprovalMaxAge is how old an approval may be when its step runs, and
// HandsApprovalMaxAhead how far ahead of the running host's clock it may be
// stamped: five minutes (§17 said fifteen; the Governor's to confirm), and
// two for clocks that disagree. The one place either is set.
const (
	HandsApprovalMaxAge   = 5 * time.Minute
	HandsApprovalMaxAhead = 2 * time.Minute
)

// handsName is what a step's id and host, and the bead a step is on, may be:
// §12's bead id pattern, which also keeps any of them from reading as a flag.
var handsName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// ValidateHandsStep reports why step, on bead, is not one the factory keeps.
func ValidateHandsStep(bead string, step HandsStep) error {
	switch {
	case !handsName.MatchString(bead):
		return fmt.Errorf("%q is not a bead id", bead)
	case !handsName.MatchString(step.ID):
		return fmt.Errorf("the step id %q is not one: letters, digits, dots, dashes and underscores, starting with a letter or digit", step.ID)
	case !handsName.MatchString(step.Host):
		return fmt.Errorf("the host %q is not one: name a host of the factory", step.Host)
	case step.As != HandsAsUser && step.As != HandsAsRoot:
		return fmt.Errorf("a step runs as %s or %s, not %q", HandsAsUser, HandsAsRoot, step.As)
	case strings.TrimSpace(step.Run) == "":
		return fmt.Errorf("the step %s runs nothing", step.ID)
	}
	return nil
}

// HandsCanonical is the canonical bytes of step on bead, what its sha256 is
// over and what the Governor's approval binds (§17): a version line, then
// each field as its decimal byte length, a colon, the field and a newline.
// The lengths are bytes, never runes, so any text hashes one way only.
func HandsCanonical(bead string, step HandsStep) []byte {
	var b strings.Builder
	b.WriteString("hands/v1\n")
	for _, field := range []string{bead, step.ID, step.Host, step.As, step.Run, step.WayBack} {
		b.WriteString(strconv.Itoa(len(field)))
		b.WriteByte(':')
		b.WriteString(field)
		b.WriteByte('\n')
	}
	return []byte(b.String())
}

// HandsSHA256 is the sha256 of step's canonical bytes, lowercase hex.
func HandsSHA256(bead string, step HandsStep) string {
	sum := sha256.Sum256(HandsCanonical(bead, step))
	return hex.EncodeToString(sum[:])
}

// HandsApprovalMessage is what the Governor's key signs to approve the step
// hashing to sha256 at approvedAt (Unix seconds): the SHA-256 of this
// string's UTF-8 is what the signature is over.
func HandsApprovalMessage(sha256Hex string, approvedAt int64) string {
	return fmt.Sprintf("hands-approve/v1\n%s\n%d\n", sha256Hex, approvedAt)
}

// HandsApprovalDigest is the SHA-256 of HandsApprovalMessage, the hash an
// approval's signature is checked over — @bsv/sdk's PrivateKey.sign hashes
// its message once with SHA-256, as the backend's challenge does.
func HandsApprovalDigest(sha256Hex string, approvedAt int64) []byte {
	sum := sha256.Sum256([]byte(HandsApprovalMessage(sha256Hex, approvedAt)))
	return sum[:]
}

// CheckHandsApprovalAge reports why an approval stamped approvedAt cannot run
// at now: older than HandsApprovalMaxAge, or stamped more than
// HandsApprovalMaxAhead ahead of now.
func CheckHandsApprovalAge(approvedAt int64, now time.Time) error {
	approved := time.Unix(approvedAt, 0)
	if age := now.Sub(approved); age > HandsApprovalMaxAge {
		return fmt.Errorf("the approval is %s old, over the %s an approval is good for: approve it again", age.Round(time.Second), HandsApprovalMaxAge)
	}
	if ahead := approved.Sub(now); ahead > HandsApprovalMaxAhead {
		return fmt.Errorf("the approval is stamped %s ahead of this host's clock, over the %s allowed", ahead.Round(time.Second), HandsApprovalMaxAhead)
	}
	return nil
}

// HandsRequest is what mw hands to mw-hands-root on its standard input for a
// root step: the step, the bead it is on, and the Governor's approval of it
// — everything the helper needs to check it all again itself.
type HandsRequest struct {
	Bead       string `json:"bead"`
	ID         string `json:"id"`
	Host       string `json:"host"`
	As         string `json:"as"`
	Run        string `json:"run"`
	WayBack    string `json:"way_back"`
	SHA256     string `json:"sha256"`
	ApprovedAt int64  `json:"approved_at"`
	Sig        string `json:"sig"`
}

// Step is the request's step.
func (r HandsRequest) Step() HandsStep {
	return HandsStep{ID: r.ID, Host: r.Host, As: r.As, Run: r.Run, WayBack: r.WayBack}
}

// ApprovalID names the approval a request carries, for a record that it has
// run: its step's hash and when it was approved, which together are all it
// signs.
func (r HandsRequest) ApprovalID() string {
	return HandsApprovalID(r.SHA256, r.ApprovedAt)
}

// HandsApprovalID names an approval by the step hash and time it signs.
func HandsApprovalID(sha256Hex string, approvedAt int64) string {
	return fmt.Sprintf("%s.%d", strings.ToLower(sha256Hex), approvedAt)
}
