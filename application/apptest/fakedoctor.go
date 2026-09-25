package apptest

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

var (
	_ application.DoctorState = (*FakeDoctorState)(nil)
	_ application.DoctorLog   = (*FakeDoctorLog)(nil)
	_ application.DoctorCheck = (*FakeDoctorCheck)(nil)
	_ application.DoctorNotes = (*FakeDoctorNotes)(nil)
)

// FakeDoctorState is an in-memory application.DoctorState: one episode per
// check, kept until Reset. Its zero value has no episode kept for any check.
type FakeDoctorState struct {
	mu       sync.Mutex
	episodes map[string]application.DoctorEpisode
	saves    int
}

// NewFakeDoctorState is a state with no episode kept for any check.
func NewFakeDoctorState() *FakeDoctorState {
	return &FakeDoctorState{episodes: map[string]application.DoctorEpisode{}}
}

// Load implements application.DoctorState.
func (f *FakeDoctorState) Load(_ context.Context, check string) (application.DoctorEpisode, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.episodes[check], nil
}

// Save implements application.DoctorState.
func (f *FakeDoctorState) Save(_ context.Context, check string, episode application.DoctorEpisode) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.episodes[check] = episode
	f.saves++
	return nil
}

// Reset implements application.DoctorState.
func (f *FakeDoctorState) Reset(_ context.Context, check string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.episodes, check)
	return nil
}

// Episode is what is kept for a check, the zero value if none is.
func (f *FakeDoctorState) Episode(check string) application.DoctorEpisode {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.episodes[check]
}

// Saves is how many times the state was written.
func (f *FakeDoctorState) Saves() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.saves
}

// FakeDoctorLog is an in-memory application.DoctorLog: every line appended,
// in order. Its zero value is an empty log.
type FakeDoctorLog struct {
	mu    sync.Mutex
	lines []string
}

// Append implements application.DoctorLog.
func (f *FakeDoctorLog) Append(_ context.Context, line string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lines = append(f.lines, line)
	return nil
}

// Lines is every line appended, in order.
func (f *FakeDoctorLog) Lines() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.lines...)
}

// Read implements application.DoctorLog.
func (f *FakeDoctorLog) Read(_ context.Context) ([]string, error) {
	return f.Lines(), nil
}

// FakeDoctorCheck is a scriptable application.DoctorCheck: what its probe and
// its cure answer are set with the fields, and how many times each was
// called is read back with the methods.
type FakeDoctorCheck struct {
	mu sync.Mutex

	// CheckName is what Name returns.
	CheckName string
	// Verdict and Reason are what Probe returns.
	Verdict application.Verdict
	Reason  string
	// CureErr is what Cure returns.
	CureErr error
	// Wait and Cap are what Damper returns.
	Wait time.Duration
	Cap  int
	// Way is what WayBack returns.
	Way string

	probes int
	cures  int
}

// Name implements application.DoctorCheck.
func (f *FakeDoctorCheck) Name() string { return f.CheckName }

// Probe implements application.DoctorCheck.
func (f *FakeDoctorCheck) Probe(context.Context) (application.Verdict, string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.probes++
	return f.Verdict, f.Reason
}

// Cure implements application.DoctorCheck.
func (f *FakeDoctorCheck) Cure(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cures++
	return f.CureErr
}

// Damper implements application.DoctorCheck.
func (f *FakeDoctorCheck) Damper() (time.Duration, int) { return f.Wait, f.Cap }

// WayBack implements application.DoctorCheck.
func (f *FakeDoctorCheck) WayBack() string { return f.Way }

// Probes is how many times Probe was called.
func (f *FakeDoctorCheck) Probes() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.probes
}

// Cures is how many times Cure was called.
func (f *FakeDoctorCheck) Cures() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.cures
}

// FakeDoctorNotes is an in-memory application.DoctorNotes: one value per
// key, kept until cleared. Err, when set, is what every write and clear
// answers instead, standing in for a notes port that cannot be reached.
type FakeDoctorNotes struct {
	mu     sync.Mutex
	notes  map[string]string
	clears int

	Err error
}

// NewFakeDoctorNotes is a notes port with nothing kept yet.
func NewFakeDoctorNotes() *FakeDoctorNotes {
	return &FakeDoctorNotes{notes: map[string]string{}}
}

// Note implements application.DoctorNotes.
func (f *FakeDoctorNotes) Note(_ context.Context, key string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return "", f.Err
	}
	return f.notes[key], nil
}

// SetNote implements application.DoctorNotes.
func (f *FakeDoctorNotes) SetNote(_ context.Context, key, value string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return f.Err
	}
	f.notes[key] = value
	return nil
}

// ClearNote implements application.DoctorNotes.
func (f *FakeDoctorNotes) ClearNote(_ context.Context, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.clears++
	if f.Err != nil {
		return f.Err
	}
	delete(f.notes, key)
	return nil
}

// Clears is how many times ClearNote was called, whether or not there was
// anything to clear.
func (f *FakeDoctorNotes) Clears() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.clears
}

// NotesWithPrefix implements application.DoctorNotes.
func (f *FakeDoctorNotes) NotesWithPrefix(_ context.Context, prefix string) (map[string]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return nil, f.Err
	}
	found := map[string]string{}
	for key, value := range f.notes {
		if strings.HasPrefix(key, prefix) {
			found[key] = value
		}
	}
	return found, nil
}

// Get is the value kept for key, and whether it is there at all.
func (f *FakeDoctorNotes) Get(key string) (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	value, ok := f.notes[key]
	return value, ok
}
