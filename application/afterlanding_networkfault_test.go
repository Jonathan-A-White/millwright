package application_test

import (
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

func TestARanIsANetworkFaultOnlyWhenItFailedThatWay(t *testing.T) {
	cases := []struct {
		name string
		ran  application.Ran
		want bool
	}{
		{"ok", application.Ran{Status: 0}, false},
		{"ssh exit 255", application.Ran{Status: 255}, true},
		{"dns in the output", application.Ran{Status: 1, Output: "ssh: Could not resolve hostname allmymind.org: Temporary failure in name resolution\n"}, true},
		{"connection refused", application.Ran{Status: 1, Output: "curl: (7) Failed to connect: Connection refused"}, true},
		{"a plain build failure", application.Ran{Status: 2, Output: "make: *** No rule to make target 'build'"}, false},
		{"stopped at the limit", application.Ran{Status: -1, TimedOut: time.Minute}, false},
		{"a fault said long before the tail", application.Ran{Status: 1, Output: "Connection refused\n" + manyLines(30) + "compile error\n"}, false},
	}
	for _, c := range cases {
		if got := c.ran.NetworkFault(); got != c.want {
			t.Errorf("%s: NetworkFault() = %v, want %v", c.name, got, c.want)
		}
	}
}

func manyLines(n int) string {
	out := ""
	for i := 0; i < n; i++ {
		out += "building\n"
	}
	return out
}
