// Package config finds the factory's settings on the machine it is running
// on: where the vault is, which host this machine is, how many sessions may
// run here at once, and where each rig is checked out. Each is read from the
// environment or from ~/.config/mw/config.toml, which looks like this:
//
//	vault = "/root/millwright-vault"
//	host  = "vps"
//	cap   = 1
//	host_silent_hours = 2
//	handoff_at = 180000
//	rig_memory_bytes = 8000
//	dispatch_sync_tries = 3
//	dispatch_sync_wait = "15s"
//	push_tries = 3
//	push_wait_seconds = 20
//	millhand_routine_model = "sonnet"
//	millhand_review_model = "opus"
//
//	[rigs]
//	millwright = "/root/millwright"
//
//	[watch]
//	ssh     = "vps"
//	host    = "vps"
//	outside = ["https://example.com", "https://www.wikipedia.org"]
//	blog    = "https://blog.example.com"
//
//	[doctor]
//	units        = ["mw-dispatch.service", "mw-millhand-tick.service"]
//	state_dir    = "/root/.local/state/mw-doctor"
//	reach        = ["api.anthropic.com:443", "github.com:443"]
//	powershell   = "/mnt/c/Windows/System32/WindowsPowerShell/v1.0/powershell.exe"
//	tunnel_unit  = "reverse-tunnel.service"
//	tunnel_host  = "vps"
//	tunnel_probe = "ss -ltn sport = :2222"
package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// The environment variables that answer for each setting, ahead of the file.
const (
	VaultEnv       = "MW_VAULT"
	HostEnv        = "MW_HOST"
	CapEnv         = "MW_CAP"
	StaleHoursEnv  = "MW_STALE_HOURS"
	HostSilenceEnv = "MW_HOST_SILENT_HOURS"
	HandoffAtEnv   = "MW_HANDOFF_AT"
	RigMemoryEnv   = "MW_RIG_MEMORY_BYTES"

	NudgeAfterMinutesEnv     = "MW_NUDGE_AFTER_MINUTES"
	NudgeSyncStaleMinutesEnv = "MW_NUDGE_SYNC_STALE_MINUTES"

	TickRecheckEnv = "MW_TICK_RECHECK_SECONDS"

	DispatchSyncTriesEnv = "MW_DISPATCH_SYNC_TRIES"
	DispatchSyncWaitEnv  = "MW_DISPATCH_SYNC_WAIT"

	PushTriesEnv       = "MW_PUSH_TRIES"
	PushWaitSecondsEnv = "MW_PUSH_WAIT_SECONDS"

	MaxAttemptsEnv = "MW_MAX_ATTEMPTS"

	MillhandRoutineModelEnv = "MW_MILLHAND_ROUTINE_MODEL"
	MillhandReviewModelEnv  = "MW_MILLHAND_REVIEW_MODEL"
)

// RigsTable is the table of the config file that says where each rig is checked
// out on this host: rig name on the left, directory on the right. TestsTable is
// the table that says how each rig's own tests are run on this host: rig name on
// the left, a command line on the right. A rig that is not in it is tested the
// way DefaultTests says. AfterLandingTable is the table of the command each rig
// has run once a landing has moved this host's checkout of it; a rig that is not
// in it has none.
const (
	RigsTable         = "rigs"
	TestsTable        = "tests"
	AfterLandingTable = "after_landing"
)

// WatchTable is the table of the config file that says what `mw watch` looks at.
const WatchTable = "watch"

// DoctorTable is the table of the config file that says what `mw doctor` checks.
const DoctorTable = "doctor"

// DefaultCap is how many sessions may run at once on a host that does not say.
// One, because the smaller of the factory's two hosts has a single core and
// under a gigabyte of memory, and because two sessions racing is the expensive
// mistake to make by default.
const DefaultCap = 1

// DefaultMaxAttempts is how many times a story is started in all, its first
// dispatch and every one after a refusal or a giving back, before mw dispatch
// stops and tells the Mayor, when nothing says otherwise. It is a fuel knob: each
// attempt is a fresh session paid for in full. application.DefaultMaxAttempts is
// the same number.
const DefaultMaxAttempts = 3

// DefaultStaleHours is how many hours a claimed story's session may show no
// new output before `mw sweep` calls it stuck, when nothing says otherwise.
const DefaultStaleHours = 2

// DefaultHostSilentHours is how long another host may go without recording a
// sync before `mw status` calls it asleep and its work stranded, when nothing
// says otherwise. Two hours, and the reason it is not one: a host's last_sync
// note reaches this host only on this host's own next sync, so the freshest
// reading of it can already be one cycle old. Two hours is two cycles of the
// hourly sync the factory runs, which is the smallest threshold that does not
// call a host asleep for the lag alone.
const DefaultHostSilentHours = 2

// DefaultNudgeAfterMinutes is how long a story claimed on this host may run
// with nothing landed, refused or blocked mailed to the Mayor about it before
// the mail notifier's quiet alarm names it, when nothing says otherwise.
const DefaultNudgeAfterMinutes = 60

