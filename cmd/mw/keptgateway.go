package main

import (
	"io"

	"github.com/Jonathan-A-White/millwright/infrastructure/beads"
)

// keepBeadsConnection makes every Gateway built until the returned stop is
// called read over one connection to the beads server, kept open for the
// command's life, and start a bd only when that connection fails (said on
// said): a command that reads many times in one run starts no bd for each, and
// each bd costs the server several new connections (mw-gq6.600, mw-gq6.608).
// more are the command's other options for those Gateways. A host with no
// server to reach leaves the Gateways as they were, reading by bd.
func keepBeadsConnection(vault string, said io.Writer, more ...beads.Option) (stop func()) {
	stop, _ = keepBeadsConnectionIfServed(vault, said, more...)
	return stop
}

// openBeadsServer is how a command reaches the vault's beads server: a test
// swaps it for a stand-in.
var openBeadsServer = beads.ServerHeadDB

// keepBeadsConnectionIfServed is keepBeadsConnection that also says whether
// there is a server to reach (false on a host that reads by bd), for a command
// whose reads are better made over the connection than by bd only when there
// is one.
func keepBeadsConnectionIfServed(vault string, said io.Writer, more ...beads.Option) (stop func(), served bool) {
	options := append(more, beads.WithKeptSaid(said))
	closeDB := func() {}
	if db := openBeadsServer(vault); db != nil {
		options = append(options, beads.WithHeadDB(db))
		closeDB = func() { db.Close() }
		served = true
	}
	followerGatewayOptions = options
	return func() {
		followerGatewayOptions = nil
		closeDB()
	}, served
}
