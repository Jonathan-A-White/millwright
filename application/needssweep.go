package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// The defaults of the [needs] table and the names of what a sweep says.
const (
	// DefaultNeedsMinAge is how long a landing must have been closed before the
	// sweep writes its VERIFIED comment (config [needs] min_age).
	DefaultNeedsMinAge = 24 * time.Hour
	// NeedsStaleLiveAge is how long his line's landing must have been closed for
	// --stale-live to write on it (PROVISIONAL, the Governor to confirm).
	NeedsStaleLiveAge = 14 * 24 * time.Hour
	// NeedsJobName signs the mail of a run: needs@<host>.
	NeedsJobName = "needs"
)

// The classes of an unverified landing, as the sweep tells them apart.
const (
	SweepInternal    = "internal"
	SweepStaleLive   = "stale-live"
	SweepReverted    = "reverted"
	SweepFollowUp    = "follow-up-open"
	SweepHisWords    = "his-words"
	SweepNoDeploy    = "no-deploy"
	SweepNotDeployed = "not-deployed"
	SweepFresh       = "fresh"
	SweepHisLine     = "his-line"
	SweepUnproven    = "unproven"
	SweepNoCheck     = "no-check"
)

// NeedsSettings are the [needs] table's keys; a zero field reads its default.
type NeedsSettings struct {
	MinAge time.Duration
}

// SweepRef is a rig's main as a checkout names it: the ref and the commit it
// is at.
type SweepRef struct {
	Name, Sha string
}

// SweepGit is what the sweep asks of a rig's checkout. The real adapter is
// infrastructure/rig's Worktrees, the tests use a fake.
type SweepGit interface {
	CommitHistory
	// MainRefs are the refs of the rig's main that exist in the checkout,
	// origin's first.
	MainRefs(ctx context.Context, dir string) ([]SweepRef, error)
	// RevertOf is the commit on ref that reverts commit and is not itself
	// undone: no later commit reverts it and no later merge of the branch
	// mw/<bead> follows it. Empty when there is none.
	RevertOf(ctx context.Context, dir, ref, commit, bead string) (string, error)
}

// NeedsSweepBeads is what the sweep reads and writes in the tracker.
type NeedsSweepBeads interface {
	TriageSource
	StoriesComments(ctx context.Context, ids []string) (map[string][]Comment, error)
	CommentOnStory(ctx context.Context, id, text string) error
}

// NeedsSweep is mw needs sweep (mw-27f7myz.7): the check the Needs you cleanup
// made by script, as a command. It reads the landings the view makes a verify
// need of, tells each apart by what can be shown of it, and with Write comments
// VERIFIED on the internal ones the factory can see in main. It never writes on
// a landing it cannot show: one reverted, one he has spoken on since, one an open
// bug names, or one too young. A reverted landing gets one line in one mail.
// One tracker write at a time; a failed one stops the run.
type NeedsSweep struct {
	Beads NeedsSweepBeads
	Git   SweepGit
	// Marks holds this host's deployed marks; nil holds none.
	Marks BuiltMarks
	// Rules reads a rig's site_url; Web asks it. Both are used by --stale-live only.
	Rules EpicRules
	Web   VerifyWeb
	// Mailbox takes the one mail of a run; nil sends none.
	Mailbox Mailbox
	// Dirs is each rig's checkout on this host.
	Dirs     map[string]string
	Settings NeedsSettings
	// RunDir is where a --write run is recorded, <run>.json.
	RunDir string
	Host   string
	Now    func() time.Time
	Out    io.Writer
}

// NeedsSweepOptions are the flags of mw needs sweep.
type NeedsSweepOptions struct {
	Write     bool
	StaleLive bool
}

// NeedsSweepLine is one unverified landing and what the sweep did or would do.
type NeedsSweepLine struct {
	Bead   string `json:"bead"`
	Title  string `json:"title"`
	Rig    string `json:"rig"`
	Class  string `json:"class"`
	Commit string `json:"commit,omitempty"`
	// Revert is the commit that reverted it, for the reverted class.
	Revert string `json:"revert,omitempty"`
	// Action is what the line says it writes: the comment, the mail line or nothing.
	Action string `json:"action"`
	Wrote  bool   `json:"wrote,omitempty"`
	text   string
}

