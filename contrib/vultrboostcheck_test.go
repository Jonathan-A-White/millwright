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
	// The cloud check says the last line of standard error when the command
	// fails: it says the box has not finished, not that it could not be asked.
	lines := strings.Split(strings.TrimSpace(errOut), "\n")
	if last := lines[len(lines)-1]; !strings.Contains(last, "has not finished") || strings.Contains(last, "could not ask") {
		t.Errorf("expected the last line of standard error to say the bootstrap has not finished, got %q", last)
	}
}

// cloud6's bootstrap died at step 10 of 13 on 2026-11-13 and its cloud-init
// said "status: error" (mw-gq6.1189): a box like it is not whole, whatever its
// disk reads. The check says so in a fault line and fails, so that the cloud
// check does not clear the box; it does not go on to judge the disk.
func TestVultrBoostCheckNamesABootstrapThatEndedInError(t *testing.T) {
	env, home := boostCheckRig(t, "all modules verified", soundKernelLog)
	// As cloud-init does when a stage failed: the word, and exit 1.
	shim := "#!/bin/sh\necho 'status: error'\nexit 1\n"
	if err := os.WriteFile(filepath.Join(home, "check-bin", "cloud-init"), []byte(shim), 0o755); err != nil {
		t.Fatal(err)
	}
	out, errOut, err := runBoost(t, env, "check", "--name", "cloud1")
	if err == nil {
		t.Fatalf("expected the check of a box whose bootstrap ended in error to fail, got:\n%s", out)
	}
	var faults []string
	for _, line := range strings.Split(out, "\n") {
		if fault, ok := strings.CutPrefix(line, "fault = "); ok {
			faults = append(faults, fault)
		}
	}
	if len(faults) != 1 || !strings.Contains(faults[0], "bootstrap ended in error") {
		t.Errorf("expected one fault line saying the bootstrap ended in error, got %q in:\n%s", faults, out)
	}
	if strings.Contains(out, "check = ") {
		t.Errorf("expected no line saying the check ran to its end, got:\n%s", out)
	}
	// The cloud check says the last line of standard error when the command
	// fails: it names the box and why, not that the box could not be asked.
	lines := strings.Split(strings.TrimSpace(errOut), "\n")
	if last := lines[len(lines)-1]; !strings.Contains(last, "cloud1") || !strings.Contains(last, "bootstrap ended in error") || strings.Contains(last, "could not ask") {
		t.Errorf("expected the last line of standard error to name cloud1 and its bootstrap's error, got %q", last)
	}
}

