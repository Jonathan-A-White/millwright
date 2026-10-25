package application

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/domain"
)

// The defaults of the [verify] table, and the names of what a proposal says.
const (
	// DefaultVerifyModel and DefaultVerifyEffort are what the matcher runs on:
	// the cheap model that reads a picture (mw-4xdnis, PROVISIONAL).
	DefaultVerifyModel  = "haiku"
	DefaultVerifyEffort = "medium"
	// DefaultVerifyConfidence is the least confidence at which a match becomes
	// a proposal (PROVISIONAL, the Governor to confirm).
	DefaultVerifyConfidence = 0.8
	// DefaultVerifySince is how far back mw verify match looks for landings
	// and for his messages and pictures.
	DefaultVerifySince = 7 * 24 * time.Hour

	// VerifyProposedWord is the first word of a proposal's comment. It is never
	// VerifiedWord: that first word is the record itself (mw-tcmhmh.1).
	VerifyProposedWord = "PROPOSED"
	// VerifiedWord is the first word of the comment that records a landing as
	// verified by the Governor's use.
	VerifiedWord = "VERIFIED"

	// VerifyKindDemo and VerifyKindLanding say what a candidate is.
	VerifyKindDemo    = "demo"
	VerifyKindLanding = "landing"

	// VerifyJobName signs the mail of a run: verify@<host>.
	VerifyJobName = "verify"
)

// verifyEpicLookback is how far back a closed story of a demo's epic is read
// for its HOW TO CHECK IT, whatever --since says: a demo outlives its landings.
const verifyEpicLookback = 30 * 24 * time.Hour

// howToCheckHeading is the heading of the section of a closing comment that
// tells the Governor how to see a change.
const howToCheckHeading = "HOW TO CHECK IT"

// VerifySettings are the [verify] table's keys; a zero field reads its default.
type VerifySettings struct {
	Model      string
	Effort     string
	Confidence float64
}

func (s VerifySettings) orDefaults() VerifySettings {
	if strings.TrimSpace(s.Model) == "" {
		s.Model = DefaultVerifyModel
	}
	if strings.TrimSpace(s.Effort) == "" {
		s.Effort = DefaultVerifyEffort
	}
	if s.Confidence <= 0 || s.Confidence > 1 {
		s.Confidence = DefaultVerifyConfidence
	}
	return s
}

// ClosedStory is a bead the tracker holds closed, with what the unverified
// landings are told apart by.
type ClosedStory struct {
	ID    string
	Title string
	// Type is the tracker's word for the kind of bead: bug, task, feature.
	Type   string
	EpicID string
	// CloseReason is why the bead was closed, as the tracker has it.
	CloseReason string
	ClosedAt    time.Time
	// Superseded is true when the bead carries a supersedes link: another
	// story landed its work, so nothing landed under this one.
	Superseded bool
}

// VerifyBeads is what mw verify match reads and writes in the tracker. The real
// adapter is infrastructure/beads' Gateway, the fake apptest.FakeVerifyBeads.
type VerifyBeads interface {
	// WorkInHand lists the stories ready and running, the open demos among them.
	WorkInHand(ctx context.Context) (WorkInHand, error)
	// ClosedStories lists the beads closed at since or later, epics left out.
	ClosedStories(ctx context.Context, since time.Time) ([]ClosedStory, error)
	// StoriesComments is the comments of each of the beads, oldest first.
	StoriesComments(ctx context.Context, ids []string) (map[string][]Comment, error)
	// CommentOnStory appends one comment to a bead.
	CommentOnStory(ctx context.Context, id, text string) error
}

// VerifyCandidate is one bead a message or a picture of his may show met: an
// open demo, or a landing nobody has recorded as verified.
type VerifyCandidate struct {
	Bead  string
	Kind  string
	Title string
	// Check is the text that says how to see it working.
	Check string
	// LandedAt is when the change landed: a landing's close time, a demo's the
	// newest close among the stories of its epic. Evidence written before it
	// cannot show the change working. Zero when nothing is known to have landed.
	LandedAt time.Time
}