// NeedsSweepResult is what a run found.
type NeedsSweepResult struct {
	Run   string
	Lines []NeedsSweepLine
	Wrote int
}

func (s NeedsSweep) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC() //mw:realclock the default of the injected Now, used where none is set
}

func (s NeedsSweep) say(format string, args ...any) {
	if s.Out != nil {
		fmt.Fprintf(s.Out, format, args...)
	}
}

func (s NeedsSweep) minAge() time.Duration {
	if s.Settings.MinAge > 0 {
		return s.Settings.MinAge
	}
	return DefaultNeedsMinAge
}

// needsSweepRecord is a --write run's record, <run>.json.
type needsSweepRecord struct {
	Run       string           `json:"run"`
	At        string           `json:"at"`
	Host      string           `json:"host"`
	Write     bool             `json:"write"`
	StaleLive bool             `json:"stale_live"`
	MinAge    string           `json:"min_age"`
	Outcome   string           `json:"outcome"`
	Lines     []NeedsSweepLine `json:"lines"`
	// Mailed are the reverted landings this run named to the Mayor.
	Mailed []string `json:"mailed,omitempty"`
}

// sweepFacts is what one run reads once and every landing is judged by.
type sweepFacts struct {
	opts     NeedsSweepOptions
	now      time.Time
	run      string
	byID     map[string]TriageBead
	bugs     []TriageBead
	mains    map[string][]SweepRef
	sites    map[string]string
	comments map[string][]Comment
}

// Run makes one run. Without Write it prints what it would do and writes
// nothing: no comment, no mail, no record.
func (s NeedsSweep) Run(ctx context.Context, opts NeedsSweepOptions) (NeedsSweepResult, error) {
	now := s.now()
	result := NeedsSweepResult{Run: now.Format("20060102T150405Z")}
	all, err := s.Beads.TriageBeads(ctx)
	if err != nil {
		return result, fmt.Errorf("mw needs sweep: reading the beads: %w", err)
	}
	facts := &sweepFacts{
		opts: opts, now: now, run: result.Run, byID: map[string]TriageBead{},
		mains: map[string][]SweepRef{}, sites: map[string]string{},
	}
	for _, b := range all {
		facts.byID[b.ID] = b
		if strings.EqualFold(b.Type, "bug") && !strings.EqualFold(b.Status, StatusClosed) {
			facts.bugs = append(facts.bugs, b)
		}
	}
	var landings []TriageBead
	var ids []string
	for _, b := range all {
		if sweepLanding(b, now) {
			landings = append(landings, b)
			ids = append(ids, b.ID)
		}
	}
	sort.Slice(landings, func(a, b int) bool {
		if !landings[a].ClosedAt.Equal(landings[b].ClosedAt) {
			return landings[a].ClosedAt.Before(landings[b].ClosedAt)
		}
		return landings[a].ID < landings[b].ID
	})
	if facts.comments, err = s.Beads.StoriesComments(ctx, ids); err != nil {
		return result, fmt.Errorf("mw needs sweep: reading the comments of %d landings: %w", len(ids), err)
	}

	for _, b := range landings {
		comments := facts.comments[b.ID]
		if verdict := newestVerdict(comments); verdict.Verified || verdict.NotMet {
			continue
		}
		result.Lines = append(result.Lines, s.classify(ctx, facts, b, comments))
	}

	mode := "a dry run: nothing written"
	if opts.Write {
		mode = "writing"
	}
	s.say("mw needs sweep (%s): %d unverified landings\n", mode, len(result.Lines))
	for _, l := range result.Lines {
		s.say("%-16s %-14s %s\n", l.Bead, l.Class, l.Action)
	}
	if !opts.Write {
		return result, nil
	}
	return s.write(ctx, facts, result)
}