// DefaultNudgeSyncStaleMinutes is how long another host's last recorded sync,
// as this host last heard it, may be behind before the mail notifier's quiet
// alarm names it, when nothing says otherwise. It is deliberately far shorter
// than DefaultHostSilentHours: the quiet alarm is meant to catch a sync gone
// stale in minutes, at night, before a person would otherwise notice.
const DefaultNudgeSyncStaleMinutes = 20

// DefaultTickRecheckSeconds is how long `mw millhand tick` waits between its two
// looks at a Millhand's pane before it closes the window of a finished one: the
// reaper's own half minute.
const DefaultTickRecheckSeconds = 30

// DefaultHandoffAt is the context size, in tokens, at which a seat's session
// must hand off when nothing says otherwise.
const DefaultHandoffAt = 180000

// DefaultRigMemoryBytes is how large a Builder's memory of one rig may grow
// before `mw status` says it is due to be pruned, when nothing says otherwise.
// Every Builder reads that file at boot, so its size is fuel paid on every
// story.
const DefaultRigMemoryBytes = 8000

// The models `mw millhand` wakes the Millhand on when nothing says otherwise: a
// routine wake and a wake by hand on Sonnet, a review wake on Opus. They are
// fuel knobs, so they are settings and not code.
const (
	DefaultMillhandRoutineModel = "sonnet"
	DefaultMillhandReviewModel  = "opus"
)

// What `mw dispatch` does when its sync cannot resolve a name, which is what a
// host just woken from standby says until its network is back: it tries the
// sync DefaultDispatchSyncTries times in all, DefaultDispatchSyncWait apart. They
// are settings and not code, and the two together may not wait past
// MaxDispatchSyncWait, which is well inside the two minutes the dispatch unit
// gives a run before systemd kills it.
const (
	DefaultDispatchSyncTries = 3
	DefaultDispatchSyncWait  = 15 * time.Second
	MaxDispatchSyncWait      = 90 * time.Second
)

// What `mw next` does when a push fails on a fault at the remote itself,
// worth trying again — never a reason the remote states and will never take
// back — when nothing says otherwise: it tries the push DefaultPushTries
// times in all, DefaultPushWaitSeconds apart. application.DefaultPushTries is
// the same number.
const (
	DefaultPushTries       = 3
	DefaultPushWaitSeconds = 20
)

// File is the config file's path under the home directory.
var File = filepath.Join(".config", "mw", "config.toml")

// Vault reports the directory holding the vault, and so the one beads
// database: $MW_VAULT if it is set, otherwise the root-table `vault` key of
// ~/.config/mw/config.toml.
func Vault() (string, error) {
	return setting("vault", VaultEnv)
}

// Host reports which of the factory's hosts this machine is — the name the
// Mayor writes into a story's Path, `vps` or `laptop`, not the machine's
// hostname: $MW_HOST if it is set, otherwise the root-table `host` key of
// ~/.config/mw/config.toml. It is what a session's seat is signed with and
// what the dispatcher asks beads for ready stories by.
func Host() (string, error) {
	return setting("host", HostEnv)
}

// Cap reports how many sessions may run on this machine at once: $MW_CAP if it
// is set, otherwise the root-table `cap` key of ~/.config/mw/config.toml, and
// DefaultCap when neither says. A host that says nothing runs one session at a
// time, which is the safe way round on a small machine.
func Cap() (int, error) {
	said := strings.TrimSpace(os.Getenv(CapEnv))
	if said == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return 0, fmt.Errorf("no %s is set and there is no home directory to read %s in: %w", CapEnv, File, err)
		}
		if said, err = valueIn(filepath.Join(home, File), "cap"); err != nil {
			return 0, err
		}
	}
	if said == "" {
		return DefaultCap, nil
	}

	atOnce, err := strconv.Atoi(said)
	if err != nil {
		return 0, fmt.Errorf("the cap on sessions running at once is %q, which is not a whole number: set %s=<n>, or `cap = <n>` in %s", said, CapEnv, File)
	}
	if atOnce < 1 {
		return 0, fmt.Errorf("the cap on sessions running at once is %d, so nothing could ever be started: set it to 1 or more", atOnce)
	}
	return atOnce, nil
}

// MaxAttempts reports how many times a story may be started in all: $MW_MAX_ATTEMPTS
// if it is set, otherwise the root-table `max_attempts` key of
// ~/.config/mw/config.toml, and DefaultMaxAttempts when neither says.
func MaxAttempts() (int, error) {
	said, err := optionalSetting("max_attempts", MaxAttemptsEnv, "")
	if err != nil {
		return 0, err
	}
	if said == "" {
		return DefaultMaxAttempts, nil
	}
	tries, err := strconv.Atoi(said)
	if err != nil {
		return 0, fmt.Errorf("the number of times a story is tried is %q, which is not a whole number: set %s=<n>, or `max_attempts = <n>` in %s", said, MaxAttemptsEnv, File)
	}
	if tries < 1 {
		return 0, fmt.Errorf("a story would be tried %d times, so none would ever be started: set max_attempts to 1 or more", tries)
	}
	return tries, nil
}