// firstWord is the first word of a comment's text.
func firstWord(text string) string {
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

// verifiedIn reports whether a comment records the bead as verified: one whose
// first word is VERIFIED.
func verifiedIn(comments []Comment) bool {
	for _, c := range comments {
		if firstWord(c.Text) == VerifiedWord {
			return true
		}
	}
	return false
}

// proposedFor reports whether a comment already proposes the bead verified from
// txid: one whose first word is PROPOSED and that names the txid.
func proposedFor(comments []Comment, txid string) bool {
	for _, c := range comments {
		if firstWord(c.Text) == VerifyProposedWord && strings.Contains(c.Text, txid) {
			return true
		}
	}
	return false
}

// landed is whether a closed bead is a landing that can be verified: a bug,
// task or feature that is not a formula step, was not superseded and was not
// closed as never released.
func (c ClosedStory) landed() bool {
	switch c.Type {
	case "bug", "task", "feature":
	default:
		return false
	}
	if strings.HasPrefix(c.ID, "mw-mol") || strings.Contains(c.Title, "State change") {
		return false
	}
	reason := strings.ToLower(strings.TrimSpace(c.CloseReason))
	return !c.Superseded && !strings.Contains(reason, "never released") && !strings.HasPrefix(reason, "superseded")
}

// howToCheckIt is the HOW TO CHECK IT section of the newest comment that has
// one: from its heading to the 'What's new:' or 'For the rig memory:' line, or
// the end. "" when no comment has it.
func howToCheckIt(comments []Comment) string {
	for i := len(comments) - 1; i >= 0; i-- {
		lines := strings.Split(comments[i].Text, "\n")
		start := -1
		for n, line := range lines {
			if strings.Contains(strings.ToUpper(line), howToCheckHeading) {
				start = n
				break
			}
		}
		if start < 0 {
			continue
		}
		end := len(lines)
		for n := start + 1; n < len(lines); n++ {
			line := strings.TrimSpace(lines[n])
			if strings.HasPrefix(line, "What's new:") || strings.HasPrefix(line, "For the rig memory:") {
				end = n
				break
			}
		}
		return strings.TrimSpace(strings.Join(lines[start:end], "\n"))
	}
	return ""
}

// VerifyCandidates are the beads the Governor's use may show met: every open
// demo (QueuedDemos, the demos mw stuck --dampers counts) with its description
// and the HOW TO CHECK IT of its epic's closed stories, and every unverified
// landing closed within since with its own HOW TO CHECK IT. A bead with a
// VERIFIED-first comment is none. Demos come first, by id, then landings by
// close time.
func VerifyCandidates(ctx context.Context, beads VerifyBeads, now time.Time, since time.Duration) ([]VerifyCandidate, error) {
	work, err := beads.WorkInHand(ctx)
	if err != nil {
		return nil, fmt.Errorf("reading the open demos: %w", err)
	}
	demos := QueuedDemos(work)
	look := max(since, verifyEpicLookback)
	if len(demos) == 0 {
		look = since
	}
	closed, err := beads.ClosedStories(ctx, now.Add(-look))
	if err != nil {
		return nil, fmt.Errorf("reading the landed stories: %w", err)
	}
	var landed []ClosedStory
	var ids []string
	for _, c := range closed {
		if c.landed() {
			landed = append(landed, c)
			ids = append(ids, c.ID)
		}
	}
	for _, d := range demos {
		ids = append(ids, d.Story.ID)
	}
	comments, err := beads.StoriesComments(ctx, uniqueStrings(ids))
	if err != nil {
		return nil, fmt.Errorf("reading the comments of the candidates: %w", err)
	}

	var out []VerifyCandidate
	sort.Slice(demos, func(a, b int) bool { return demos[a].Story.ID < demos[b].Story.ID })
	for _, d := range demos {
		if verifiedIn(comments[d.Story.ID]) {
			continue
		}
		var check strings.Builder
		check.WriteString("The demo's description:\n" + strings.TrimSpace(d.Description) + "\n")
		var landedAt time.Time
		for _, c := range landed {
			if d.EpicID == "" || c.EpicID != d.EpicID || c.ID == d.Story.ID {
				continue
			}
			if c.ClosedAt.After(landedAt) {
				landedAt = c.ClosedAt
			}
			if section := howToCheckIt(comments[c.ID]); section != "" {
				check.WriteString(fmt.Sprintf("\nLanded under the same epic, %s (%s):\n%s\n", c.ID, c.Title, section))
			}
		}
		out = append(out, VerifyCandidate{Bead: d.Story.ID, Kind: VerifyKindDemo, Title: d.Story.Title, Check: check.String(), LandedAt: landedAt})
	}
	sort.SliceStable(landed, func(a, b int) bool { return landed[a].ClosedAt.Before(landed[b].ClosedAt) })
	have := map[string]bool{}
	for _, c := range out {
		have[c.Bead] = true
	}
	for _, c := range landed {
		if have[c.ID] || c.ClosedAt.Before(now.Add(-since)) || verifiedIn(comments[c.ID]) {
			continue
		}
		if section := howToCheckIt(comments[c.ID]); section != "" {
			out = append(out, VerifyCandidate{Bead: c.ID, Kind: VerifyKindLanding, Title: c.Title, Check: section + "\n", LandedAt: c.ClosedAt})
		}
	}
	return out, nil
}

// VerifyEvidence is one message of his, or one picture, or both: the files the
// inbox pass saved under one txid.
type VerifyEvidence struct {
	// Txid is the message's txid, as the record names it: direct:<hash>.
	Txid string
	// At is when the files were written, the newest of them.
	At time.Time
	// Files are the full paths: the .txt of the message and its pictures.
	Files []string
}

// KeptWords is the text of his message txid as the inbox kept it whole
// (<txid>.message.json, PosternKeptFile) in dir; false when none is kept.
func KeptWords(dir, txid string) (string, bool) {
	raw, err := os.ReadFile(PosternKeptFile(dir, txid))
	if err != nil {
		return "", false
	}
	var kept keptMessage
	if json.Unmarshal(raw, &kept) != nil {
		return "", false
	}
	return kept.Text, true
}

// evidenceWords is the words he sent with a piece of evidence that holds a
// picture and no .txt of its own: the text the inbox kept for its txid. A picture
// read without them reads as a screenshot of the thing working, though they may
// ask for it (mw-gq6.742). ok is false when the evidence holds no picture, has a
// .txt of its own, or no words are kept.
func evidenceWords(e VerifyEvidence) (text string, ok bool) {
	picture := false
	for _, path := range e.Files {
		if strings.EqualFold(filepath.Ext(path), ".txt") {
			return "", false
		}
		picture = true
	}
	if !picture {
		return "", false
	}
	text, ok = KeptWords(filepath.Dir(e.Files[0]), e.Txid)
	return strings.TrimSpace(text), ok && strings.TrimSpace(text) != ""
}

// verifyEvidenceExts are the files of the inbox that are evidence: his text and
// his pictures. A voice note, a record's JSON and a transcript are left out.
var verifyEvidenceExts = map[string]bool{".txt": true, ".jpg": true, ".jpeg": true, ".png": true, ".webp": true}

// txidOfStem is the txid a saved file's name stands for: posternFileName made
// the colon of "direct:<hash>" a dash.
func txidOfStem(stem string) string {
	if rest, ok := strings.CutPrefix(stem, "direct-"); ok {
		return "direct:" + rest
	}
	return stem
}

// ReadVerifyEvidence reads the saved messages and pictures of the inbox dir:
// those written at since or later, or, when txids are given, those and no
// others. Newest last. A txid that names no file is an error.
func ReadVerifyEvidence(dir string, since time.Time, txids []string) ([]VerifyEvidence, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			entries = nil
		} else {
			return nil, fmt.Errorf("reading the inbox %s: %w", dir, err)
		}
	}
	wanted := map[string]bool{}
	for _, txid := range txids {
		wanted[txidOfStem(posternFileName(strings.TrimSpace(txid)))] = true
	}
	found := map[string]*VerifyEvidence{}
	for _, e := range entries {
		ext := strings.ToLower(filepath.Ext(e.Name()))
		if e.IsDir() || !verifyEvidenceExts[ext] {
			continue
		}
		txid := txidOfStem(strings.TrimSuffix(e.Name(), filepath.Ext(e.Name())))
		if len(wanted) > 0 && !wanted[txid] {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if len(wanted) == 0 && info.ModTime().Before(since) {
			continue
		}
		one := found[txid]
		if one == nil {
			one = &VerifyEvidence{Txid: txid}
			found[txid] = one
		}
		one.Files = append(one.Files, filepath.Join(dir, e.Name()))
		if info.ModTime().After(one.At) {
			one.At = info.ModTime().UTC()
		}
	}
	var out []VerifyEvidence
	for _, one := range found {
		sort.Strings(one.Files)
		out = append(out, *one)
	}
	sort.Slice(out, func(a, b int) bool {
		if !out[a].At.Equal(out[b].At) {
			return out[a].At.Before(out[b].At)
		}
		return out[a].Txid < out[b].Txid
	})
	for txid := range wanted {
		hit := false
		for _, one := range out {
			hit = hit || one.Txid == txid
		}
		if !hit {
			return out, fmt.Errorf("no message or picture of %s is saved in %s", txid, dir)
		}
	}
	return out, nil
}