// A cloud-init that says "status: done" is a bootstrap that finished: the box
// is judged by its disk, and a sound one passes.
func TestVultrBoostCheckOfASoundBoxWhoseBootstrapIsDonePasses(t *testing.T) {
	env, home := boostCheckRig(t, "all modules verified", soundKernelLog)
	shim := "#!/bin/sh\necho 'status: done'\n"
	if err := os.WriteFile(filepath.Join(home, "check-bin", "cloud-init"), []byte(shim), 0o755); err != nil {
		t.Fatal(err)
	}
	out, errOut, err := runBoost(t, env, "check", "--name", "cloud1")
	if err != nil {
		t.Fatalf("check: %v: %s", err, errOut)
	}
	if strings.Contains(out, "fault = ") || !strings.Contains(out, "\ncheck = 2 checkouts") {
		t.Errorf("expected no fault and a line saying the check ran, got:\n%s", out)
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

// boostSnapshotRig is boostCheckRig with what snapshot reaches in front: mw
// (secrets get), age (it decrypts a state that holds the instance abc-123), jq's
// own and a curl that notes its calls, answering a snapshot. The age key and the
// encrypted state are files of the vault's. It returns the environment, the
// box's home and the file curl notes its calls in.
func boostSnapshotRig(t *testing.T) (env []string, home, curlCalls string) {
	t.Helper()
	env, home = boostCheckRig(t, "all modules verified", soundKernelLog)
	curlCalls = filepath.Join(home, "curl-calls")
	run := filepath.Join(home, "run")
	for _, dir := range []string{run, filepath.Join(home, ".config", "mw")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	state := `{"resources":[{"type":"vultr_instance","instances":[{"attributes":{"id":"abc-123"}}]}]}`
	bin := filepath.Join(home, "check-bin")
	for path, text := range map[string]string{
		filepath.Join(bin, "mw"):  "#!/bin/bash\n[ \"$1 $2\" = 'secrets get' ] && echo token-9\n",
		filepath.Join(bin, "age"): "#!/bin/bash\necho '" + state + "'\n",
		filepath.Join(bin, "curl"): "#!/bin/bash\necho \"$*\" >>" + curlCalls + "\ncat >/dev/null\n" +
			"echo '{\"snapshot\":{\"id\":\"snap-1\"}}'\n",
		filepath.Join(home, ".config", "mw", "age.key"): "AGE-SECRET-KEY-1\n",
		// The state the vault keeps for cloud1, which the age stand-in "decrypts".
		filepath.Join(home, "vault", "hosts", "vultr", "terraform.tfstate.age"): "encrypted\n",
	} {
		if err := os.WriteFile(path, []byte(text), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	env = append(env, "VULTR_BOOST_RUN="+run)
	return env, home, curlCalls
}

// A box whose bootstrap ended in error is no image to make boxes from: snapshot
// asks the box first and refuses, and asks Vultr for nothing (mw-gq6.1277).
func TestVultrBoostSnapshotRefusesABoxWhoseBootstrapEndedInError(t *testing.T) {
	env, home, curlCalls := boostSnapshotRig(t)
	shim := "#!/bin/sh\necho 'status: error'\nexit 1\n"
	if err := os.WriteFile(filepath.Join(home, "check-bin", "cloud-init"), []byte(shim), 0o755); err != nil {
		t.Fatal(err)
	}
	out, errOut, err := runBoost(t, env, "snapshot", "--name", "cloud1")
	if err == nil {
		t.Fatalf("expected snapshot of a box whose bootstrap ended in error to fail, got:\n%s", out)
	}
	lines := strings.Split(strings.TrimSpace(errOut), "\n")
	if last := lines[len(lines)-1]; !strings.Contains(last, "cloud1") || !strings.Contains(last, "bootstrap ended in error") {
		t.Errorf("expected the last line of standard error to name cloud1 and its bootstrap's error, got %q", last)
	}
	if got := readOrEmpty(curlCalls); got != "" {
		t.Errorf("Vultr was asked for a snapshot of a box whose bootstrap ended in error: curl was called with %q", got)
	}
}

// A box whose bootstrap has not finished is not snapshotted either, and one whose
// bootstrap is done is.
func TestVultrBoostSnapshotWaitsForABootstrapAndTakesABoxWhoseBootstrapIsDone(t *testing.T) {
	env, home, curlCalls := boostSnapshotRig(t)
	cloudInit := filepath.Join(home, "check-bin", "cloud-init")
	if err := os.WriteFile(cloudInit, []byte("#!/bin/sh\necho 'status: running'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, errOut, err := runBoost(t, env, "snapshot", "--name", "cloud1")
	if err == nil {
		t.Fatalf("expected snapshot of a box still bootstrapping to fail")
	}
	lines := strings.Split(strings.TrimSpace(errOut), "\n")
	if last := lines[len(lines)-1]; !strings.Contains(last, "has not finished") {
		t.Errorf("expected the last line of standard error to say the bootstrap has not finished, got %q", last)
	}
	if got := readOrEmpty(curlCalls); got != "" {
		t.Errorf("Vultr was asked for a snapshot of a box still bootstrapping: curl was called with %q", got)
	}
	if err := os.WriteFile(cloudInit, []byte("#!/bin/sh\necho 'status: done'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	out, errOut, err := runBoost(t, env, "snapshot", "--name", "cloud1")
	if err != nil {
		t.Fatalf("snapshot of a box whose bootstrap is done: %v: %s", err, errOut)
	}
	if !strings.Contains(out, "snap-1") || !strings.Contains(readOrEmpty(curlCalls), "snapshots") {
		t.Errorf("expected the snapshot asked of Vultr and named, got out %q, curl %q", out, readOrEmpty(curlCalls))
	}
}
