package handsroot_test

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	ec "github.com/bsv-blockchain/go-sdk/primitives/ec"

	"github.com/Jonathan-A-White/millwright/domain"
	"github.com/Jonathan-A-White/millwright/infrastructure/handsroot"
)

// helperNow is the clock every helper test reads.
var helperNow = time.Unix(1790000300, 0)

// installedHelper is a Helper laid out as the installer lays it out, in a
// temporary directory owned by whoever runs the test, which stands in for
// root: the key and host files, the used record's directory, this user's
// uid as the one that must own them.
type installedHelper struct {
	helper   handsroot.Helper
	governor *ec.PrivateKey
	dir      string
}

func install(t *testing.T) *installedHelper {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the helper is for a Unix host")
	}
	governor, err := ec.NewPrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	etc := filepath.Join(dir, "etc", "mw-hands")
	if err := os.MkdirAll(etc, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(etc, "governor.pub"), []byte(hex.EncodeToString(governor.PubKey().Compressed())+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(etc, "host"), []byte("desktop\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	uid := os.Getuid()
	return &installedHelper{
		governor: governor,
		dir:      dir,
		helper: handsroot.Helper{
			GovernorKeyPath: filepath.Join(etc, "governor.pub"),
			HostPath:        filepath.Join(etc, "host"),
			UsedPath:        filepath.Join(dir, "var", "lib", "mw-hands", "used"),
			RootUID:         uid,
			Euid:            func() int { return uid },
			Now:             func() time.Time { return helperNow },
			Shell:           "/bin/sh",
			Timeout:         10 * time.Second,
			Env:             []string{"PATH=/usr/bin:/bin"},
		},
	}
}

// request is an approved request for a root step on desktop that runs run.
func (h *installedHelper) request(t *testing.T, run string) domain.HandsRequest {
	t.Helper()
	step := domain.HandsStep{ID: "linger", Host: "desktop", As: "root", Run: run, WayBack: "true"}
	sha := domain.HandsSHA256("mw-f758y.8", step)
	at := helperNow.Add(-time.Minute).Unix()
	return domain.HandsRequest{
		Bead: "mw-f758y.8", ID: step.ID, Host: step.Host, As: step.As, Run: step.Run, WayBack: step.WayBack,
		SHA256: sha, ApprovedAt: at, Sig: sign(t, h.governor, sha, at),
	}
}

func (h *installedHelper) run(t *testing.T, req any) (int, string, string) {
	t.Helper()
	raw, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	var out, errs bytes.Buffer
	code := h.helper.Run(context.Background(), bytes.NewReader(raw), &out, &errs)
	return code, out.String(), errs.String()
}

// An approved root step runs, its standard output and error streamed
// together to the helper's standard output, and the helper leaves with the
// step's own exit status; the approval is recorded as used.
func TestHelperRunsAnApprovedStepAndLeavesWithItsStatus(t *testing.T) {
	h := install(t)
	req := h.request(t, "echo out; echo err >&2; exit 3")

	code, out, errs := h.run(t, req)

	if code != 3 {
		t.Fatalf("expected the step's exit status 3, got %d (stderr %q)", code, errs)
	}
	if !strings.Contains(out, "out") || !strings.Contains(out, "err") {
		t.Fatalf("expected both streams on standard output, got %q", out)
	}
	used, err := os.ReadFile(h.helper.UsedPath)
	if err != nil || !strings.Contains(string(used), req.ApprovalID()) {
		t.Fatalf("expected the approval recorded as used, got %q: %v", used, err)
	}
	info, err := os.Stat(h.helper.UsedPath)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("expected the used record 0600, got %v: %v", info.Mode(), err)
	}
	dirInfo, err := os.Stat(filepath.Dir(h.helper.UsedPath))
	if err != nil || dirInfo.Mode().Perm() != 0o700 {
		t.Fatalf("expected the used record's directory 0700, got %v: %v", dirInfo.Mode(), err)
	}
}

// On a host reached as root, root itself runs the helper through sudo -n,
// so the helper sees root's own environment, with sudo's SUDO_* naming root,
// or none at all. It reads nothing of its environment: the step runs, in
// the helper's fixed environment alone, either way.
func TestHelperRunsTheStepWhateverSudoLeftInTheEnvironment(t *testing.T) {
	cases := map[string]map[string]string{
		"root through sudo -n": {"SUDO_USER": "root", "SUDO_UID": "0", "SUDO_GID": "0", "SUDO_COMMAND": "/usr/local/sbin/mw-hands-root", "HOME": "/root", "USER": "root"},
		"no SUDO_ at all":      {"SUDO_USER": "", "SUDO_UID": "", "SUDO_GID": "", "SUDO_COMMAND": ""},
	}
	for name, env := range cases {
		t.Run(name, func(t *testing.T) {
			for key, value := range env {
				t.Setenv(key, value)
				if value == "" {
					os.Unsetenv(key)
				}
			}
			h := install(t)
			code, out, errs := h.run(t, h.request(t, "env"))
			if code != 0 {
				t.Fatalf("expected the approved step run, got %d: %s", code, errs)
			}
			if strings.Contains(out, "SUDO_") || !strings.Contains(out, "PATH=/usr/bin:/bin") {
				t.Fatalf("expected the step in the helper's fixed environment only, got %q", out)
			}
		})
	}
}

// An approval runs once: a second request carrying it is refused.
func TestHelperRefusesAnApprovalAlreadyUsed(t *testing.T) {
	h := install(t)
	req := h.request(t, "true")
	if code, _, errs := h.run(t, req); code != 0 {
		t.Fatalf("expected the first run to succeed, got %d: %s", code, errs)
	}
	code, out, errs := h.run(t, req)
	if code != handsroot.RefusedExit || !strings.Contains(errs, "already") || out != "" {
		t.Fatalf("expected the replay refused, got %d %q %q", code, out, errs)
	}
}

// The approval is recorded before the step runs: a step that never finishes
// cleanly still cannot be run again.
func TestHelperRecordsTheApprovalBeforeRunningTheStep(t *testing.T) {
	h := install(t)
	h.helper.Timeout = 300 * time.Millisecond
	req := h.request(t, "sleep 30")

	code, _, errs := h.run(t, req)
	if code != handsroot.TimedOutExit || !strings.Contains(errs, "10") && !strings.Contains(errs, "gave up") {
		t.Fatalf("expected the step stopped at its limit, got %d %q", code, errs)
	}
	if code, _, _ := h.run(t, req); code != handsroot.RefusedExit {
		t.Fatalf("expected the approval used even though the step never finished, got %d", code)
	}
}

// Every other way a request can be wrong is refused with 126 and one line on
// standard error, and runs nothing.
func TestHelperRefusesAnythingNotApprovedExactly(t *testing.T) {
	marker := func(h *installedHelper) string { return filepath.Join(h.dir, "ran") }
	cases := map[string]func(t *testing.T, h *installedHelper) any{
		"a changed step": func(t *testing.T, h *installedHelper) any {
			req := h.request(t, "touch "+marker(h))
			req.Run = "touch " + marker(h) + "; true"
			return req
		},
		"another key's signature": func(t *testing.T, h *installedHelper) any {
			req := h.request(t, "touch "+marker(h))
			other, _ := ec.NewPrivateKey()
			req.Sig = sign(t, other, req.SHA256, req.ApprovedAt)
			return req
		},
		"an approval over 15 minutes old": func(t *testing.T, h *installedHelper) any {
			req := h.request(t, "touch "+marker(h))
			req.ApprovedAt = helperNow.Add(-16 * time.Minute).Unix()
			req.Sig = sign(t, h.governor, req.SHA256, req.ApprovedAt)
			return req
		},
		"an approval from the future": func(t *testing.T, h *installedHelper) any {
			req := h.request(t, "touch "+marker(h))
			req.ApprovedAt = helperNow.Add(3 * time.Minute).Unix()
			req.Sig = sign(t, h.governor, req.SHA256, req.ApprovedAt)
			return req
		},
		"a step for another host": func(t *testing.T, h *installedHelper) any {
			step := domain.HandsStep{ID: "x", Host: "laptop", As: "root", Run: "touch " + marker(h)}
			sha := domain.HandsSHA256("mw-1", step)
			at := helperNow.Unix()
			return domain.HandsRequest{Bead: "mw-1", ID: "x", Host: "laptop", As: "root", Run: step.Run, SHA256: sha, ApprovedAt: at, Sig: sign(t, h.governor, sha, at)}
		},
		"a step to run as the user": func(t *testing.T, h *installedHelper) any {
			step := domain.HandsStep{ID: "x", Host: "desktop", As: "user", Run: "touch " + marker(h)}
			sha := domain.HandsSHA256("mw-1", step)
			at := helperNow.Unix()
			return domain.HandsRequest{Bead: "mw-1", ID: "x", Host: "desktop", As: "user", Run: step.Run, SHA256: sha, ApprovedAt: at, Sig: sign(t, h.governor, sha, at)}
		},
		"not JSON": func(t *testing.T, h *installedHelper) any { return "touch " + marker(h) },
	}
	for name, build := range cases {
		t.Run(name, func(t *testing.T) {
			h := install(t)
			code, out, errs := h.run(t, build(t, h))
			if code != handsroot.RefusedExit || out != "" || strings.Count(strings.TrimSpace(errs), "\n") != 0 || errs == "" {
				t.Fatalf("expected one refusal line and 126, got %d %q %q", code, out, errs)
			}
			if _, err := os.Stat(marker(h)); err == nil {
				t.Fatal("expected nothing run")
			}
		})
	}
}

// The helper trusts only what root owns: it refuses to run unless it runs as
// root, and refuses a key or host file, or their directory, that anyone
// else owns or could write.
func TestHelperTrustsOnlyWhatRootOwns(t *testing.T) {
	cases := map[string]func(h *installedHelper){
		"not run as root": func(h *installedHelper) {
			other := h.helper.RootUID + 1
			h.helper.Euid = func() int { return other }
		},
		"a key file owned by another": func(h *installedHelper) {
			h.helper.RootUID = os.Getuid() + 1
			h.helper.Euid = func() int { return os.Getuid() + 1 }
		},
		"a group-writable key file": func(h *installedHelper) {
			_ = os.Chmod(h.helper.GovernorKeyPath, 0o664)
		},
		"a world-writable key directory": func(h *installedHelper) {
			_ = os.Chmod(filepath.Dir(h.helper.GovernorKeyPath), 0o777)
		},
		"a key file that is a link": func(h *installedHelper) {
			real := h.helper.GovernorKeyPath + ".real"
			_ = os.Rename(h.helper.GovernorKeyPath, real)
			_ = os.Symlink(real, h.helper.GovernorKeyPath)
		},
		"no host file": func(h *installedHelper) { _ = os.Remove(h.helper.HostPath) },
	}
	for name, spoil := range cases {
		t.Run(name, func(t *testing.T) {
			h := install(t)
			req := h.request(t, "touch "+filepath.Join(h.dir, "ran"))
			spoil(h)
			code, _, errs := h.run(t, req)
			if code != handsroot.RefusedExit || errs == "" {
				t.Fatalf("expected a refusal, got %d %q", code, errs)
			}
			if _, err := os.Stat(filepath.Join(h.dir, "ran")); err == nil {
				t.Fatal("expected nothing run")
			}
		})
	}
}

// A request past the size cap is refused unread.
func TestHelperRefusesAnOversizedRequest(t *testing.T) {
	h := install(t)
	req := h.request(t, "true")
	req.WayBack = strings.Repeat("x", handsroot.RequestLimit)
	code, _, errs := h.run(t, req)
	if code != handsroot.RefusedExit || !strings.Contains(errs, "large") {
		t.Fatalf("expected an oversized request refused, got %d %q", code, errs)
	}
}

// The installed helper reads the fixed paths and nothing from its
// environment: there is no knob but the code.
func TestInstalledHelperUsesTheFixedPaths(t *testing.T) {
	h := handsroot.Installed()
	if h.GovernorKeyPath != "/etc/mw-hands/governor.pub" || h.HostPath != "/etc/mw-hands/host" ||
		h.UsedPath != "/var/lib/mw-hands/used" || h.RootUID != 0 || h.Shell != "/bin/sh" || h.Timeout != 10*time.Minute {
		t.Fatalf("expected the fixed installed layout, got %+v", h)
	}
}
