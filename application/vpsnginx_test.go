package application_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// aSite is the VPS's nginx site file with the given lines inside its
// postern_api upstream block.
func aSite(servers ...string) string {
	return "# mw-api-upstream\nupstream postern_api {\n    " + strings.Join(servers, "\n    ") + "\n}\n\nserver {\n    listen 80;\n}\n"
}

// aVPS is a VPS that answers with the given site text, or with err.
type aVPS struct {
	site     string
	err      error
	lacks    bool
	lacksErr error
}

func (v aVPS) NginxSite(context.Context) (string, error) { return v.site, v.err }
func (v aVPS) BinaryLacks(context.Context, string) (bool, error) {
	return v.lacks, v.lacksErr
}

func readVPS(vps aVPS, homeText string) application.VPSNginxReading {
	return application.VPSNginx{
		Home: &apptest.FakeHomeFile{Text: homeText, Missing: homeText == ""},
		VPS:  vps,
	}.Read(context.Background())
}

const laptopHome = "laptop 2026-09-29T00:10:00Z mw@laptop"

func TestHomeFirstAndTheOtherBackendBackupIsOk(t *testing.T) {
	got := readVPS(aVPS{site: aSite("server laptop.mw:8787;", "server desktop.mw:8787 backup;")}, laptopHome)
	if got.State != application.VPSNginxOK {
		t.Fatalf("expected ok, got %+v", got)
	}
	if want := "VPS NGINX ok (home first, 1 backup)"; got.Line() != want {
		t.Errorf("line = %q, want %q", got.Line(), want)
	}
}

func TestTwoPlainServersIsAFaultThatNamesBothAndTheFix(t *testing.T) {
	got := readVPS(aVPS{site: aSite("server laptop.mw:8787;", "server desktop.mw:8787;")}, laptopHome)
	if got.State != application.VPSNginxFault {
		t.Fatalf("expected a fault, got %+v", got)
	}
	for _, want := range []string{"laptop.mw:8787", "desktop.mw:8787", "round-robin"} {
		if !strings.Contains(got.Why, want) {
			t.Errorf("the fault %q does not say %q", got.Why, want)
		}
	}
	for _, want := range []string{"server desktop.mw:8787 backup;", "nginx -t", "systemctl reload nginx", ".bak"} {
		if !strings.Contains(got.Fix, want) {
			t.Errorf("the fix %q does not say %q", got.Fix, want)
		}
	}
	if !strings.HasPrefix(got.Line(), "VPS NGINX FAULT") {
		t.Errorf("line = %q, want it to open VPS NGINX FAULT", got.Line())
	}
}

func TestAnotherHostFirstWhileHomeIsLaptopIsAFault(t *testing.T) {
	got := readVPS(aVPS{site: aSite("server desktop.mw:8787;", "server laptop.mw:8787 backup;")}, laptopHome)
	if got.State != application.VPSNginxFault {
		t.Fatalf("expected a fault, got %+v", got)
	}
	if !strings.Contains(got.Why, "desktop.mw:8787") || !strings.Contains(got.Why, "laptop") {
		t.Errorf("the fault %q should name the first server and the home", got.Why)
	}
	for _, want := range []string{"server desktop.mw:8787 backup;", "server laptop.mw:8787;"} {
		if !strings.Contains(got.Fix, want) {
			t.Errorf("the fix %q does not say %q", got.Fix, want)
		}
	}
}

func TestTheHomeFollowsTheHomeFile(t *testing.T) {
	got := readVPS(aVPS{site: aSite("server desktop.mw:8787;", "server laptop.mw:8787 backup;")}, "desktop 2026-09-29T00:10:00Z mw@desktop")
	if got.State != application.VPSNginxOK {
		t.Fatalf("with the desktop home, desktop first is ok; got %+v", got)
	}
}

func TestAnUpstreamWithNoServerTakingTrafficOrNoHomeIsAFault(t *testing.T) {
	for name, servers := range map[string][]string{
		"all backup": {"server laptop.mw:8787 backup;", "server desktop.mw:8787 backup;"},
		"no home":    {"server desktop.mw:8787;"},
	} {
		got := readVPS(aVPS{site: aSite(servers...)}, laptopHome)
		if got.State != application.VPSNginxFault || got.Fix == "" {
			t.Errorf("%s: expected a fault with a fix, got %+v", name, got)
		}
	}
}

func TestParametersAndCommentsOnAServerLineAreRead(t *testing.T) {
	got := readVPS(aVPS{site: aSite("server laptop.mw:8787 max_fails=3 fail_timeout=5s; # home", "# server x.mw:1;", "server desktop.mw:8787 weight=1 backup;")}, laptopHome)
	if got.State != application.VPSNginxOK {
		t.Fatalf("expected ok, got %+v", got)
	}
}

func TestAnUnreadableFileOrAnUnreachableVPSIsNotChecked(t *testing.T) {
	for name, vps := range map[string]aVPS{
		"unreachable": {err: errors.New("ssh: connect to host: timed out")},
		"no block":    {site: "server {\n    listen 80;\n}\n"},
		"empty":       {site: ""},
	} {
		got := readVPS(vps, laptopHome)
		if got.State != application.VPSNginxNotChecked || got.Why == "" {
			t.Errorf("%s: expected not checked with a reason, got %+v", name, got)
		}
		if !strings.HasPrefix(got.Line(), "VPS NGINX not checked (") {
			t.Errorf("%s: line = %q", name, got.Line())
		}
	}
}

func TestAHomeThatCannotBeToldIsNotChecked(t *testing.T) {
	got := readVPS(aVPS{site: aSite("server laptop.mw:8787;", "server desktop.mw:8787 backup;")}, "")
	if got.State != application.VPSNginxNotChecked {
		t.Fatalf("expected not checked, got %+v", got)
	}
}

func TestTheLineSaysWhenTheVPSBinaryLacksTheNamedCommit(t *testing.T) {
	site := aSite("server laptop.mw:8787;", "server desktop.mw:8787 backup;")
	reader := application.VPSNginx{
		Home:   &apptest.FakeHomeFile{Text: laptopHome},
		VPS:    aVPS{site: site, lacks: true},
		Commit: "bd54ab8",
	}
	got := reader.Read(context.Background())
	if got.State != application.VPSNginxOK || !strings.Contains(got.Line(), "bin/mw lacks bd54ab8") {
		t.Errorf("line = %q, want ok and the missing commit named", got.Line())
	}

	reader.VPS = aVPS{site: site}
	if got := reader.Read(context.Background()); strings.Contains(got.Line(), "lacks") {
		t.Errorf("a binary that has the commit should not be named: %q", got.Line())
	}

	reader.VPS = aVPS{site: site, lacksErr: errors.New("no revision")}
	if got := reader.Read(context.Background()); strings.Contains(got.Line(), "lacks") || got.State != application.VPSNginxOK {
		t.Errorf("a binary that cannot be told leaves the point out: %q", got.Line())
	}
}

func TestMwStatusPrintsTheVPSNginxLine(t *testing.T) {
	var out strings.Builder
	tracker := &apptest.FakeTracker{}
	status := application.Status{
		Tracker: tracker, Notes: tracker, Host: "laptop", Seat: "builder", Out: &out,
		VPSNginx: application.VPSNginx{
			Home: &apptest.FakeHomeFile{Text: laptopHome},
			VPS:  aVPS{site: aSite("server laptop.mw:8787;", "server desktop.mw:8787 backup;")},
		},
	}
	if _, err := status.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "VPS NGINX ok (home first, 1 backup)\n") {
		t.Errorf("status did not print the line:\n%s", out.String())
	}
}
