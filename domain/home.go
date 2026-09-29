package domain

import (
	"fmt"
	"strings"
	"time"
)

// HomeHosts are the hosts that can be the factory's home. The VPS never is.
var HomeHosts = []string{"desktop", "laptop"}

// HomeRecord is what the vault's `home` file says: which host is home, when that
// last changed (UTC) and who changed it.
type HomeRecord struct {
	Host string
	At   time.Time
	By   string
}

// ParseHome reads the text of a `home` file: one line of three fields, the home
// host's name, the UTC time of the last change (RFC 3339, ending in Z) and the
// actor that made it. Anything else is refused, so that a file half written or
// written by hand wrongly is never taken for an answer.
func ParseHome(text string) (HomeRecord, error) {
	lines := strings.Split(strings.TrimRight(text, "\r\n"), "\n")
	if len(lines) != 1 {
		return HomeRecord{}, fmt.Errorf("the home file is one line: <host> <UTC time> <actor>, not %d lines", len(lines))
	}
	fields := strings.Fields(lines[0])
	if len(fields) != 3 {
		return HomeRecord{}, fmt.Errorf("the home file is one line: <host> <UTC time> <actor>, got %q", lines[0])
	}

	host := fields[0]
	known := false
	for _, name := range HomeHosts {
		known = known || host == name
	}
	if !known {
		return HomeRecord{}, fmt.Errorf("the home file names %q as home, which is not one of %s", host, strings.Join(HomeHosts, ", "))
	}
	if !strings.HasSuffix(fields[1], "Z") {
		return HomeRecord{}, fmt.Errorf("the home file's time %q is not UTC (it ends in Z)", fields[1])
	}
	at, err := time.Parse(time.RFC3339, fields[1])
	if err != nil {
		return HomeRecord{}, fmt.Errorf("the home file's time %q is not an RFC 3339 time: %w", fields[1], err)
	}
	return HomeRecord{Host: host, At: at.UTC(), By: fields[2]}, nil
}

// String is the record as the home file holds it: one line, newline-ended.
func (h HomeRecord) String() string {
	return h.Host + " " + h.At.UTC().Format(time.RFC3339) + " " + h.By + "\n"
}
