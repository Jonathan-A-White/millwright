package application

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Fuel is what one session burned, as the harness counted it. Cached tokens are
// kept apart from fresh ones because they are most of a session's total and a
// tenth of its price: a factory that reported one number would learn nothing
// about where its fuel goes.
type Fuel struct {
	Input      int
	Output     int
	CacheRead  int
	CacheWrite int
}

// Total is every token the session was charged for, cached or not.
func (f Fuel) Total() int { return f.Input + f.Output + f.CacheRead + f.CacheWrite }

// String is the fuel as the ledger records it: the total, then where it went.
func (f Fuel) String() string {
	return fmt.Sprintf("%s tokens (in %s · out %s · cache read %s · cache write %s)",
		Thousands(f.Total()), Thousands(f.Input), Thousands(f.Output),
		Thousands(f.CacheRead), Thousands(f.CacheWrite))
}

// SessionResult is what a headless harness leaves behind in the story's run
// directory: whether the session got where it was going, and what it cost.
// Everything here is read from the file the session wrote, and nothing in it is
// trusted to be there — a session that died has none of it.
type SessionResult struct {
	// Subtype is the harness's own word for how the session ended, and IsError
	// is its own verdict on whether that was a failure.
	Subtype string
	IsError bool
	// Said is what the harness reported: the session's last word on a good run,
	// the reason on a bad one.
	Said string
	// TerminalReason is why the harness stopped, when it says.
	TerminalReason string
	SessionID      string
	Model          string
	// Turns and Duration are the harness's `num_turns` and `duration_ms`, and
	// they cover only what the result message itself covers. When Woken is
	// true that is the last turn alone, not the session.
	Turns    int
	Duration time.Duration
	// APIDuration is `duration_api_ms`: how long the whole session spent
	// waiting on the model. It is not wall-clock time — a session sits idle
	// while a command runs — but unlike Duration it does cover the whole
	// session, so it is the only length in the file a woken session can show.
	APIDuration time.Duration
	// CostUSD is the list-price equivalent the harness computed. On a
	// subscription it is not a bill; it is still the one number that compares
	// two stories worked on different models. It is a whole-session figure
	// whatever else in the file is not.
	CostUSD float64
	// Woken says this result message is not the run's first: a background task
	// notification or a monitor woke the session after an earlier result had
	// already been counted, and Turns and Duration start from that wake-up.
	Woken bool
	// WholeSessionFuel says Fuel was read from `modelUsage`, which counts the
	// whole session including its subagents. When it is false all the harness
	// gave was `usage`, and Fuel is the last turn's alone.
	WholeSessionFuel bool
	// Denials is how many times the session was refused permission to do
	// something. A session that was stopped from working says so here.
	Denials int
	Fuel    Fuel
}

// Finished reports whether the session got where it was going. Anything else —
// an error, a harness that stopped early, a subtype that is not a success — is
// a session whose work nobody should land without looking at it.
func (r SessionResult) Finished() bool {
	if r.IsError {
		return false
	}
	return r.Subtype == "" || r.Subtype == "success"
}

// Trouble is the plain reason a session is not finished, for a comment on the
// story. It is the harness's own words wherever the harness gave any.
func (r SessionResult) Trouble() string {
	said := firstLine(r.Said)
	switch {
	case said != "" && r.Subtype != "":
		return fmt.Sprintf("%s (%s)", said, r.Subtype)
	case said != "":
		return said
	case r.Subtype != "":
		return "the session ended as " + r.Subtype
	case r.TerminalReason != "":
		return "the session ended: " + r.TerminalReason
	default:
		return "the session reported that it failed, without saying why"
	}
}

// Spent is the fuel of one session on one line, for the ledger: what it burned,
// over what, how many turns it took, what that is worth at list price, and how
// long it took.
//
// Each figure says what it covers, because in one result file they do not all
// cover the same thing. A session that a task notification woke leaves a result
// whose `num_turns` and `duration_ms` are the last turn's only — mw-gq6.33 ran
// fifteen minutes and made two commits, and its file says 1 turn and 3.6
// seconds — while its cost and its `modelUsage` are the whole session's. A
// ledger that printed "1 turns, 3s" beside 2 million tokens would teach the
// next person the wrong thing about what a story costs.
func (r SessionResult) Spent() string {
	spent := []string{r.Fuel.String(), r.fuelCovers(), r.turnsTaken()}
	if r.CostUSD > 0 {
		spent = append(spent, fmt.Sprintf("$%.2f", r.CostUSD))
	}
	if clock := r.timeTaken(); clock != "" {
		spent = append(spent, clock)
	}
	return strings.Join(spent, ", ")
}

// fuelCovers is the ledger's word for what the token count is a count of.
func (r SessionResult) fuelCovers() string {
	if r.WholeSessionFuel {
		return "whole session"
	}
	return "last turn only"
}

// turnsTaken is the turn count, said so that a reader knows whether it is the
// session's or only what came after the last wake-up.
func (r SessionResult) turnsTaken() string {
	turns := "turns"
	if r.Turns == 1 {
		turns = "turn"
	}
	if r.Woken {
		return fmt.Sprintf("%d %s after the last wake-up", r.Turns, turns)
	}
	return fmt.Sprintf("%d %s", r.Turns, turns)
}

