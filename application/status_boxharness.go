package application

// The harness half of mw status: what it asks of each cloud box to fill the
// HARNESS section (the session panes the harness crashed in, the integrity
// mark), split off application/status.go to keep it under the length ceiling
// (mw-gq6.1236).

import (
	"context"
	"strings"
	"time"
)

// HarnessCrashDay is how far back a box's panes are read for the harness's
// crashes, and BoxHarnessCrashesNamed how many of them in that time name the
// box: one crash is the harness's bad luck, two on a box say the box is
// suspect (mw-gq6.1139).
const (
	HarnessCrashDay          = 24 * time.Hour
	BoxHarnessCrashesNamed   = 2
	harnessCrashBannerPrefix = "Bun v"
)

// BoxPane is one session pane a box kept: Window names it (the story it ran),
// At is when it was last active and Text what it showed.
type BoxPane struct {
	Window string
	At     time.Time
	Text   string
}

// BoxPanes is what mw status asks of a cloud box to count its harness crashes:
// the session panes it kept at or after since. The adapter reaches the box over
// ssh; an error is a box that could not be read, not a box with none.
type BoxPanes interface {
	Panes(ctx context.Context, box string, since time.Time) ([]BoxPane, error)
}

// BoxIntegrity is what mw status asks of a cloud box for the integrity mark it
// keeps (integrityMark): when, in RFC 3339, its factory checkout last met a
// corrupt object or a session of it died with a segmentation fault. An empty
// string is a box with none; an error is a box that could not be read.
type BoxIntegrity interface {
	IntegrityMark(ctx context.Context, box string) (string, error)
}

// isHarnessCrash reports whether a pane shows the harness crashing: the Bun
// runtime's banner ("Bun v1.4.4 (...) Linux x64") together with its crash
// words. A segfault alone is not it (any program can), nor is a pane that only
// mentions a Bun version.
func isHarnessCrash(text string) bool {
	if !strings.Contains(text, harnessCrashBannerPrefix) {
		return false
	}
	low := strings.ToLower(text)
	return strings.Contains(low, "segmentation fault") || strings.Contains(low, "panic(") || strings.Contains(low, "has crashed")
}

// BoxHarness is one cloud box's line of HARNESS: the Claude Code version the
// cloud's book says it was brought level to ("" when it never was), the panes
// of the harness's crashes in the last day, and why its panes could not be
// read when they could not.
type BoxHarness struct {
	Box      string
	Version  string
	Crashes  []BoxPane
	ReadFail string

	// Integrity is when the box's integrity mark was written, when that was
	// under integrityFaultWindow before now; zero otherwise. IntegrityFail is why
	// the mark could not be read.
	Integrity     time.Time
	IntegrityFail string
}

// boxHarness reads HARNESS for the boxes of the cloud's reading. A box still
// being made or restored runs no session and is not reached.
func (s Status) boxHarness(ctx context.Context, reading *CloudReading) []BoxHarness {
	var out []BoxHarness
	since := reading.Now.Add(-HarnessCrashDay)
	for _, box := range reading.Boxes {
		if box.Making || box.Restoring {
			continue
		}
		line := BoxHarness{Box: box.Name, Version: box.Harness}
		if s.BoxPanes != nil {
			panes, err := s.BoxPanes.Panes(ctx, box.Name, since)
			if err != nil {
				line.ReadFail = oneLine(err.Error())
			}
			for _, pane := range panes {
				if !pane.At.Before(since) && isHarnessCrash(pane.Text) {
					line.Crashes = append(line.Crashes, pane)
				}
			}
		}
		if s.BoxIntegrity != nil {
			line.Integrity, line.IntegrityFail = s.boxIntegrity(ctx, box.Name, reading.Now)
		}
		out = append(out, line)
	}
	return out
}

// boxIntegrity reads a box's integrity mark: the time of a fault under an hour
// old, or why the mark could not be read (mw-gq6.1200).
func (s Status) boxIntegrity(ctx context.Context, box string, now time.Time) (time.Time, string) {
	held, err := s.BoxIntegrity.IntegrityMark(ctx, box)
	if err != nil {
		return time.Time{}, oneLine(err.Error())
	}
	at, err := time.Parse(time.RFC3339, strings.TrimSpace(held))
	if err != nil || now.Sub(at) >= integrityFaultWindow {
		return time.Time{}, ""
	}
	return at, ""
}
