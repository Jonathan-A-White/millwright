package doctor

import (
	"context"
	"net"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

// reachDialTimeout is how long Reach gives one host to resolve and
// TCP-connect before trying the next.
const reachDialTimeout = 5 * time.Second

// Reach is ok if any of hosts resolves and TCP-connects within
// reachDialTimeout: the one way every check that needs to know whether the
// internet itself is up asks, so a dead internet reads the same to all of
// them.
func Reach(ctx context.Context, hosts []string) bool {
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

// NetReach adapts Reach to application.Reach, over a fixed set of hosts: the
// same test the wifi and tunnel checks make, asked here on the Millhand
// tick's behalf so a local fault reads the same way in all three places.
type NetReach struct {
	Hosts []string
}

var _ application.Reach = NetReach{}

// Reachable implements application.Reach.
func (n NetReach) Reachable(ctx context.Context) bool {
	return Reach(ctx, n.Hosts)
}
