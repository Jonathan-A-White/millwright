package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DoctorWindowsDrive reports where a WSL host sees the Windows drive holding
// its vhdx and what its report calls it: the `[doctor]` table's
// `windows_drive_mount` key of ~/.config/mw/config.toml (a full path, the
// Laptop's "/mnt/c") and `windows_drive_name` ("C:"; the mount's last name
// upper-cased and followed by a colon when unset). Both are empty when the key
// is unset, which is a host whose drive is sealed from WSL (the desktop): the
// Windows task's report is read there (mw-gq6.1490).
func DoctorWindowsDrive() (mount, drive string, err error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", fmt.Errorf("there is no home directory to read %s in: %w", File, err)
	}
	path := filepath.Join(home, File)
	table, err := tableIn(path, DoctorTable)
	if err != nil {
		return "", "", err
	}
	mount = strings.TrimSpace(table["windows_drive_mount"])
	if mount == "" {
		return "", "", nil
	}
	if !filepath.IsAbs(mount) {
		return "", "", fmt.Errorf("the doctor's windows_drive_mount is %q in %s: it must be a full path", mount, path)
	}
	if drive = strings.TrimSpace(table["windows_drive_name"]); drive != "" {
		return mount, drive, nil
	}
	return mount, strings.ToUpper(filepath.Base(mount)) + ":", nil
}
