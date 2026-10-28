package domain_test

import (
	"reflect"
	"testing"

	"github.com/Jonathan-A-White/millwright/domain"
)

func heldForGraph() []domain.GraphBead {
	return []domain.GraphBead{
		{ID: "k-2", Status: "deferred", Labels: []string{"held-for-jonah"}},
		{ID: "k-1", Status: "deferred", Labels: []string{"held-for-jonah", "friction"}},
		{ID: "k-3", Status: "closed", Labels: []string{"held-for-jonah"}},
		{ID: "m-1", Status: "open", Labels: []string{"held-for-kestrel"}},
		{ID: "z-1", Status: "closed", Labels: []string{"held-for-done-event"}},
		{ID: "x-1", Status: "open", Labels: []string{"held-for-", "hitl"}},
	}
}

func TestHeldForLabelsCountsOpenBeadsPerLabelAndSkipsALabelWithNone(t *testing.T) {
	got := domain.HeldForLabels(heldForGraph())
	want := []domain.HeldFor{
		{Label: "held-for-jonah", Beads: []string{"k-1", "k-2"}},
		{Label: "held-for-kestrel", Beads: []string{"m-1"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestHeldForEventNamesTheOpenBeadsOfThatEventAndNoneForAnother(t *testing.T) {
	graph := heldForGraph()
	if got := domain.HeldForEvent(graph, "Jonah"); !reflect.DeepEqual(got, []string{"k-1", "k-2"}) {
		t.Fatalf("got %v, want k-1 and k-2", got)
	}
	for _, event := range []string{"done-event", "nothing", ""} {
		if got := domain.HeldForEvent(graph, event); len(got) != 0 {
			t.Fatalf("event %q: got %v, want none", event, got)
		}
	}
}

func TestHeldForLabelFindsTheLabelThatParksABead(t *testing.T) {
	cases := []struct {
		labels []string
		want   string
	}{
		{nil, ""},
		{[]string{"friction", "bug"}, ""},
		{[]string{"held-for-"}, ""},
		{[]string{"friction", "Held-For-Jonah"}, "held-for-jonah"},
		{[]string{"held-for-mw-gq6.12", "held-for-jonah"}, "held-for-mw-gq6.12"},
	}
	for _, c := range cases {
		if got := domain.HeldForLabel(c.labels); got != c.want {
			t.Errorf("HeldForLabel(%v) = %q, want %q", c.labels, got, c.want)
		}
	}
}
