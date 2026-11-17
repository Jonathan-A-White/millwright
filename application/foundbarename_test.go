package application

import (
	"slices"
	"testing"
)

// foundPaths, and so every caller of it, drops a bare name that a path of the
// same text ends with (mw-gq6.1415).
func TestFoundPathsDropsABareNameAPathOfTheTextEndsWith(t *testing.T) {
	text := "The step (features/steps/sermons.steps.tsx:87) and sermons.steps.tsx:246 and Reader.tsx:12"
	want := []string{"features/steps/sermons.steps.tsx", "Reader.tsx"}
	if got := foundPaths(text); !slices.Equal(got, want) {
		t.Errorf("expected %v, got %v", want, got)
	}
}

// The symbols of a finding do not take the second mention of a file for a
// symbol (the caller foundSymbols).
func TestFoundSymbolsTakesNoBareFileNameItsFullPathNamesForASymbol(t *testing.T) {
	got := foundSymbols("`readRows` fails (features/steps/sermons.steps.tsx:87), again in sermons.steps.tsx:246")
	if len(got) != 1 || !got["readRows"] {
		t.Errorf("expected the symbol readRows alone, got %v", got)
	}
}

// The fault words of a finding take no word from a file name (the caller
// foundFaultWords).
func TestFoundFaultWordsTakeNoWordFromABareFileNameItsFullPathNames(t *testing.T) {
	got := foundFaultWords("rows vanish (features/steps/sermons.steps.tsx:87) and sermons.steps.tsx:246")
	if len(got) != 2 || !got["rows"] || !got["vanish"] {
		t.Errorf("expected the words rows and vanish, got %v", got)
	}
}
