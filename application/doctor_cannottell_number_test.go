package application_test

import (
	"context"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// TestDoctorCannotTellReasonWithAMovingNumber holds mw-gq6.1231: a cannot-tell
// reason is said once, so a reason that differs from the last said only by a
// number that moves on its own (minutes of a stall, commits a kit pin is
// behind) is the same reason, and must not write the note, and so wake the
// Millhand, again. A reason whose identifiers or words change is still said.
//
// The checks whose cannot-tell reasons carry a number that can move with
// nothing else changing are named here:
//
//   - follower-stalled (application/followerstall.go): the minutes the follower
//     is behind, "over 10 minutes" then "over 20 minutes";
//   - kit-lag (infrastructure/doctor/kitlag.go): "<rig>: <kit> <n> behind";
//   - stale-landings, no-session-claims, stray-previews and rig-kinds carry a
//     count of the list they print, which moves only with the list, so a
//     changed list is still said.
func TestDoctorCannotTellReasonWithAMovingNumber(t *testing.T) {
	type said struct {
		name   string
		check  application.DoctorCheck
		move   func()
		stated bool // the second reason is said again
	}

	stall := newStallWorld(t)
	stall.change("c1", 12)
	stall.viewAt = stall.t0.Add(12 * time.Minute) // the view is not the trouble

	kit := &apptest.FakeDoctorCheck{
		CheckName: "kit-lag", Verdict: application.DoctorCannotTell,
		Reason: "1 rig pins of a kit are behind its head, so the kit's fixes have not reached the rig: argus: nav-kit 3 behind",
	}
	stuck := &apptest.FakeDoctorCheck{
		CheckName: "no-session-claims", Verdict: application.DoctorCannotTell,
		Reason: "STUCK: 1 in progress with an expired lease and no host or session named: mw-gq6.50 (held by mw@cloud1, lease ran out 2026-11-13T10:00:00Z; mw retry mw-gq6.50)",
	}
	wording := &apptest.FakeDoctorCheck{
		CheckName: "wifi", Verdict: application.DoctorCannotTell,
		Reason: "powershell not found: this host is not Windows-backed",
	}

	cases := []said{
		{"follower-stalled", stall.check, func() { stall.change("c2", 22); stall.viewAt = stall.t0.Add(22 * time.Minute) }, false},
		{"kit-lag", kit, func() {
			kit.Reason = "1 rig pins of a kit are behind its head, so the kit's fixes have not reached the rig: argus: nav-kit 4 behind"
		}, false},
		{"no-session-claims, another story", stuck, func() {
			stuck.Reason = "STUCK: 1 in progress with an expired lease and no host or session named: mw-gq6.51 (held by mw@cloud1, lease ran out 2026-11-13T10:00:00Z; mw retry mw-gq6.51)"
		}, true},
		{"wifi, other words", wording, func() { wording.Reason = "powershell not found: some other reason" }, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			state := apptest.NewFakeDoctorState()
			log := &apptest.FakeDoctorLog{}
			notes := apptest.NewFakeDoctorNotes()
			now := doctorNow
			d := application.Doctor{
				Checks: application.DoctorChecks{tc.check},
				State:  state, Log: log, Notes: notes, Host: "vps",
				Now: func() time.Time { return now },
			}
			if _, err := d.Run(context.Background(), "", false); err != nil {
				t.Fatalf("first run: %v", err)
			}
			if notes.Sets() != 1 || len(log.Lines()) != 1 {
				t.Fatalf("the first reason is said once: %d note writes, log %v", notes.Sets(), log.Lines())
			}

			tc.move()
			if _, err := d.Run(context.Background(), "", false); err != nil {
				t.Fatalf("second run: %v", err)
			}
			wantSets, wantLines := 1, 1
			if tc.stated {
				wantSets, wantLines = 2, 2
			}
			if notes.Sets() != wantSets || len(log.Lines()) != wantLines {
				t.Errorf("after the move: %d note writes (want %d), log %v (want %d lines)",
					notes.Sets(), wantSets, log.Lines(), wantLines)
			}
		})
	}
}
