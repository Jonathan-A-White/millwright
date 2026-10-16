package main

import (
	"context"
	"io"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/flushlog"
)

// beadsFlushSpringJob is the follower's job that commits the beads database's
// pending writes every application.BeadsFlushEvery, for bd's batch auto-commit
// mode (mw-gq6.508): a write in that mode is in no commit, so in no backup,
// until someone flushes. It works only while this host is home, for the home is
// where the database lives, and leaves the record mw doctor's beads-flush check
// reads. A working set that is already clean is a pass.
func beadsFlushSpringJob(host string, out io.Writer) (application.SpringJob, error) {
	dir, err := config.Vault()
	if err != nil {
		return application.SpringJob{}, err
	}
	file, err := config.BeadsFlushFile()
	if err != nil {
		return application.SpringJob{}, err
	}
	return application.ClockJob("beads-flush", application.BeadsFlushEvery, func(ctx context.Context) error {
		if at, err := application.IsHome(ctx, mwVault(dir, host), host); err != nil || !at {
			return err
		}
		return application.BeadsFlush{Tracker: mwGateway(dir, host), Log: flushlog.New(file)}.Run(ctx)
	}), nil
}
