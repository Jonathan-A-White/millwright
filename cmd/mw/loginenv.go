package main

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// standardPathDirs are the directories a login shell puts on PATH that a process
// started with no login environment lacks.
var standardPathDirs = []string{"/usr/local/sbin", "/usr/local/bin", "/usr/sbin", "/usr/bin", "/sbin", "/bin", "/snap/bin"}

// loginEnvironment is what a command that runs with no login environment calls first.
// It is a variable so that the tests of this package, which run in one process with
// the real host's files, are not left with the host's beads.env in their environment
// (TestMain makes it do nothing; the tests of the environment itself call
// ensureLoginEnvironment, and the one of the wiring puts it back).
var loginEnvironment = ensureLoginEnvironment

// ensureLoginEnvironment gives mw, started with no login environment, what the
// commands it runs need: bd, tmux and git on PATH (~/.local/bin and the standard
// directories, added after what PATH already names) and the host's beads connection
// (the names in ~/.config/mw/beads.env, each only if the environment does not set
// it). The Windows tasks of mw giveway run `wsl.exe --exec mw ...`, which has
// neither: bd was not found and the note for the home failed, and without tmux on
// PATH no Builder session's pane is listed (mw-2891blf.26). A bd that WAS found
// without beads.env would open a stray embedded store (CLAUDE.md), so both go
// together. Nothing here fails: what cannot be read is left as it was.
func ensureLoginEnvironment() {
	var want []string
	if home, err := os.UserHomeDir(); err == nil {
		want = append(want, filepath.Join(home, ".local", "bin"))
	}
	want = append(want, standardPathDirs...)
	have := map[string]bool{}
	var dirs []string
	for _, dir := range strings.Split(os.Getenv("PATH"), ":") {
		if dir != "" {
			have[dir] = true
			dirs = append(dirs, dir)
		}
	}
	for _, dir := range want {
		if !have[dir] {
			have[dir] = true
			dirs = append(dirs, dir)
		}
	}
	os.Setenv("PATH", strings.Join(dirs, ":"))

	file := beadsEnvFile()
	if file == "" {
		return
	}
	f, err := os.Open(file)
	if err != nil {
		return
	}
	defer f.Close()
	scan := bufio.NewScanner(f)
	for scan.Scan() {
		name, value, ok := parseEnvAssignment(scan.Text())
		if !ok {
			continue
		}
		if _, set := os.LookupEnv(name); !set {
			os.Setenv(name, value)
		}
	}
}

// parseEnvAssignment reads one line of a file of shell assignments, as beads.env is
// ("NAME=value", "export NAME=value", the value bare or in one pair of quotes).
// Comments, blank lines and anything else are not assignments.
func parseEnvAssignment(line string) (name, value string, ok bool) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", "", false
	}
	line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
	name, value, found := strings.Cut(line, "=")
	if !found || name == "" {
		return "", "", false
	}
	for i, r := range name {
		if !(r == '_' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || i > 0 && r >= '0' && r <= '9') {
			return "", "", false
		}
	}
	if n := len(value); n >= 2 && (value[0] == '"' || value[0] == '\'') && value[n-1] == value[0] {
		value = value[1 : n-1]
	}
	return name, value, true
}
