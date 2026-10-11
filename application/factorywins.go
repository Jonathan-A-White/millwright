package application

import (
	"fmt"
	"sort"
	"strings"
)

// MaxCheapWins is how many suggestions mw report prints under CHEAP WINS.
const MaxCheapWins = 3

// The thresholds of the rules (PROVISIONAL): a reason is dominant at this share
// of a rig's refusals and this many of them, a refusal rate has risen at this
// many points, and a gate is well over its usual at this multiple of it.
const (
	WinDominantShare  = 30.0
	WinDominantMin    = 3
	WinRateRisePoints = 5.0
	WinGateOverUsual  = 1.5
)

// The rules, in the order they are tried. Quality comes first.
const (
	WinCausesUnset    = "R1"
	WinDominantReason = "R2"
	WinAttemptsUsed   = "R3"
	WinRefusalRate    = "R4"
	WinParFlag        = "R5"
	WinGateSlow       = "R6"
)

// CheapWin is one suggestion a rule made over the report's numbers. Shown says
// it is one of the first MaxCheapWins that held, the ones printed.
type CheapWin struct {
	Rule  string `json:"rule"`
	Text  string `json:"text"`
	Shown bool   `json:"shown"`
}

// HeavyStory is a story of the window that used up its attempts or needed
// DefaultMaxAttempts of them.
type HeavyStory struct {
	ID       string
	Attempts int
}

// WinInputs is what the rules read: the report's rig blocks, its gate trends,
// each kind's calibration, and the stories that used their attempts up.
type WinInputs struct {
	Rigs  []ReportBlock
	Gates []GateTrend
	Kinds []KindCalibration
	Heavy []HeavyStory
}

// CheapWins runs the rules in their fixed order and returns each that holds,
// with the first MaxCheapWins marked Shown. It reads its inputs and nothing
// else: the section suggests, and the bigger call stays the Mayor's.
func CheapWins(in WinInputs) []CheapWin {
	wins := []CheapWin{}
	add := func(rule, text string) {
		if text != "" {
			wins = append(wins, CheapWin{Rule: rule, Text: text})
		}
	}
	add(WinCausesUnset, winCausesUnset(in.Rigs))
	add(WinDominantReason, winDominantReason(in.Rigs))
	add(WinAttemptsUsed, winAttemptsUsed(in.Heavy))
	add(WinRefusalRate, winRefusalRate(in.Rigs))
	add(WinParFlag, winParFlag(in.Kinds))
	add(WinGateSlow, winGateSlow(in.Gates))
	for i := range wins {
		wins[i].Shown = i < MaxCheapWins
	}
	return wins
}

// winCausesUnset is R1: refusals nobody has named a cause for.
func winCausesUnset(rigs []ReportBlock) string {
	n := 0
	for _, rig := range rigs {
		n += rig.Quality.Causes[CauseUnset]
	}
	switch n {
	case 0:
		return ""
	case 1:
		return "1 refusal has no cause: mw cause it"
	}
	return fmt.Sprintf("%d refusals have no cause: mw cause them", n)
}

// winDominantReason is R2: the rig whose commonest refusal reason is the biggest
// share of its refusals, at WinDominantShare percent and WinDominantMin refusals
// at least.
func winDominantReason(rigs []ReportBlock) string {
	var rig, reason string
	var count int
	var share float64
	for _, block := range rigs {
		q := block.Quality
		if len(q.Reasons) == 0 || q.Refused == 0 {
			continue
		}
		top := q.Reasons[0]
		topShare := float64(top.Count) * 100 / float64(q.Refused)
		if top.Count < WinDominantMin || topShare < WinDominantShare {
			continue
		}
		if topShare > share || (topShare == share && top.Count > count) {
			rig, reason, count, share = block.Name, top.Reason, top.Count, topShare
		}
	}
	if rig == "" {
		return ""
	}
	fix := "look for what they share"
	if reason == "tests-fail" {
		fix = "gate check first"
	}
	return fmt.Sprintf("%s is %.0f%% of %s refusals: %s", reason, share, rig, fix)
}

// winAttemptsUsed is R3: stories that used up their attempts or needed as many
// as a story may have.
func winAttemptsUsed(heavy []HeavyStory) string {
	if len(heavy) == 0 {
		return ""
	}
	sorted := append([]HeavyStory{}, heavy...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Attempts != sorted[j].Attempts {
			return sorted[i].Attempts > sorted[j].Attempts
		}
		return sorted[i].ID < sorted[j].ID
	})
	if len(sorted) == 1 {
		return fmt.Sprintf("%s used %d attempts: split it smaller", sorted[0].ID, sorted[0].Attempts)
	}
	return fmt.Sprintf("%s +%d more used %d+ attempts: split them",
		sorted[0].ID, len(sorted)-1, DefaultMaxAttempts)
}

// winRefusalRate is R4: the rig whose refusal rate rose most against the window
// before, if it rose WinRateRisePoints or more.
func winRefusalRate(rigs []ReportBlock) string {
	var name string
	var best float64
	for _, rig := range rigs {
		if rig.Previous.Landed+rig.Previous.Refused == 0 {
			continue
		}
		rise := rig.Quality.RefusalRate - rig.Previous.RefusalRate
		if rise >= WinRateRisePoints && rise > best {
			name, best = rig.Name, rise
		}
	}
	if name == "" {
		return ""
	}
	return fmt.Sprintf("%s refusals up %.0f points: mw report --rig %s", name, best, name)
}

// winParFlag is R5: the flagged kind whose par is furthest off.
func winParFlag(kinds []KindCalibration) string {
	var worst *KindCalibration
	for i := range kinds {
		if kinds[i].Flag != "" && (worst == nil || kinds[i].MedianErrorPercent > worst.MedianErrorPercent) {
			worst = &kinds[i]
		}
	}
	if worst == nil {
		return ""
	}
	return fmt.Sprintf("par for kind %s is off %.0f%%: %s", worst.Kind, worst.MedianErrorPercent, worst.Flag)
}

// winGateSlow is R6: the host whose latest gate on a rig is the furthest over
// its usual, at WinGateOverUsual times it or more.
func winGateSlow(gates []GateTrend) string {
	var worst *GateTrend
	var worstOver float64
	for i, g := range gates {
		if g.UsualS <= 0 || g.LatestS < WinGateOverUsual*g.UsualS {
			continue
		}
		if over := g.LatestS / g.UsualS; over > worstOver {
			worst, worstOver = &gates[i], over
		}
	}
	if worst == nil {
		return ""
	}
	return fmt.Sprintf("%s gate on %s %s, usual %s: check its load",
		worst.Host, worst.Rig, gateClock(seconds(worst.LatestS)), gateClock(seconds(worst.UsualS)))
}

// writeCheapWins prints the section that ends the report: the shown wins,
// numbered from 1, each cut to ReportWidth columns, or one line saying none held.
func writeCheapWins(b *strings.Builder, wins []CheapWin) {
	b.WriteString("\n")
	n := 0
	for _, w := range wins {
		if !w.Shown {
			continue
		}
		if n == 0 {
			b.WriteString("CHEAP WINS\n")
		}
		n++
		prefix := fmt.Sprintf("%d. ", n)
		fmt.Fprintf(b, "%s%s\n", prefix, fitLine(w.Text, ReportWidth-len(prefix)))
	}
	if n == 0 {
		b.WriteString("CHEAP WINS: none seen\n")
	}
}
