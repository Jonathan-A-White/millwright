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
//	beads_sync = "remote"
//	beads_backup_minutes = 30
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
//	units          = ["mw-dispatch.service", "mw-millhand-tick.service"]
//	state_dir      = "/root/.local/state/mw-doctor"
//	reach          = ["api.anthropic.com:443", "github.com:443"]
//	powershell     = "/mnt/c/Windows/System32/WindowsPowerShell/v1.0/powershell.exe"
//	tunnel_unit    = "reverse-tunnel.service"
//	tunnel_host    = "vps"
//	tunnel_probe   = "ss -ltn sport = :2222"
//	doctor_wg_hub  = "10.88.0.1:22"
//	doctor_wg_unit = "wg-quick@wg0"
//	tmp_leftovers_budget_bytes = 200000000
package config

import (
	"bufio"
	"fmt"
	"net"
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

	PosternBackendEnv       = "MW_POSTERN_BACKEND"
	PosternFloatSatsEnv     = "MW_POSTERN_FLOAT_SATS"
	PosternGovernorKeyEnv   = "MW_POSTERN_GOVERNOR_KEY"
	PosternKeyFileEnv       = "MW_POSTERN_KEY_FILE"
	PosternSnapshotPathEnv  = "MW_POSTERN_SNAPSHOT_PATH"
	PosternViewPathEnv      = "MW_POSTERN_VIEW_PATH"
	PosternChannelEnv       = "MW_POSTERN_CHANNEL"
	PosternTranscribeCmdEnv = "MW_POSTERN_TRANSCRIBE_CMD"
	PosternDataEnv          = "MW_POSTERN_DATA"
	PosternWatchdogEnv      = "MW_POSTERN_WATCHDOG_TARGET"

	HandsRootHelperEnv = "MW_HANDS_ROOT_HELPER"

	BeadsSyncEnv          = "MW_BEADS_SYNC"
	BeadsBackupMinutesEnv = "MW_BEADS_BACKUP_MINUTES"
)