// StaleHours reports how many hours a claimed story's session may show no new
// output before `mw sweep` calls it stuck: $MW_STALE_HOURS if it is set,
// otherwise the root-table `stale_hours` key of ~/.config/mw/config.toml, and
// DefaultStaleHours when neither says.
func StaleHours() (int, error) {
	said := strings.TrimSpace(os.Getenv(StaleHoursEnv))
	if said == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return 0, fmt.Errorf("no %s is set and there is no home directory to read %s in: %w", StaleHoursEnv, File, err)
		}
		if said, err = valueIn(filepath.Join(home, File), "stale_hours"); err != nil {
			return 0, err
		}
	}
	if said == "" {
		return DefaultStaleHours, nil
	}

	hours, err := strconv.Atoi(said)
	if err != nil {
		return 0, fmt.Errorf("the stale threshold is %q, which is not a whole number of hours: set %s=<n>, or `stale_hours = <n>` in %s", said, StaleHoursEnv, File)
	}
	if hours < 1 {
		return 0, fmt.Errorf("the stale threshold is %d hours, so a session would be called stuck the moment it was claimed: set it to 1 or more", hours)
	}
	return hours, nil
}

// HostSilentHours reports how long another host may go without recording a
// sync before `mw status` calls it asleep and lists its work as stranded:
// $MW_HOST_SILENT_HOURS if it is set, otherwise the root-table
// `host_silent_hours` key of ~/.config/mw/config.toml, and
// DefaultHostSilentHours when neither says.
func HostSilentHours() (int, error) {
	said := strings.TrimSpace(os.Getenv(HostSilenceEnv))
	if said == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return 0, fmt.Errorf("no %s is set and there is no home directory to read %s in: %w", HostSilenceEnv, File, err)
		}
		if said, err = valueIn(filepath.Join(home, File), "host_silent_hours"); err != nil {
			return 0, err
		}
	}
	if said == "" {
		return DefaultHostSilentHours, nil
	}

	hours, err := strconv.Atoi(said)
	if err != nil {
		return 0, fmt.Errorf("the host silence threshold is %q, which is not a whole number of hours: set %s=<n>, or `host_silent_hours = <n>` in %s", said, HostSilenceEnv, File)
	}
	if hours < 1 {
		return 0, fmt.Errorf("the host silence threshold is %d hours, so every other host would be called asleep the moment it synced: set it to 1 or more", hours)
	}
	return hours, nil
}

// NudgeAfterMinutes reports how long a story claimed on this host may run with
// nothing mailed to the Mayor about it before the mail notifier's quiet alarm
// names it: $MW_NUDGE_AFTER_MINUTES if it is set, otherwise the root-table
// `nudge_after_minutes` key of ~/.config/mw/config.toml, and
// DefaultNudgeAfterMinutes when neither says.
func NudgeAfterMinutes() (int, error) {
	said := strings.TrimSpace(os.Getenv(NudgeAfterMinutesEnv))
	if said == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return 0, fmt.Errorf("no %s is set and there is no home directory to read %s in: %w", NudgeAfterMinutesEnv, File, err)
		}
		if said, err = valueIn(filepath.Join(home, File), "nudge_after_minutes"); err != nil {
			return 0, err
		}
	}
	if said == "" {
		return DefaultNudgeAfterMinutes, nil
	}

	minutes, err := strconv.Atoi(said)
	if err != nil {
		return 0, fmt.Errorf("the quiet alarm's story limit is %q, which is not a whole number of minutes: set %s=<n>, or `nudge_after_minutes = <n>` in %s", said, NudgeAfterMinutesEnv, File)
	}
	if minutes < 1 {
		return 0, fmt.Errorf("the quiet alarm's story limit is %d minutes, so a claimed story would be named the moment it was: set it to 1 or more", minutes)
	}
	return minutes, nil
}

// NudgeSyncStaleMinutes reports how long another host's last recorded sync may
// be behind before the mail notifier's quiet alarm names it: $MW_NUDGE_SYNC_STALE_MINUTES
// if it is set, otherwise the root-table `nudge_sync_stale_minutes` key of
// ~/.config/mw/config.toml, and DefaultNudgeSyncStaleMinutes when neither says.
func NudgeSyncStaleMinutes() (int, error) {
	said := strings.TrimSpace(os.Getenv(NudgeSyncStaleMinutesEnv))
	if said == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return 0, fmt.Errorf("no %s is set and there is no home directory to read %s in: %w", NudgeSyncStaleMinutesEnv, File, err)
		}
		if said, err = valueIn(filepath.Join(home, File), "nudge_sync_stale_minutes"); err != nil {
			return 0, err
		}
	}
	if said == "" {
		return DefaultNudgeSyncStaleMinutes, nil
	}

	minutes, err := strconv.Atoi(said)
	if err != nil {
		return 0, fmt.Errorf("the quiet alarm's sync staleness limit is %q, which is not a whole number of minutes: set %s=<n>, or `nudge_sync_stale_minutes = <n>` in %s", said, NudgeSyncStaleMinutesEnv, File)
	}
	if minutes < 1 {
		return 0, fmt.Errorf("the quiet alarm's sync staleness limit is %d minutes, so another host would be named the moment it synced: set it to 1 or more", minutes)
	}
	return minutes, nil
}

