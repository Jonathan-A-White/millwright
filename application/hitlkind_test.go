package application_test

import (
	"reflect"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain"
)

func TestAHitlStoryIsOfTheKindItsLabelNames(t *testing.T) {
	cases := []struct {
		labels []string
		kind   string
		kinds  []string
		hitl   bool
	}{
		{[]string{"hitl"}, "hands", nil, true},
		{[]string{"hitl", "hitl:review"}, "review", []string{"review"}, true},
		{[]string{"hitl", "HITL:Decision"}, "decision", []string{"decision"}, true},
		{[]string{"hitl:verify"}, "verify", []string{"verify"}, true},
		{[]string{"hitl", "hitl:review", "hitl:decision"}, "review", []string{"review", "decision"}, true},
		{[]string{"hitl", "hitl:review", "hitl:review"}, "review", []string{"review"}, true},
		{[]string{"hitl", "hitl:other"}, "hands", nil, true},
		{[]string{"hitl:other"}, "hands", nil, false},
		{[]string{"demo"}, "hands", nil, false},
	}
	for _, c := range cases {
		d := application.StoryDetail{Story: domain.Story{ID: "x"}, Labels: c.labels}
		if d.Hitl() != c.hitl || d.HitlKind() != c.kind || !reflect.DeepEqual(d.HitlKinds(), c.kinds) {
			t.Errorf("%v: hitl %v kind %q kinds %v; want %v %q %v", c.labels, d.Hitl(), d.HitlKind(), d.HitlKinds(), c.hitl, c.kind, c.kinds)
		}
	}
}