// timeTaken is how long the session took. For a session nothing woke, that is
// the clock the harness reported. For one that was woken, the harness's clock
// is the last turn's and would read as a fifteen-minute session that took three
// seconds, so the whole session's API time is given in its place, named for
// what it is. A file with neither says nothing rather than something false.
func (r SessionResult) timeTaken() string {
	if r.Woken {
		if r.APIDuration > 0 {
			return Clock(r.APIDuration) + " of API time"
		}
		return ""
	}
	if r.Duration > 0 {
		return Clock(r.Duration)
	}
	return ""
}

// resultFile is the harness's result JSON, read leniently. Every field is
// optional and two spellings of the cost are accepted, because what is read
// here was written by a program the factory does not own: a field that moved is
// a fuel number lost, never a close-out that fails.
type resultFile struct {
	Subtype        string            `json:"subtype"`
	IsError        bool              `json:"is_error"`
	Result         string            `json:"result"`
	Error          string            `json:"error"`
	Content        string            `json:"content"`
	TerminalReason string            `json:"terminal_reason"`
	SessionID      string            `json:"session_id"`
	Model          string            `json:"model"`
	Turns          int               `json:"num_turns"`
	DurationMS     int64             `json:"duration_ms"`
	APIDurationMS  int64             `json:"duration_api_ms"`
	ResultIndex    int               `json:"result_index"`
	TotalCostUSD   *float64          `json:"total_cost_usd"`
	Cost           *float64          `json:"cost"`
	Denials        []json.RawMessage `json:"permission_denials"`
	Usage          struct {
		Input      int `json:"input_tokens"`
		Output     int `json:"output_tokens"`
		CacheRead  int `json:"cache_read_input_tokens"`
		CacheWrite int `json:"cache_creation_input_tokens"`
	} `json:"usage"`
	// ModelUsage is the harness's per-model breakdown, one entry per model the
	// session used, its own included and every subagent's beside it. Claude
	// Code's own documentation says to account from this and not from `usage`:
	// `usage` "counts only the top-level agent loop", and in a run that emitted
	// more than one result message it "covers only that turn", while
	// `modelUsage` and `total_cost_usd` "carry the running total for the whole
	// call so far".
	ModelUsage map[string]struct {
		Input      int `json:"inputTokens"`
		Output     int `json:"outputTokens"`
		CacheRead  int `json:"cacheReadInputTokens"`
		CacheWrite int `json:"cacheCreationInputTokens"`
	} `json:"modelUsage"`
}

// fuel is the whole session's fuel where the file holds it and the last turn's
// where it does not, and which of the two it got.
func (f resultFile) fuel() (Fuel, bool) {
	if len(f.ModelUsage) == 0 {
		return Fuel{
			Input:      f.Usage.Input,
			Output:     f.Usage.Output,
			CacheRead:  f.Usage.CacheRead,
			CacheWrite: f.Usage.CacheWrite,
		}, false
	}
	var whole Fuel
	for _, model := range f.ModelUsage {
		whole.Input += model.Input
		whole.Output += model.Output
		whole.CacheRead += model.CacheRead
		whole.CacheWrite += model.CacheWrite
	}
	return whole, true
}

// ReadSessionResult reads what a session reported. A file that is not JSON at
// all is an error: it is the difference between a session that failed and a
// session that never ran, and a close-out must not mistake one for the other.
func ReadSessionResult(printed string) (SessionResult, error) {
	trimmed := strings.TrimSpace(printed)
	if trimmed == "" {
		return SessionResult{}, fmt.Errorf("the session's result is empty")
	}

	var file resultFile
	if err := json.Unmarshal([]byte(trimmed), &file); err != nil {
		return SessionResult{}, fmt.Errorf("the session's result is not the JSON a harness writes: %w", err)
	}

	fuel, wholeSession := file.fuel()
	result := SessionResult{
		Subtype:          file.Subtype,
		IsError:          file.IsError,
		Said:             firstOf(file.Error, file.Result, file.Content),
		TerminalReason:   file.TerminalReason,
		SessionID:        file.SessionID,
		Model:            file.Model,
		Turns:            file.Turns,
		Duration:         time.Duration(file.DurationMS) * time.Millisecond,
		APIDuration:      time.Duration(file.APIDurationMS) * time.Millisecond,
		Denials:          len(file.Denials),
		Woken:            file.ResultIndex > 0,
		WholeSessionFuel: wholeSession,
		Fuel:             fuel,
	}
	switch {
	case file.TotalCostUSD != nil:
		result.CostUSD = *file.TotalCostUSD
	case file.Cost != nil:
		result.CostUSD = *file.Cost
	}
	return result, nil
}

// firstOf is the first of these that says anything.
func firstOf(said ...string) string {
	for _, text := range said {
		if strings.TrimSpace(text) != "" {
			return strings.TrimSpace(text)
		}
	}
	return ""
}

// Thousands writes a token count the way a person reads one.
func Thousands(n int) string {
	digits := fmt.Sprintf("%d", n)
	sign := ""
	if strings.HasPrefix(digits, "-") {
		sign, digits = "-", digits[1:]
	}

	var grouped strings.Builder
	for i, digit := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			grouped.WriteByte(',')
		}
		grouped.WriteRune(digit)
	}
	return sign + grouped.String()
}

// Clock writes a duration the way a ledger reads one: whole minutes once there
// are any, seconds below that.
func Clock(d time.Duration) string {
	switch {
	case d >= time.Hour:
		return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
	case d >= time.Minute:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	default:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
}
