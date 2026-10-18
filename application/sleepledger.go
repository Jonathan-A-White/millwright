package application

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// SleepLedgerDir and SleepLedgerFile name the improvements ledger: under
// plans/, one JSON line for each improvement the Mayor approved.
const (
	SleepLedgerDir  = "improvements"
	SleepLedgerFile = "ledger.jsonl"
)

// SleepOutcomeFile is the file under plans/sleep-<date>/ that keeps the score
// of that pass's calls.
const SleepOutcomeFile = "outcome.json"

// SleepOutcomeDays is how long a pass's calls are left before they are scored.
const SleepOutcomeDays = 14

// The verdicts a measured improvement is given.
const (
	VerdictKeep     = "keep"
	VerdictRollback = "rollback"
)

// SleepLedgerEntry is one improvement the Mayor approved, as the ledger keeps
// it: what was predicted, what it cost, how it is measured, and, once measured,
// the result and the verdict.
type SleepLedgerEntry struct {
	ID    string `json:"id"`
	Filed string `json:"filed,omitempty"`
	// Title through Why are the idea as mw loop rank reads it (looprank.go):
	// the Governor's criteria as fields. Bead is the story filed for the idea,
	// empty while none is.
	Title     string `json:"title,omitempty"`
	Bead      string `json:"bead,omitempty"`
	Expect    string `json:"expect,omitempty"`
	Quality   string `json:"quality,omitempty"`
	HisTime   string `json:"his_time,omitempty"`
	Fuel      string `json:"fuel,omitempty"`
	Speed     string `json:"speed,omitempty"`
	Compounds *bool  `json:"compounds,omitempty"`
	Size      string `json:"size,omitempty"`
	Why       *struct {
		Quality string `json:"quality,omitempty"`
		HisTime string `json:"his_time,omitempty"`
		Fuel    string `json:"fuel,omitempty"`
		Speed   string `json:"speed,omitempty"`
	} `json:"why,omitempty"`
	Predicted struct {
		Tokens  string `json:"tokens,omitempty"`
		Speed   string `json:"speed,omitempty"`
		Quality string `json:"quality,omitempty"`
	} `json:"predicted"`
	Cost struct {
		Size   string `json:"size,omitempty"`
		Tokens string `json:"tokens,omitempty"`
	} `json:"cost"`
	Measure struct {
		Metric   string `json:"metric,omitempty"`
		Baseline string `json:"baseline,omitempty"`
		Window   string `json:"window,omitempty"`
	} `json:"measure"`
	Landed     string `json:"landed,omitempty"`
	MeasuredAt string `json:"measured_at,omitempty"`
	Result     string `json:"result,omitempty"`
	// Verdict is keep or rollback once measured, and empty before.
	Verdict string `json:"verdict,omitempty"`
	Lesson  string `json:"lesson,omitempty"`
}

