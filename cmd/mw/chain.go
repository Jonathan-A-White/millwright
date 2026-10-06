package main

import (
	"io"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/bsv"
	"github.com/Jonathan-A-White/millwright/infrastructure/chainlookup"
	"github.com/Jonathan-A-White/millwright/infrastructure/postern"
)

// newChain is the chain every command that puts a record on chain, reads the
// postern key's balance or looks a transaction up gets: BSV testnet, its
// records signed by keys and broadcast through backend, its transactions read
// on WhatsOnChain. A send whose spent coins could not be remembered is said on
// warn; a nil warn says nothing. This is where another chain goes.
func newChain(backend *postern.HTTP, keys *postern.KeyFile, warn io.Writer) application.Chain {
	return bsv.New(backend, keys, chainlookup.New(""), warn)
}
