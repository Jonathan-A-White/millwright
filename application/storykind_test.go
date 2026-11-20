package application

import (
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/domain"
)

func TestStoryKindIsLabelAware(t *testing.T) {
	story := func(title, typ string, labels ...string) StoryDetail {
		return StoryDetail{Story: domain.Story{Title: title}, Type: typ, Labels: labels}
	}
	for _, c := range []struct {
		name string
		d    StoryDetail
		want string
	}{
		{"plain", story("Add a thing", "task"), KindFeature},
		{"bug by type", story("Add a thing", "bug"), KindBug},
		{"bug by title", story("[bug] it breaks", "task"), KindBug},
		{"bug by label only", story("it breaks", "task", "bug"), KindBug},
		{"taste by title", story("[taste] the button", "task"), KindTaste},
		{"friction by title", story("[friction] slow", "task"), KindFriction},
		{"friction by label", story("slow", "task", "friction"), KindFriction},
		{"tester by label", story("Test: x", "task", "tester"), KindTester},
		{"content", story("Write the pack", "task", "model-story"), KindContent},
		{"a bug labelled model-story is a bug", story("[bug] x", "bug", "model-story"), KindBug},
	} {
		if got := StoryKind(c.d); got != c.want {
			t.Errorf("%s: StoryKind is %q, want %q", c.name, got, c.want)
		}
	}
	if !IsFeatureKind(KindFeature) || !IsFeatureKind(KindContent) || IsFeatureKind(KindBug) || IsFeatureKind(KindTaste) {
		t.Errorf("IsFeatureKind: only feature and content are features")
	}
}

func TestFeatureShareSplitsLandingsByPriorityThenKind(t *testing.T) {
	entries := []reportEntry{
		{id: "a", landed: true, kind: KindFeature},
		{id: "b", landed: true, kind: KindContent},
		{id: "c", landed: true, kind: KindBug},
		{id: "d", landed: true, kind: KindTaste},
		{id: "e", landed: true, kind: KindFeature, express: true},
		{id: "f", landed: true, kind: KindBug, express: true},
		// An unknown story is a feature; a refusal is no landing.
		{id: "g", landed: true},
		{id: "h", refused: true, kind: KindBug},
	}
	q := sumQuality(entries)
	if q.Landed != 7 || q.Features != 3 || q.Content != 1 || q.Fixes != 2 || q.Express != 2 {
		t.Fatalf("quality is %+v, want landed 7, features 3 (content 1), fixes 2, express 2", q)
	}
	if q.Features+q.Fixes+q.Express != q.Landed {
		t.Errorf("features + fixes + express is not landed")
	}
	share := ReportBlock{Name: "lampas", Quality: q}.FeatureShare()
	if share.Rig != "lampas" || share.Percent != 43 {
		t.Errorf("share is %+v, want lampas at 43%%", share)
	}
	line := share.Line()
	if !strings.HasPrefix(line, "FEATURES lampas 3/7 (43%)") || !strings.Contains(line, "content 1") || !strings.Contains(line, "express 2") {
		t.Errorf("line is %q", line)
	}
}

func TestFeatureShareLinesAreOnePerRigAfterTheParts(t *testing.T) {
	r := FactoryReportResult{Rigs: []ReportBlock{
		{Name: "lampas", Quality: Quality{Landed: 10, Features: 4, Content: 1, Fixes: 4, Express: 2}},
		{Name: "orrery", Quality: Quality{}},
	}}
	var b strings.Builder
	r.writeFeatures(&b)
	got := b.String()
	if n := strings.Count(got, "\nFEATURES "); n != 2 {
		t.Errorf("want 2 FEATURES lines, got %d in %q", n, got)
	}
	if shares := r.featureShares(); len(shares) != 2 || shares[1].Percent != 0 {
		t.Errorf("shares are %+v", shares)
	}
	brief := ReadLoopBrief(LoopInputs{Result: r})
	if len(brief.Share.Features) != 2 || brief.Share.Features[0].Features != 4 {
		t.Errorf("the brief's share carries %+v", brief.Share.Features)
	}
	if !strings.Contains(brief.String(), "\nFEATURES lampas 4/10 (40%)") {
		t.Errorf("the brief does not print the line:\n%s", brief.String())
	}
	if empty := ReadLoopBrief(LoopInputs{}); empty.Share.Features == nil {
		t.Errorf("a brief with no rigs says null for features, not a list")
	}
}
