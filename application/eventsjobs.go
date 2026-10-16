package application

import (
	"fmt"
	"io"
	"strings"
)

// WriteEventJobs lists the follower's jobs one to a line: the job's name, then
// what springs it, as mw events jobs prints them. What a job's fields say is
// what the line says, so a job added to the follower is listed without a
// second place to keep.
func WriteEventJobs(out io.Writer, jobs []SpringJob) {
	for _, job := range jobs {
		fmt.Fprintf(out, "%-16s %s\n", job.Name, job.springs())
	}
}

// springs says in a few words what runs the job.
func (j SpringJob) springs() string {
	var how []string
	if j.Wants != nil {
		how = append(how, "events")
	}
	if j.Probe != nil {
		how = append(how, "a check each pass")
	}
	if j.Watch != nil {
		how = append(how, "a watch of its own")
	}
	if j.Every > 0 {
		reason := j.Reason
		if reason == "" {
			reason = "clock"
		}
		how = append(how, fmt.Sprintf("every %s (%s)", j.Every, reason))
	}
	said := strings.Join(how, ", ")
	if said == "" {
		said = "nothing but a manual run"
	}
	if j.About != "" {
		said += "; " + j.About
	}
	return said
}
