package doctor_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/infrastructure/doctor"
)

// vpsReading is a VPSNginxReader that says what it was told.
type vpsReading struct{ reading application.VPSNginxReading }

func (v *vpsReading) Read(context.Context) application.VPSNginxReading { return v.reading }

var (
	vpsOK       = application.VPSNginxReading{State: application.VPSNginxOK, Backups: 1}
	vpsRoundRob = application.VPSNginxReading{State: application.VPSNginxFault, Why: "laptop.mw:8787 and desktop.mw:8787 both take traffic", Fix: "cp f f.bak && sed -i x f && nginx -t && systemctl reload nginx"}
	vpsOther    = application.VPSNginxReading{State: application.VPSNginxFault, Why: "desktop.mw:8787 is first, but home is laptop", Fix: "sed y"}
	vpsDown     = application.VPSNginxReading{State: application.VPSNginxNotChecked, Why: "ssh: timed out"}
)

// vpsRig is the vps-nginx check over a real Store in a temp dir, built afresh
// for each run as each timer run of mw doctor does.
type vpsRig struct {
	t      *testing.T
	store  *doctor.Store
	now    time.Time
	home   string
	read   *vpsReading
	nextEm uint64
	alarms []string
	clears []clearCall
}

func newVPSRig(t *testing.T) *vpsRig {
	t.Helper()
	return &vpsRig{
		t: t, store: doctor.New(t.TempDir()), now: time.Date(2026, 10, 6, 1, 0, 0, 0, time.UTC), nextEm: 100,
		home: "laptop 2026-09-29T00:10:00Z mw@laptop", read: &vpsReading{},
	}
}

func (r *vpsRig) run(reading application.VPSNginxReading) application.DoctorResult {
	r.t.Helper()
	r.now = r.now.Add(5 * time.Minute)
	r.read.reading = reading
	check := doctor.NewVPSNginx(&apptest.FakeHomeFile{Text: r.home}, "laptop", r.read, r.store)
	check.Alarm = func(_ context.Context, text string) (uint64, error) {
		r.alarms = append(r.alarms, text)
		r.nextEm++
		return r.nextEm, nil
	}
	check.Clear = func(_ context.Context, text string, clears uint64) error {
		r.clears = append(r.clears, clearCall{text, clears})
		return nil
	}
	report, err := application.Doctor{
		Checks: application.DoctorChecks{check}, State: r.store, Log: r.store, Host: "laptop",
		Now: func() time.Time { return r.now },
	}.Run(context.Background(), "", false)
	if err != nil {
		if _, fault := application.DoctorFaults(err); !fault {
			r.t.Fatalf("doctor run: %v", err)
		}
	}
	return report.Results[0]
}

func TestAnOkUpstreamAlarmsNothing(t *testing.T) {
	r := newVPSRig(t)
	if res := r.run(vpsOK); res.Verdict != "ok" || len(r.alarms) != 0 {
		t.Errorf("expected ok and no alarm, got %v with %q", res, r.alarms)
	}
}

func TestAWrongUpstreamTellsTheGovernorOnceNamingTheFault(t *testing.T) {
	r := newVPSRig(t)
	r.run(vpsRoundRob)
	if len(r.alarms) != 1 {
		t.Fatalf("expected one alarm, got %q", r.alarms)
	}
	for _, want := range []string{"both take traffic", "nginx -t", "systemctl reload nginx"} {
		if !strings.Contains(r.alarms[0], want) {
			t.Errorf("the alarm %q does not say %q", r.alarms[0], want)
		}
	}
	for i := 0; i < 5; i++ {
		r.run(vpsRoundRob)
	}
	if len(r.alarms) != 1 {
		t.Errorf("the same fault on later ticks should not alarm again, got %d alarms", len(r.alarms))
	}
}

func TestAChangedFaultAlarmsAgain(t *testing.T) {
	r := newVPSRig(t)
	r.run(vpsRoundRob)
	r.run(vpsOther)
	if len(r.alarms) != 2 {
		t.Errorf("a different fault is a change of state: expected 2 alarms, got %q", r.alarms)
	}
}

func TestAnUnreachableVPSIsNotCheckedAndKeepsWhatWasTold(t *testing.T) {
	r := newVPSRig(t)
	if res := r.run(vpsDown); res.Verdict != "ok" || !strings.Contains(res.Reason, "not checked") || len(r.alarms) != 0 {
		t.Errorf("an unreachable VPS says not checked, no alarm: %v %q", res, r.alarms)
	}
	r.run(vpsRoundRob)
	r.run(vpsDown)
	r.run(vpsRoundRob)
	if len(r.alarms) != 1 {
		t.Errorf("a gap in checking is not a change of state: expected 1 alarm, got %q", r.alarms)
	}
}

func TestPutRightClearsTheEmergencyAndAFaultAfterThatAlarmsAgain(t *testing.T) {
	r := newVPSRig(t)
	r.run(vpsRoundRob)
	r.run(vpsOK)
	if len(r.clears) != 1 || r.clears[0].clears != 101 {
		t.Fatalf("expected the emergency (seq 101) cleared once, got %v", r.clears)
	}
	r.run(vpsOK)
	if len(r.clears) != 1 {
		t.Errorf("clearing is said once, got %v", r.clears)
	}
	r.run(vpsRoundRob)
	if len(r.alarms) != 2 {
		t.Errorf("a fault after being put right is a change of state: expected 2 alarms, got %q", r.alarms)
	}
}

func TestAHostThatIsNotHomeChecksNothing(t *testing.T) {
	r := newVPSRig(t)
	r.home = "desktop 2026-09-29T00:10:00Z mw@desktop"
	res := r.run(vpsRoundRob)
	if res.Verdict != "ok" || len(r.alarms) != 0 || !strings.Contains(res.Reason, "n/a") {
		t.Errorf("a host that is not home is n/a, got %v %q", res, r.alarms)
	}
}
