package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
)

// GristRunStore keeps the raw record of every grind under the mill's state
// directory, runs/<txid>/, and lists it. Nothing in it is ever deleted by mw.
// The real adapter is infrastructure/grist's Runs.
type GristRunStore interface {
	// Keep writes one run's files: input.json, attachment-N.<ext>, scorers.json,
	// answer.json and timing.json.
	Keep(ctx context.Context, run GristRun) error
	// List reports the runs received at or after since (all of them when it is
	// zero), oldest first.
	List(ctx context.Context, since time.Time) ([]GristRunLine, error)
	// Timings reports the timing of every run kept, oldest first.
	Timings(ctx context.Context) ([]GristRunTiming, error)
}

// GristRun is everything one grind took in and gave out.
type GristRun struct {
	Txid string
	// Input is the app's request as the harness session was given it, with
	// reading_result in it when the grind scored a recording. Never audio.
	Input json.RawMessage
	// Attachments are the photos and recordings of the grist, as opened.
	Attachments []GristRunAttachment
	// Scorers is what each engine said of each recording; empty when nothing
	// was scored.
	Scorers []GristRunScore
	Answer  GristRunAnswer
	Timing  GristRunTiming
}

// GristRunAttachment is one attachment of a run: the file name's extension,
// with its dot, and the bytes.
type GristRunAttachment struct {
	Ext  string
	Data []byte
}

// GristRunScore is one recording scored: which attachment it was (from 1), its
// type, the text it was read against and the language, and what each engine
// said, keyed by the engine's name: a ReadingResult, or {"error": "..."}.
type GristRunScore struct {
	Attachment int                        `json:"attachment"`
	Mime       string                     `json:"mime"`
	Target     string                     `json:"target_text"`
	Lang       string                     `json:"lang"`
	Results    map[string]json.RawMessage `json:"reading_result"`
}

// GristRunAnswer is how the grind ended.
type GristRunAnswer struct {
	Status  string          `json:"status"`
	Reason  string          `json:"reason,omitempty"`
	Answer  json.RawMessage `json:"answer,omitempty"`
	Said    string          `json:"said,omitempty"`
	Turns   int             `json:"turns,omitempty"`
	CostUSD float64         `json:"total_cost_usd,omitempty"`
}

// GristRunTiming is when each part of a grind happened, and how many seconds
// each took. ScoredAt is empty when nothing was scored. Sent is the grist
// record's own time, so Received minus Sent is the queue wait; Scorers is the
// seconds each engine took, summed over the recordings, and both are left out
// when not known.
type GristRunTiming struct {
	Txid           string             `json:"txid"`
	App            string             `json:"app"`
	Kind           string             `json:"kind"`
	Model          string             `json:"model"`
	Effort         string             `json:"effort"`
	Sent           *time.Time         `json:"sent,omitempty"`
	Received       time.Time          `json:"received"`
	ScoredAt       *time.Time         `json:"scored_at,omitempty"`
	HarnessStarted time.Time          `json:"harness_started"`
	Answered       time.Time          `json:"answered"`
	ScoringSeconds float64            `json:"scoring_seconds"`
	Scorers        map[string]float64 `json:"scorers,omitempty"`
	HarnessSeconds float64            `json:"harness_seconds"`
	Seconds        float64            `json:"seconds"`
}

// GristRunLine is one run as mw grist runs lists it.
type GristRunLine struct {
	Txid     string
	Kind     string
	Model    string
	Received time.Time
	Seconds  float64
	// Status is answered, refused or failed.
	Status string
}

// GristRuns is `mw grist runs`: the runs the mill has kept.
type GristRuns struct {
	Runs GristRunStore
	// Out is where the list is printed; nil prints nothing.
	Out io.Writer
}

// Run lists the runs received since the time given, all of them when it is
// zero.
func (g GristRuns) Run(ctx context.Context, since time.Time) ([]GristRunLine, error) {
	if g.Runs == nil {
		return nil, fmt.Errorf("mw grist runs: nowhere the mill keeps its runs")
	}
	lines, err := g.Runs.List(ctx, since)
	if err != nil {
		return nil, err
	}
	if g.Out != nil {
		g.print(lines, since)
	}
	return lines, nil
}

func (g GristRuns) print(lines []GristRunLine, since time.Time) {
	if len(lines) == 0 {
		if since.IsZero() {
			fmt.Fprintln(g.Out, "the mill has kept no runs")
		} else {
			fmt.Fprintf(g.Out, "the mill has kept no runs since %s\n", since.UTC().Format(time.RFC3339))
		}
		return
	}
	for _, line := range lines {
		fmt.Fprintf(g.Out, "%-22s  %-16s  %-8s  %7.1fs  %s\n",
			shortTxid(line.Txid), line.Kind, line.Model, line.Seconds, line.Status)
	}
}

// GristRunName is the directory a run is kept in: the txid when it is made of
// characters fit for one path segment, else the hash of it, so a txid can
// never name a place outside runs/.
func GristRunName(txid string) string {
	ok := txid != "" && !strings.HasPrefix(txid, ".")
	for _, r := range txid {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune(":._-", r)) {
			ok = false
		}
	}
	if ok {
		return txid
	}
	sum := sha256.Sum256([]byte(txid))
	return "txid-" + hex.EncodeToString(sum[:])[:32]
}
