package application

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

// slotRecording is a MergeSlot that says what was taken and given back, and
// fails when told to.
type slotRecording struct {
	taken    []string
	released int
	fail     error
}

func (s *slotRecording) Take(_ context.Context, rigDir, holder string) (Holding, error) {
	if s.fail != nil {
		return nil, s.fail
	}
	s.taken = append(s.taken, rigDir+": "+holder)
	return &holdingRecording{slot: s, who: holder}, nil
}

type holdingRecording struct {
	slot *slotRecording
	who  string
}

func (h *holdingRecording) Release(context.Context) error { h.slot.released++; return nil }
func (h *holdingRecording) HeldBy() string                { return h.who }

func landedSmokeOf(rig string) LandedSmoke {
	return LandedSmoke{Rig: rig, Number: 4, Target: "main", Commit: "0123456789abcdef", Story: rig + "-9", Apps: []string{"cairn"}}
}

// mw-apvyg8j: the process a landing's smoke is handed to makes the smoke the
// batch's own landing would have, and follows the rig's earlier smoke.
func TestTheSmokeHandedApartIsMadeUnderTheRigsSmokeSlotAndAFailureIsWrittenAndMailed(t *testing.T) {
	ctx := context.Background()
	stub := &smokeStub{fail: true}
	tracker := &smokeComments{}
	slot := &slotRecording{}
	var said strings.Builder
	n := Next{
		Tracker: tracker, Host: "laptop", Out: &said, SmokeSlot: slot,
		Rigs:  map[string]string{"cairn": "/rigs/cairn"},
		Smoke: GristSmokeAfter{Smoke: stub, Book: GristSmokeBook{Notes: smokeNoteStore{}, Host: "laptop"}},
	}
	if err := (BatchLand{Next: n}).SmokeLanded(ctx, landedSmokeOf("cairn")); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(stub.ran, []string{"cairn"}) {
		t.Fatalf("expected cairn smoked, got %v", stub.ran)
	}
	if len(slot.taken) != 1 || !strings.HasPrefix(slot.taken[0], "/rigs/cairn: ") || slot.released != 1 {
		t.Fatalf("expected the rig's smoke slot taken and given back once, got %v, released %d", slot.taken, slot.released)
	}
	if len(tracker.said) != 1 || !strings.HasPrefix(tracker.said[0], "cairn-9: ") {
		t.Fatalf("expected the failure written on the batch's newest story, got %v", tracker.said)
	}
	if !strings.Contains(said.String(), "mw land: grist smoke:") {
		t.Fatalf("expected the smoke's result said in the log, got %q", said.String())
	}
}

// A smoke slot that cannot be taken is said, and the smoke is made all the same:
// a smoke never made is the worse for the rig.
func TestTheSmokeHandedApartIsMadeEvenWhenItsSlotCannotBeTaken(t *testing.T) {
	stub := &smokeStub{}
	var said strings.Builder
	n := Next{
		Host: "laptop", Out: &said, SmokeSlot: &slotRecording{fail: errors.New("the wait ran out")},
		Rigs:  map[string]string{"cairn": "/rigs/cairn"},
		Smoke: GristSmokeAfter{Smoke: stub, Book: GristSmokeBook{Notes: smokeNoteStore{}, Host: "laptop"}},
	}
	if err := (BatchLand{Next: n}).SmokeLanded(context.Background(), landedSmokeOf("cairn")); err != nil {
		t.Fatal(err)
	}
	if len(stub.ran) != 1 || !strings.Contains(said.String(), "gave up") {
		t.Fatalf("expected the smoke made and the give-up said, got ran %v, said %q", stub.ran, said.String())
	}
}

// A smoke that cannot be begun apart is made by mw land itself, as it was.
type refusingApart struct{}

func (refusingApart) Begin(context.Context, LandedSmoke) error { return errors.New("no systemd") }

func TestASmokeThatCannotBeBegunApartIsMadeByTheLanding(t *testing.T) {
	ctx := context.Background()
	stub := &smokeStub{}
	var said strings.Builder
	n := Next{Host: "laptop", Out: &said, Smoke: GristSmokeAfter{Smoke: stub, Book: GristSmokeBook{Notes: smokeNoteStore{}, Host: "laptop"}}}
	last := &closeOut{id: "cairn-9", target: "main", smokeApps: []string{"cairn"}}
	last.path.Rig = "cairn"
	once := &NextReport{}
	once.Rig.Moved = true
	b := BatchLand{Next: n, Apart: refusingApart{}}
	b.smoke(ctx, last, once, &BatchReport{Rig: "cairn", Number: 4, Target: "main", Commit: "0123456789"}, &NextReport{})
	if len(stub.ran) != 1 {
		t.Fatalf("expected the smoke made by the landing, got %v", stub.ran)
	}
	if !strings.Contains(said.String(), "could not be begun apart") {
		t.Fatalf("expected the refusal said, got %q", said.String())
	}
}