// write comments on each line that has a text, one tracker call at a time, then
// mails the Mayor the reverted landings not named before, and keeps the record.
func (s NeedsSweep) write(ctx context.Context, f *sweepFacts, result NeedsSweepResult) (NeedsSweepResult, error) {
	rec := needsSweepRecord{
		Run: result.Run, At: f.now.Format(time.RFC3339), Host: s.Host, Write: true, StaleLive: f.opts.StaleLive,
		MinAge: s.minAge().String(), Outcome: "done",
	}
	var stopped error
	for i := range result.Lines {
		l := &result.Lines[i]
		if l.text == "" {
			continue
		}
		if err := s.Beads.CommentOnStory(ctx, l.Bead, l.text); err != nil {
			stopped = fmt.Errorf("mw needs sweep: commenting on %s failed, stopped (%d written): %w", l.Bead, result.Wrote, err)
			rec.Outcome = "stopped at " + l.Bead + ": " + err.Error()
			break
		}
		l.Wrote = true
		l.Action = "commented VERIFIED (" + l.Class + ")"
		result.Wrote++
		s.say("commented on %s\n", l.Bead)
	}
	var mailErr error
	if stopped == nil {
		var fresh []NeedsSweepLine
		told := s.mailedBefore()
		for _, l := range result.Lines {
			if l.Class == SweepReverted && !told[l.Bead] {
				fresh = append(fresh, l)
			}
		}
		if len(fresh) > 0 && s.Mailbox != nil {
			if mailErr = s.mailReverted(ctx, fresh); mailErr == nil {
				for _, l := range fresh {
					rec.Mailed = append(rec.Mailed, l.Bead)
				}
			} else {
				rec.Outcome = "done; the mail was not sent"
			}
		}
	}
	rec.Lines = result.Lines
	keepErr := s.keep(rec)
	s.say("mw needs sweep: %d commented, run %s\n", result.Wrote, result.Run)
	return result, errors.Join(stopped, mailErr, keepErr)
}

// sweepLanding is whether b is a landing the view could make a verify need of:
// closed as landed (run:landed) within PosternViewVerifyWindow, not a hands
// bead, not a formula step, not superseded or never released.
func sweepLanding(b TriageBead, now time.Time) bool {
	if !strings.EqualFold(b.Status, StatusClosed) || b.ClosedAt.IsZero() || now.Sub(b.ClosedAt) >= PosternViewVerifyWindow {
		return false
	}
	if !hasLanded(StoryDetail{Labels: b.Labels}) || hasHitlLabel(b.Labels) {
		return false
	}
	return ClosedStory{ID: b.ID, Title: b.Title, Type: b.Type, CloseReason: b.CloseReason}.landed()
}

// sweepHisWords is whether a comment is his own words on the bead: a postern
// message of his or an ANSWER to its card.
func sweepHisWords(c Comment) bool {
	text := strings.TrimSpace(c.Text)
	return strings.HasPrefix(text, "The Governor ") || strings.HasPrefix(text, "ANSWER ")
}

// internalLanding is whether the landing's closing step said it gives the
// Governor nothing to look at.
func internalLanding(comments []Comment) (internal, hasCheck bool) {
	if howTo := howToCheck(comments); howTo != "" {
		return howToIsInternal(howTo), true
	}
	for _, c := range comments {
		if strings.Contains(c.Text, "Internal: nothing for the Governor to look at") {
			return true, true
		}
	}
	return false, false
}

func (f *sweepFacts) rigOf(b TriageBead) string {
	for seen := 0; seen < 8; seen++ {
		if b.Rig != "" {
			return b.Rig
		}
		parent, ok := f.byID[b.Parent]
		if !ok || b.Parent == "" {
			return ""
		}
		b = parent
	}
	return ""
}

