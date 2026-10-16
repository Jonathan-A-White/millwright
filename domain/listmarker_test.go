package domain

import "testing"

func TestAListLinesBulletOrNumberIsTrimmed(t *testing.T) {
	for line, want := range map[string]string{
		"Sweep: x":        "Sweep: x",
		"  Sweep: x":      "Sweep: x",
		"5. Sweep: x":     "Sweep: x",
		"5) Sweep: x":     "Sweep: x",
		"12.  Sweep: x":   "Sweep: x",
		"- Sweep: x":      "Sweep: x",
		"* Sweep: x":      "Sweep: x",
		"+ Sweep: x":      "Sweep: x",
		"• Sweep: x":      "Sweep: x",
		"  - 5. Sweep: x": "5. Sweep: x",
		"-Sweep: x":       "-Sweep: x",
		"5.Sweep: x":      "5.Sweep: x",
		"2.5 versions":    "2.5 versions",
		"**Sweep:** x":    "**Sweep:** x",
		"":                "",
	} {
		if got := TrimListMarker(line); got != want {
			t.Errorf("TrimListMarker(%q) = %q, want %q", line, got, want)
		}
	}
}
