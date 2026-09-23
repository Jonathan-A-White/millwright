package doctor

import (
	"context"
	"net"
	"time"
)

// reachDialTimeout is how long reach gives one host to resolve and
// TCP-connect before trying the next.
const reachDialTimeout = 5 * time.Second

// reach is ok if any of hosts resolves and TCP-connects within
// reachDialTimeout: the one way every check that needs to know whether the
// internet itself is up asks, so a dead internet reads the same to all of
// them.
func reach(ctx context.Context, hosts []string) bool {
	for _, host := range hosts {
		dialer := net.Dialer{Timeout: reachDialTimeout}
		conn, err := dialer.DialContext(ctx, "tcp", host)
		if err == nil {
			conn.Close()
			return true
		}
	}
	return false
}
