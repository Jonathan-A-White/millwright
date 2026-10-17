package application

import (
	"context"
	"strings"
	"testing"
)

// mw-gq6.562: a landing on a host that names no [after_landing] for the rig says
// the home deploys it, not that nothing does.
func TestALandingOnAHostWithNoCommandSaysItWasHandedToTheHome(t *testing.T) {
	cases := []struct {
		name, host, home string
		want             string
	}{
		{"another host is home", "cloud1", "desktop 2026-10-01T05:00:00Z mayor\n", "after landing: handed to the home (desktop): this host names no command for postern"},
		{"this host is home", "desktop", "desktop 2026-10-01T05:00:00Z mayor\n", "after landing: none: this host names no command for postern"},
		{"the home cannot be told", "cloud1", "", "after landing: none: this host names no command for postern"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			n := Next{AfterLanding: &noCommand{}, Tracker: &smokeComments{}, Host: tc.host}
			if tc.home != "" {
				n.Home = homeText(tc.home)
			}
			c := &closeOut{id: "mw-1.1", target: "main", rigDir: "/rigs/postern"}
			c.path.Rig = "postern"
			report := &NextReport{Rig: Advanced{Moved: true}, Landed: true}

			n.afterLanding(context.Background(), c, report)

			if joined := strings.Join(report.Notes, "\n"); !strings.Contains(joined, tc.want) {
				t.Errorf("expected %q in the notes, got %q", tc.want, joined)
			}
		})
	}
}

// noCommand is a host whose [after_landing] table names nothing.
type noCommand struct{}

func (noCommand) Command(string) string                            { return "" }
func (noCommand) Run(context.Context, string, string) (Ran, error) { return Ran{}, nil }

// homeText is a vault's home file, read for a fixed answer.
type homeText string

func (h homeText) ReadHome(context.Context) (string, error) { return string(h), nil }