// TickRecheckSeconds reports how many seconds `mw millhand tick` waits between
// its two looks at a Millhand's pane: $MW_TICK_RECHECK_SECONDS if it is set,
// otherwise the root-table `tick_recheck_seconds` key of
// ~/.config/mw/config.toml, and DefaultTickRecheckSeconds when neither says.
// Zero is no wait.
func TickRecheckSeconds() (int, error) {
	said := strings.TrimSpace(os.Getenv(TickRecheckEnv))
	if said == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return 0, fmt.Errorf("no %s is set and there is no home directory to read %s in: %w", TickRecheckEnv, File, err)
		}
		if said, err = valueIn(filepath.Join(home, File), "tick_recheck_seconds"); err != nil {
			return 0, err
		}
	}
	if said == "" {
		return DefaultTickRecheckSeconds, nil
	}

	seconds, err := strconv.Atoi(said)
	if err != nil {
		return 0, fmt.Errorf("the tick's recheck wait is %q, which is not a whole number of seconds: set %s=<n>, or `tick_recheck_seconds = <n>` in %s", said, TickRecheckEnv, File)
	}
	if seconds < 0 {
		return 0, fmt.Errorf("the tick's recheck wait is %d seconds, which is before the first look: set it to 0 or more", seconds)
	}
	return seconds, nil
}

// RigMemoryBytes reports how large a Builder's memory of one rig may grow
// before `mw status` warns that it is due to be pruned: $MW_RIG_MEMORY_BYTES if
// it is set, otherwise the root-table `rig_memory_bytes` key of
// ~/.config/mw/config.toml, and DefaultRigMemoryBytes when neither says.
func RigMemoryBytes() (int, error) {
	said := strings.TrimSpace(os.Getenv(RigMemoryEnv))
	if said == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return 0, fmt.Errorf("no %s is set and there is no home directory to read %s in: %w", RigMemoryEnv, File, err)
		}
		if said, err = valueIn(filepath.Join(home, File), "rig_memory_bytes"); err != nil {
			return 0, err
		}
	}
	if said == "" {
		return DefaultRigMemoryBytes, nil
	}

	bytes, err := strconv.Atoi(said)
	if err != nil {
		return 0, fmt.Errorf("the rig memory budget is %q, which is not a whole number of bytes: set %s=<n>, or `rig_memory_bytes = <n>` in %s", said, RigMemoryEnv, File)
	}
	if bytes < 1 {
		return 0, fmt.Errorf("the rig memory budget is %d bytes, so every rig's memory would be over it: set it to 1 or more", bytes)
	}
	return bytes, nil
}

// HandoffAt reports the context size, in tokens, at which a seat's session must
// hand off: $MW_HANDOFF_AT if it is set, otherwise the root-table `handoff_at`
// key of ~/.config/mw/config.toml, and DefaultHandoffAt when neither says.
func HandoffAt() (int, error) {
	said := strings.TrimSpace(os.Getenv(HandoffAtEnv))
	if said == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return 0, fmt.Errorf("no %s is set and there is no home directory to read %s in: %w", HandoffAtEnv, File, err)
		}
		if said, err = valueIn(filepath.Join(home, File), "handoff_at"); err != nil {
			return 0, err
		}
	}
	if said == "" {
		return DefaultHandoffAt, nil
	}

	tokens, err := strconv.Atoi(said)
	if err != nil {
		return 0, fmt.Errorf("the handoff limit is %q, which is not a whole number of tokens: set %s=<n>, or `handoff_at = <n>` in %s", said, HandoffAtEnv, File)
	}
	if tokens < 1 {
		return 0, fmt.Errorf("the handoff limit is %d tokens, so every session would be told to hand off at once: set it to 1 or more", tokens)
	}
	return tokens, nil
}

// DispatchSyncTries reports how many times `mw dispatch` tries its sync when it
// fails because a name could not be resolved: $MW_DISPATCH_SYNC_TRIES if it is
// set, otherwise the root-table `dispatch_sync_tries` key of
// ~/.config/mw/config.toml, and DefaultDispatchSyncTries when neither says. One
// is no retry.
func DispatchSyncTries() (int, error) {
	tries, _, err := dispatchSync()
	return tries, err
}

