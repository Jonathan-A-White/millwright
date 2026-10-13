package main

import (
	"context"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
)

// contributeBackSpringJob is the job that files the monthly contribute-back
// review: from day (UTC) of each month, on the home only, one research bead
// under mw-gq6 asking which code we built for a gap would be welcome upstream.
// It files a bead and sends nothing anywhere.
func contributeBackSpringJob(host string, day int) (application.SpringJob, error) {
	dir, err := config.Vault()
	if err != nil {
		return application.SpringJob{}, err
	}
	gateway := mwGateway(dir, host)
	review := &application.ContributeBack{
		Day: day, Epic: application.ContributeBackEpic, Tracker: gateway, Notes: gateway,
		Home: func(ctx context.Context) (bool, error) {
			return application.IsHome(ctx, mwVault(dir, host), host)
		},
	}
	return review.Job(), nil
}
