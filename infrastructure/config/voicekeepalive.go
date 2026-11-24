package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// VoiceKeepAliveTable is the table of the config file that holds when the
// church Voice number's monthly text is sent: `day` (of the month) and `hour`
// (0 to 23), both in America/New_York. Only the home sends, so the home's own
// file is the one read (mw-gq6.1700).
const VoiceKeepAliveTable = "voice_keepalive"

// Defaults are the 1st at 21:00 (9 pm); application.VoiceKeepAliveDefaultDay
// and VoiceKeepAliveDefaultHour are the same.
const (
	DefaultVoiceKeepAliveDay  = 1
	DefaultVoiceKeepAliveHour = 21
)

// VoiceKeepAliveWhen reports the day of the month and the hour (America/New_York)
// from which the monthly text is sent: the [voice_keepalive] table's `day`
// (1 to 28) and `hour` (0 to 23) of the host's config file, 1 and 21 where it
// says nothing. A value out of range, or not a whole number, is an error naming
// the key.
func VoiceKeepAliveWhen() (day, hour int, err error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return 0, 0, fmt.Errorf("there is no home directory to read %s in: %w", File, err)
	}
	path := filepath.Join(home, File)
	table, err := tableIn(path, VoiceKeepAliveTable)
	if err != nil {
		return 0, 0, err
	}
	day, err = voiceKeepAliveKey(table, path, "day", DefaultVoiceKeepAliveDay, 1, 28)
	if err != nil {
		return 0, 0, err
	}
	hour, err = voiceKeepAliveKey(table, path, "hour", DefaultVoiceKeepAliveHour, 0, 23)
	if err != nil {
		return 0, 0, err
	}
	return day, hour, nil
}

func voiceKeepAliveKey(table map[string]string, path, key string, def, low, high int) (int, error) {
	said := strings.TrimSpace(table[key])
	if said == "" {
		return def, nil
	}
	n, err := strconv.Atoi(said)
	if err != nil || n < low || n > high {
		return 0, fmt.Errorf("the [%s] table of %s says %s = %q: it must be a whole number from %d to %d", VoiceKeepAliveTable, path, key, said, low, high)
	}
	return n, nil
}
