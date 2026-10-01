// Package hostload reads how busy this host is from /proc/loadavg.
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
	return application.LoadReading{Load: load, Cores: cores}, nil
}
