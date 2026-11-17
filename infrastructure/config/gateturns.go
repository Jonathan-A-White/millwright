package config

import (
	"fmt"
	"strconv"
	"time"
)

// GateTurnsLimitEnv overrides the root-table gate_turns_limit_minutes key.
const GateTurnsLimitEnv = "MW_GATE_TURNS_LIMIT_MINUTES"

// GateTurnsLimit reports how long the turns of one gate on this host (a batch's,
// or a close-out's of a rig whose gate is named as parts) may wait for other gates
// in all before the gate keeps the lock for its remaining parts (mw-gq6.1420):
// $MW_GATE_TURNS_LIMIT_MINUTES if it is set, otherwise the root-table
// `gate_turns_limit_minutes` key of ~/.config/mw/config.toml, and 0 when neither
// says, which is application.DefaultGateTurnsLimit.
func GateTurnsLimit() (time.Duration, error) {
	said, err := optionalSetting("gate_turns_limit_minutes", GateTurnsLimitEnv, "")
	if err != nil || said == "" {
		return 0, err
	}
	minutes, err := strconv.Atoi(said)
	if err != nil || minutes < 1 {
		return 0, fmt.Errorf("the limit of a gate's turns is %q, which is not a whole number of minutes over 0: set %s=<minutes>, or `gate_turns_limit_minutes = <minutes>` in %s", said, GateTurnsLimitEnv, File)
	}
	return time.Duration(minutes) * time.Minute, nil
}
