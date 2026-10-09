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
	reading.SwapInKBPerS, reading.SwapOutKBPerS, reading.SwapKnown = p.swapRate()
	return reading, nil
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
	time.Sleep(p.SwapSample)
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
