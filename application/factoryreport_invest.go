package application

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/domain"
)

// LabelInvestment is the label of a story that is an investment rather than
// an app's own work; mw report counts the landings that carry it.
const LabelInvestment = "investment"

// DefaultInvestShare is the share of landings meant to be investments when the
// config's [factory] invest_share does not say: one in three while the apps are
// young (mw-6ww.118, Q3), one in five once Lampas is in daily use.
const DefaultInvestShare = 0.33

// How far from its target a window's investment share is: under half of it,
// or over double it.
const (
	ShareUnder = "under"
	ShareOver  = "over"
)

// Which bound of the config's [factory] invest_share_min and invest_share_max
// the week calls for: BoundMax when quality is falling, so the share may be
// raised up to the maximum, BoundMin when an app is starving, so it is asked
// down to the minimum.
const (
	BoundMax = "max"
	BoundMin = "min"
)

// StarvedP1Hours is how long a ready P1 story may sit without being claimed
// before its rig counts as starving.
const StarvedP1Hours = 24

// StarvedLandingsFall is how far a rig's landings may fall week over week, as
// a share of the week before, before its rig counts as starving: a third.
const StarvedLandingsFall = 1.0 / 3

// InvestShare is the share of a window's landings that carried LabelInvestment,
// beside the window before and the target. Share, PreviousShare and Target are
// in percent, as the refusal rate is.
type InvestShare struct {
	Invested         int     `json:"invested"`
	Landed           int     `json:"landed"`
	Share            float64 `json:"share"`
	PreviousInvested int     `json:"previous_invested"`
	PreviousLanded   int     `json:"previous_landed"`
	PreviousShare    float64 `json:"previous_share"`
	// Arrow is the share against the window before, with no verdict: whether
	// a move is good is the target's to say. Zero when either window landed nothing.
	Arrow  Trend   `json:"arrow"`
	Target float64 `json:"target"`
	// Off is ShareUnder or ShareOver, and empty when the share is from half
	// the target to double it, or nothing landed.
	Off string `json:"off,omitempty"`
	// Min and Max are the bounds on the target in percent ([factory]
	// invest_share_min and invest_share_max). Bound is BoundMax or BoundMin,
	// the one the week calls for, empty when it calls for neither; Reason says
	// why, in the words the SHARE line prints.
	Min    float64 `json:"min"`
	Max    float64 `json:"max"`
	Bound  string  `json:"bound,omitempty"`
	Reason string  `json:"reason,omitempty"`
}

// readInvestShare reads the share off the whole factory's block, which is nil
// when nothing landed, was refused or was filed; target, min and max are
// fractions, and 0 or less is the default of each. rigs are the rigs' blocks
// and starvedP1 the rig that has a ready P1 story untouched StarvedP1Hours, or
// empty; they say whether the week calls for a bound.
func readInvestShare(whole *ReportBlock, rigs []ReportBlock, starvedP1 string, target, min, max float64) InvestShare {
	if target <= 0 {
		target = DefaultInvestShare
	}
	if min <= 0 {
		min = domain.DefaultInvestShareMin
	}
	if max <= 0 {
		max = domain.DefaultInvestShareMax
	}
	if whole == nil {
		whole = &ReportBlock{}
	}
	now, before := whole.Quality, whole.Previous
	s := InvestShare{
		Invested: now.Invested, Landed: now.Landed, Share: percentOf(now.Invested, now.Landed),
		PreviousInvested: before.Invested, PreviousLanded: before.Landed, PreviousShare: percentOf(before.Invested, before.Landed),
		Target: math.Round(target*1000) / 10, Min: math.Round(min*1000) / 10, Max: math.Round(max*1000) / 10,
	}
	s.Bound, s.Reason = investBound(now, before, rigs, starvedP1)
	s.Arrow = trendIf(now.Landed > 0 && before.Landed > 0, s.Share, s.PreviousShare, false)
	s.Arrow.Verdict = ""
	share := fraction(float64(now.Invested), float64(now.Landed))
	switch {
	case now.Landed == 0:
	case share < target/2:
		s.Off = ShareUnder
	case share > target*2:
		s.Off = ShareOver
	}
	return s
}

// investBound says which bound the week calls for and why. Quality falling
// wins over an app starving: escaped bugs up week over week, then the refusal
// rate up; then a ready P1 untouched, then the first rig (by name) whose
// landings fell by a third or more.
func investBound(now, before Quality, rigs []ReportBlock, starvedP1 string) (bound, reason string) {
	switch {
	case now.EscapedBugs > before.EscapedBugs:
		return BoundMax, fmt.Sprintf("quality falling: escaped bugs %d -> %d", before.EscapedBugs, now.EscapedBugs)
	case before.Landed+before.Refused > 0 && now.RefusalRate > before.RefusalRate:
		return BoundMax, fmt.Sprintf("quality falling: refusals %s -> %s", percentText(before.RefusalRate), percentText(now.RefusalRate))
	case starvedP1 != "":
		return BoundMin, fmt.Sprintf("an app starving: %s has a ready P1 untouched %d h", starvedP1, StarvedP1Hours)
	}
	sorted := append([]ReportBlock(nil), rigs...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })
	for _, rig := range sorted {
		was, is := rig.Previous.Landed, rig.Quality.Landed
		if rig.Name != ReportUnknown && was > 0 && float64(was-is) >= float64(was)*StarvedLandingsFall-1e-9 {
			return BoundMin, fmt.Sprintf("an app starving: %s landings %d -> %d", rig.Name, was, is)
		}
	}
	return "", ""
}

