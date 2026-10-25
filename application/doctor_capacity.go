package application

import (
	"fmt"
	"strings"

	"github.com/Jonathan-A-White/millwright/domain"
)

// CapacityReport is what mw doctor capacity prints for one rig: a line per
// capacity fact, the value the rig's file records or that it is missing, and
// the facts it does not record (mw-gq6.761).
type CapacityReport struct {
	Rig     string
	Lines   []string
	Missing []string
}

// Faulty reports whether the rig's file leaves any capacity fact out.
func (r CapacityReport) Faulty() bool { return len(r.Missing) > 0 }

// ReportCapacity lists the capacity facts a deployed rig records before an
// event and names each one its file does not hold. It reads nothing and
// changes nothing.
func ReportCapacity(rig string, c domain.Capacity) CapacityReport {
	report := CapacityReport{Rig: rig}
	file := fmt.Sprintf("rigs/%s.toml", rig)
	for _, fact := range []struct {
		label, key, unit string
		value            int
	}{
		{"expected concurrent viewers", "capacity_viewers", "", c.ExpectedViewers},
		{"SSE cap", "capacity_sse_cap", "", c.SSECap},
		{"nginx worker_connections", "capacity_nginx_worker_connections", "", c.NginxWorkerConnections},
		{"memory free", "capacity_memory_free_mb", " MB", c.MemoryFreeMB},
	} {
		if fact.value > 0 {
			report.Lines = append(report.Lines, fmt.Sprintf("%s: %s %d%s", rig, fact.label, fact.value, fact.unit))
			continue
		}
		line := fmt.Sprintf("%s: %s MISSING (%s in %s)", rig, fact.label, fact.key, file)
		report.Lines = append(report.Lines, line)
		report.Missing = append(report.Missing, line)
	}
	return report
}

// String is the report as printed.
func (r CapacityReport) String() string {
	var b strings.Builder
	for _, line := range r.Lines {
		b.WriteString(line + "\n")
	}
	if !r.Faulty() {
		b.WriteString("ok: " + r.Rig + " records every capacity fact\n")
	}
	return b.String()
}
