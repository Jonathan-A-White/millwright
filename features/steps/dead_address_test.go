package steps

import (
	"net"
	"strings"
	"testing"
	"time"
)

// A scenario's "dead" address must stay dead while other tests run in
// parallel: a port picked by closing a listener can be bound again by one of
// them (mw-gq6.1031). The test takes the part of that other test: it binds
// the port the address names, when it can, and the address must still refuse.
func TestDeadProxyStaysDeadWhenItsPortIsTakenAgain(t *testing.T) {
	proxy, err := refusedProxy()
	if err != nil {
		t.Fatal(err)
	}
	address := strings.TrimPrefix(proxy, "http://")
	if ln, err := net.Listen("tcp", address); err == nil {
		defer ln.Close()
	}
	conn, err := net.DialTimeout("tcp", address, 2*time.Second)
	if err == nil {
		conn.Close()
		t.Fatalf("the dead proxy %s answered after its port was taken again", proxy)
	}
}