// classify tells one unverified landing apart and, when it is one the sweep may
// write on, fills in the text of its comment.
func (s NeedsSweep) classify(ctx context.Context, f *sweepFacts, b TriageBead, comments []Comment) NeedsSweepLine {
	rig := f.rigOf(b)
	line := NeedsSweepLine{Bead: b.ID, Title: b.Title, Rig: rig}
	skip := func(class, why string) NeedsSweepLine {
		line.Class, line.Action = class, "nothing: "+why
		return line
	}
	commit := landingCommit(b.CloseReason)
	dir := s.Dirs[rig]
	switch {
	case commit == "":
		return skip(SweepUnproven, "the close reason names no commit")
	case dir == "":
		return skip(SweepUnproven, "this host names no checkout of "+rig)
	}
	line.Commit = commit

	refs, ok := f.mains[dir]
	if !ok {
		var err error
		if refs, err = s.Git.MainRefs(ctx, dir); err != nil {
			return skip(SweepUnproven, "main of "+rig+" could not be read: "+err.Error())
		}
		f.mains[dir] = refs
	}
	var main SweepRef
	var contained bool
	var askErr error
	for _, ref := range refs {
		in, err := s.Git.Contains(ctx, dir, ref.Sha, commit)
		if err != nil {
			askErr = err
			continue
		}
		if in {
			main, contained = ref, true
			break
		}
	}
	switch {
	case !contained && askErr != nil:
		return skip(SweepUnproven, fmt.Sprintf("commit %s could not be found in %s main: %v", shortSha(commit), rig, askErr))
	case !contained:
		return skip(SweepUnproven, fmt.Sprintf("commit %s is not in %s main", shortSha(commit), rig))
	}

	revert, err := s.Git.RevertOf(ctx, dir, main.Name, commit, b.ID)
	if err != nil {
		return skip(SweepUnproven, "the history of "+rig+" could not be read for a revert: "+err.Error())
	}
	if revert != "" {
		line.Class, line.Revert = SweepReverted, revert
		line.Action = fmt.Sprintf("no comment; mail the Mayor: reverted by %s, no later merge of the branch", shortSha(revert))
		return line
	}

	for _, c := range comments {
		if c.Created.After(b.ClosedAt) && sweepHisWords(c) {
			return skip(SweepHisWords, "his words are on the bead after it closed")
		}
	}
	for _, bug := range f.bugs {
		if namesBead(bug.Title, b.ID) || namesBead(bug.Description, b.ID) || containsString(bug.Needs, b.ID) {
			return skip(SweepFollowUp, "the open bug "+bug.ID+" names it")
		}
	}
	age := f.now.Sub(b.ClosedAt)
	if age < s.minAge() {
		return skip(SweepFresh, fmt.Sprintf("closed %s ago, under min_age %s", sweepAge(age), s.minAge()))
	}
	internal, hasCheck := internalLanding(comments)
	if !hasCheck {
		return skip(SweepNoCheck, "its closing step gives no HOW TO CHECK IT and no Internal line")
	}

	deployed := ""
	if s.Marks != nil {
		deployed, _ = s.Marks.Built(ctx, deployedMark(rig))
		deployed = strings.TrimSpace(deployed)
	}
	if internal {
		if deployed != "" {
			if in, err := s.Git.Contains(ctx, dir, deployed, commit); err != nil || !in {
				return skip(SweepNotDeployed, fmt.Sprintf("commit %s is not in the deployed commit %s", shortSha(commit), shortSha(deployed)))
			}
		}
		line.Class = SweepInternal
		line.Action = fmt.Sprintf("would comment VERIFIED (internal; commit %s in %s)", shortSha(commit), main.Name)
		line.text = s.verifiedText(f, b, rig, commit, main, deployed, age, "Internal: nothing for the Governor to look at.")
		return line
	}

	// His line: a landing he was to see.
	if deployed == "" {
		return skip(SweepNoDeploy, "no deploy is seen for "+rig+" on this host; his walk stays his")
	}
	if in, err := s.Git.Contains(ctx, dir, deployed, commit); err != nil || !in {
		return skip(SweepNotDeployed, fmt.Sprintf("commit %s is not in the deployed commit %s", shortSha(commit), shortSha(deployed)))
	}
	if !f.opts.StaleLive || age < NeedsStaleLiveAge {
		why := "his line; his own walk stays open to him"
		if !f.opts.StaleLive && age >= NeedsStaleLiveAge {
			why += " (--stale-live would read the live page)"
		}
		return skip(SweepHisLine, why)
	}
	site, live := s.livePageStates(ctx, f, rig, deployed)
	if !live {
		return skip(SweepHisLine, "his line; the live page of "+rig+" was not seen to state the deployed build")
	}
	line.Class = SweepStaleLive
	line.Action = fmt.Sprintf("would comment VERIFIED (stale-live; %s states build %s)", site, shortSha(deployed))
	tail := fmt.Sprintf("The page %s states the deployed build %s. His own walk stays open to him.", site, deployed[:min(7, len(deployed))])
	line.text = s.verifiedText(f, b, rig, commit, main, deployed, age, tail)
	return line
}

