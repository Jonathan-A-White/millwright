package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// CloudTable is the table of the config file that turns the elastic cloud on
// (docs/vultr-boost.md, "Elastic"), and CloudCapsTable the one that says how
// many sessions each host runs at once, for counting the free ones: host name
// on the left, a number on the right (`desktop = 2`).
const (
	CloudTable     = "cloud"
	CloudCapsTable = "cloud.caps"
)

// The [cloud] settings when the table does not say: the Governor's cap
// (mw-5gr3k0.4), and Vultr's High Performance 8 vCPU / 16 GB price.
const (
	DefaultCloudProvider      = "vultr"
	DefaultCloudMaxBoxes      = 2
	DefaultCloudMonthlyCapUSD = 75.0
	DefaultCloudIdleMinutes   = 30
	DefaultCloudHourlyUSD     = 0.132
	DefaultCloudBoxCap        = 2
	// DefaultCloudUpMinutes is how long a make may run before it reads the
	// instance back: a snapshot restore took about 50 minutes (mw-gq6.465).
	DefaultCloudUpMinutes = 60
	// DefaultCloudSlowMakeMinutes is how long a make may run before it is a
	// cloud event and a DAMPERS line (mw-gq6.1113).
	DefaultCloudSlowMakeMinutes = 30
	// DefaultCloudNoDispatchMinutes is how long after its make a box may go
	// with no dispatch reaching the beads server before the check destroys it
	// (mw-gq6.1143); it is past the 20 minutes after which the book says the
	// box is up, not dispatching.
	DefaultCloudNoDispatchMinutes = 30
)

// cloudPrefix is what a [cloud] prefix may be: a box is the prefix and a number.
var cloudPrefix = regexp.MustCompile(`^[a-z][a-z-]*$`)

// CloudSettings is what the `[cloud]` table says.
type CloudSettings struct {
	Provider      string
	MaxBoxes      int
	MonthlyCapUSD float64
	IdleMinutes   int
	HourlyUSD     float64
	// BoxCap is how many sessions a box runs at once.
	BoxCap int
	// UpMinutes is how long a make may run before it reads the instance back
	// from Vultr, and a check gives a make that left no word 15 more.
	UpMinutes int
	// SlowMakeMinutes is how long a make may run, unfinished, before the check
	// says so as a cloud event and DAMPERS names the box.
	SlowMakeMinutes int
	// NoDispatchMinutes is how long after its make a box may go with no
	// dispatch of its own reaching the beads server before the check destroys
	// it, as a box that will never take a story.
	NoDispatchMinutes int
	// Prefix begins the name of each box, Prefix1, Prefix2, ...; empty is
	// "cloud" (application.DefaultCloudPrefix) (mw-gq6.1250).
	Prefix string
	// Snapshot is the Vultr snapshot a box is made from; empty makes it from
	// Ubuntu and bootstraps it in full.
	Snapshot string
	// Command is the provider's wrapper: a full path, by default
	// contrib/vultr-boost in this host's checkout of millwright.
	Command string
	// HostCaps are the sessions each host runs at once, from [cloud.caps];
	// empty when the table is not there.
	HostCaps map[string]int
}