// VerifyStringsHeading opens a block of the strings mw verify <id> looks for in
// the live bundle: in a bead's description or a comment, the line
// "Verify strings:" and, below it, one string per line up to the next blank
// line. A string may also follow the heading on its own line.
const VerifyStringsHeading = "Verify strings:"

// VerifyDeployBeads is what mw verify <id> reads of the tracker.
type VerifyDeployBeads interface {
	ShowBeads(ctx context.Context, ids []string) ([]StoryDetail, error)
	StoriesComments(ctx context.Context, ids []string) (map[string][]Comment, error)
}

// VerifyWeb asks a URL over HTTP. The real adapter is in cmd/mw; the tests ask a
// fake server through it.
type VerifyWeb interface {
	// Get is the status and the body of a GET of url. A reply of any status is
	// no error: only a request that got no reply is.
	Get(ctx context.Context, url string) (status int, body []byte, err error)
}

// VerifyDeploy is mw verify <id> (mw-gq6.760): the half of verifying a landing
// that needs no judgment, at no cost in tokens. It says whether the host's
// deployed mark of the story's rig is the commit the bead says it landed at,
// whether the rig's healthz answers, and whether the live bundle holds each
// string the bead names. It writes nothing on the bead.
type VerifyDeploy struct {
	Beads VerifyDeployBeads
	// Rules reads the rig's file in the vault, for its site_url and healthz_url.
	Rules EpicRules
	// Marks holds the host's deployed and deploy-failed marks; nil holds none.
	Marks BuiltMarks
	Web   VerifyWeb
	Out   io.Writer
}