// The environment variables bd itself reads to reach a Dolt database server
// rather than a database of its own: a host whose beads_sync is shared sets
// them, and mw doctor's beads-server check dials what they name. They are
// bd's, not mw's, so they have no key in the config file.
const (
	BeadsDoltServerHostEnv = "BEADS_DOLT_SERVER_HOST"
	BeadsDoltServerPortEnv = "BEADS_DOLT_SERVER_PORT"
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

// HandsHostsTable is the table of the config file that says how this host
// reaches each other host a hands step may be for: host name on the left, an
// ssh prefix on the right (`laptop = "ssh laptop"`).
const HandsHostsTable = "hands_hosts"

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

// DefaultPosternBackend is where the postern's backend is reached when
// nothing says otherwise: the Governor's Desktop, on this factory's own
// network.
const DefaultPosternBackend = "http://desktop.mw:8787"

// DefaultPosternFloatSats is the postern's float cap, in testnet satoshis,
// when nothing says otherwise: mw refuses every send that would carry the
// float past it. PROVISIONAL until the Governor confirms it with the tree.
const DefaultPosternFloatSats = 100000

// DefaultPosternKeyFile is where the Mayor's postern key is kept under the
// home directory when nothing says otherwise: host-local, outside the vault
// and its backups, like .mayor-acting.
var DefaultPosternKeyFile = filepath.Join(".config", "mw", "postern.key")

// DefaultPosternSnapshotPath is where mw postern snapshot writes the
// encrypted snapshot under the home directory when nothing says otherwise:
// state, not config, so it lives under .local/state rather than .config.
var DefaultPosternSnapshotPath = filepath.Join(".local", "state", "mw", "snapshot.bin")

// DefaultPosternInboxDir is where mw postern inbox writes a message's
// downloaded and decrypted attachment under the home directory: state,
// alongside the snapshot, never the vault or a rig. There is no setting for
// it.
var DefaultPosternInboxDir = filepath.Join(".local", "state", "mw", "postern", "inbox")

// PosternBackend reports the URL of the postern's backend: $MW_POSTERN_BACKEND
// if it is set, otherwise the root-table `postern_backend` key of
// ~/.config/mw/config.toml, and DefaultPosternBackend when neither says.
func PosternBackend() (string, error) {
	return optionalSetting("postern_backend", PosternBackendEnv, DefaultPosternBackend)
}

// PosternFloatSats reports the postern's float cap, in testnet satoshis, that
// mw enforces on every send: $MW_POSTERN_FLOAT_SATS if it is set, otherwise
// the root-table `postern_float_sats` key of ~/.config/mw/config.toml, and
// DefaultPosternFloatSats when neither says.
func PosternFloatSats() (int, error) {
	said, err := optionalSetting("postern_float_sats", PosternFloatSatsEnv, "")
	if err != nil {
		return 0, err
	}
	if said == "" {
		return DefaultPosternFloatSats, nil
	}
	sats, err := strconv.Atoi(said)
	if err != nil {
		return 0, fmt.Errorf("the postern float cap is %q, which is not a whole number of satoshis: set %s=<n>, or `postern_float_sats = <n>` in %s", said, PosternFloatSatsEnv, File)
	}
	if sats < 0 {
		return 0, fmt.Errorf("the postern float cap is %d satoshis, which is negative: set it to 0 or more", sats)
	}
	return sats, nil
}

// PosternGovernorKey reports the Governor's compressed public key, as hex,
// that the postern backend answers to: $MW_POSTERN_GOVERNOR_KEY if it is
// set, otherwise the root-table `postern_governor_key` key of
// ~/.config/mw/config.toml, and empty when neither says.
func PosternGovernorKey() (string, error) {
	return optionalSetting("postern_governor_key", PosternGovernorKeyEnv, "")
}

// PosternKeyFile reports where the Mayor's postern key is kept:
// $MW_POSTERN_KEY_FILE if it is set, otherwise the root-table
// `postern_key_file` key of ~/.config/mw/config.toml, a full path either
// way, and DefaultPosternKeyFile under the home directory when neither says.
func PosternKeyFile() (string, error) {
	said, err := optionalSetting("postern_key_file", PosternKeyFileEnv, "")
	if err != nil {
		return "", err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("no %s is set and there is no home directory to read %s in: %w", PosternKeyFileEnv, File, err)
	}
	if said != "" {
		if !filepath.IsAbs(said) {
			return "", fmt.Errorf("the postern key file is %q in %s: it must be a full path", said, filepath.Join(home, File))
		}
		return said, nil
	}
	return filepath.Join(home, DefaultPosternKeyFile), nil
}

// PosternSnapshotPath reports where mw postern snapshot writes the encrypted
// snapshot of every live epic: $MW_POSTERN_SNAPSHOT_PATH if it is set,
// otherwise the root-table `postern_snapshot_path` key of
// ~/.config/mw/config.toml, a full path either way, and
// DefaultPosternSnapshotPath under the home directory when neither says.
func PosternSnapshotPath() (string, error) {
	said, err := optionalSetting("postern_snapshot_path", PosternSnapshotPathEnv, "")
	if err != nil {
		return "", err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("no %s is set and there is no home directory to read %s in: %w", PosternSnapshotPathEnv, File, err)
	}
	if said != "" {
		if !filepath.IsAbs(said) {
			return "", fmt.Errorf("the postern snapshot path is %q in %s: it must be a full path", said, filepath.Join(home, File))
		}
		return said, nil
	}
	return filepath.Join(home, DefaultPosternSnapshotPath), nil
}

// DefaultPosternViewPath is where mw postern view writes the encrypted live
// view under the home directory when nothing says otherwise, and so where
// mw postern serve tells the postern backend to read it from
// (POSTERN_VIEW_FILE): state, beside the backend's own.
var DefaultPosternViewPath = filepath.Join(".local", "state", "postern", "view.b64")

// PosternViewPath reports where the encrypted live view is written and read:
// $MW_POSTERN_VIEW_PATH if it is set, otherwise the root-table
// `postern_view_path` key of ~/.config/mw/config.toml, a full path either
// way, and DefaultPosternViewPath under the home directory when neither says.
func PosternViewPath() (string, error) {
	said, err := optionalSetting("postern_view_path", PosternViewPathEnv, "")
	if err != nil {
		return "", err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("no %s is set and there is no home directory to read %s in: %w", PosternViewPathEnv, File, err)
	}
	if said != "" {
		if !filepath.IsAbs(said) {
			return "", fmt.Errorf("the postern view path is %q in %s: it must be a full path", said, filepath.Join(home, File))
		}
		return said, nil
	}
	return filepath.Join(home, DefaultPosternViewPath), nil
}

// PosternInboxDir reports the full path to the directory mw postern inbox
// writes a decrypted attachment to: DefaultPosternInboxDir under the home
// directory. There is no setting for it.
func PosternInboxDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("there is no home directory to write a postern attachment in: %w", err)
	}
	return filepath.Join(home, DefaultPosternInboxDir), nil
}