// ParseSleepLedger reads the ledger's lines. Blank lines are skipped; a line
// that is not an entry, has no id, or has a verdict other than keep or
// rollback is an error that names its line.
func ParseSleepLedger(text string) ([]SleepLedgerEntry, error) {
	var entries []SleepLedgerEntry
	for n, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var entry SleepLedgerEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			return nil, fmt.Errorf("%s line %d: %w", SleepLedgerFile, n+1, err)
		}
		if entry.ID == "" {
			return nil, fmt.Errorf("%s line %d: no id", SleepLedgerFile, n+1)
		}
		if v := entry.Verdict; v != "" && v != VerdictKeep && v != VerdictRollback {
			return nil, fmt.Errorf("%s line %d: the verdict %q is neither %s nor %s", SleepLedgerFile, n+1, v, VerdictKeep, VerdictRollback)
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

// String is the entry as one line: its id, dates, verdict (PENDING until it
// has one), result and lesson.
func (e SleepLedgerEntry) String() string {
	verdict := "PENDING"
	if e.Verdict != "" {
		verdict = strings.ToUpper(e.Verdict)
	}
	line := e.ID
	if e.Filed != "" {
		line += "  filed " + e.Filed
	}
	if e.Landed != "" {
		line += "  landed " + e.Landed
	}
	if e.MeasuredAt != "" {
		line += "  measured " + e.MeasuredAt
	}
	line += "  " + verdict
	if e.Measure.Metric != "" {
		line += "  " + e.Measure.Metric
		if e.Measure.Baseline != "" {
			line += " (baseline " + e.Measure.Baseline + ")"
		}
	}
	if e.Result != "" {
		line += ": " + e.Result
	}
	if e.Lesson != "" {
		line += " -- " + e.Lesson
	}
	return line
}

// SleepLedgerVault reads the ledger: plans/improvements/ledger.jsonl, empty
// when the vault has none.
type SleepLedgerVault interface {
	ReadImprovements(ctx context.Context) (string, error)
}

// SleepLedger is mw sleep ledger: it prints the improvements ledger, a line an
// improvement. It reads and writes nothing of the factory's.
type SleepLedger struct {
	Vault SleepLedgerVault
	Out   io.Writer
}

// Run prints the ledger.
func (l SleepLedger) Run(ctx context.Context) error {
	text, err := l.Vault.ReadImprovements(ctx)
	if err != nil {
		return fmt.Errorf("mw sleep ledger: %w", err)
	}
	entries, err := ParseSleepLedger(text)
	if err != nil {
		return fmt.Errorf("mw sleep ledger: %w", err)
	}
	if len(entries) == 0 {
		fmt.Fprintf(l.Out, "mw sleep ledger: no improvement is in plans/%s/%s yet\n", SleepLedgerDir, SleepLedgerFile)
		return nil
	}
	for _, entry := range entries {
		fmt.Fprintln(l.Out, entry)
	}
	return nil
}

// SleepOutcomeVault is the vault as the outcome uses it: a pass's plan files,
// the file it adds there, and the commit that keeps it.
type SleepOutcomeVault interface {
	// ReadPlanFile is plans/<dir>/<name>; it errors when the file is not there.
	ReadPlanFile(ctx context.Context, dir, name string) (string, error)
	PutPlanFile(ctx context.Context, dir, name, contents string) (string, error)
	Commit(ctx context.Context, message string, paths []string) ([]string, error)
}

// SleepOutcomeBeads is what the outcome reads of the work beads: where each is
// now and whether the Governor verified it.
type SleepOutcomeBeads interface {
	TriageSource
	StoriesComments(ctx context.Context, ids []string) (map[string][]Comment, error)
}

// SleepCallVerdict says what became of one call.
type SleepCallVerdict string

// The fates of a call. The first and second are held; the rest are not.
const (
	CallLanded     SleepCallVerdict = "landed"     // the bead is closed
	CallHeld       SleepCallVerdict = "held"       // the bead is open and still where the pass put it
	CallThawed     SleepCallVerdict = "thawed"     // the pass iced it and it is open at a priority above the icebox
	CallOverridden SleepCallVerdict = "overridden" // the pass set a priority and someone set another
	CallGone       SleepCallVerdict = "gone"       // the bead is not in the tracker
)

// SleepCallScore is one call and what became of it.
type SleepCallScore struct {
	Bead     string           `json:"bead"`
	To       int              `json:"to"`
	Now      int              `json:"now"`
	Verdict  SleepCallVerdict `json:"verdict"`
	Verified bool             `json:"verified,omitempty"`
}

// SleepScore is a pass's calls scored: how many held, of how many.
type SleepScore struct {
	Date       string           `json:"date"`
	ScoredAt   string           `json:"scored_at"`
	Calls      int              `json:"calls"`
	Held       int              `json:"held"`
	Landed     int              `json:"landed"`
	Verified   int              `json:"verified"`
	Thawed     int              `json:"thawed"`
	Overridden int              `json:"overridden"`
	Scores     []SleepCallScore `json:"scores"`
}

// sleepCall is one priority call of a pass's recommendations.
type sleepCall struct {
	bead string
	to   int
	ice  bool
}

var sleepDate = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// SleepOutcome is mw sleep outcome <date>: it scores the priority calls of the
// pass of that date against where the beads are now, and keeps the score in
// the pass's plans directory.
type SleepOutcome struct {
	Vault SleepOutcomeVault
	Beads SleepOutcomeBeads
	// Now is the clock; nil is time.Now.
	Now func() time.Time
	Out io.Writer
}

// Run scores the calls of the pass made on date (YYYY-MM-DD). A pass younger
// than SleepOutcomeDays is scored all the same, and the report says how far
// along it is.
func (o SleepOutcome) Run(ctx context.Context, date string) (SleepScore, error) {
	score := SleepScore{Date: date}
	if !sleepDate.MatchString(date) {
		return score, fmt.Errorf("mw sleep outcome: %q is not a date, write it as YYYY-MM-DD", date)
	}
	passed, err := time.Parse(time.DateOnly, date)
	if err != nil {
		return score, fmt.Errorf("mw sleep outcome: %q is not a date: %w", date, err)
	}
	now := time.Now().UTC()
	if o.Now != nil {
		now = o.Now().UTC()
	}
	dir := SleepPlansPrefix + date
	text, err := o.Vault.ReadPlanFile(ctx, dir, "recommendations.json")
	if err != nil {
		return score, fmt.Errorf("mw sleep outcome: no pass was kept for %s (plans/%s/recommendations.json): %w", date, dir, err)
	}
	calls, err := parseSleepCalls(text)
	if err != nil {
		return score, fmt.Errorf("mw sleep outcome: plans/%s/recommendations.json: %w", dir, err)
	}
	beads, err := o.Beads.TriageBeads(ctx)
	if err != nil {
		return score, fmt.Errorf("mw sleep outcome: reading the beads: %w", err)
	}
	byID := map[string]TriageBead{}
	for _, b := range beads {
		byID[b.ID] = b
	}
	var ids []string
	for _, call := range calls {
		ids = append(ids, call.bead)
	}
	comments, err := o.Beads.StoriesComments(ctx, ids)
	if err != nil {
		return score, fmt.Errorf("mw sleep outcome: reading the comments: %w", err)
	}

	score.ScoredAt = now.Format(time.RFC3339)
	score.Calls = len(calls)
	for _, call := range calls {
		s := SleepCallScore{Bead: call.bead, To: call.to}
		bead, ok := byID[call.bead]
		s.Now = bead.Priority
		for _, c := range comments[call.bead] {
			if commentMarksVerified(c.Text) {
				s.Verified = true
			}
		}
		switch {
		case !ok:
			s.Verdict = CallGone
		case strings.EqualFold(bead.Status, StatusClosed):
			s.Verdict = CallLanded
		case call.ice && bead.Priority < iceBand:
			s.Verdict = CallThawed
		case !call.ice && bead.Priority != call.to:
			s.Verdict = CallOverridden
		default:
			s.Verdict = CallHeld
		}
		switch s.Verdict {
		case CallLanded:
			score.Landed++
			score.Held++
		case CallHeld:
			score.Held++
		case CallThawed:
			score.Thawed++
		case CallOverridden:
			score.Overridden++
		}
		if s.Verified {
			score.Verified++
		}
		score.Scores = append(score.Scores, s)
	}

	age := int(now.Sub(passed).Hours() / 24)
	fmt.Fprintf(o.Out, "mw sleep outcome %s: %d of %d calls held (landed %d, verified %d, thawed %d, overridden %d)\n",
		date, score.Held, score.Calls, score.Landed, score.Verified, score.Thawed, score.Overridden)
	if age < SleepOutcomeDays {
		fmt.Fprintf(o.Out, "  day %d of %d: the calls are not yet at the age they are judged at\n", age, SleepOutcomeDays)
	}
	for _, s := range score.Scores {
		mark := ""
		if s.Verified {
			mark = " (verified)"
		}
		fmt.Fprintf(o.Out, "  %-14s called P%d, now P%d: %s%s\n", s.Bead, s.To, s.Now, s.Verdict, mark)
	}

	body, err := json.MarshalIndent(score, "", "  ")
	if err != nil {
		return score, err
	}
	if _, err := o.Vault.PutPlanFile(ctx, dir, SleepOutcomeFile, string(body)+"\n"); err != nil {
		return score, fmt.Errorf("mw sleep outcome: the score was not written: %w", err)
	}
	commitCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), sleepCommitBound)
	defer cancel()
	if _, err := o.Vault.Commit(commitCtx, "sleep outcome "+date, []string{path.Join("plans", dir, SleepOutcomeFile)}); err != nil {
		return score, fmt.Errorf("mw sleep outcome: committing the score: %w", err)
	}
	return score, nil
}