// VerifyDeployResult is how the three checks came out.
type VerifyDeployResult struct {
	Deploy  VerifyCheckOutcome
	Healthz VerifyCheckOutcome
	// Named is how many strings the bead names, Found how many are in the bundle.
	Named, Found int
}

// VerifyCheckOutcome is one check's word in the summary line.
type VerifyCheckOutcome string

const (
	VerifyOK       VerifyCheckOutcome = "OK"
	VerifyMismatch VerifyCheckOutcome = "MISMATCH"
	VerifyFail     VerifyCheckOutcome = "FAIL"
	// VerifyNone is a check the rig has nothing to run for: no healthz key.
	VerifyNone VerifyCheckOutcome = "none"
)

// Failed is whether any check failed; a check with nothing to run did not.
func (r VerifyDeployResult) Failed() bool {
	return r.Deploy != VerifyOK || r.Healthz == VerifyFail || r.Found < r.Named
}

// Line is the one summary line.
func (r VerifyDeployResult) Line(id string) string {
	return fmt.Sprintf("verify %s: deploy %s, healthz %s, strings %d/%d found", id, r.Deploy, r.Healthz, r.Found, r.Named)
}

// verifyBodyLimit bounds what is printed of a healthz body.
const verifyBodyLimit = 200

// Run does the checks of one bead, saying each on Out. The error is for a run
// that could not be made (no such bead); a check that failed is in the result.
func (v VerifyDeploy) Run(ctx context.Context, id string) (VerifyDeployResult, error) {
	found, err := v.Beads.ShowBeads(ctx, []string{id})
	if err != nil {
		return VerifyDeployResult{}, fmt.Errorf("mw verify: reading %s: %w", id, err)
	}
	if len(found) != 1 {
		return VerifyDeployResult{}, fmt.Errorf("mw verify: the tracker has no bead %s", id)
	}
	bead := found[0]
	comments, err := v.Beads.StoriesComments(ctx, []string{id})
	if err != nil {
		return VerifyDeployResult{}, fmt.Errorf("mw verify: reading the comments of %s: %w", id, err)
	}
	rig := strings.TrimSpace(bead.Merged().Rig)
	if rig == "" {
		return VerifyDeployResult{}, fmt.Errorf("mw verify: %s names no rig in its path", id)
	}
	var requirements domain.EpicRequirements
	if v.Rules != nil {
		if requirements, err = v.Rules.EpicRequirements(ctx, rig); err != nil {
			return VerifyDeployResult{}, fmt.Errorf("mw verify: reading the file of rig %s: %w", rig, err)
		}
	}

	var result VerifyDeployResult
	result.Deploy = v.checkDeploy(ctx, rig, bead.CloseReason)
	result.Healthz = v.checkHealthz(ctx, rig, requirements.HealthzURL)
	texts := []string{bead.Description}
	for _, c := range comments[id] {
		texts = append(texts, c.Text)
	}
	result.Named, result.Found = v.checkStrings(ctx, rig, requirements.SiteURL, VerifyStrings(texts...))
	fmt.Fprintln(v.Out, result.Line(id))
	return result, nil
}