// DispatchSyncWait reports how long `mw dispatch` waits between those tries:
// $MW_DISPATCH_SYNC_WAIT if it is set, otherwise the root-table
// `dispatch_sync_wait` key of ~/.config/mw/config.toml, written as a duration
// (`"15s"`), and DefaultDispatchSyncWait when neither says. Zero is allowed.
func DispatchSyncWait() (time.Duration, error) {
	_, wait, err := dispatchSync()
	return wait, err
}

// dispatchSync reads both knobs and refuses a pair that would wait past what
// the dispatch unit allows, so that a run is never killed for waiting.
func dispatchSync() (int, time.Duration, error) {
	tries := DefaultDispatchSyncTries
	said, err := optionalSetting("dispatch_sync_tries", DispatchSyncTriesEnv, "")
	if err != nil {
		return 0, 0, err
	}
	if said != "" {
		if tries, err = strconv.Atoi(said); err != nil {
			return 0, 0, fmt.Errorf("the number of times dispatch tries its sync is %q, which is not a whole number: set %s=<n>, or `dispatch_sync_tries = <n>` in %s", said, DispatchSyncTriesEnv, File)
		}
		if tries < 1 {
			return 0, 0, fmt.Errorf("dispatch would try its sync %d times, so it would never sync: set it to 1 or more (1 is no retry)", tries)
		}
	}

	wait := DefaultDispatchSyncWait
	if said, err = optionalSetting("dispatch_sync_wait", DispatchSyncWaitEnv, ""); err != nil {
		return 0, 0, err
	}
	if said != "" {
		if wait, err = time.ParseDuration(said); err != nil {
			return 0, 0, fmt.Errorf("the wait between dispatch's sync tries is %q, which is not a duration such as 15s: set %s=<duration>, or `dispatch_sync_wait = \"<duration>\"` in %s", said, DispatchSyncWaitEnv, File)
		}
		if wait < 0 {
			return 0, 0, fmt.Errorf("the wait between dispatch's sync tries is %s, which is negative: set it to 0 or more", wait)
		}
	}

	if total := time.Duration(tries-1) * wait; total > MaxDispatchSyncWait {
		return 0, 0, fmt.Errorf("dispatch would wait %s in all (%d tries, %s apart), and the dispatch unit is stopped at 2 minutes: keep the waiting to %s or less",
			total, tries, wait, MaxDispatchSyncWait)
	}
	return tries, wait, nil
}

// PushTries reports how many times `mw next` tries a push again when it fails
// on a fault at the remote itself, worth trying again: $MW_PUSH_TRIES if it is
// set, otherwise the root-table `push_tries` key of ~/.config/mw/config.toml,
// and DefaultPushTries when neither says. One is no retry.
func PushTries() (int, error) {
	said, err := optionalSetting("push_tries", PushTriesEnv, "")
	if err != nil {
		return 0, err
	}
	if said == "" {
		return DefaultPushTries, nil
	}
	tries, err := strconv.Atoi(said)
	if err != nil {
		return 0, fmt.Errorf("the number of times a push is tried is %q, which is not a whole number: set %s=<n>, or `push_tries = <n>` in %s", said, PushTriesEnv, File)
	}
	if tries < 1 {
		return 0, fmt.Errorf("a push would be tried %d times, so it would never be tried at all: set push_tries to 1 or more (1 is no retry)", tries)
	}
	return tries, nil
}

// PushWaitSeconds reports how many seconds `mw next` waits between those
// tries: $MW_PUSH_WAIT_SECONDS if it is set, otherwise the root-table
// `push_wait_seconds` key of ~/.config/mw/config.toml, and
// DefaultPushWaitSeconds when neither says. Zero is allowed, so a test never
// sleeps.
func PushWaitSeconds() (int, error) {
	said, err := optionalSetting("push_wait_seconds", PushWaitSecondsEnv, "")
	if err != nil {
		return 0, err
	}
	if said == "" {
		return DefaultPushWaitSeconds, nil
	}
	seconds, err := strconv.Atoi(said)
	if err != nil {
		return 0, fmt.Errorf("the wait between push tries is %q, which is not a whole number of seconds: set %s=<n>, or `push_wait_seconds = <n>` in %s", said, PushWaitSecondsEnv, File)
	}
	if seconds < 0 {
		return 0, fmt.Errorf("the wait between push tries is %d seconds, which is negative: set it to 0 or more", seconds)
	}
	return seconds, nil
}

// MillhandRoutineModel reports the model a routine wake, and a wake by hand, of
// the Millhand runs on: $MW_MILLHAND_ROUTINE_MODEL if it is set, otherwise the
// root-table `millhand_routine_model` key of ~/.config/mw/config.toml, and
// DefaultMillhandRoutineModel when neither says. Whether it names a model the
// factory runs on is for the harness to say when a session is started on it.
func MillhandRoutineModel() (string, error) {
	return optionalSetting("millhand_routine_model", MillhandRoutineModelEnv, DefaultMillhandRoutineModel)
}

