package application

import (
	"fmt"
	"math"
	"strconv"
	"strings"
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
}

// readInvestShare reads the share off the whole factory's block, which is nil
// when nothing landed, was refused or was filed; target is a fraction, and 0
// or less is DefaultInvestShare.
func readInvestShare(whole *ReportBlock, target float64) InvestShare {
	if target <= 0 {
		target = DefaultInvestShare
	}
	if whole == nil {
		whole = &ReportBlock{}
	}
	now, before := whole.Quality, whole.Previous
	s := InvestShare{
		Invested: now.Invested, Landed: now.Landed, Share: percentOf(now.Invested, now.Landed),
		PreviousInvested: before.Invested, PreviousLanded: before.Landed, PreviousShare: percentOf(before.Invested, before.Landed),
		Target: math.Round(target*1000) / 10,
	}
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
}

// summaryText is the share as the phone summary has it: "investments 3/9
// (33%) ↑, target 33%", the target's words in place of the target when far off.
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
