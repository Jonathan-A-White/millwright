package application_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

func TestVisionSectionCutsOnACharacterBoundary(t *testing.T) {
	visions := apptest.NewFakeVisions()
	// "é" is two bytes, so byte 3000 falls inside one when the text starts
	// with a single ASCII byte.
	visions.Set("/rigs/cairn", "x"+strings.Repeat("é", 2000))
	got := application.VisionSection(context.Background(), visions, "/rigs/cairn")
	body := strings.TrimSuffix(strings.TrimPrefix(got, "THE APP'S VISION (/rigs/cairn/VISION.md)\n"), "\n(cut)")
	if body == got || len(body) > application.VisionCap || len(body) < application.VisionCap-1 {
		t.Fatalf("expected a cut of about %d bytes, got %d bytes:\n%.80s", application.VisionCap, len(body), got)
	}
	if strings.ContainsRune(body, '�') {
		t.Fatalf("the cut split a character")
	}
}

func TestVisionSectionSaysWhenTheFileCannotBeRead(t *testing.T) {
	visions := apptest.NewFakeVisions()
	visions.Err = errors.New("permission denied")
	got := application.VisionSection(context.Background(), visions, "/rigs/cairn")
	if !strings.Contains(got, "/rigs/cairn/VISION.md") || !strings.Contains(got, "permission denied") {
		t.Fatalf("got %q", got)
	}
}
