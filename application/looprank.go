package application

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/Jonathan-A-White/millwright/domain"
)

// The effects an idea may expect on one of his axes: up is better.
const (
	AxisUp    = "up"
	AxisLevel = "level"
	AxisDown  = "down"
)

// ideaSizes are the sizes of an idea, smallest first.
var ideaSizes = []string{"XS", "S", "M", "L", "XL"}

// axisRank orders an axis's effect: up first, down last. ok is false for a word
// that is none of the three.
func axisRank(effect string) (rank int, ok bool) {
	switch strings.ToLower(strings.TrimSpace(effect)) {
	case AxisUp:
		return 0, true
	case AxisLevel:
		return 1, true
	case AxisDown:
		return 2, true
	}
	return 0, false
}

func axisArrow(effect string) string {
	switch strings.ToLower(strings.TrimSpace(effect)) {
	case AxisUp:
		return "↑"
	case AxisLevel:
		return "→"
	case AxisDown:
		return "↓"
	}
	return "?"
}

func sizeRank(size string) (int, bool) {
	for n, s := range ideaSizes {
		if strings.EqualFold(strings.TrimSpace(size), s) {
			return n, true
		}
	}
	return 0, false
}

// ParseSleepIdeas reads the "ideas" of a sleep pass's recommendations.json into
// ledger records (SleepLedgerEntry, the improvements ledger's own record): none
// and no error when the recommendations carry no ideas.
func ParseSleepIdeas(recommendations string) ([]SleepLedgerEntry, error) {
	var object struct {
		Ideas []SleepLedgerEntry `json:"ideas"`
	}
	if err := json.Unmarshal([]byte(recommendations), &object); err != nil {
		return nil, fmt.Errorf("the pass's ideas: %w", err)
	}
	return object.Ideas, nil
}

// RankedIdea is one unfiled idea in the order mw loop rank prints it.
type RankedIdea struct {
	Rank      int    `json:"rank"`
	ID        string `json:"id"`
	Title     string `json:"title"`
	Quality   string `json:"quality,omitempty"`
	HisTime   string `json:"his_time,omitempty"`
	Fuel      string `json:"fuel,omitempty"`
	Speed     string `json:"speed,omitempty"`
	Compounds bool   `json:"compounds"`
	Size      string `json:"size,omitempty"`
	Expect    string `json:"expect,omitempty"`
	// Unscored says a field is missing or unreadable; Missing names them.
	Unscored bool     `json:"unscored"`
	Missing  []string `json:"missing,omitempty"`

	key [6]int
}

// rankIdea reads one ledger record as a ranked idea.
func rankIdea(e SleepLedgerEntry) RankedIdea {
	r := RankedIdea{ID: e.ID, Title: strings.TrimSpace(e.Title), Quality: e.Quality, HisTime: e.HisTime, Fuel: e.Fuel,
		Speed: e.Speed, Size: strings.ToUpper(strings.TrimSpace(e.Size)), Expect: strings.TrimSpace(e.Expect)}
	if r.Title == "" {
		r.Title = e.ID
	}
	for n, axis := range []struct{ name, effect string }{
		{"quality", e.Quality}, {"his_time", e.HisTime}, {"fuel", e.Fuel}, {"speed", e.Speed}} {
		rank, ok := axisRank(axis.effect)
		if !ok {
			r.Missing = append(r.Missing, axis.name)
		}
		r.key[n] = rank
	}
	if e.Compounds == nil {
		r.Missing = append(r.Missing, "compounds")
	} else if r.Compounds = *e.Compounds; !r.Compounds {
		r.key[4] = 1
	}
	if size, ok := sizeRank(e.Size); ok {
		r.key[5] = size
	} else {
		r.Missing = append(r.Missing, "size")
	}
	acceptance := r.Expect
	if !strings.HasPrefix(strings.ToLower(acceptance), "expect:") {
		acceptance = "Expect: " + acceptance
	}
	if r.Expect == "" {
		r.Missing = append(r.Missing, "expect")
	} else if _, err := domain.ParseExpect(acceptance); err != nil {
		r.Missing = append(r.Missing, "expect (not a line of the form "+domain.ExpectForms+")")
	} else {
		r.Expect = acceptance
	}
	r.Unscored = len(r.Missing) > 0
	return r
}

// RankIdeas orders the ideas with no bead filed: quality, then his time, fuel
// and speed (up first, down last), a compounding change before one that does
// not, the smallest size first, then the ledger's own order. An idea missing a
// field is listed after every scored one, in the ledger's order, never dropped.
func RankIdeas(entries []SleepLedgerEntry) []RankedIdea {
	var ranked []RankedIdea
	for _, e := range entries {
		if strings.TrimSpace(e.Bead) == "" {
			ranked = append(ranked, rankIdea(e))
		}
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		a, b := ranked[i], ranked[j]
		if a.Unscored != b.Unscored {
			return !a.Unscored
		}
		if a.Unscored {
			return false
		}
		return lessKey(a.key, b.key)
	})
	for n := range ranked {
		ranked[n].Rank = n + 1
	}
	return ranked
}

func lessKey(a, b [6]int) bool {
	for n := range a {
		if a[n] != b[n] {
			return a[n] < b[n]
		}
	}
	return false
}

// String is the idea as one line of mw loop rank: its rank, title, the four
// axes as arrows, C when it compounds, its size and its Expect line; an
// unscored idea says what is missing instead.
func (r RankedIdea) String() string {
	axes := fmt.Sprintf("quality%s his_time%s fuel%s speed%s", axisArrow(r.Quality), axisArrow(r.HisTime), axisArrow(r.Fuel), axisArrow(r.Speed))
	compounds := " "
	if r.Compounds {
		compounds = "C"
	}
	size := r.Size
	if size == "" {
		size = "?"
	}
	line := fmt.Sprintf("%d. %s  %s  %s  %s  %s", r.Rank, strings.Join(strings.Fields(r.Title), " "), axes, compounds, size, r.Expect)
	if r.Unscored {
		line += "  unscored: missing " + strings.Join(r.Missing, ", ")
	}
	return line
}

// LoopRank is mw loop rank: it prints the improvements ledger's ideas that have
// no bead filed, in the order the weekly sitting files them from. It reads the
// ledger the way mw sleep ledger does and changes nothing.
type LoopRank struct {
	Vault SleepLedgerVault
	// Top cuts the list to its first Top ideas; 0 prints them all.
	Top  int
	JSON bool
	Out  io.Writer
}

// Run prints the ranking.
func (l LoopRank) Run(ctx context.Context) error {
	text, err := l.Vault.ReadImprovements(ctx)
	if err != nil {
		return fmt.Errorf("mw loop rank: %w", err)
	}
	entries, err := ParseSleepLedger(text)
	if err != nil {
		return fmt.Errorf("mw loop rank: %w", err)
	}
	ranked := RankIdeas(entries)
	if l.Top > 0 && len(ranked) > l.Top {
		ranked = ranked[:l.Top]
	}
	if l.JSON {
		if ranked == nil {
			ranked = []RankedIdea{}
		}
		enc := json.NewEncoder(l.Out)
		enc.SetIndent("", "  ")
		return enc.Encode(ranked)
	}
	if len(ranked) == 0 {
		fmt.Fprintf(l.Out, "mw loop rank: no unfiled idea is in plans/%s/%s\n", SleepLedgerDir, SleepLedgerFile)
		return nil
	}
	for _, r := range ranked {
		fmt.Fprintln(l.Out, r)
	}
	return nil
}
