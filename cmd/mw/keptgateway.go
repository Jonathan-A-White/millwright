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
	options := append(more, beads.WithKeptSaid(said))
	closeDB := func() {}
	if db := beads.ServerHeadDB(vault); db != nil {
		options = append(options, beads.WithHeadDB(db))
		closeDB = func() { db.Close() }
	}
	followerGatewayOptions = options
	return func() {
		followerGatewayOptions = nil
		closeDB()
	}
}