// The channels mw postern send delivers a message by: straight to the
// postern backend (postern's docs/protocol.md §9), or in a funded testnet
// transaction (§4). DefaultPosternChannel is chain, what every host did
// before the direct channel existed: a host sends directly only once its
// config says `postern_channel = "direct"`, which the move to the desktop
// sets when the backend that takes direct records runs there (the vault's
// hosts/desktop-move.md step 4.1). Direct is the Governor's 2026-09-28
// decision for where the factory ends up, not a default that a merge may
// switch on under a backend that answers 404 to it.
const (
	PosternChannelDirect  = "direct"
	PosternChannelChain   = "chain"
	DefaultPosternChannel = PosternChannelChain
)

// PosternChannel reports how mw postern send delivers a message:
// $MW_POSTERN_CHANNEL if it is set, otherwise the root-table
// `postern_channel` key of ~/.config/mw/config.toml, and
// DefaultPosternChannel when neither says. Anything but direct or chain, in
// any case, is refused.
func PosternChannel() (string, error) {
	said, err := optionalSetting("postern_channel", PosternChannelEnv, DefaultPosternChannel)
	if err != nil {
		return "", err
	}
	switch channel := strings.ToLower(strings.TrimSpace(said)); channel {
	case PosternChannelDirect, PosternChannelChain:
		return channel, nil
	default:
		return "", fmt.Errorf("the postern channel is %q, which is neither %s nor %s: set %s=<channel>, or `postern_channel = \"<channel>\"` in %s",
			said, PosternChannelDirect, PosternChannelChain, PosternChannelEnv, File)
	}
}

// DefaultHandsRootHelper is where contrib/install-hands-root installs
// mw-hands-root on every host, and where mw looks for it through sudo -n.
const DefaultHandsRootHelper = "/usr/local/sbin/mw-hands-root"

// HandsRootHelper reports the path mw hands a root step to, through sudo -n,
// on whichever host it runs (postern's docs/protocol.md §17):
// $MW_HANDS_ROOT_HELPER if it is set, otherwise the root-table
// `hands_root_helper` key of ~/.config/mw/config.toml, a full path either
// way, and DefaultHandsRootHelper when neither says. The sudoers line
// contrib/install-hands-root writes names only the default.
func HandsRootHelper() (string, error) {
	said, err := optionalSetting("hands_root_helper", HandsRootHelperEnv, DefaultHandsRootHelper)
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(said) {
		return "", fmt.Errorf("the hands root helper is %q: it must be a full path, as sudo names it", said)
	}
	return said, nil
}

// HandsHosts reports how this host reaches each other host a hands step may
// be for, read from the `[hands_hosts]` table of ~/.config/mw/config.toml:
// host name to an ssh prefix, split on whitespace when it is used. A host not
// in it is one no step is run on from here, and a machine with no such table
// runs steps for itself only.
func HandsHosts() (map[string]string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("there is no home directory to read %s in: %w", File, err)
	}
	return tableIn(filepath.Join(home, File), HandsHostsTable)
}

// PosternData reports the postern backend's data directory, its POSTERN_DATA:
// $MW_POSTERN_DATA if it is set, otherwise the root-table `postern_data` key of
// ~/.config/mw/config.toml, a full path either way. It is an error for neither to
// say: mw postern mirror has no default to copy.
func PosternData() (string, error) {
	said, err := optionalSetting("postern_data", PosternDataEnv, "")
	if err != nil {
		return "", err
	}
	if said == "" {
		return "", fmt.Errorf("no postern data directory: set %s=<path>, or `postern_data = \"<path>\"` in %s (the backend's POSTERN_DATA)", PosternDataEnv, File)
	}
	if !filepath.IsAbs(said) {
		return "", fmt.Errorf("the postern data directory is %q: it must be a full path, the same on both hosts", said)
	}
	return strings.TrimRight(said, "/"), nil
}

// PosternWatchdogTarget reports where the VPS watchdog's copy of the postern's
// vapid keys and push subscriptions goes, an rsync destination
// (`root@vps:/var/lib/postern-watchdog/`): $MW_POSTERN_WATCHDOG_TARGET if it is
// set, otherwise the root-table `postern_watchdog_target` key of
// ~/.config/mw/config.toml. Empty when neither says, and then none is copied.
func PosternWatchdogTarget() (string, error) {
	return optionalSetting("postern_watchdog_target", PosternWatchdogEnv, "")
}

