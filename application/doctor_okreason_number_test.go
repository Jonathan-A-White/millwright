package application_test

import (
	"context"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// TestDoctorOKReasonWithAMovingNumber holds mw-gq6.1239: an ok with a reason is
// said once, so a reason that differs from the last said only by a number that
// moves on its own is the same reason and must not write the log again. A
// reason whose words or identifiers change is still said.
//
// The checks whose ok reasons carry a number that can move with nothing else
// changing are named here:
//
//   - tmp-leftovers (infrastructure/doctor/tmpleftovers.go): "<n> bytes of dead
//     leftovers found, under the <n> byte budget" and the go-build line, byte
//     counts that grow with the leftovers;
//   - boost-asleep (infrastructure/doctor/boostasleep.go): "<boost> synced 5m
//     ago, but a push 3m ago (threshold 30m)", durations that grow with the
//     clock;
//   - wsl-trim (infrastructure/doctor/wsltrim.go): "fstrim.timer last ran <n>
//     days ago", which moves daily.
func TestDoctorOKReasonWithAMovingNumber(t *testing.T) {
	type said struct {
		name   string
		reason string
		next   string
		stated bool // the second reason is said again
	}
	cases := []said{
		{"tmp-leftovers, bytes grow",
			"4096 bytes of dead leftovers found, under the 1048576 byte budget",
			"8192 bytes of dead leftovers found, under the 1048576 byte budget", false},
		{"tmp-leftovers, go-build",
			"go-build is 900000 bytes, past the 800000 byte budget, but a go process is live: left alone",
			"go-build is 950000 bytes, past the 800000 byte budget, but a go process is live: left alone", false},
		{"boost-asleep, durations grow",
			"cloud1 synced 12m ago, but a push 3m ago (threshold 30m)",
			"cloud1 synced 17m ago, but a push 8m ago (threshold 30m)", false},
		{"boost-asleep, hours",
			"cloud1 synced 1h05m ago, but a push 3m ago (threshold 30m)",
			"cloud1 synced 1h10m ago, but a push 8s ago (threshold 30m)", false},
		{"wsl-trim, days",
			"fstrim.timer last ran 3 days ago",
			"fstrim.timer last ran 4 days ago", false},
		{"another host in the reason",
			"cloud1 synced 12m ago, but a push 3m ago (threshold 30m)",
			"cloud2 synced 12m ago, but a push 3m ago (threshold 30m)", true},
		{"other words",
			"4096 bytes of dead leftovers found, under the 1048576 byte budget",
			"go-build is 900000 bytes, past the 800000 byte budget, but a go process is live: left alone", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			check := &apptest.FakeDoctorCheck{CheckName: "tmp-leftovers", Verdict: application.DoctorOK, Reason: tc.reason}
			state := apptest.NewFakeDoctorState()
			log := &apptest.FakeDoctorLog{}
			notes := apptest.NewFakeDoctorNotes()
			now := doctorNow
			d := application.Doctor{
				Checks: application.DoctorChecks{check},
				State:  state, Log: log, Notes: notes, Host: "vps",
				Now: func() time.Time { return now },
			}
			if _, err := d.Run(context.Background(), "", false); err != nil {
				t.Fatalf("first run: %v", err)
			}
			if len(log.Lines()) != 1 {
				t.Fatalf("the first reason is said once: log %v", log.Lines())
			}

			check.Reason = tc.next
			if _, err := d.Run(context.Background(), "", false); err != nil {
				t.Fatalf("second run: %v", err)
			}
			want := 1
			if tc.stated {
				want = 2
			}
			if len(log.Lines()) != want {
				t.Errorf("after the move: log %v (want %d lines)", log.Lines(), want)
			}
		})
	}
}
