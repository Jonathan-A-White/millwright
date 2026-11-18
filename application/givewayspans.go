package application

import (
	"context"
	"encoding/json"
	"sort"
	"time"
)

// A host gives way to the person at its screen by stopping every process of its
// Builder sessions and of the factory's apart scopes (giveway.go). A stopped
// process does not run, but the clocks go on: a wait of a check that was stopped
// with its limit running wakes with the span the host gave way spent, though it
// waited for nobody in it (mw-gq6.1458). The span is the person at the screen,
// not the holder's wait, so the waits that a give-way stops ask the host's
// give-way record how long it gave way and do not count it.

// GiveWaySpanLines is how many of giveway.log's last lines are read for the spans
// a host gave way: a renew every five minutes is under 300 lines a day, so this
// reaches back days, past the longest wait a check holds.
const GiveWaySpanLines = 2000

// GiveWaySpans is how long a host gave way between two times: the part of each
// span it gave way that falls between from and to, counted once.
type GiveWaySpans interface {
	GaveWayBetween(ctx context.Context, from, to time.Time) time.Duration
}

var _ GiveWaySpans = GiveWay{}

// giveWaySpan is one span a host gave way.
type giveWaySpan struct{ from, to time.Time }

// gaveWaySpan is the span an off (or expired) line of giveway.log says the host
// gave way: gave_way_ms back from when the off began, to the last continue
// (took_ms). ok is false for any other line.
func gaveWaySpan(line string) (giveWaySpan, bool) {
	var entry GiveWayEntry
	if json.Unmarshal([]byte(line), &entry) != nil || entry.GaveWayMs == nil {
		return giveWaySpan{}, false
	}
	if entry.Event != "off" && entry.Event != "expired" {
		return giveWaySpan{}, false
	}
	to := entry.At
	if entry.TookMs != nil {
		to = to.Add(time.Duration(*entry.TookMs) * time.Millisecond)
	}
	return giveWaySpan{from: entry.At.Add(-time.Duration(*entry.GaveWayMs) * time.Millisecond), to: to}, true
}

// GaveWayBetween implements GiveWaySpans from the host's own files: each off and
// expired line of giveway.log, and the marker while it stands, which is counted
// up to to (a process woken by the off reads it before the off has written its
// line; Off writes the line before it takes the marker away, so one of the two
// always names the span). Spans that overlap are counted once. A log or marker
// that cannot be read counts as no span: the wait is then what it always was.
func (g GiveWay) GaveWayBetween(ctx context.Context, from, to time.Time) time.Duration {
	if !to.After(from) {
		return 0
	}
	var spans []giveWaySpan
	if g.Log != nil {
		if lines, err := g.Log.Tail(ctx, GiveWaySpanLines); err == nil {
			for _, line := range lines {
				if span, ok := gaveWaySpan(line); ok {
					spans = append(spans, span)
				}
			}
		}
	}
	if g.Marker != nil {
		if marker, there, err := g.Marker.Read(ctx); err == nil && there && !marker.Since.IsZero() {
			spans = append(spans, giveWaySpan{from: marker.Since, to: to})
		}
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i].from.Before(spans[j].from) })
	var total time.Duration
	var reached time.Time
	for _, span := range spans {
		start, end := latest(span.from, from, reached), earliest(span.to, to)
		if end.After(start) {
			total += end.Sub(start)
		}
		if end.After(reached) {
			reached = end
		}
	}
	return total
}

func latest(ts ...time.Time) time.Time {
	out := ts[0]
	for _, t := range ts[1:] {
		if t.After(out) {
			out = t
		}
	}
	return out
}

func earliest(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

// lastGaveWay is the last span giveway.log says the host gave way, read from its
// lines oldest first; ok is false when none says one.
func lastGaveWay(lines []string) (span giveWaySpan, ok bool) {
	for i := len(lines) - 1; i >= 0; i-- {
		if span, ok := gaveWaySpan(lines[i]); ok {
			return span, true
		}
	}
	return giveWaySpan{}, false
}
