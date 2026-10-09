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

	"github.com/Jonathan-A-White/millwright/application"
)

// Proc reads the load average of the machine it runs on.
type Proc struct {
	// File is where the kernel keeps the load average; empty is /proc/loadavg.
	File string
	// MemFile is where the kernel keeps the memory figures; empty is /proc/meminfo.
	MemFile string
	// Cores is how many cores there are; zero is runtime.NumCPU.
	Cores int
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
	return reading, nil
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
