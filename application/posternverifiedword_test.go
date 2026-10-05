package application

import "testing"

func TestBeginsWithVerifiedWord(t *testing.T) {
	for text, want := range map[string]bool{
		"VERIFIED":           true,
		"VERIFIED. ok":       true,
		"  VERIFIED, thanks": true,
		"VERIFIED!":          true,
		"VERIFIEDLY":         false,
		"verified":           false,
		"I verified it":      false,
		"NOT VERIFIED":       false,
		"":                   false,
	} {
		if got := beginsWithVerifiedWord(text); got != want {
			t.Errorf("beginsWithVerifiedWord(%q) = %v, want %v", text, got, want)
		}
	}
}
