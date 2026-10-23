package application

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// touchesOnly is a landing that changed the pathspecs it holds.
type touchesOnly map[string]bool

func (t touchesOnly) Changed(_ context.Context, _, _, _, pathspec string) (bool, error) {
	return t[pathspec], nil
}

// movingCheckout is the home's checkout of an app's rig, one commit behind
// origin's main until it is advanced.
type movingCheckout struct{ advanced int }

func (*movingCheckout) Fetch(context.Context, string) error { return nil }
func (*movingCheckout) Tip(context.Context, string, string, string) (string, error) {
	return "new", nil
}
func (m *movingCheckout) Head(context.Context, string) (string, error) {
	if m.advanced > 0 {
		return "new", nil
	}
	return "old", nil
}
func (*movingCheckout) Uncommitted(context.Context, string) ([]string, error) { return nil, nil }
func (m *movingCheckout) Advance(context.Context, string, string, string) (Advanced, error) {
	if m.advanced > 0 {
		return Advanced{Level: true}, nil
	}
	m.advanced++
	return Advanced{Moved: true}, nil
}

// mw-gq6.728: a landing of an app's grist made on a host that names no
// [grist-apps] cannot be smoked there; it says the home's tick has it, or that it
// was not smoked, and never nothing.
func TestALandingOfGristOnAHostWithNoGristAppsSaysWhoSmokesIt(t *testing.T) {
	cases := []struct {
		name, host, home, rig string
		touched               touchesOnly
		want                  string
	}{
		{"another host is home", "cloud1", "desktop 2026-10-01T05:00:00Z mayor\n", "lampas", touchesOnly{GristSmokeGrindsPath: true}, "grist smoke: handed to the home (desktop)"},
		{"the client paths changed", "cloud1", "desktop 2026-10-01T05:00:00Z mayor\n", "lampas", touchesOnly{"web/grist": true}, "grist smoke: handed to the home (desktop)"},
		{"this host is home", "desktop", "desktop 2026-10-01T05:00:00Z mayor\n", "lampas", touchesOnly{GristSmokeGrindsPath: true}, "grist smoke: not run, this host names no [grist-apps]"},
		{"the home cannot be told", "cloud1", "", "lampas", touchesOnly{GristSmokeGrindsPath: true}, "grist smoke: not run, this host names no [grist-apps]"},
		{"the factory's own mill changed", "cloud1", "desktop 2026-10-01T05:00:00Z mayor\n", FactoryRig, touchesOnly{GristSmokeFactoryPaths[0]: true}, "grist smoke: not run, this host names no [grist-apps]"},
		{"nothing of grist changed", "cloud1", "desktop 2026-10-01T05:00:00Z mayor\n", "lampas", touchesOnly{"README.md": true}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stub := &smokeStub{}
			n := Next{Tracker: &smokeComments{}, Host: tc.host,
				Smoke: GristSmokeAfter{Touches: tc.touched, Smoke: stub, ClientPaths: map[string][]string{"lampas": {"web/grist"}}}}
			if tc.home != "" {
				n.Home = homeText(tc.home)
			}
			c := &closeOut{id: "mw-1.1", target: "main", rigDir: "/rigs/" + tc.rig}
			c.path.Rig = tc.rig

			n.readSmokeElsewhere(context.Background(), c, "origin/main", c.branch)
			report := &NextReport{Rig: Advanced{Moved: true}, Landed: true}
			n.smoke(context.Background(), c, report)

			joined := strings.Join(report.Notes, "\n")
			switch {
			case tc.want == "" && strings.Contains(joined, "grist smoke"):
				t.Errorf("expected nothing said of a smoke, got %q", joined)
			case tc.want != "" && !strings.Contains(joined, tc.want):
				t.Errorf("expected %q in the notes, got %q", tc.want, joined)
			}
			if len(stub.ran) != 0 {
				t.Errorf("a host with no [grist-apps] must smoke nothing, ran %v", stub.ran)
			}
		})
	}
}

// mw-gq6.728: the smoke a landing on another host handed to the home is made by
// the home's tick, once: the tick that moves the checkout smokes, and the next
// finds it level and does not.
func TestTheHomesTickSmokesALandingMadeElsewhereOnce(t *testing.T) {
	ctx := context.Background()
	stub := &smokeStub{}
	checkout := &movingCheckout{}
	level := GristLevel{
		Apps:     map[string]string{"lampas": "/home/lampas"},
		Rigs:     map[string]string{"lampas": "/home/lampas"},
		Checkout: checkout,
		Smoke: GristSmokeAfter{
			Touches: touchesOnly{GristSmokeGrindsPath: true}, Smoke: stub,
			Apps: map[string]string{"lampas": "/home/lampas"}, Rigs: map[string]string{"lampas": "/home/lampas"},
			Book: GristSmokeBook{Notes: smokeNoteStore{}, Host: "desktop"},
		},
		Host: "desktop",
	}
	for i := 0; i < 2; i++ {
		if err := level.Run(ctx); err != nil {
			t.Fatalf("tick %d: %v", i+1, err)
		}
	}
	if fmt.Sprint(stub.ran) != "[lampas]" {
		t.Fatalf("expected lampas smoked once on the home, got %v", stub.ran)
	}
}