// MillhandReviewModel reports the model a review wake of the Millhand runs on:
// $MW_MILLHAND_REVIEW_MODEL if it is set, otherwise the root-table
// `millhand_review_model` key of ~/.config/mw/config.toml, and
// DefaultMillhandReviewModel when neither says.
func MillhandReviewModel() (string, error) {
	return optionalSetting("millhand_review_model", MillhandReviewModelEnv, DefaultMillhandReviewModel)
}

// Rigs reports where each rig the factory works is checked out on this machine,
// by rig name, read from the `[rigs]` table of ~/.config/mw/config.toml. A
// machine with no such table works no rigs, which is not an error here: it is
// the dispatcher that says so, naming the rig the story wanted.
func Rigs() (map[string]string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("there is no home directory to read %s in: %w", File, err)
	}
	path := filepath.Join(home, File)

	rigs, err := tableIn(path, RigsTable)
	if err != nil {
		return nil, err
	}
	for name, dir := range rigs {
		if !filepath.IsAbs(dir) {
			return nil, fmt.Errorf("the rig %s is at %q in %s: a rig's checkout is named by its full path", name, dir, path)
		}
	}
	return rigs, nil
}

// Tests reports how each rig's own tests are run on this machine, by rig name,
// read from the `[tests]` table of ~/.config/mw/config.toml. A rig that says
// nothing is tested by the adapter's own default, and a machine with no such
// table is not an error: `make test` is what most rigs mean by their tests.
//
// It is a command line rather than a program and its arguments because a rig
// says what its tests are in its own words, and because the factory must be
// able to point a check at something trivial — a test of mw itself must never
// run mw's own test suite inside itself.
func Tests() (map[string]string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("there is no home directory to read %s in: %w", File, err)
	}
	return tableIn(filepath.Join(home, File), TestsTable)
}

// AfterLanding reports the command each rig has run after a landing has moved
// this host's checkout of it, by rig name, read from the `[after_landing]` table
// of ~/.config/mw/config.toml: `millwright = "make build"` is how a host that
// runs its own mw from the rig's bin/ keeps it current. A rig that says nothing
// has nothing run, and a machine with no such table is not an error.
//
// It is a command line, as a test is in `[tests]`, and for the same reason: a
// rig says what it means in its own words, and the shell reads them.
func AfterLanding() (map[string]string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("there is no home directory to read %s in: %w", File, err)
	}
	return tableIn(filepath.Join(home, File), AfterLandingTable)
}

// WatchSettings is what the `[watch]` table says: how to reach the host that is
// watched, what it is called in beads, which two places outside both hosts say
// this host's own network is up, and the blog whose answering is a sign the host
// is alive.
type WatchSettings struct {
	SSH     string
	Host    string
	Outside []string
	Blog    string
}

// Watch reports what `mw watch` looks at, read from the `[watch]` table of
// ~/.config/mw/config.toml: `ssh` (the name ssh knows the watched host by),
// `host` (its name in beads), `outside` (a list of URLs, two of them) and
// `blog` (a URL). A machine with no such table watches nothing, which is not an
// error: it is the zero value, and `mw watch` says so. A table that names no
// ssh, no host or no outside place is one `mw watch` cannot use, and is refused
// saying what is missing; the blog alone is optional, and without it the blog is
// not one of the signs of life.
func Watch() (WatchSettings, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return WatchSettings{}, fmt.Errorf("there is no home directory to read %s in: %w", File, err)
	}
	path := filepath.Join(home, File)
	table, err := tableIn(path, WatchTable)
	if err != nil {
		return WatchSettings{}, err
	}
	if len(table) == 0 {
		return WatchSettings{}, nil
	}

	watch := WatchSettings{SSH: table["ssh"], Host: table["host"], Outside: list(table["outside"]), Blog: table["blog"]}
	var missing []string
	if watch.SSH == "" {
		missing = append(missing, "ssh")
	}
	if watch.Host == "" {
		missing = append(missing, "host")
	}
	if len(watch.Outside) == 0 {
		missing = append(missing, "outside")
	}
	if len(missing) > 0 {
		return WatchSettings{}, fmt.Errorf("the [%s] table of %s does not say %s: mw watch needs the ssh name of the host, its name in beads and the places outside that show this host's network is up",
			WatchTable, path, strings.Join(missing, ", "))
	}
	return watch, nil
}

// DefaultDoctorUnits are the user units mw doctor's daemon-reload check asks
// systemctl about when the [doctor] table says nothing: the units this rig
// ships.
var DefaultDoctorUnits = []string{
	"mw-dispatch.service", "mw-millhand-tick.service", "mw-millhand-review.service", "mw-doctor.service",
}

// DefaultDoctorStateDir is where mw doctor keeps its episode state and its
// log, under the home directory, when the [doctor] table says nothing.
var DefaultDoctorStateDir = filepath.Join(".local", "state", "mw-doctor")

