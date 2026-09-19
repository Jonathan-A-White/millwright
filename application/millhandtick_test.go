package application

import "testing"

func TestMayorMayBeGone(t *testing.T) {
	for line, want := range map[string]bool{
		"down signs=none":         true,
		"down signs=blog,sync":    true,
		"unwell mayor_gone":       true,
		"unwell load1,mayor_gone": true,
		"unwell mayor=gone":       true,
		"unwell load1":            false,
		"unwell acting_mismatch":  false,
		"unwell no-verdict":       false,
		"unwell":                  false,
		"stale":                   false,
		"ok":                      false,
		"":                        false,
	} {
		if got := mayorMayBeGone(line); got != want {
			t.Errorf("mayorMayBeGone(%q) = %v, want %v", line, got, want)
		}
	}
}
