package pause

import (
	"context"
	"fmt"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"
)

// BuilderReach is application.BuilderReach: a line is typed into a story's
// session on another host by running `mw note <story> --type-here <line>` there
// over ssh, the same way a pause reaches a host.
type BuilderReach struct {
	// Reach and Gaps are Fleet's: an ssh prefix by host name, and the hosts left
	// out of it, each with why.
	Reach map[string]string
	Gaps  map[string]string
	// Run runs a program and returns its standard output; empty runs for real.
	Run func(ctx context.Context, argv []string) (string, error)
}

var _ application.BuilderReach = BuilderReach{}

// Type implements application.BuilderReach.
func (r BuilderReach) Type(ctx context.Context, host, storyID, line string) error {
	prefix := strings.Fields(r.Reach[host])
	if len(prefix) < 2 {
		if why := r.Gaps[host]; why != "" {
			return fmt.Errorf("no way to reach %s: %s", host, why)
		}
		return fmt.Errorf("no way to reach %s: set `%s = \"ssh <alias>\"` under [hands_hosts] in the config file", host, host)
	}
	argv := append(append(append([]string{}, prefix[:len(prefix)-1]...), "-o", "BatchMode=yes", "-o", "ConnectTimeout=15", prefix[len(prefix)-1]),
		"bash -lc "+quote("mw note "+quote(storyID)+" --type-here "+quote(line)))
	run := r.Run
	if run == nil {
		run = execute
	}
	if _, err := run(ctx, argv); err != nil {
		return fmt.Errorf("%s: %w", strings.Join(prefix, " "), err)
	}
	return nil
}
