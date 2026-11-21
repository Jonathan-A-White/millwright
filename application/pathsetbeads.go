package application

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/Jonathan-A-White/millwright/domain"
)

// servedBeadsWords say a criterion needs the served beads: the dolt-beads trace,
// or a run against the beads server on 127.0.0.1:3307 (BEADS_DOLT_*). Only a
// home host has that server, and a Boost box has none (mw-gq6.1555).
var servedBeadsWords = regexp.MustCompile(`dolt-beads/trace|BEADS_DOLT|127\.0\.0\.1:3307`)

// servedBeadsWarning is the one warning, empty for none, of a story whose host is
// auto and whose acceptance has a criterion that needs the served beads: dispatch
// may start it on a box with no beads server, where its Builder cannot run that
// criterion. A warning only; the host is the Mayor's to pin.
func servedBeadsWarning(id, host, acceptance string) string {
	if h := strings.TrimSpace(host); h != "" && h != domain.HostAuto {
		return ""
	}
	var numbers []string
	for _, criterion := range domain.AcceptanceCriteria(acceptance) {
		if servedBeadsWords.MatchString(criterion.Text) {
			numbers = append(numbers, fmt.Sprint(criterion.Number))
		}
	}
	if len(numbers) == 0 {
		return ""
	}
	return fmt.Sprintf("WARN: %s: criterion %s of its acceptance needs the served beads (contrib/dolt-beads/trace or the server on 127.0.0.1:3307) "+
		"and its host is host=auto, so it may start on a box with no beads server, where no Builder can run it. "+
		"Pin it to the beads home: mw path set %s host=<home host> host_why=\"needs the served beads\"", id, strings.Join(numbers, ", "), id)
}
