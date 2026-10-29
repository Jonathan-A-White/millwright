package contrib_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// boostAddressRig is a vault with a tfvars file, a hub's wg0.conf and an ssh
// that runs the command it is given here, with the hub's path pointed at the
// conf, noting that it was called. It returns the environment to run the
// wrapper in and the file ssh notes its calls in.
func boostAddressRig(t *testing.T, tfvars, wg0 string) (env []string, sshCalls string) {
	t.Helper()
	dir := t.TempDir()
	vault := filepath.Join(dir, "vault")
	if err := os.MkdirAll(filepath.Join(vault, "hosts", "vultr"), 0o755); err != nil {
		t.Fatal(err)
	}
	conf := filepath.Join(dir, "wg0.conf")
	sshCalls = filepath.Join(dir, "ssh-calls")
	bin := filepath.Join(dir, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	for path, text := range map[string]string{
		filepath.Join(vault, "hosts", "vultr", "vultr-boost.tfvars"): tfvars,
		conf: wg0,
		filepath.Join(bin, "ssh"): "#!/bin/bash\n" +
			"echo \"$*\" >>" + sshCalls + "\n" +
			"cmd=${@: -1}\n" +
			"exec sh -c \"${cmd//\\/etc\\/wireguard\\/wg0.conf/" + conf + "}\"\n",
	} {
		if err := os.WriteFile(path, []byte(text), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return append(os.Environ(),
		"PATH="+bin+":"+os.Getenv("PATH"),
		"MW_VAULT="+vault,
		"HOME="+dir,
	), sshCalls
}

const boostWg0 = `[Interface]
Address = 10.88.0.1/24
ListenPort = 51820

# peer laptop (enrolled 2026-09-01)
[Peer]
PublicKey = AAA=
AllowedIPs = 10.88.0.2/32

# peer cloud1 (enrolled 2026-10-20)
[Peer]
PublicKey = BBB=
AllowedIPs = 10.88.0.21/32

# peer cloud2 (enrolled 2026-10-28)
[Peer]
PublicKey = CCC=
AllowedIPs = 10.88.0.22/32
`

func runBoost(t *testing.T, env []string, args ...string) (string, string, error) {
	t.Helper()
	cmd := command(t, "./vultr-boost", args...)
	cmd.Env = env
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

func TestVultrBoostAddressPrintsTheAddressOffTheHubsPeerBlock(t *testing.T) {
	env, _ := boostAddressRig(t, "wg_endpoint = \"203.0.113.9:51820\"\nuser = \"bob\"\n", boostWg0)
	out, errOut, err := runBoost(t, env, "address", "--name", "cloud1")
	if err != nil {
		t.Fatalf("address: %v: %s", err, errOut)
	}
	if got, want := out, "10.88.0.21\nbob\n"; got != want {
		t.Errorf("address printed %q, want %q (the address, then the box's user)", got, want)
	}
	out, _, err = runBoost(t, env, "address", "--name", "cloud2")
	if err != nil || out != "10.88.0.22\nbob\n" {
		t.Errorf("address of cloud2 printed %q, %v", out, err)
	}
}

func TestVultrBoostAddressUserDefaultsToMw(t *testing.T) {
	env, _ := boostAddressRig(t, "wg_endpoint = \"203.0.113.9:51820\"\n", boostWg0)
	out, errOut, err := runBoost(t, env, "address", "--name", "cloud1")
	if err != nil || out != "10.88.0.21\nmw\n" {
		t.Errorf("address printed %q, %v: %s", out, err, errOut)
	}
}

func TestVultrBoostAddressOfABoxTheHubLacksSaysWhy(t *testing.T) {
	env, _ := boostAddressRig(t, "wg_endpoint = \"203.0.113.9:51820\"\n", boostWg0)
	out, errOut, err := runBoost(t, env, "address", "--name", "cloud7")
	if err == nil {
		t.Fatalf("expected a failure for a box that is no peer, printed %q", out)
	}
	if out != "" || !strings.Contains(errOut, "cloud7 is not a peer of the hub") {
		t.Errorf("stdout %q, stderr %q: want nothing printed and the reason", out, errOut)
	}
}

func TestVultrBoostAddressDryRunPrintsTheReadAndRunsNothing(t *testing.T) {
	env, sshCalls := boostAddressRig(t, "wg_endpoint = \"203.0.113.9:51820\"\n", boostWg0)
	out, errOut, err := runBoost(t, env, "address", "--dry-run", "--name", "cloud1")
	if err != nil {
		t.Fatalf("address --dry-run: %v: %s", err, errOut)
	}
	if !strings.Contains(out, "address --dry-run: nothing below is run") || !strings.Contains(out, "read cloud1's address off /etc/wireguard/wg0.conf") {
		t.Errorf("expected the hub read to be printed, got %q", out)
	}
	if _, err := os.Stat(sshCalls); err == nil {
		t.Errorf("--dry-run called ssh: %s", mustRead(t, sshCalls))
	}
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}
