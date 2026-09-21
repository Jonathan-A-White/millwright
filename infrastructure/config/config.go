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
package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
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

	TickRecheckEnv = "MW_TICK_RECHECK_SECONDS"

	MillhandRoutineModelEnv = "MW_MILLHAND_ROUTINE_MODEL"
	MillhandReviewModelEnv  = "MW_MILLHAND_REVIEW_MODEL"
)

// RigsTable is the table of the config file that says where each rig is checked
// out on this host: rig name on the left, directory on the right. TestsTable is
// the table that says how each rig's own tests are run on this host: rig name on
// the left, a command line on the right. A rig that is not in it is tested the
// way DefaultTests says.
const (
	RigsTable  = "rigs"
	TestsTable = "tests"
)

// WatchTable is the table of the config file that says what `mw watch` looks at.
const WatchTable = "watch"

// DefaultCap is how many sessions may run at once on a host that does not say.
// One, because the smaller of the factory's two hosts has a single core and
// under a gigabyte of memory, and because two sessions racing is the expensive
// mistake to make by default.
const DefaultCap = 1

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
