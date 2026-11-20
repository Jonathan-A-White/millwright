package application

import (
	"fmt"
	"strings"
)

// ExpressPriority is the lowest priority that goes by the express lane: a story
// at P0 or P1 is express whatever its kind.
const ExpressPriority = 1

// FeatureShare is one rig's landings of the window told by lane: the express
// ones, the features (content among them) and the fixes.
type FeatureShare struct {
	Rig      string `json:"rig"`
	Landed   int    `json:"landed"`
	Features int    `json:"features"`
	Content  int    `json:"content"`
	Fixes    int    `json:"fixes"`
	Express  int    `json:"express"`
	// Percent is Features of Landed, rounded, 0 when nothing landed.
	Percent int `json:"percent"`
}

// FeatureShare is the block's landings by lane.
func (k ReportBlock) FeatureShare() FeatureShare {
	q := k.Quality
	return FeatureShare{Rig: k.Name, Landed: q.Landed, Features: q.Features, Content: q.Content, Fixes: q.Fixes,
		Express: q.Express, Percent: int(percentOf(q.Features, q.Landed) + 0.5)}
}

// Line is the share as one line: "FEATURES lampas 42/219 (19%), new 30, content
// 12. fixes 139, express 38."
func (s FeatureShare) Line() string {
	return fmt.Sprintf("FEATURES %s %d/%d (%d%%), new %d, content %d. fixes %d, express %d.",
		s.Rig, s.Features, s.Landed, s.Percent, s.Features-s.Content, s.Content, s.Fixes, s.Express)
}

// featureShares is the share of each rig of the report, in its order; an empty
// list, not nil, when it has none.
func (r FactoryReportResult) featureShares() []FeatureShare {
	shares := []FeatureShare{}
	for _, rig := range r.Rigs {
		shares = append(shares, rig.FeatureShare())
	}
	return shares
}

// writeFeatures prints a FEATURES line for each rig, after the parts.
func (r FactoryReportResult) writeFeatures(b *strings.Builder) {
	shares := r.featureShares()
	if len(shares) == 0 {
		return
	}
	b.WriteString("\n")
	for _, s := range shares {
		wrapInto(b, "", s.Line())
	}
}
