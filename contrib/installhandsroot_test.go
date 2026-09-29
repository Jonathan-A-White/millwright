package contrib_test

// This test drives contrib/install-hands-root against a temporary prefix
// (MW_HANDS_INSTALL_PREFIX) with stand-ins for id, getent, chown and visudo
// on the front of PATH: it never writes under the real /etc or /usr, never
// changes an owner, and never asks the real visudo about the real sudoers.
// The script is copied into a throwaway rig beside a stand-in
// bin/mw-hands-root, since it finds the helper beside itself.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const installKey = "03f01d6b9018ab421dd410404cb869072065522bf85734008f105cf385a023a80f"

// installRig is a throwaway rig holding the installer and a built helper, a
// home for the user jwhite with mw's config, one for root with its own, a
// prefix to install under, and stand-in commands.
type installRig struct {
	script, prefix, home, rootHome, bin, log string
}

func newInstallRig(t *testing.T, config string) *installRig {
	t.Helper()
	dir := t.TempDir()
	r := &installRig{
		script:   filepath.Join(dir, "rig", "contrib", "install-hands-root"),
		prefix:   filepath.Join(dir, "root"),
		home:     filepath.Join(dir, "home", "jwhite"),
		rootHome: filepath.Join(dir, "home", "root"),
		bin:      filepath.Join(dir, "stand-ins"),
		log:      filepath.Join(dir, "calls.log"),
	}
	source, err := os.ReadFile("install-hands-root")
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{filepath.Dir(r.script), filepath.Join(dir, "rig", "bin"), filepath.Join(r.home, ".config", "mw"), filepath.Join(r.rootHome, ".config", "mw"), r.bin} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write := func(path, body string, mode os.FileMode) {
		if err := os.WriteFile(path, []byte(body), mode); err != nil {
			t.Fatal(err)
		}
	}
	write(r.script, string(source), 0o755)
	write(filepath.Join(dir, "rig", "bin", "mw-hands-root"), "the helper, built\n", 0o755)
	write(filepath.Join(r.home, ".config", "mw", "config.toml"), config, 0o644)
	write(filepath.Join(r.bin, "id"), "#!/bin/sh\necho \"${STAND_IN_UID:-0}\"\n", 0o755)
	write(filepath.Join(r.rootHome, ".config", "mw", "config.toml"), rootConfig, 0o644)
	write(filepath.Join(r.bin, "getent"), "#!/bin/sh\ncase $2 in\njwhite) echo \"jwhite:x:1000:1000::"+r.home+":/bin/bash\" ;;\nroot) echo \"root:x:0:0:root:"+r.rootHome+":/bin/bash\" ;;\n*) exit 2 ;;\nesac\n", 0o755)
	write(filepath.Join(r.bin, "chown"), "#!/bin/sh\necho \"chown $*\" >> "+r.log+"\n", 0o755)
	write(filepath.Join(r.bin, "visudo"), "#!/bin/sh\necho \"visudo $*\" >> "+r.log+"\nexit ${STAND_IN_VISUDO:-0}\n", 0o755)
	return r
}

