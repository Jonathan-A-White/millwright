package main

import (
	"time"

	"github.com/Jonathan-A-White/millwright/infrastructure/config"
)

// closePushWait is the base wait between the tries of a close that met a passing
// fault (application.CloseRetry), the same push wait mw next uses; a setting that
// cannot be read is the default.
func closePushWait() time.Duration {
	seconds, err := config.PushWaitSeconds()
	if err != nil {
		seconds = config.DefaultPushWaitSeconds
	}
	return time.Duration(seconds) * time.Second
}