var (
	batchCommit = regexp.MustCompile(`batch commit ([0-9a-f]{7,40})`)
	anyCommit   = regexp.MustCompile(`\b[0-9a-f]{7,40}\b`)
)

// landingCommit is the commit the bead says its story landed at: the batch
// commit of a batch landing, else the commit a landing's close reason names.
func landingCommit(closeReason string) string {
	if m := batchCommit.FindStringSubmatch(closeReason); m != nil {
		return m[1]
	}
	if strings.HasPrefix(closeReason, "landed") {
		return anyCommit.FindString(closeReason)
	}
	return ""
}

func (v VerifyDeploy) checkDeploy(ctx context.Context, rig, closeReason string) VerifyCheckOutcome {
	landed := landingCommit(closeReason)
	var deployed, failed string
	if v.Marks != nil {
		deployed, _ = v.Marks.Built(ctx, deployedMark(rig))
		failed, _ = v.Marks.Built(ctx, deployFailedMark(rig))
	}
	deployed = strings.TrimSpace(deployed)
	failedAt, tries, _ := strings.Cut(strings.TrimSpace(failed), " ")
	if failed != "" {
		fmt.Fprintf(v.Out, "deploy-failed mark of %s: commit %s failed %s time(s)\n", rig, failedAt, tries)
	}
	switch {
	case landed == "":
		fmt.Fprintf(v.Out, "deploy MISMATCH: no landing commit on the bead (close reason: %q)\n", closeReason)
	case deployed == "":
		fmt.Fprintf(v.Out, "deploy MISMATCH: %s has no deployed mark on this host; the bead landed at %s\n", rig, landed)
	case !sameCommit(deployed, landed):
		fmt.Fprintf(v.Out, "deploy MISMATCH: %s is deployed at %s, the bead landed at %s\n", rig, deployed, landed)
	case failed != "" && sameCommit(failedAt, landed):
		fmt.Fprintf(v.Out, "deploy MISMATCH: %s is marked deployed at %s, yet a deploy-failed mark stands at that commit\n", rig, deployed)
	default:
		fmt.Fprintf(v.Out, "deploy OK: %s is deployed at %s, the commit the bead landed at (%s)\n", rig, deployed, landed)
		return VerifyOK
	}
	return VerifyMismatch
}

func (v VerifyDeploy) checkHealthz(ctx context.Context, rig, healthz string) VerifyCheckOutcome {
	if healthz == "" {
		fmt.Fprintf(v.Out, "no healthz for %s\n", rig)
		return VerifyNone
	}
	status, body, err := v.Web.Get(ctx, healthz)
	said := strings.TrimSpace(string(body))
	if len(said) > verifyBodyLimit {
		said = said[:verifyBodyLimit] + "..."
	}
	switch {
	case err != nil:
		fmt.Fprintf(v.Out, "healthz FAIL: %s: %v\n", healthz, err)
	case status < 200 || status > 299:
		fmt.Fprintf(v.Out, "healthz FAIL: %s answered %d: %s\n", healthz, status, said)
	default:
		fmt.Fprintf(v.Out, "healthz OK: %s answered %d: %s\n", healthz, status, said)
		return VerifyOK
	}
	return VerifyFail
}