func (r *installRig) run(t *testing.T, stdin string, env []string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command(r.script, args...)
	cmd.Env = append([]string{"PATH=" + r.bin + ":/usr/bin:/bin", "MW_HANDS_INSTALL_PREFIX=" + r.prefix, "HOME=/nowhere"}, env...)
	cmd.Stdin = strings.NewReader(stdin)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (r *installRig) read(t *testing.T, path string) (string, os.FileMode) {
	t.Helper()
	full := filepath.Join(r.prefix, path)
	info, err := os.Stat(full)
	if err != nil {
		t.Fatalf("expected %s: %v", path, err)
	}
	raw, _ := os.ReadFile(full)
	return string(raw), info.Mode().Perm()
}

func (r *installRig) nothingWritten(t *testing.T) {
	t.Helper()
	for _, path := range []string{"usr/local/sbin/mw-hands-root", "etc/mw-hands/governor.pub", "etc/mw-hands/host", "etc/sudoers.d/mw-hands"} {
		if _, err := os.Stat(filepath.Join(r.prefix, path)); err == nil {
			t.Errorf("expected nothing written, found %s", path)
		}
	}
}

const installConfig = "vault = \"/home/jwhite/millwright-vault\"\nhost = \"desktop\"  # this one\npostern_governor_key = \"03F01D6B9018AB421DD410404CB869072065522BF85734008F105CF385A023A80F\"\n\n[doctor]\npostern_governor_key = \"02ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff\"\n"

// rootConfig is root's own mw config, on a host reached as root.
const rootConfig = "vault = \"/root/millwright-vault\"\nhost = \"vps\"\npostern_governor_key = \"" + installKey + "\"\n"

// One line installs the helper, the Governor's key from the user's own mw
// config (root table only), this host's name and the sudoers line naming
// only the helper — each root's, each in its mode — and prints the way back.
func TestInstallHandsRootInstallsFromTheUsersConfig(t *testing.T) {
	r := newInstallRig(t, installConfig)

	out, err := r.run(t, "", nil, "jwhite", "--yes")
	if err != nil {
		t.Fatalf("install-hands-root failed: %v\n%s", err, out)
	}
	if body, mode := r.read(t, "usr/local/sbin/mw-hands-root"); body != "the helper, built\n" || mode != 0o755 {
		t.Errorf("expected the built helper, 0755, got %q %o", body, mode)
	}
	if body, mode := r.read(t, "etc/mw-hands/governor.pub"); body != installKey+"\n" || mode != 0o644 {
		t.Errorf("expected the root table's key, lowercase, 0644, got %q %o", body, mode)
	}
	if body, mode := r.read(t, "etc/mw-hands/host"); body != "desktop\n" || mode != 0o644 {
		t.Errorf("expected the host, 0644, got %q %o", body, mode)
	}
	if body, mode := r.read(t, "etc/sudoers.d/mw-hands"); body != "jwhite ALL=(root) NOPASSWD: /usr/local/sbin/mw-hands-root\n" || mode != 0o440 {
		t.Errorf("expected exactly the one sudoers line, 0440, got %q %o", body, mode)
	}
	if _, mode := r.read(t, "var/lib/mw-hands"); mode != 0o700 {
		t.Errorf("expected the record's directory 0700, got %o", mode)
	}
	leftovers, _ := filepath.Glob(filepath.Join(r.prefix, "*", "*", ".*"))
	more, _ := filepath.Glob(filepath.Join(r.prefix, "etc", "*", ".*"))
	if len(leftovers)+len(more) != 0 {
		t.Errorf("expected no temporary files left, found %v %v", leftovers, more)
	}
	calls, _ := os.ReadFile(r.log)
	if strings.Count(string(calls), "chown root:root") < 5 || !strings.Contains(string(calls), "visudo -cf "+filepath.Join(r.prefix, "etc", "sudoers.d", ".mw-hands.new")) {
		t.Errorf("expected every file made root's and the sudoers line checked, got:\n%s", calls)
	}
	for _, want := range []string{installKey, "sha256 ", "The way back", "desktop"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in the output, got:\n%s", want, out)
		}
	}
}

// On a host reached as root, mw runs as root: the installer takes root's own
// mw config and installs the helper, the key, the host and the record's
// directory exactly as for any user, and no sudoers file, since root runs
// `sudo -n` with none. Its question and its way back say so.
func TestInstallHandsRootForRootWritesNoSudoers(t *testing.T) {
	r := newInstallRig(t, installConfig)

	out, err := r.run(t, "", nil, "root", "--yes")
	if err != nil {
		t.Fatalf("install-hands-root root failed: %v\n%s", err, out)
	}
	if body, mode := r.read(t, "usr/local/sbin/mw-hands-root"); body != "the helper, built\n" || mode != 0o755 {
		t.Errorf("expected the built helper, 0755, got %q %o", body, mode)
	}
	if body, mode := r.read(t, "etc/mw-hands/governor.pub"); body != installKey+"\n" || mode != 0o644 {
		t.Errorf("expected root's config's key, 0644, got %q %o", body, mode)
	}
	if body, mode := r.read(t, "etc/mw-hands/host"); body != "vps\n" || mode != 0o644 {
		t.Errorf("expected root's config's host, 0644, got %q %o", body, mode)
	}
	if _, mode := r.read(t, "var/lib/mw-hands"); mode != 0o700 {
		t.Errorf("expected the record's directory 0700, got %o", mode)
	}
	if _, err := os.Stat(filepath.Join(r.prefix, "etc", "sudoers.d")); err == nil {
		t.Error("expected no sudoers file, nor its directory, for root")
	}
	calls, _ := os.ReadFile(r.log)
	if strings.Count(string(calls), "chown root:root") < 4 || strings.Contains(string(calls), "visudo") {
		t.Errorf("expected every file made root's and no visudo, got:\n%s", calls)
	}
	for _, want := range []string{"no sudoers", "The way back", "vps"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in the output, got:\n%s", want, out)
		}
	}
	if _, back, _ := strings.Cut(out, "The way back"); !strings.Contains(back, "no sudoers") {
		t.Errorf("expected the way back to say there is no sudoers file, got:\n%s", out)
	}
	if strings.Contains(out, "sudoers.d") {
		t.Errorf("expected no sudoers file named for root, got:\n%s", out)
	}

	s := newInstallRig(t, installConfig)
	out, err = s.run(t, "n\n", nil, "root")
	if err == nil || !strings.Contains(out, "no sudoers") || !strings.Contains(out, "Nothing written.") {
		t.Fatalf("expected the question to say no sudoers and a no to write nothing, got %v\n%s", err, out)
	}
	s.nothingWritten(t)
}