// starvedP1Rig is the first rig by name, narrowed to rig and host when they
// are set, that has a ready P1 story (open, unclaimed or last claimed
// StarvedP1Hours ago, and filed that long ago too) among stories; empty when
// none has.
func starvedP1Rig(stories []StoryDetail, now time.Time, rig, host string) string {
	cutoff := now.Add(-StarvedP1Hours * time.Hour)
	var rigs []string
	for _, d := range stories {
		path := d.Merged()
		if d.IsEpic || d.Priority != 1 || d.Hitl() || !strings.EqualFold(strings.TrimSpace(d.Status), StatusOpen) || strings.TrimSpace(d.Assignee) != "" {
			continue
		}
		if path.Rig == "" || (rig != "" && path.Rig != rig) || (host != "" && path.Host != host) {
			continue
		}
		if (!d.ClaimedAt.IsZero() && d.ClaimedAt.After(cutoff)) || d.Created.IsZero() || d.Created.After(cutoff) {
			continue
		}
		rigs = append(rigs, path.Rig)
	}
	if len(rigs) == 0 {
		return ""
	}
	sort.Strings(rigs)
	return rigs[0]
}

// percentText is a percentage as a line has it: "20%".
func percentText(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64) + "%"
}

// boundWords says which bound the week calls for and why, the one writer of it
// for the report's text and the phone summary: "bound: up to 33% allowed
// (quality falling: ...)", "bound: down to 10% asked (an app starving: ...)",
// or nothing.
func (s InvestShare) boundWords() string {
	switch s.Bound {
	case BoundMax:
		return fmt.Sprintf("bound: up to %s allowed (%s)", percentText(s.Max), s.Reason)
	case BoundMin:
		return fmt.Sprintf("bound: down to %s asked (%s)", percentText(s.Min), s.Reason)
	}
	return ""
}

// target is the target as it reads in a line: "33%".
func (s InvestShare) target() string {
	return strconv.FormatFloat(s.Target, 'f', -1, 64) + "%"
}

// offWords says how far from the target the share is: "under half the 33%
// target", or nothing when it is not far off.
func (s InvestShare) offWords() string {
	switch s.Off {
	case ShareUnder:
		return "under half the " + s.target() + " target"
	case ShareOver:
		return "over double the " + s.target() + " target"
	}
	return ""
}

// shareOf is "3/9 (33%)", or "none landed".
func shareOf(invested, landed int) string {
	if landed == 0 {
		return "none landed"
	}
	return fmt.Sprintf("%d/%d (%.0f%%)", invested, landed, percentOf(invested, landed))
}

// writeInvestShare prints the line that ends QUALITY: the share, the share
// before and the target, and under it the words when the share is far off.
func writeInvestShare(b *strings.Builder, s *InvestShare) {
	if s == nil {
		return
	}
	line := fmt.Sprintf("INVESTMENTS %s%s, before %s, target %s",
		shareOf(s.Invested, s.Landed), s.Arrow.Text(), shareOf(s.PreviousInvested, s.PreviousLanded), s.target())
	fmt.Fprintf(b, "\n%s\n", fitLine(line, ReportWidth))
	if words := s.offWords(); words != "" {
		fmt.Fprintf(b, "  %s\n", words)
	}
	if words := s.boundWords(); words != "" {
		wrapInto(b, "  ", words)
	}
}

// summaryText is the share as the phone summary has it: "investments 3/9
// (33%) ↑, target 33%", the target's words in place of the target when far off.
// The bound's words are boundWords, on a line of their own.
func (s InvestShare) summaryText() string {
	tail := "target " + s.target()
	if words := s.offWords(); words != "" {
		tail = words
	}
	return fmt.Sprintf("investments %s%s, %s", shareOf(s.Invested, s.Landed), s.Arrow.Text(), tail)
}

// winInvestShare is R7: a share of landings that were investments under half
// the target or over double it.
func winInvestShare(s *InvestShare) string {
	if s == nil || s.Off == "" {
		return ""
	}
	fix := "file more"
	if s.Off == ShareOver {
		fix = "file fewer"
	}
	return fmt.Sprintf("investments %.0f%%, %s: %s", s.Share, s.offWords(), fix)
}