// PosternTranscribeCmd reports the command mw postern inbox transcribes a
// Governor's voice note with, on this host and never a third party's
// (postern's docs/protocol.md §14): $MW_POSTERN_TRANSCRIBE_CMD if it is set,
// otherwise the root-table `postern_transcribe_cmd` key of
// ~/.config/mw/config.toml, split on whitespace with the audio file's path
// appended. Empty when neither says, and then no voice note is transcribed.
func PosternTranscribeCmd() (string, error) {
	return optionalSetting("postern_transcribe_cmd", PosternTranscribeCmdEnv, "")
}

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

// The ways `mw sync` may treat this host's beads database, the root-table
// `beads_sync` key of the config file. application.BeadsSyncMode names the
// same three.
//
//   - remote: every host keeps a copy of its own and brings it level with the
//     others through the tracker's remote on every sync. The default: what
//     every host did before the factory moved to one home.
//   - backup: this host holds the one database every other host reaches. The
//     tracker's remote is a backup of it, pushed every beads_backup_minutes,
//     and never what keeps the hosts level.
//   - shared: this host's bd reaches another host's database over the
//     network (BEADS_DOLT_SERVER_HOST and the rest) and keeps no copy of its
//     own, so it has nothing to push, pull or collect.
const (
	BeadsSyncRemote  = "remote"
	BeadsSyncBackup  = "backup"
	BeadsSyncShared  = "shared"
	DefaultBeadsSync = BeadsSyncRemote
)

// DefaultBeadsBackupMinutes is how long a host whose beads_sync is backup
// waits between two backups of the one database, when nothing says otherwise.
const DefaultBeadsBackupMinutes = 30

// DefaultBeadsDoltServerPort is the port bd reaches a Dolt database server
// on when BEADS_DOLT_SERVER_PORT says nothing: bd's own server mode default
// (docs/research/beads-hosts-and-rigs.md, section 5).
const DefaultBeadsDoltServerPort = "3307"

// BeadsSync reports how `mw sync` treats this host's beads database:
// $MW_BEADS_SYNC if it is set, otherwise the root-table `beads_sync` key of
// ~/.config/mw/config.toml, and DefaultBeadsSync when neither says. Anything
// but the three choices is refused, naming them.
func BeadsSync() (string, error) {
	said, err := optionalSetting("beads_sync", BeadsSyncEnv, DefaultBeadsSync)
	if err != nil {
		return "", err
	}
	switch said {
	case BeadsSyncRemote, BeadsSyncBackup, BeadsSyncShared:
		return said, nil
	}
	return "", fmt.Errorf("beads_sync is %q, which is not one of its three choices: %q (every host keeps a copy, level through the remote), %q (this host holds the one database, the remote a backup) or %q (this host's bd reaches another host's database); set %s=<choice>, or `beads_sync = \"<choice>\"` in %s",
		said, BeadsSyncRemote, BeadsSyncBackup, BeadsSyncShared, BeadsSyncEnv, File)
}

// BeadsBackupMinutes reports how many minutes a host whose beads_sync is
// backup waits between two backups: $MW_BEADS_BACKUP_MINUTES if it is set,
// otherwise the root-table `beads_backup_minutes` key of
// ~/.config/mw/config.toml, and DefaultBeadsBackupMinutes when neither says.
func BeadsBackupMinutes() (int, error) {
	said, err := optionalSetting("beads_backup_minutes", BeadsBackupMinutesEnv, "")
	if err != nil {
		return 0, err
	}
	if said == "" {
		return DefaultBeadsBackupMinutes, nil
	}
	minutes, err := strconv.Atoi(said)
	if err != nil {
		return 0, fmt.Errorf("the beads backup interval is %q, which is not a whole number of minutes: set %s=<n>, or `beads_backup_minutes = <n>` in %s", said, BeadsBackupMinutesEnv, File)
	}
	if minutes < 1 {
		return 0, fmt.Errorf("the beads backup interval is %d minutes, so every sync would push a backup: set it to 1 or more", minutes)
	}
	return minutes, nil
}

