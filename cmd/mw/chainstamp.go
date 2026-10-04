package main

import (
	"context"
	"io"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/rig"
	"github.com/Jonathan-A-White/millwright/infrastructure/stampqueue"
)

// chainStampEvery is how often the follower looks for queued chain stamps to
// broadcast: a failed one is retried on the next look.
const chainStampEvery = time.Minute

// stampQueue is this host's chain stamp queue, in the directory config names.
func stampQueue() (*stampqueue.Queue, error) {
	dir, err := config.StampsDir()
	if err != nil {
		return nil, err
	}
	return stampqueue.New(dir), nil
}

// chainStampRun is one pass of the chain-stamp job. It builds what it works
// with on every pass, so that a key or a setting put right is read the next
// minute, and a host that has nothing queued needs neither: a pass over an
// empty queue touches no key and no backend.
func chainStampRun(out io.Writer) func(context.Context) error {
	return func(ctx context.Context) error {
		queue, err := stampQueue()
		if err != nil {
			return err
		}
		pending, err := queue.Pending(ctx)
		if err != nil {
			return err
		}
		if len(pending) == 0 {
			return nil
		}
		keys, err := posternKeys()
		if err != nil {
			return err
		}
		governorKey, err := config.PosternGovernorKey()
		if err != nil {
			return err
		}
		backend, err := posternBackend(keys)
		if err != nil {
			return err
		}
		gateway, _, err := posternGateway()
		if err != nil {
			return err
		}
		rigs, err := config.Rigs()
		if err != nil {
			return err
		}
		return application.ChainStamp{
			Queue:       queue,
			Postern:     backend,
			Keys:        keys,
			Cipher:      posternCipher(keys),
			Tracker:     gateway,
			GovernorKey: governorKey,
			Notes:       rig.New(),
			Rigs:        rigs,
			Err:         out,
		}.Run(ctx)
	}
}
