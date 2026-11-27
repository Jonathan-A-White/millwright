package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
)

// AfterLandingKey is where a host leaves a fingerprint of each rig's
// [after_landing] line, on every sync, beside HarnessKey: mw doctor's
// after-landing-agrees check reads it for each other host (mw-gq6.1765). The
// value is one `<rig> <sha256>` line per rig the host has a line for; the line
// itself is never published, as it may name a host or a path.
func AfterLandingKey(host string) string { return "host." + host + ".after_landing" }

// AfterLandingFingerprints is the note value that says which [after_landing]
// line a host holds for each rig: the sha256 of each non-empty line, one
// `<rig> <hex>` line per rig, sorted by rig. A rig with an empty line has none,
// and is left out. No lines at all is "".
func AfterLandingFingerprints(lines map[string]string) string {
	rigs := make([]string, 0, len(lines))
	for rig, line := range lines {
		if strings.TrimSpace(line) != "" {
			rigs = append(rigs, rig)
		}
	}
	sort.Strings(rigs)
	var b strings.Builder
	for _, rig := range rigs {
		sum := sha256.Sum256([]byte(strings.TrimSpace(lines[rig])))
		b.WriteString(rig + " " + hex.EncodeToString(sum[:]) + "\n")
	}
	return b.String()
}

// ParseAfterLandingFingerprints reads what AfterLandingFingerprints wrote: the
// fingerprint of each rig it names. A line that is not `<rig> <hex>` is skipped.
func ParseAfterLandingFingerprints(note string) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(note, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 {
			out[fields[0]] = fields[1]
		}
	}
	return out
}

// noteAfterLanding leaves a fingerprint of each of this host's [after_landing]
// lines for the other hosts to read, as noteHarness leaves the version of its
// Claude Code; best effort. A host with no line any more takes its note back, so
// the others do not compare with a line it no longer holds; a config that
// cannot be read leaves the note as it was. A note already saying it is not
// written again.
func (s Sync) noteAfterLanding(ctx context.Context) {
	if s.AfterLanding == nil {
		return
	}
	lines, err := s.AfterLanding()
	if err != nil {
		return
	}
	want := AfterLandingFingerprints(lines)
	kept, err := s.Tracker.Note(ctx, AfterLandingKey(s.Host))
	if err != nil || kept == want {
		return
	}
	if want == "" {
		_ = s.Tracker.ClearNote(ctx, AfterLandingKey(s.Host))
		return
	}
	_ = s.Tracker.SetNote(ctx, AfterLandingKey(s.Host), want)
}
