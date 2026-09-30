package application

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// AutoBackupInterval is how long the home waits between two backups when
// beads_sync is auto and beads_backup_minutes says nothing: short, because
// the backup is all that survives the home dying, and what it lacks is what is
// lost.
const AutoBackupInterval = 5 * time.Minute

// BoostServerSuffix is what follows the home's name to make the WireGuard name
// its beads Dolt server answers on: laptop.mw.
const BoostServerSuffix = ".mw"

// BoostServerHost is the host a boost's bd reaches the home's Dolt server on:
// override when one is said, otherwise the home's name on WireGuard.
func BoostServerHost(home, override string) string {
	if override != "" {
		return override
	}
	return home + BoostServerSuffix
}

// HomeServerHost is the host the home's own bd reaches its Dolt server on:
// dolt-beads.service binds every address, so the loopback one always answers.
const HomeServerHost = "127.0.0.1"

// SessionServerHost is the BEADS_DOLT_SERVER_HOST a session this host starts is
// given, to follow the home as it moves rather than what beads.env said when it
// was written (mw-j3iis.2). On a boost it is the home's server (BoostServerHost);
// on the home it is HomeServerHost, but only when holdsServer says the vault is
// in server mode (it holds .beads/dolt), so a home that stayed embedded has
// nothing set. It is empty, and the session keeps what its environment had, for
// any beads_sync but auto and for a home that cannot be told: nothing is guessed.
func SessionServerHost(ctx context.Context, files HomeFile, host string, configured BeadsSyncMode, override string, holdsServer bool) string {
	if configured != BeadsSyncAuto {
		return ""
	}
	resolved, err := ResolveBeadsSync(ctx, files, host, configured)
	if err != nil {
		return ""
	}
	switch {
	case resolved.Mode == BeadsSyncShared:
		return BoostServerHost(resolved.Home, override)
	case holdsServer:
		return HomeServerHost
	}
	return ""
}

// ResolvedBeadsSync is a beads_sync mode as this host acts on it: never auto.
type ResolvedBeadsSync struct {
	// Mode is remote, backup or shared.
	Mode BeadsSyncMode
	// Why says how an auto mode was chosen: "auto: home" or "auto: boost of
	// laptop". Empty for a mode that was configured as it is.
	Why string
	// Home is the home host's name, set only for auto.
	Home string
}

// ResolveBeadsSync turns the configured mode into the one this host acts on.
// Remote, backup and shared are themselves; auto is backup on the home and
// shared on a boost. A home that cannot be told is a *HomeUnknown, and the
// caller must do nothing: auto never guesses a writer.
func ResolveBeadsSync(ctx context.Context, files HomeFile, host string, configured BeadsSyncMode) (ResolvedBeadsSync, error) {
	if configured != BeadsSyncAuto {
		return ResolvedBeadsSync{Mode: configured}, nil
	}
	if files == nil {
		return ResolvedBeadsSync{}, &HomeUnknown{Why: errors.New("there is no home file to read")}
	}
	record, err := WhereIsHome(ctx, files)
	if err != nil {
		return ResolvedBeadsSync{}, err
	}
	if record.Host == host {
		return ResolvedBeadsSync{Mode: BeadsSyncBackup, Why: "auto: home", Home: record.Host}, nil
	}
	return ResolvedBeadsSync{
		Mode: BeadsSyncShared,
		Why:  fmt.Sprintf("auto: boost of %s", record.Host),
		Home: record.Host,
	}, nil
}

// resolveAuto is ResolveBeadsSync for this sync, refusing in plain words when
// the home cannot be told.
func (s Sync) resolveAuto(ctx context.Context) (ResolvedBeadsSync, error) {
	resolved, err := ResolveBeadsSync(ctx, s.Home, s.Host, BeadsSyncAuto)
	if err != nil {
		return ResolvedBeadsSync{}, fmt.Errorf("beads_sync is auto but %w: set beads_sync to remote, backup or shared, or fix the home file (nothing was done)", err)
	}
	return resolved, nil
}