// DoctorUnits reports the user units mw doctor's daemon-reload check asks
// systemctl about, read from the `[doctor]` table's `units` key of
// ~/.config/mw/config.toml, and DefaultDoctorUnits when the table says
// nothing.
func DoctorUnits() ([]string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("there is no home directory to read %s in: %w", File, err)
	}
	table, err := tableIn(filepath.Join(home, File), DoctorTable)
	if err != nil {
		return nil, err
	}
	if units := list(table["units"]); len(units) > 0 {
		return units, nil
	}
	return DefaultDoctorUnits, nil
}

// DoctorStateDir reports where mw doctor keeps its episode state and its log:
// the `[doctor]` table's `state_dir` key of ~/.config/mw/config.toml, a full
// path, and DefaultDoctorStateDir under the home directory when the table
// says nothing.
func DoctorStateDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("there is no home directory to read %s in: %w", File, err)
	}
	path := filepath.Join(home, File)
	table, err := tableIn(path, DoctorTable)
	if err != nil {
		return "", err
	}
	if dir := strings.TrimSpace(table["state_dir"]); dir != "" {
		if !filepath.IsAbs(dir) {
			return "", fmt.Errorf("the doctor's state_dir is %q in %s: it must be a full path", dir, path)
		}
		return dir, nil
	}
	return filepath.Join(home, DefaultDoctorStateDir), nil
}

// DefaultDoctorReach are the host:port pairs mw doctor's wifi check tries to
// reach when the [doctor] table says nothing: two places outside this
// factory, on different providers, so one of them being down is not mistaken
// for this host's own network being down.
var DefaultDoctorReach = []string{"api.anthropic.com:443", "github.com:443"}

// DefaultDoctorPowershell is where mw doctor's wifi check looks for
// powershell.exe when the [doctor] table says nothing: WSL's mount of the
// path Windows itself uses. Its presence is what tells the check it is
// running on a Windows-backed host at all; its absence makes the check inert.
const DefaultDoctorPowershell = "/mnt/c/Windows/System32/WindowsPowerShell/v1.0/powershell.exe"

// DoctorReach reports the host:port pairs mw doctor's wifi check probes for
// reachability, read from the `[doctor]` table's `reach` key of
// ~/.config/mw/config.toml, and DefaultDoctorReach when the table says
// nothing.
func DoctorReach() ([]string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("there is no home directory to read %s in: %w", File, err)
	}
	table, err := tableIn(filepath.Join(home, File), DoctorTable)
	if err != nil {
		return nil, err
	}
	if reach := list(table["reach"]); len(reach) > 0 {
		return reach, nil
	}
	return DefaultDoctorReach, nil
}

// DoctorPowershell reports the path to powershell.exe mw doctor's wifi check
// tests for, to tell whether this host is Windows-backed: the `[doctor]`
// table's `powershell` key of ~/.config/mw/config.toml, and
// DefaultDoctorPowershell when the table says nothing.
func DoctorPowershell() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("there is no home directory to read %s in: %w", File, err)
	}
	table, err := tableIn(filepath.Join(home, File), DoctorTable)
	if err != nil {
		return "", err
	}
	if powershell := strings.TrimSpace(table["powershell"]); powershell != "" {
		return powershell, nil
	}
	return DefaultDoctorPowershell, nil
}

// DefaultDoctorTunnelUnit is the user unit mw doctor's tunnel check restarts
// when the [doctor] table says nothing: this rig's own reverse tunnel.
const DefaultDoctorTunnelUnit = "reverse-tunnel.service"

// DefaultDoctorTunnelProbe is the command mw doctor's tunnel check runs over
// ssh on the VPS to check the tunnel's listener, when the [doctor] table
// says nothing.
const DefaultDoctorTunnelProbe = "ss -ltn sport = :2222"

// DoctorTunnelUnit reports the user unit mw doctor's tunnel check restarts
// when the tunnel is down: the `[doctor]` table's `tunnel_unit` key of
// ~/.config/mw/config.toml, and DefaultDoctorTunnelUnit when the table says
// nothing.
func DoctorTunnelUnit() (string, error) {
	return doctorTunnelSetting("tunnel_unit", DefaultDoctorTunnelUnit)
}

// DoctorTunnelProbe reports the command mw doctor's tunnel check runs over
// ssh on the VPS to check the tunnel's listener: the `[doctor]` table's
// `tunnel_probe` key of ~/.config/mw/config.toml, and
// DefaultDoctorTunnelProbe when the table says nothing.
func DoctorTunnelProbe() (string, error) {
	return doctorTunnelSetting("tunnel_probe", DefaultDoctorTunnelProbe)
}

// DoctorTunnelHost reports the VPS's ssh name mw doctor's tunnel check
// connects to: the `[doctor]` table's `tunnel_host` key of
// ~/.config/mw/config.toml, and the `[watch]` table's `host` when the
// [doctor] table says nothing — the tunnel this check minds carries the same
// ssh `mw watch` rides.
func DoctorTunnelHost() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("there is no home directory to read %s in: %w", File, err)
	}
	table, err := tableIn(filepath.Join(home, File), DoctorTable)
	if err != nil {
		return "", err
	}
	if host := strings.TrimSpace(table["tunnel_host"]); host != "" {
		return host, nil
	}
	watch, err := Watch()
	if err != nil {
		return "", err
	}
	return watch.Host, nil
}