// Cloud reports the elastic cloud this host runs, read from the `[cloud]` and
// `[cloud.caps]` tables of ~/.config/mw/config.toml, and whether there is one:
// a machine with no [cloud] table makes no box, which is not an error. Each
// setting the table leaves out is its default; one that is not a number above
// zero (max_boxes may be 0, which makes no box), a provider that is not vultr,
// or a command that is not a full path is refused saying which.
func Cloud() (CloudSettings, bool, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return CloudSettings{}, false, fmt.Errorf("there is no home directory to read %s in: %w", File, err)
	}
	path := filepath.Join(home, File)
	table, err := tableIn(path, CloudTable)
	if err != nil || len(table) == 0 {
		return CloudSettings{}, false, err
	}
	cloud := CloudSettings{
		Provider:          DefaultCloudProvider,
		MaxBoxes:          DefaultCloudMaxBoxes,
		MonthlyCapUSD:     DefaultCloudMonthlyCapUSD,
		IdleMinutes:       DefaultCloudIdleMinutes,
		HourlyUSD:         DefaultCloudHourlyUSD,
		BoxCap:            DefaultCloudBoxCap,
		UpMinutes:         DefaultCloudUpMinutes,
		SlowMakeMinutes:   DefaultCloudSlowMakeMinutes,
		NoDispatchMinutes: DefaultCloudNoDispatchMinutes,
		Prefix:            strings.TrimSpace(table["prefix"]),
		Snapshot:          strings.TrimSpace(table["snapshot"]),
		Command:           strings.TrimSpace(table["command"]),
	}
	if said := strings.TrimSpace(table["provider"]); said != "" && said != DefaultCloudProvider {
		return CloudSettings{}, false, fmt.Errorf("the [%s] table of %s says provider = %q: the one provider is %q", CloudTable, path, said, DefaultCloudProvider)
	}
	if cloud.Prefix != "" && !cloudPrefix.MatchString(cloud.Prefix) {
		return CloudSettings{}, false, fmt.Errorf("the [%s] table of %s says prefix = %q: it is lower-case letters and hyphens, starting with a letter, and a box's number follows it", CloudTable, path, cloud.Prefix)
	}
	for key, into := range map[string]*int{"max_boxes": &cloud.MaxBoxes, "idle_minutes": &cloud.IdleMinutes, "box_cap": &cloud.BoxCap, "up_minutes": &cloud.UpMinutes, "slow_make_minutes": &cloud.SlowMakeMinutes, "no_dispatch_minutes": &cloud.NoDispatchMinutes} {
		said := strings.TrimSpace(table[key])
		if said == "" {
			continue
		}
		n, err := strconv.Atoi(said)
		if err != nil || n < 0 || n == 0 && key != "max_boxes" {
			return CloudSettings{}, false, fmt.Errorf("the [%s] table of %s says %s = %q: it must be a whole number above zero", CloudTable, path, key, said)
		}
		*into = n
	}
	for key, into := range map[string]*float64{"monthly_cap_usd": &cloud.MonthlyCapUSD, "hourly_usd": &cloud.HourlyUSD} {
		said := strings.TrimSpace(table[key])
		if said == "" {
			continue
		}
		f, err := strconv.ParseFloat(said, 64)
		if err != nil || f <= 0 {
			return CloudSettings{}, false, fmt.Errorf("the [%s] table of %s says %s = %q: it must be a number of dollars above zero", CloudTable, path, key, said)
		}
		*into = f
	}
	if cloud.Command == "" {
		rigs, err := Rigs()
		if err != nil {
			return CloudSettings{}, false, err
		}
		if dir := rigs["millwright"]; dir != "" {
			cloud.Command = filepath.Join(dir, "contrib", "vultr-boost")
		}
	}
	if cloud.Command == "" || !filepath.IsAbs(cloud.Command) {
		return CloudSettings{}, false, fmt.Errorf("the [%s] table of %s has no command = \"<full path to contrib/vultr-boost>\", and [rigs] names no millwright checkout to find it in", CloudTable, path)
	}
	caps, err := tableIn(path, CloudCapsTable)
	if err != nil {
		return CloudSettings{}, false, err
	}
	for host, said := range caps {
		n, err := strconv.Atoi(strings.TrimSpace(said))
		if err != nil || n < 0 {
			return CloudSettings{}, false, fmt.Errorf("the [%s] table of %s says %s = %q: it must be a whole number of sessions", CloudCapsTable, path, host, said)
		}
		if cloud.HostCaps == nil {
			cloud.HostCaps = map[string]int{}
		}
		cloud.HostCaps[host] = n
	}
	return cloud, true, nil
}
