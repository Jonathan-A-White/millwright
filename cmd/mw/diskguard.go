package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/diskguard"
	"github.com/Jonathan-A-White/millwright/infrastructure/hostload"
	"github.com/Jonathan-A-White/millwright/infrastructure/hostlock"

	"github.com/spf13/cobra"
)

// diskGuardLockFile is the file, in the dispatch state directory, that keeps two
// ticks from running the disk guard at once.
const diskGuardLockFile = "disk-guard.lock"

// hostDiskGuard is this host's stop floor (mw-6ww.185.7): stop_free_gb of the
// config's room, over the host's own give-way, load reading, sweep and tidies. It
// is false when the host has no give-way (no state directory) or no home
// directory: it is held to nothing then.
func hostDiskGuard(host string, room config.RoomSettings, mail application.Mailbox, out io.Writer) (application.DiskGuard, bool) {
	way, ok := hostGiveWay(host)
	if !ok {
		return application.DiskGuard{}, false
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return application.DiskGuard{}, false
	}
	guard := application.DiskGuard{
		Way: way, Load: hostload.Proc{}, Holders: diskguard.Holders{Home: home}, Mailbox: mail,
		Lock:       hostlock.NewTry(filepath.Join(home, DispatchStateDir), diskGuardLockFile),
		StopFreeGB: room.StopFreeGB, Host: host, Out: out,
	}
	if self, err := os.Executable(); err == nil {
		guard.Free = diskguard.Freer{Mw: self}
	}
	return guard, true
}

// runDiskGuard is the guard's tick, made before the dispatch starts anything: a
// host under its stop floor freezes its Builders, frees space and starts nothing
// (the freeze is give way's marker, which the dispatch reads). A guard that fails
// costs the tick that look and nothing else.
func runDiskGuard(cmd *cobra.Command, host string, room config.RoomSettings, mail application.Mailbox) {
	guard, ok := hostDiskGuard(host, room, mail, cmd.OutOrStdout())
	if !ok {
		return
	}
	if _, err := guard.Run(cmd.Context()); err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "no disk guard this tick: %v\n", err)
	}
}