// iceBand is the priority the icebox is: P4.
const iceBand = 4

// sleepCallJSON reads a call as the sleep pass writes it ({"bead", "to": "P1"})
// and as the Mayor's own recommendations.json has it ({"id", "to": 1,
// "ice": true}).
type sleepCallJSON struct {
	Bead string          `json:"bead"`
	ID   string          `json:"id"`
	To   json.RawMessage `json:"to"`
	Ice  bool            `json:"ice"`
}

// parseSleepCalls reads a recommendations.json: an object with a "calls" list,
// or a list of calls.
func parseSleepCalls(text string) ([]sleepCall, error) {
	var raw []sleepCallJSON
	trimmed := bytes.TrimSpace([]byte(text))
	if bytes.HasPrefix(trimmed, []byte("[")) {
		if err := json.Unmarshal(trimmed, &raw); err != nil {
			return nil, err
		}
	} else {
		var object struct {
			Calls []sleepCallJSON `json:"calls"`
		}
		if err := json.Unmarshal(trimmed, &object); err != nil {
			return nil, err
		}
		raw = object.Calls
	}
	var calls []sleepCall
	for n, r := range raw {
		id := r.Bead
		if id == "" {
			id = r.ID
		}
		if id == "" {
			return nil, fmt.Errorf("call %d names no bead", n+1)
		}
		to, err := parseCallPriority(r.To)
		if err != nil {
			return nil, fmt.Errorf("call %d (%s): %w", n+1, id, err)
		}
		calls = append(calls, sleepCall{bead: id, to: to, ice: r.Ice || to >= iceBand})
	}
	if len(calls) == 0 {
		return nil, errors.New("holds no call")
	}
	return calls, nil
}

// parseCallPriority reads a priority written 2 or "P2".
func parseCallPriority(raw json.RawMessage) (int, error) {
	text := strings.Trim(strings.TrimSpace(string(raw)), `"`)
	text = strings.TrimPrefix(strings.ToUpper(text), "P")
	n, err := strconv.Atoi(text)
	if err != nil || n < 0 || n > iceBand {
		return 0, fmt.Errorf("%s is not a priority from P0 to P4", string(raw))
	}
	return n, nil
}