// Without --yes it asks, and writes nothing unless told yes.
func TestInstallHandsRootAsksBeforeWritingAnything(t *testing.T) {
	r := newInstallRig(t, installConfig)
	out, err := r.run(t, "n\n", nil, "jwhite")
	if err == nil || !strings.Contains(out, "[y/N]") || !strings.Contains(out, "Nothing written.") {
		t.Fatalf("expected a no to write nothing, got %v\n%s", err, out)
	}
	r.nothingWritten(t)

	if out, err := r.run(t, "y\n", nil, "jwhite"); err != nil {
		t.Fatalf("expected a yes to install, got %v\n%s", err, out)
	}
	r.read(t, "etc/sudoers.d/mw-hands")
}

// A key given on the line wins over the config's, and must be one.
func TestInstallHandsRootTakesAKeyGivenAndRefusesOneThatIsNot(t *testing.T) {
	r := newInstallRig(t, installConfig)
	given := "02" + strings.Repeat("ab", 32)
	if out, err := r.run(t, "", nil, "jwhite", given, "--yes"); err != nil {
		t.Fatalf("installing with a given key: %v\n%s", err, out)
	}
	if body, _ := r.read(t, "etc/mw-hands/governor.pub"); body != given+"\n" {
		t.Fatalf("expected the given key, got %q", body)
	}

	s := newInstallRig(t, installConfig)
	for _, bad := range []string{"04" + strings.Repeat("ab", 32), "03abc", strings.Repeat("z", 66)} {
		if out, err := s.run(t, "", nil, "jwhite", bad, "--yes"); err == nil || !strings.Contains(out, "compressed public key") {
			t.Errorf("expected %q refused, got %v\n%s", bad, err, out)
		}
	}
	s.nothingWritten(t)
}

// Everything it needs is checked before it writes: root, a built helper, the
// user, a key and a host — and visudo's word on the sudoers line.
func TestInstallHandsRootRefusesBeforeWritingAnything(t *testing.T) {
	cases := map[string]struct {
		config  string
		env     []string
		args    []string
		unbuilt bool
		want    string
	}{
		"not root":         {installConfig, []string{"STAND_IN_UID=1000"}, []string{"jwhite", "--yes"}, false, "sudo"},
		"no helper built":  {installConfig, nil, []string{"jwhite", "--yes"}, true, "make -C "},
		"no such user":     {installConfig, nil, []string{"nobody-here", "--yes"}, false, "no user"},
		"not a user name":  {installConfig, nil, []string{"jw/hite", "--yes"}, false, "not a user name"},
		"a leading digit":  {installConfig, nil, []string{"1jwhite", "--yes"}, false, "not a user name"},
		"no key anywhere":  {"host = \"desktop\"\n", nil, []string{"jwhite", "--yes"}, false, "postern_governor_key"},
		"no host":          {"postern_governor_key = \"" + installKey + "\"\n", nil, []string{"jwhite", "--yes"}, false, "no host"},
		"visudo refuses":   {installConfig, []string{"STAND_IN_VISUDO=1"}, []string{"jwhite", "--yes"}, false, "visudo"},
		"a stray argument": {installConfig, nil, []string{"jwhite", installKey, "extra", "--yes"}, false, "usage"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			r := newInstallRig(t, c.config)
			if c.unbuilt {
				_ = os.Remove(filepath.Join(filepath.Dir(filepath.Dir(r.script)), "bin", "mw-hands-root"))
			}
			out, err := r.run(t, "", c.env, c.args...)
			if err == nil || !strings.Contains(out, c.want) {
				t.Fatalf("expected a refusal saying %q, got %v\n%s", c.want, err, out)
			}
			r.nothingWritten(t)
			if _, err := os.Stat(filepath.Join(r.prefix, "etc", "sudoers.d", ".mw-hands.new")); err == nil {
				t.Fatal("expected the sudoers check's copy removed")
			}
		})
	}
}

// A re-run replaces the same files and keeps the record of approvals run.
func TestInstallHandsRootRerunKeepsTheRecordOfApprovalsRun(t *testing.T) {
	r := newInstallRig(t, installConfig)
	if out, err := r.run(t, "", nil, "jwhite", "--yes"); err != nil {
		t.Fatalf("installing: %v\n%s", err, out)
	}
	used := filepath.Join(r.prefix, "var", "lib", "mw-hands", "used")
	if err := os.WriteFile(used, []byte("abc.1790000000\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	given := "02" + strings.Repeat("cd", 32)
	if out, err := r.run(t, "", nil, "jwhite", given, "--yes"); err != nil {
		t.Fatalf("re-installing: %v\n%s", err, out)
	}
	if raw, _ := os.ReadFile(used); string(raw) != "abc.1790000000\n" {
		t.Fatalf("expected the record kept, got %q", raw)
	}
	if body, _ := r.read(t, "etc/mw-hands/governor.pub"); body != given+"\n" {
		t.Fatalf("expected the key replaced, got %q", body)
	}
}
