// Package hostload reads how busy this host is from /proc/loadavg and how much
// memory it has left from /proc/meminfo.
package hostload

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/doctor"
)

// Proc reads the load average of the machine it runs on.
type Proc struct {
	// File is where the kernel keeps the load average; empty is /proc/loadavg.
	File string
	// MemFile is where the kernel keeps the memory figures; empty is /proc/meminfo.
	MemFile string
	// Cores is how many cores there are; zero is runtime.NumCPU.
	Cores int
	// SwapFile is where the kernel keeps its swap counters; empty is
	// /proc/vmstat. SwapSample is how long the swap rate is sampled over, which
	// is how long a Load takes: zero samples nothing and leaves the swap rate
	// unknown, so that only a close-out pays for it.
	SwapFile   string
	SwapSample time.Duration
	// Sleep is how the swap sample waits out SwapSample; nil is time.Sleep.
	Sleep func(time.Duration)
	// DriveFile is the report the Windows task writes into a WSL host of the
	// free space on its drive; empty is doctor.WSLDriveFreeFile under the home
	// directory. Now is the clock that report's age is read on; nil is time.Now.
	DriveFile string
	Now       func() time.Time
	// Mount, MountDrive and Statfs are the Windows drive as this host sees it
	// (the Laptop's /mnt/c, reported as "C:"): with a Mount that is there, the
	// reading writes the report itself from the mount first, so no Windows task
	// is needed (mw-gq6.1490). Empty Mount and DriveFile read the config's
	// [doctor] windows_drive_mount; a test that names its DriveFile reads none.
	// Statfs nil reads the real free bytes.
	Mount      string
	MountDrive string
	Statfs     func(path string) (int64, error)
	// ProcDir is where the kernel lists the host's processes, read for the part
	// workers running their part (LoadReading.PartWorkers); empty is /proc.
	ProcDir string
	// VersionFile is the kernel's version line, read to know a WSL host
	// (LoadReading.WSL, mw-gq6.1527); empty is /proc/version.
	VersionFile string
}

var _ application.HostLoad = Proc{}

// Load implements application.HostLoad.
func (p Proc) Load(_ context.Context) (application.LoadReading, error) {
	file := p.File
	if file == "" {
		file = "/proc/loadavg"
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		return application.LoadReading{}, fmt.Errorf("reading the load average: %w", err)
	}
	fields := strings.Fields(string(raw))
	if len(fields) == 0 {
		return application.LoadReading{}, fmt.Errorf("reading the load average: %s is empty", file)
	}
	load, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return application.LoadReading{}, fmt.Errorf("reading the load average: %q is not a number", fields[0])
	}
	cores := p.Cores
	if cores == 0 {
		cores = runtime.NumCPU()
	}
	reading := application.LoadReading{Load: load, Cores: cores}
	reading.MemAvailableMB, reading.MemKnown = p.memAvailableMB()
	reading.DriveName, reading.DriveFreeBytes, reading.DriveKnown = p.driveFree()
	reading.SwapInKBPerS, reading.SwapOutKBPerS, reading.SwapKnown = p.swapRate()
	reading.PartWorkers = p.partWorkers()
	reading.WSL = p.wsl()
	return reading, nil
}

// wsl is whether the kernel's version line names Microsoft's WSL kernel
// ('6.6.87.2-microsoft-standard-WSL2', or WSL 1's '-Microsoft'); a file that
// cannot be read says it is not.
func (p Proc) wsl() bool {
	file := p.VersionFile
	if file == "" {
		file = "/proc/version"
	}
	raw, err := os.ReadFile(file)
	return err == nil && strings.Contains(strings.ToLower(string(raw)), "microsoft")
}

// swapRate is how many kilobytes a second the host swapped in and out over
// SwapSample, from the pswpin and pswpout counters of the swap file, and whether
// it could be told: no sample time, or a file that cannot be read, leaves it
// unknown rather than failing the whole reading.
func (p Proc) swapRate() (in, out float64, known bool) {
	if p.SwapSample <= 0 {
		return 0, 0, false
	}
	file := p.SwapFile
	if file == "" {
		file = "/proc/vmstat"
	}
	inBefore, outBefore, ok := swapCounters(file)
	if !ok {
		return 0, 0, false
	}
	started := time.Now()
	sleep := p.Sleep
	if sleep == nil {
		sleep = time.Sleep
	}
	sleep(p.SwapSample)
	inAfter, outAfter, ok := swapCounters(file)
	elapsed := time.Since(started).Seconds()
	if !ok || elapsed <= 0 || inAfter < inBefore || outAfter < outBefore {
		return 0, 0, false
	}
	kb := float64(os.Getpagesize()) / 1024
	return float64(inAfter-inBefore) * kb / elapsed, float64(outAfter-outBefore) * kb / elapsed, true
}

// swapCounters are the pages swapped in and out since boot.
func swapCounters(file string) (in, out uint64, ok bool) {
	raw, err := os.ReadFile(file)
	if err != nil {
		return 0, 0, false
	}
	var gotIn, gotOut bool
	for _, line := range strings.Split(string(raw), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		switch fields[0] {
		case "pswpin":
			in, gotIn = parseCounter(fields[1])
		case "pswpout":
			out, gotOut = parseCounter(fields[1])
		}
	}
	return in, out, gotIn && gotOut
}

func parseCounter(text string) (uint64, bool) {
	n, err := strconv.ParseUint(text, 10, 64)
	return n, err == nil
}

// memAvailableMB is the MemAvailable line of the memory file in megabytes, and
// whether there was one to read: a file that cannot be read, or has no such
// line, leaves the memory unknown rather than failing the whole reading.
func (p Proc) memAvailableMB() (int64, bool) {
	file := p.MemFile
	if file == "" {
		file = "/proc/meminfo"
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		return 0, false
	}
	for _, line := range strings.Split(string(raw), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] != "MemAvailable:" {
			continue
		}
		kb, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil || kb < 0 {
			return 0, false
		}
		return kb / 1024, true
	}
	return 0, false
}

// driveFree is the Windows drive's free space from the task's report, and
// whether there was a current one: no report (the host is not WSL), one that
// cannot be read, or one older than doctor.WSLDriveFreeMaxAge (the task has
// stopped) leaves it unknown rather than failing the whole reading
// (mw-gq6.1381).
func (p Proc) driveFree() (name string, free int64, known bool) {
	now := time.Now
	if p.Now != nil {
		now = p.Now
	}
	mount, mountDrive := p.Mount, p.MountDrive
	if mount == "" && p.DriveFile == "" {
		// A config that cannot be read leaves the Windows task's report to read.
		mount, mountDrive, _ = config.DoctorWindowsDrive()
	}
	_ = doctor.RefreshWSLDriveFree(p.DriveFile, mount, mountDrive, p.Statfs, now())
	drive, free, at, ok := doctor.ReadWSLDriveFree(p.DriveFile)
	if !ok {
		return "", 0, false
	}
	if now().Sub(at) > doctor.WSLDriveFreeMaxAge {
		return "", 0, false
	}
	return drive, free, true
}