// doctorTunnelSetting reads one [doctor] table key, and fallback when the
// table says nothing about it.
func doctorTunnelSetting(key, fallback string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("there is no home directory to read %s in: %w", File, err)
	}
	table, err := tableIn(filepath.Join(home, File), DoctorTable)
	if err != nil {
		return "", err
	}
	if value := strings.TrimSpace(table[key]); value != "" {
		return value, nil
	}
	return fallback, nil
}

// list reads a value that is a list of strings: `["a", "b"]`, or one string
// with commas in it, `"a, b"`. Nothing but the strings is kept, and none is
// empty.
func list(value string) []string {
	value = strings.TrimSpace(value)
	value = strings.TrimSuffix(strings.TrimPrefix(value, "["), "]")
	var items []string
	for _, item := range strings.Split(value, ",") {
		if item = strings.Trim(strings.TrimSpace(item), `"'`); item != "" {
			items = append(items, item)
		}
	}
	return items
}

// RigNames is the rigs this machine has, in a settled order, for a message a
// person reads.
func RigNames(rigs map[string]string) []string {
	names := make([]string, 0, len(rigs))
	for name := range rigs {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// setting reads one root-table key: the environment first, the config file
// after it, and the way to set it if neither answers.
func setting(key, env string) (string, error) {
	if value := strings.TrimSpace(os.Getenv(env)); value != "" {
		return value, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("no %s is set and there is no home directory to read %s in: %w", env, File, err)
	}
	path := filepath.Join(home, File)

	value, err := valueIn(path, key)
	if err != nil {
		return "", err
	}
	if value == "" {
		return "", fmt.Errorf("the %s is not set: export %s=<value>, or put `%s = \"<value>\"` in %s", key, env, key, path)
	}
	return value, nil
}

// optionalSetting is setting for a key that has a default: the environment
// first, the config file after it, and the default when neither says.
func optionalSetting(key, env, fallback string) (string, error) {
	if value := strings.TrimSpace(os.Getenv(env)); value != "" {
		return value, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("no %s is set and there is no home directory to read %s in: %w", env, File, err)
	}
	value, err := valueIn(filepath.Join(home, File), key)
	if err != nil {
		return "", err
	}
	if value == "" {
		return fallback, nil
	}
	return value, nil
}

// valueIn reads one root-table key out of a config file, or "" if the file has
// no such key. A missing file is not an error: the environment may still be how
// this machine is told what it needs to know.
//
// Only the handful of TOML this needs is understood: `key = value` in the root
// table, with the value optionally quoted, and `#` comments. Keys after a
// `[table]` header belong to that table and are skipped.
func valueIn(path, key string) (string, error) {
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", path, err)
	}
	defer file.Close()

	lines := bufio.NewScanner(file)
	for lines.Scan() {
		line := strings.TrimSpace(lines.Text())
		if strings.HasPrefix(line, "[") {
			break // the root table has ended; every later key belongs to a table
		}
		name, value, found := strings.Cut(line, "=")
		if !found || strings.TrimSpace(name) != key {
			continue
		}
		return unquote(value), nil
	}
	if err := lines.Err(); err != nil {
		return "", fmt.Errorf("reading %s: %w", path, err)
	}
	return "", nil
}

// tableIn reads every `key = value` of one `[table]` in a config file. A
// missing file, or a file without that table, is an empty table rather than an
// error: a machine may simply have no rigs checked out yet.
func tableIn(path, table string) (map[string]string, error) {
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	defer file.Close()

	values := map[string]string{}
	inside := false
	lines := bufio.NewScanner(file)
	for lines.Scan() {
		line := strings.TrimSpace(lines.Text())
		if strings.HasPrefix(line, "[") {
			inside = strings.TrimSpace(strings.Trim(line, "[]")) == table
			continue
		}
		if !inside || strings.HasPrefix(line, "#") {
			continue
		}
		name, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		if name = strings.TrimSpace(name); name != "" {
			values[strings.Trim(name, `"'`)] = unquote(value)
		}
	}
	if err := lines.Err(); err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	return values, nil
}

// unquote trims a TOML value down to the string it holds: a quoted value keeps
// everything between the quotes, a bare one loses any trailing comment.
func unquote(value string) string {
	value = strings.TrimSpace(value)
	for _, quote := range []string{`"`, `'`} {
		if strings.HasPrefix(value, quote) {
			if end := strings.Index(value[1:], quote); end >= 0 {
				return value[1 : end+1]
			}
		}
	}
	if comment := strings.Index(value, "#"); comment >= 0 {
		value = value[:comment]
	}
	return strings.TrimSpace(value)
}