// bundleRef finds the hashed bundle a rig's page loads: assets/index-<hash>.js.
var bundleRef = regexp.MustCompile(`["']([^"']*assets/index-[^"']+\.js)["']`)

// checkStrings looks for each of wanted in the bundles the rig's public page
// names. It returns how many strings were named and how many were found.
func (v VerifyDeploy) checkStrings(ctx context.Context, rig, site string, wanted []string) (named, found int) {
	if len(wanted) == 0 {
		fmt.Fprintln(v.Out, "no strings named on the bead")
		return 0, 0
	}
	named = len(wanted)
	bundle, why := v.liveBundle(ctx, rig, site)
	if why != "" {
		fmt.Fprintln(v.Out, why)
	}
	for _, want := range wanted {
		if inBundle(bundle, want) {
			found++
			fmt.Fprintf(v.Out, "found: %s\n", want)
		} else {
			fmt.Fprintf(v.Out, "not found: %s\n", want)
		}
	}
	return named, found
}

// liveBundle is the text of every bundle the rig's page names, or why there is
// none.
func (v VerifyDeploy) liveBundle(ctx context.Context, rig, site string) (string, string) {
	if site == "" {
		return "", fmt.Sprintf("no site_url for %s", rig)
	}
	status, page, err := v.Web.Get(ctx, site)
	switch {
	case err != nil:
		return "", fmt.Sprintf("the page %s could not be read: %v", site, err)
	case status < 200 || status > 299:
		return "", fmt.Sprintf("the page %s answered %d", site, status)
	}
	base, err := url.Parse(site)
	if err != nil {
		return "", fmt.Sprintf("site_url %q of %s is not a URL: %v", site, rig, err)
	}
	var text strings.Builder
	seen := map[string]bool{}
	for _, m := range bundleRef.FindAllStringSubmatch(string(page), -1) {
		ref, err := url.Parse(m[1])
		if err != nil || seen[m[1]] {
			continue
		}
		seen[m[1]] = true
		address := base.ResolveReference(ref).String()
		code, body, err := v.Web.Get(ctx, address)
		if err != nil || code < 200 || code > 299 {
			return text.String(), fmt.Sprintf("the bundle %s could not be read (status %d, %v)", address, code, err)
		}
		text.Write(body)
		text.WriteByte('\n')
	}
	if len(seen) == 0 {
		return "", fmt.Sprintf("no assets/index-*.js bundle named on %s", site)
	}
	return text.String(), ""
}

// inBundle is whether the bundle holds want as it stands, or as a JavaScript
// string literal escapes its quotes. The search is for a fixed string: nothing
// in want is a pattern, and no shell sees a $ in it.
func inBundle(bundle, want string) bool {
	for _, form := range []string{want, strings.ReplaceAll(want, "'", `\'`), strings.ReplaceAll(want, `"`, `\"`)} {
		if strings.Contains(bundle, form) {
			return true
		}
	}
	return false
}

// VerifyStrings reads the strings the texts name under VerifyStringsHeading, in
// order, each once. A bullet ("- " or "* ") and one pair of backticks or double
// quotes round a string are taken off it.
func VerifyStrings(texts ...string) []string {
	var wanted []string
	add := func(line string) {
		line = strings.TrimSpace(line)
		line = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(line, "- "), "* "))
		if len(line) >= 2 && (line[0] == '`' || line[0] == '"') && line[len(line)-1] == line[0] {
			line = line[1 : len(line)-1]
		}
		if line == "" {
			return
		}
		for _, have := range wanted {
			if have == line {
				return
			}
		}
		wanted = append(wanted, line)
	}
	for _, text := range texts {
		inBlock := false
		for _, line := range strings.Split(text, "\n") {
			trimmed := strings.TrimSpace(line)
			switch {
			case strings.HasPrefix(trimmed, VerifyStringsHeading):
				inBlock = true
				add(strings.TrimPrefix(trimmed, VerifyStringsHeading))
			case trimmed == "":
				inBlock = false
			case inBlock:
				add(trimmed)
			}
		}
	}
	return wanted
}
