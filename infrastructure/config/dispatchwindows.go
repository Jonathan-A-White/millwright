package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	// The zone database rides in the binary, so that a window's tz is known on
	// a host that has no /usr/share/zoneinfo.
	_ "time/tzdata"
)

// DispatchWindowsTable is the array of tables in the config file that makes a
// host's per-host knobs follow the clock (mw-gq6.429):
//
//	[[dispatch.windows]]
//	name = "night"                  # optional: how mw status names it
//	from = "23:00"                  # HH:MM on the clock of tz
//	to   = "06:00"                  # before from: the window wraps midnight
//	tz   = "America/New_York"       # a zone of the tz database, so a change of clocks is read
//	cap  = 4                        # any of the keys below; one it does not name keeps its root value
//
// While the clock of tz is from or later and before to, the window's keys
// stand in for the root keys of the same name (and room_load_per_core and
// room_min_free_mb for the [dispatch] table's). The first window in the file
// that holds the instant wins.
const DispatchWindowsTable = "dispatch.windows"

// windowKnob is a key a window may carry, and how its value is judged.
type windowKnob struct {
	key   string
	check func(string) bool
	must  string
}

func wholeAtLeast(low, high int) func(string) bool {
	return func(said string) bool {
		n, err := strconv.Atoi(said)
		return err == nil && n >= low && n <= high
	}
}

// windowKnobs are the per-host knobs mw reads at dispatch, which a window may
// carry: each resolves through knobIn (or RoomAt, which asks the same
// activeWindow).
var windowKnobs = []windowKnob{
	{"cap", wholeAtLeast(1, 1<<30), "a whole number of 1 or more"},
	{"builder_jobs", wholeAtLeast(1, 1<<30), "a whole number of 1 or more"},
	{"builder_nice", wholeAtLeast(0, 19), "a whole number from 0 (off) to 19"},
	{"room_load_per_core", func(said string) bool {
		n, err := strconv.ParseFloat(said, 64)
		return err == nil && n > 0 && n < 1e12
	}, "a number above zero, like 1.0"},
	{"room_min_free_mb", func(said string) bool {
		n, err := strconv.ParseInt(said, 10, 64)
		return err == nil && n >= 1
	}, "a whole number of megabytes, 1 or more"},
}

var windowClock = regexp.MustCompile(`^([01][0-9]|2[0-3]):([0-5][0-9])$`)

// DispatchWindow is one [[dispatch.windows]] entry that was read whole.
type DispatchWindow struct {
	Name     string
	From, To int // minutes after midnight on the clock of Zone
	ToText   string
	Zone     string
	loc      *time.Location
	// Values are the knobs the window carries, by key.
	Values map[string]string
}

// Holds reports whether the clock of the window's zone reads inside it at now:
// from or later, before to; one whose from is after its to wraps midnight.
func (w DispatchWindow) Holds(now time.Time) bool {
	local := now.In(w.loc)
	at := local.Hour()*60 + local.Minute()
	if w.From < w.To {
		return at >= w.From && at < w.To
	}
	return at >= w.From || at < w.To
}

// Why is how mw status names the window: "night window to 06:00
// America/New_York", or "window to 06:00 America/New_York" when it has no name.
func (w DispatchWindow) Why() string {
	why := "window to " + w.ToText + " " + w.Zone
	if w.Name != "" {
		why = w.Name + " " + why
	}
	return why
}

// ReadDispatchWindows reads the [[dispatch.windows]] entries of the config file
// at path. A malformed entry is left out of the windows and gives one problem,
// naming its key, so that the root values apply while it is wrong. A missing
// file has none.
func ReadDispatchWindows(path string) ([]DispatchWindow, []error) {
	entries, err := arrayTablesIn(path, DispatchWindowsTable)
	if err != nil {
		return nil, []error{err}
	}
	var windows []DispatchWindow
	var problems []error
	for i, entry := range entries {
		window, err := parseDispatchWindow(entry)
		if err != nil {
			problems = append(problems, fmt.Errorf("window %d of [[%s]] in %s: %w", i+1, DispatchWindowsTable, path, err))
			continue
		}
		windows = append(windows, window)
	}
	return windows, problems
}

