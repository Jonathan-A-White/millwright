package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/eventlog"
)

// followerUnit is the user unit that runs mw events follow.
const followerUnit = "mw-view-follow.service"

// followerStallCheck is mw doctor's follower-stalled check over this host's
// follower, event log and view file (mw-gq6.893).
func followerStallCheck(gateway application.BeadFeed) (*application.FollowerStallCheck, error) {
	logPath, err := config.EventsLogPath()
	if err != nil {
		return nil, err
	}
	viewPath, err := config.PosternViewPath()
	if err != nil {
		return nil, err
	}
	return &application.FollowerStallCheck{
		Up:   followerUp,
		Log:  eventlog.New(logPath),
		Feed: gateway,
		ViewWritten: func(context.Context) (time.Time, bool, error) {
			info, err := os.Stat(viewPath)
			if errors.Is(err, os.ErrNotExist) {
				return time.Time{}, false, nil
			}
			if err != nil {
				return time.Time{}, false, err
			}
			return info.ModTime(), true, nil
		},
	}, nil
}

// followerUp says whether the follower's user unit is active here: false where
// the unit is not installed or systemd cannot be asked, which is a host that
// does not run it, or one unit-failed speaks for.
func followerUp(ctx context.Context) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	err := exec.CommandContext(ctx, "systemctl", "--user", "is-active", "--quiet", followerUnit).Run()
	if err == nil {
		return true, nil
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return false, nil
	}
	if errors.Is(err, exec.ErrNotFound) {
		return false, nil
	}
	return false, err
}