// BeadsServerAddress reports the host:port this host's bd reaches its Dolt
// database server on: $BEADS_DOLT_SERVER_HOST, on $BEADS_DOLT_SERVER_PORT or
// DefaultBeadsDoltServerPort. Both are bd's own environment, so the config
// file has no say; "" when no host is set, which is every host whose
// beads_sync is not shared.
func BeadsServerAddress() (string, error) {
	host := strings.TrimSpace(os.Getenv(BeadsDoltServerHostEnv))
	if host == "" {
		return "", nil
	}
	port := strings.TrimSpace(os.Getenv(BeadsDoltServerPortEnv))
	if port == "" {
		port = DefaultBeadsDoltServerPort
	}
	if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
		return "", fmt.Errorf("%s is %q, which is not a port number", BeadsDoltServerPortEnv, port)
	}
	return net.JoinHostPort(host, port), nil
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
	return doctorTableSetting("tunnel_unit", DefaultDoctorTunnelUnit)
}

// DoctorTunnelProbe reports the command mw doctor's tunnel check runs over
// ssh on the VPS to check the tunnel's listener: the `[doctor]` table's
// `tunnel_probe` key of ~/.config/mw/config.toml, and
// DefaultDoctorTunnelProbe when the table says nothing.
func DoctorTunnelProbe() (string, error) {
	return doctorTableSetting("tunnel_probe", DefaultDoctorTunnelProbe)
}

// DefaultDoctorWgHub is the hub's ssh host:port mw doctor's wg check dials
// over wg0, when the [doctor] table says nothing: this rig's own wireguard
// hub.
const DefaultDoctorWgHub = "10.88.0.1:22"

// DefaultDoctorWgUnit is the unit mw doctor's wg check restarts when the hub
// is unreachable over wg0, when the [doctor] table says nothing.
const DefaultDoctorWgUnit = "wg-quick@wg0"

// DoctorWgHub reports the hub's ssh host:port mw doctor's wg check dials
// over wg0: the `[doctor]` table's `doctor_wg_hub` key of
// ~/.config/mw/config.toml, and DefaultDoctorWgHub when the table says
// nothing.
func DoctorWgHub() (string, error) {
	return doctorTableSetting("doctor_wg_hub", DefaultDoctorWgHub)
}

// DoctorWgUnit reports the unit mw doctor's wg check restarts when the hub is
// unreachable over wg0: the `[doctor]` table's `doctor_wg_unit` key of
// ~/.config/mw/config.toml, and DefaultDoctorWgUnit when the table says
// nothing.
func DoctorWgUnit() (string, error) {
	return doctorTableSetting("doctor_wg_unit", DefaultDoctorWgUnit)
}

// DefaultDoctorTmpLeftoversBudgetBytes is how many bytes of this factory's own
// dead temp leftovers — a killed bd's dolt spool files, /tmp/bd, a stale
// /tmp/claude-0/<session> dir — mw doctor's tmp-leftovers check holds a host
// to, and separately holds ~/.cache/go-build to, when the [doctor] table says
// nothing. It is the same 200_000_000 application.DefaultTmpLeftoversBudgetBytes
// reads as.
const DefaultDoctorTmpLeftoversBudgetBytes int64 = 200_000_000

// DoctorTmpLeftoversBudgetBytes reports the byte budget mw doctor's
// tmp-leftovers check holds this host's own dead temp leftovers to, and
// separately holds ~/.cache/go-build to: the `[doctor]` table's
// `tmp_leftovers_budget_bytes` key of ~/.config/mw/config.toml, and
// DefaultDoctorTmpLeftoversBudgetBytes when the table says nothing.
func DoctorTmpLeftoversBudgetBytes() (int64, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return 0, fmt.Errorf("there is no home directory to read %s in: %w", File, err)
	}
	table, err := tableIn(filepath.Join(home, File), DoctorTable)
	if err != nil {
		return 0, err
	}
	said := strings.TrimSpace(table["tmp_leftovers_budget_bytes"])
	if said == "" {
		return DefaultDoctorTmpLeftoversBudgetBytes, nil
	}
	bytes, err := strconv.ParseInt(said, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("the doctor's tmp_leftovers_budget_bytes is %q, which is not a whole number of bytes: set `tmp_leftovers_budget_bytes = <n>` in %s", said, File)
	}
	if bytes < 1 {
		return 0, fmt.Errorf("the doctor's tmp_leftovers_budget_bytes is %d, so every host would be over budget: set it to 1 or more", bytes)
	}
	return bytes, nil
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

// doctorTableSetting reads one [doctor] table key, and fallback when the
// table says nothing about it.
func doctorTableSetting(key, fallback string) (string, error) {
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
