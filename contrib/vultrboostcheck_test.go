package contrib_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// boostCheckRig is boostAddressRig with a box's home under it (the ssh stand-in
// runs the script here): a vault and a millwright checkout, each one commit,
// and a go, a sudo and a dmesg in front. go says what goSays and fails when it
// is not "all modules verified"; dmesg prints kernelLog.
func boostCheckRig(t *testing.T, goSays, kernelLog string) (env []string, home string) {
	t.Helper()
	env, _ = boostAddressRig(t, "wg_endpoint = \"203.0.113.9:51820\"\n", boostWg0)
	for _, e := range env {
		if v, ok := strings.CutPrefix(e, "HOME="); ok {
			home = v
		}
	}
	for _, name := range []string{"millwright-vault", "millwright"} {
		dir := filepath.Join(home, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# "+name+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		for _, args := range [][]string{
			{"init", "-q", "-b", "main"},
			{"add", "README.md"},
			{"-c", "user.name=mw", "-c", "user.email=mw@example.invalid", "-c", "commit.gpgsign=false", "commit", "-q", "-m", "first"},
		} {
			cmd := exec.Command("git", args...)
			cmd.Dir = dir
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("git %v in %s: %v: %s", args, dir, err, out)
			}
		}
	}
	// A story's worktree has a .git file: it is no checkout of its own.
	if err := os.MkdirAll(filepath.Join(home, "aside"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "aside", ".git"), []byte("gitdir: elsewhere\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// go is where a box has it, which the check puts ahead of the PATH it is given.
	bin, goBin := filepath.Join(home, "check-bin"), filepath.Join(home, ".local", "go", "bin")
	for _, dir := range []string{bin, goBin} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	logFile := filepath.Join(home, "kernel.log")
	goExit := "0"
	if goSays != "all modules verified" {
		goExit = "1"
	}
	for path, text := range map[string]string{
		logFile:                     kernelLog,
		filepath.Join(goBin, "go"):  "#!/bin/sh\necho '" + goSays + "'\nexit " + goExit + "\n",
		filepath.Join(bin, "sudo"):  "#!/bin/sh\n[ \"$1\" = -n ] && shift\nexec \"$@\"\n",
		filepath.Join(bin, "dmesg"): "#!/bin/sh\ncat " + logFile + "\n",
	} {
		if err := os.WriteFile(path, []byte(text), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for i, e := range env {
		if v, ok := strings.CutPrefix(e, "PATH="); ok {
			env[i] = "PATH=" + bin + ":" + v
		}
	}
	return env, home
}

const soundKernelLog = "[   34.220785] EXT4-fs (vda2): mounted filesystem 68966df0 ro with ordered data mode. Quota mode: none.\n" +
	"[   36.337030] EXT4-fs (vda2): re-mounted 68966df0 r/w.\n"

func TestVultrBoostCheckOfASoundBoxPrintsNoFaultAndSaysItRan(t *testing.T) {
	env, _ := boostCheckRig(t, "all modules verified", soundKernelLog)
	out, errOut, err := runBoost(t, env, "check", "--name", "cloud1")
	if err != nil {
		t.Fatalf("check: %v: %s", err, errOut)
	}
	if strings.Contains(out, "fault = ") {
		t.Errorf("a sound box has faults:\n%s", out)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if last := lines[len(lines)-1]; !strings.HasPrefix(last, "check = 2 checkouts") {
		t.Errorf("expected a last line saying the check ran over the two checkouts, got %q", last)
	}
}

// What cloud4 and cloud5 held on 2026-11-13: a checkout git cannot walk, a
// module cache whose files changed, and EXT4-fs errors in the kernel log.
func TestVultrBoostCheckNamesACorruptCheckoutAModuleCacheAndTheKernelsErrors(t *testing.T) {
	env, home := boostCheckRig(t, "modernc.org/libc v1.77.1: dir has been modified (/home/mw/go/pkg/mod/modernc.org/libc@v1.77.1)",
		soundKernelLog+
			"[ 5731.118201] EXT4-fs error (device vda2): ext4_validate_block_bitmap:423: comm git: bg 311: bad block bitmap checksum\n"+
			"[ 5802.640333] EXT4-fs error (device vda2): ext4_validate_block_bitmap:423: comm go: bg 498: bad block bitmap checksum\n")
	// The vault loses the tree of its one commit, as a pack cut short loses objects.
	objects := filepath.Join(home, "millwright-vault", ".git", "objects")
	tree, err := exec.Command("git", "-C", filepath.Join(home, "millwright-vault"), "rev-parse", "HEAD^{tree}").Output()
	if err != nil {
		t.Fatal(err)
	}
	id := strings.TrimSpace(string(tree))
	if err := os.Remove(filepath.Join(objects, id[:2], id[2:])); err != nil {
		t.Fatal(err)
	}
	out, errOut, err := runBoost(t, env, "check", "--name", "cloud1")
	if err != nil {
		t.Fatalf("a check that found faults ran to its end, and exits 0: %v: %s", err, errOut)
	}
	var faults []string
	for _, line := range strings.Split(out, "\n") {
		if fault, ok := strings.CutPrefix(line, "fault = "); ok {
			faults = append(faults, fault)
		}
	}
	if len(faults) != 3 {
		t.Fatalf("expected three faults, got %d:\n%s", len(faults), out)
	}
	for i, want := range []string{"~/millwright-vault: git fsck: ", "~/millwright: go mod verify: modernc.org/libc v1.77.1: dir has been modified", "dmesg: 2 lines of EXT4-fs error, the last: [ 5802.640333]"} {
		if !strings.HasPrefix(faults[i], want) {
			t.Errorf("fault %d is %q, want it to begin %q", i+1, faults[i], want)
		}
	}
	if !strings.Contains(out, "\ncheck = 2 checkouts") {
		t.Errorf("expected the last line to say the check ran, got:\n%s", out)
	}
}

func TestVultrBoostCheckOfABoxWithNoAddressFailsAndGivesNoVerdict(t *testing.T) {
	env, _ := boostCheckRig(t, "all modules verified", soundKernelLog)
	out, errOut, err := runBoost(t, env, "check", "--name", "cloud9")
	if err == nil {
		t.Fatalf("expected a box that is not a peer of the hub to fail the command, got:\n%s", out)
	}
	if strings.Contains(out, "check = ") || !strings.Contains(errOut, "cloud9") {
		t.Errorf("expected no verdict and an error naming the box, got out %q, err %q", out, errOut)
	}
}

// A snapshot can carry a checkout the bootstrap mends: a box whose cloud-init
// has not finished is not judged, and the cloud check asks it again.
func TestVultrBoostCheckGivesNoVerdictWhileTheBoxsBootstrapRuns(t *testing.T) {
	env, home := boostCheckRig(t, "all modules verified", soundKernelLog)
	shim := "#!/bin/sh\necho 'status: running'\n"
	if err := os.WriteFile(filepath.Join(home, "check-bin", "cloud-init"), []byte(shim), 0o755); err != nil {
		t.Fatal(err)
	}
	out, errOut, err := runBoost(t, env, "check", "--name", "cloud1")
	if err == nil {
		t.Fatalf("expected no verdict of a box still bootstrapping, got:\n%s", out)
	}
	if strings.Contains(out, "check = ") || strings.Contains(out, "fault = ") || !strings.Contains(errOut, "has not finished") {
		t.Errorf("expected neither a fault nor a check line, and why on standard error, got out %q, err %q", out, errOut)
	}
}

// cloud6 came up on 2026-11-13 with the end of its disk unwritten by the
// snapshot restore: files of the snapshot's own read as zeros, and git fsck, go
// mod verify and dmesg found nothing. A file older than the boot that begins
// with a block of zeros is a fault; one written since, or in a testdata
// directory, is not.
func TestVultrBoostCheckNamesASnapshotFileThatReadsAsZeros(t *testing.T) {
	env, home := boostCheckRig(t, "all modules verified", soundKernelLog)
	// Older than any boot of the host this test runs on, as the snapshot's files
	// are older than the boot of a box made from it.
	snapshotDay := time.Date(2001, 10, 15, 14, 29, 27, 0, time.UTC)
	for path, old := range map[string]bool{
		"postern/node_modules/playwright/lib/matchers/expect.js":    true,
		"postern/node_modules/zeros-written-by-a-session.bin":       false,
		".local/go/src/compress/flate/testdata/huffman-null-max.in": true,
	} {
		full := filepath.Join(home, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, make([]byte, 8192), 0o644); err != nil {
			t.Fatal(err)
		}
		if old {
			if err := os.Chtimes(full, snapshotDay, snapshotDay); err != nil {
				t.Fatal(err)
			}
		}
	}
	out, errOut, err := runBoost(t, env, "check", "--name", "cloud1")
	if err != nil {
		t.Fatalf("check: %v: %s", err, errOut)
	}
	var faults []string
	for _, line := range strings.Split(out, "\n") {
		if fault, ok := strings.CutPrefix(line, "fault = "); ok {
			faults = append(faults, fault)
		}
	}
	if len(faults) != 1 || !strings.HasPrefix(faults[0], "1 of the ") || !strings.Contains(faults[0], "~/postern/node_modules/playwright/lib/matchers/expect.js") || !strings.Contains(faults[0], "did not write the whole disk") {
		t.Fatalf("expected one fault naming the snapshot's zero-filled file, got %q in:\n%s", faults, out)
	}
}