// livePageStates reads the rig's site_url page once per run and says whether it
// states the first seven characters of the deployed commit.
func (s NeedsSweep) livePageStates(ctx context.Context, f *sweepFacts, rig, deployed string) (site string, states bool) {
	if s.Rules == nil || s.Web == nil || len(deployed) < 7 {
		return "", false
	}
	site, known := f.sites[rig]
	if !known {
		requirements, err := s.Rules.EpicRequirements(ctx, rig)
		if err != nil {
			return "", false
		}
		site = strings.TrimSpace(requirements.SiteURL)
		f.sites[rig] = site
	}
	if site == "" {
		return "", false
	}
	page := readLivePage(ctx, s.Web, rig, site)
	if !page.Read() {
		return site, false
	}
	short := deployed[:7]
	return site, strings.Contains(page.Page, short) || strings.Contains(page.Bundle, short)
}

// verifiedText is the VERIFIED comment: its first word is VERIFIED, it says it is
// not his use, and names the commit, where it was seen and when.
func (s NeedsSweep) verifiedText(f *sweepFacts, b TriageBead, rig, commit string, main SweepRef, deployed string, age time.Duration, tail string) string {
	seen := fmt.Sprintf("commit %s is in %s main (%s at %s)", shortSha(commit), rig, main.Name, shortSha(main.Sha))
	if deployed != "" {
		seen += fmt.Sprintf(" and in the deployed commit %s", shortSha(deployed))
	}
	return fmt.Sprintf("%s by the factory's own check, not by his use (mw needs sweep, run %s). Checked %s: %s; closed %s ago; no message of the Governor's on this bead since it closed and no open bug names it. %s",
		VerifiedWord, f.run, f.now.Format(verifyTimeFormat), seen, sweepAge(age), tail)
}

func shortSha(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}

func sweepAge(d time.Duration) string {
	if d >= 48*time.Hour {
		return fmt.Sprintf("%d days", int(d.Hours()/24))
	}
	return fmt.Sprintf("%d hours", int(d.Hours()))
}

// mailedBefore is the reverted landings an earlier run's record says it mailed.
func (s NeedsSweep) mailedBefore() map[string]bool {
	told := map[string]bool{}
	if s.RunDir == "" {
		return told
	}
	entries, _ := os.ReadDir(s.RunDir)
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(s.RunDir, e.Name()))
		if err != nil {
			continue
		}
		var rec needsSweepRecord
		if json.Unmarshal(raw, &rec) == nil {
			for _, id := range rec.Mailed {
				told[id] = true
			}
		}
	}
	return told
}

// mailReverted sends the Mayor the one mail of the run: a line for each landing
// closed as landed whose merge was reverted.
func (s NeedsSweep) mailReverted(ctx context.Context, lines []NeedsSweepLine) error {
	var body strings.Builder
	for _, l := range lines {
		fmt.Fprintf(&body, "%s  %s\n  landed at %s in %s, reverted by %s, and no later merge of its branch is in main. It is closed as landed and is not in main.\n\n",
			l.Bead, l.Title, shortSha(l.Commit), l.Rig, l.Revert)
	}
	body.WriteString("The sweep wrote no comment on these. Whether to land them again is yours.")
	_, err := s.Mailbox.Send(ctx, NewMessage{
		From: NeedsJobName + "@" + s.Host, To: MayorMailbox,
		Subject: fmt.Sprintf("Reverted: %d %s closed as landed", len(lines), plural(len(lines), "landing", "landings")),
		Body:    body.String(),
	})
	if err != nil {
		return fmt.Errorf("mailing the reverted landings: %w", err)
	}
	return nil
}

// keep writes the run's record, <run>.json in RunDir; a run with no RunDir keeps none.
func (s NeedsSweep) keep(rec needsSweepRecord) error {
	if s.RunDir == "" {
		return nil
	}
	body, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.RunDir, 0o755); err != nil {
		return fmt.Errorf("the run's record was not written: %w", err)
	}
	if err := os.WriteFile(filepath.Join(s.RunDir, rec.Run+".json"), append(body, '\n'), 0o644); err != nil {
		return fmt.Errorf("the run's record was not written: %w", err)
	}
	return nil
}