func parseDispatchWindow(entry map[string]string) (DispatchWindow, error) {
	known := map[string]bool{"name": true, "from": true, "to": true, "tz": true}
	for _, knob := range windowKnobs {
		known[knob.key] = true
	}
	var unknown []string
	for key := range entry {
		if !known[key] {
			unknown = append(unknown, key)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return DispatchWindow{}, fmt.Errorf("%s = %q is not a key a window takes (it takes name, from, to, tz and %s)",
			unknown[0], entry[unknown[0]], knobNames())
	}
	window := DispatchWindow{Name: strings.TrimSpace(entry["name"]), Values: map[string]string{}}
	var err error
	if window.From, err = windowMinutes("from", entry["from"]); err != nil {
		return DispatchWindow{}, err
	}
	if window.To, err = windowMinutes("to", entry["to"]); err != nil {
		return DispatchWindow{}, err
	}
	window.ToText = strings.TrimSpace(entry["to"])
	if window.From == window.To {
		return DispatchWindow{}, fmt.Errorf("to = %q is the same time as from: a window of no time or all of it is not one", window.ToText)
	}
	window.Zone = strings.TrimSpace(entry["tz"])
	if window.Zone == "" || window.Zone == "Local" {
		return DispatchWindow{}, fmt.Errorf("tz = %q: say a zone of the tz database, like \"America/New_York\"", window.Zone)
	}
	if window.loc, err = time.LoadLocation(window.Zone); err != nil {
		return DispatchWindow{}, fmt.Errorf("tz = %q is not a zone of the tz database, like \"America/New_York\"", window.Zone)
	}
	for _, knob := range windowKnobs {
		said := strings.TrimSpace(entry[knob.key])
		if said == "" {
			continue
		}
		if !knob.check(said) {
			return DispatchWindow{}, fmt.Errorf("%s = %q: it must be %s", knob.key, said, knob.must)
		}
		window.Values[knob.key] = said
	}
	if len(window.Values) == 0 {
		return DispatchWindow{}, fmt.Errorf("it carries none of %s, so it changes nothing", knobNames())
	}
	return window, nil
}

func knobNames() string {
	names := make([]string, 0, len(windowKnobs))
	for _, knob := range windowKnobs {
		names = append(names, knob.key)
	}
	return strings.Join(names, ", ")
}

func windowMinutes(key, said string) (int, error) {
	said = strings.TrimSpace(said)
	match := windowClock.FindStringSubmatch(said)
	if match == nil {
		if said == "" {
			return 0, fmt.Errorf("it has no %s: say %s = \"HH:MM\", like \"23:00\"", key, key)
		}
		return 0, fmt.Errorf("%s = %q is not a time: say \"HH:MM\" on a 24-hour clock, like \"23:00\"", key, said)
	}
	hours, _ := strconv.Atoi(match[1])
	minutes, _ := strconv.Atoi(match[2])
	return hours*60 + minutes, nil
}

// activeWindow is the first well-formed window of the config file at path that
// holds now, or nil. A file that cannot be read, or a window that is malformed,
// is no window: the root values apply (DispatchWindowProblems says what is
// wrong, for mw doctor).
func activeWindow(path string, now time.Time) *DispatchWindow {
	windows, _ := ReadDispatchWindows(path)
	for i := range windows {
		if windows[i].Holds(now) {
			return &windows[i]
		}
	}
	return nil
}

// DispatchWindowProblems reads this host's config file and says what is wrong
// with its [[dispatch.windows]] entries, each naming its key; none when they
// are well formed or there are none.
func DispatchWindowProblems() []error {
	path, err := FilePath()
	if err != nil {
		return []error{err}
	}
	_, problems := ReadDispatchWindows(path)
	return problems
}

// knobIn is the one place a per-host knob is resolved: $env if it is set,
// otherwise the carrying window of the config file that holds now, otherwise
// the root-table key of the file. why names the window when it is what answered
// and is "" otherwise. An empty value means nothing said.
func knobIn(key, env string, now time.Time) (value, why string, err error) {
	if value := strings.TrimSpace(os.Getenv(env)); value != "" {
		return value, "", nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", fmt.Errorf("no %s is set and there is no home directory to read %s in: %w", env, File, err)
	}
	path := filepath.Join(home, File)
	if window := activeWindow(path, now); window != nil {
		if value := window.Values[key]; value != "" {
			return value, window.Why(), nil
		}
	}
	value, err = valueIn(path, key)
	return value, "", err
}

// arrayTablesIn reads every `[[table]]` entry of a config file as its own
// map of `key = value`. A missing file has none.
func arrayTablesIn(path, table string) ([]map[string]string, error) {
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	defer file.Close()

	var entries []map[string]string
	inside := false
	lines := bufio.NewScanner(file)
	for lines.Scan() {
		line := strings.TrimSpace(lines.Text())
		if strings.HasPrefix(line, "[") {
			inside = strings.HasPrefix(line, "[[") && strings.TrimSpace(strings.Trim(line, "[]")) == table
			if inside {
				entries = append(entries, map[string]string{})
			}
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
			entries[len(entries)-1][strings.Trim(name, `"'`)] = unquote(value)
		}
	}
	if err := lines.Err(); err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	return entries, nil
}
