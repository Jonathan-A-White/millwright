package application

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
)

// The per-rig tables beside [after_landing] that a landing's deploy reads and
// that must agree between hosts for the same reason the line must: a host with
// [after_landing_limit] at 5m stops a rig's deploy where another allows 20m, and
// one with another [after_landing_deploy_step] counts the rig deployed at another
// step (mw-gq6.1766). Each host leaves a fingerprint of each rig's value with the
// line's, in AfterLandingKey's note, as `<table> <rig> <hex>`; a new per-host
// table that must agree is added here and to AfterLandingTables's reader in
// cmd/mw.
const (
	AfterLandingLimitTable      = "after_landing_limit"
	AfterLandingDeployStepTable = "after_landing_deploy_step"
)

// AfterLandingTableNames are the tables whose per-rig values are compared, in
// the order they are said.
var AfterLandingTableNames = []string{AfterLandingLimitTable, AfterLandingDeployStepTable}

// AfterLandingNoValue is what the note says for a rig a host has a line for and
// no value in a table: an old note, written before the table was compared, and a
// host that holds no limit look alike when the rig is left out, so a host that
// never set [after_landing_limit] (the default 5m) was not flagged against one
// that set 20m (mw-gq6.1768). It is not a sha256, so no value can be taken for it.
const AfterLandingNoValue = "none"

// AfterLandingTableFingerprints is the part of the note that says which value a
// host holds for each rig in each table: the sha256 of each non-empty value, or
// AfterLandingNoValue for a rig named with an empty one, one `<table> <rig> <hex>`
// line per rig, sorted by table then rig. No rigs at all is "".
func AfterLandingTableFingerprints(tables map[string]map[string]string) string {
	names := make([]string, 0, len(tables))
	for name := range tables {
		names = append(names, name)
	}
	sort.Strings(names)
	var b strings.Builder
	for _, name := range names {
		rigs := make([]string, 0, len(tables[name]))
		for rig := range tables[name] {
			rigs = append(rigs, rig)
		}
		sort.Strings(rigs)
		for _, rig := range rigs {
			value := strings.TrimSpace(tables[name][rig])
			if value == "" {
				b.WriteString(name + " " + rig + " " + AfterLandingNoValue + "\n")
				continue
			}
			sum := sha256.Sum256([]byte(value))
			b.WriteString(name + " " + rig + " " + hex.EncodeToString(sum[:]) + "\n")
		}
	}
	return b.String()
}

// ParseAfterLandingTableFingerprints reads what AfterLandingTableFingerprints
// wrote: by table, the fingerprint of each rig it names. A line that is not
// `<table> <rig> <hex>` is skipped.
func ParseAfterLandingTableFingerprints(note string) map[string]map[string]string {
	out := map[string]map[string]string{}
	for _, line := range strings.Split(note, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 3 {
			continue
		}
		if out[fields[0]] == nil {
			out[fields[0]] = map[string]string{}
		}
		out[fields[0]][fields[1]] = fields[2]
	}
	return out
}
