package application

import (
	"strconv"
	"strings"
	"time"
)

// BashMaxTimeoutEnv is the variable Claude Code reads for the longest a foreground
// Bash call may be given: its default is 600000 milliseconds, and a larger value
// raises it. A rig's file may give its Builders a longer call (builder_call), and
// a story's session on that rig is started with this set to it.
const BashMaxTimeoutEnv = "BASH_MAX_TIMEOUT_MS"

// DefaultBashMaxTimeout is the cap on a foreground Bash call when
// BashMaxTimeoutEnv says nothing larger: 600 seconds.
const DefaultBashMaxTimeout = 600 * time.Second

// CallReserve is how much of a long call mw check leaves unspent, so that it ends
// in words inside the call rather than in the tool's own silent timeout.
const CallReserve = 50 * time.Second

// BuilderCallMin and BuilderCallMax are what a rig's builder_call may be: no less
// than ten minutes, which is a call worth the key, and no more than 55 minutes, so
// that one call ends inside the harness's one-hour prompt cache.
const (
	BuilderCallMin = 10 * time.Minute
	BuilderCallMax = 55 * time.Minute
)

// CheckCallLimits are the three times a foreground mw check keeps to: how much of
// its tool call it may spend (Budget), the most it waits for the slot, the gate
// lock, a calm host and a part or the specs still running (Wait), and how long its
// place in the queue is kept when a call ends (Keep).
type CheckCallLimits struct {
	Budget time.Duration
	Wait   time.Duration
	Keep   time.Duration
}

// CheckCallLimitsFor reads the limits from BASH_MAX_TIMEOUT_MS as the check's own
// environment holds it. Unset, empty, not a whole number of milliseconds, or no
// more than the 600 seconds a call is given anyway, the limits are today's: 540
// seconds of the call, nine minutes of waiting and a place kept ten minutes. A
// longer call gives the budget of that call less CallReserve, and the waits may
// spend all of it; the place is kept ten minutes as ever.
func CheckCallLimitsFor(maxTimeoutMs string) CheckCallLimits {
	today := CheckCallLimits{Budget: CheckCallBudget, Wait: CheckPartSlotWait, Keep: CheckKeepPlace}
	ms, err := strconv.ParseInt(strings.TrimSpace(maxTimeoutMs), 10, 64)
	if err != nil || ms <= 0 {
		return today
	}
	call := time.Duration(ms) * time.Millisecond
	if call <= DefaultBashMaxTimeout {
		return today
	}
	budget := call - CallReserve
	return CheckCallLimits{Budget: budget, Wait: budget, Keep: CheckKeepPlace}
}
