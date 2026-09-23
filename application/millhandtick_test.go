package application

import (
	"context"
	"strings"
	"testing"
)

// fakeDoctorNotesByKey is a minimal in-memory DoctorNotes for tests that only
// need to seed keys directly and read what the tick did with them.
type fakeDoctorNotesByKey struct {
	notes map[string]string
}

func (f *fakeDoctorNotesByKey) Note(_ context.Context, key string) (string, error) {
	return f.notes[key], nil
}

func (f *fakeDoctorNotesByKey) SetNote(_ context.Context, key, value string) error {
	f.notes[key] = value
	return nil
}

func (f *fakeDoctorNotesByKey) ClearNote(_ context.Context, key string) error {
	delete(f.notes, key)
	return nil
}

func (f *fakeDoctorNotesByKey) NotesWithPrefix(_ context.Context, prefix string) (map[string]string, error) {
	found := map[string]string{}
	for key, value := range f.notes {
		if strings.HasPrefix(key, prefix) {
			found[key] = value
		}
	}
	return found, nil
}

// TestATickWakesOnlyForItsOwnHostsDoctorNotes is the regression for
// mw-i80dx.8: a tick must read only its own host's doctor.<host>.<check>
// notes, and leave another host's alone.
func TestATickWakesOnlyForItsOwnHostsDoctorNotes(t *testing.T) {
	notes := &fakeDoctorNotesByKey{notes: map[string]string{
		DoctorNoteKey("laptop", "wifi"): "2026-09-23T12:00:00Z faulty no reach",
		DoctorNoteKey("vps", "tunnel"):  "2026-09-23T12:00:00Z damped restart failed",
	}}
	tick := MillhandTick{DoctorNotes: notes, Host: "laptop"}

	reasons, notes2, err := tick.doctor(context.Background())
	if err != nil {
		t.Fatalf("doctor: %v", err)
	}
	if len(notes2) != 0 {
		t.Errorf("expected no notes about a problem reading the store, got %v", notes2)
	}
	if len(reasons) != 1 || !strings.Contains(reasons[0], "wifi") {
		t.Fatalf("expected one reason naming wifi, got %v", reasons)
	}
	for _, r := range reasons {
		if strings.Contains(r, "tunnel") {
			t.Errorf("expected no reason for the vps's tunnel note, got %v", reasons)
		}
	}
}

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
