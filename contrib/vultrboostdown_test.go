package contrib_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const boostWg0WithCloud9 = boostWg0 + `
# peer cloud9 (enrolled 2026-11-13)
[Peer]
PublicKey = DDD=
AllowedIPs = 10.88.0.29/32
`

// boostDownRig is boostAddressRig with a hub that holds cloud9 as a peer, and
// stand-ins for what down reaches: mw (secrets get), terraform (never to be
// run: the vault holds no state), curl (Vultr, listing the instances named
// instances as a JSON body, and answering a DELETE 204) and the hub's wg-enrol.
// It returns the environment, the file curl notes its calls in and the file
// wg-enrol notes its calls in.
func boostDownRig(t *testing.T, instances string) (env []string, curlCalls, enrolCalls string) {
	t.Helper()
	env, _ = boostAddressRig(t, "region = \"ewr\"\nwg_endpoint = \"203.0.113.9:51820\"\nbeads_host = \"10.88.0.1\"\n", boostWg0WithCloud9)
	home := ""
	for _, e := range env {
		if v, ok := strings.CutPrefix(e, "HOME="); ok {
			home = v
		}
	}
	curlCalls = filepath.Join(home, "curl-calls")
	enrolCalls = filepath.Join(home, "enrol-calls")
	bin := filepath.Join(home, "down-bin")
	run := filepath.Join(home, "run")
	for _, dir := range []string{bin, run, filepath.Join(home, ".config", "mw")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	enrol := filepath.Join(bin, "wg-enrol")
	curl := "#!/bin/bash\n" +
		"for a; do last=$a; done\n" +
		"method=GET; while [ $# -gt 0 ]; do [ \"$1\" = -X ] && method=$2; shift; done\n" +
		"echo \"$method $last\" >>" + curlCalls + "\n" +
		"cat >/dev/null\n" +
		"if [ \"$method\" = DELETE ]; then printf '\\n204'; exit 0; fi\n" +
		"printf '%s\\n200' '" + instances + "'\n"
	files := map[string]string{
		filepath.Join(bin, "mw"):                        "#!/bin/bash\n[ \"$1 $2\" = 'secrets get' ] && echo token-9\n",
		filepath.Join(bin, "terraform"):                 "#!/bin/bash\necho terraform was run >&2\nexit 1\n",
		filepath.Join(bin, "age"):                       "#!/bin/bash\nexit 1\n",
		filepath.Join(bin, "age-keygen"):                "#!/bin/bash\necho age1recipient\n",
		filepath.Join(bin, "curl"):                      curl,
		enrol:                                           "#!/bin/bash\necho \"$*\" >>" + enrolCalls + "\n",
		filepath.Join(home, ".config", "mw", "age.key"): "AGE-SECRET-KEY-1\n",
	}
	for path, text := range files {
		if err := os.WriteFile(path, []byte(text), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for i, e := range env {
		if v, ok := strings.CutPrefix(e, "PATH="); ok {
			env[i] = "PATH=" + bin + ":" + v
		}
	}
	env = append(env, "VULTR_BOOST_RUN="+run, "VULTR_BOOST_HUB_ENROL="+enrol)
	return env, curlCalls, enrolCalls
}

func readOrEmpty(path string) string {
	b, _ := os.ReadFile(path)
	return string(b)
}

const cloud9Listed = `{"instances":[{"id":"abc-123","label":"cloud9","status":"active","server_status":"none"}]}`

func TestVultrBoostDownWithNoStateLeavesThePeerOfABoxVultrStillHolds(t *testing.T) {
	env, curlCalls, enrolCalls := boostDownRig(t, cloud9Listed)
	out, errOut, err := runBoost(t, env, "down", "--yes", "--name", "cloud9")
	if err != nil {
		t.Fatalf("down: %v: %s", err, errOut)
	}
	if got := readOrEmpty(enrolCalls); got != "" {
		t.Errorf("the peer was taken off the hub while Vultr holds cloud9: wg-enrol was called with %q", got)
	}
	if !strings.Contains(out, "abc-123") || !strings.Contains(out, "peer") {
		t.Errorf("expected the output to name the instance Vultr holds and say the peer is left, got %q", out)
	}
	if strings.Contains(readOrEmpty(curlCalls), "DELETE") {
		t.Errorf("down with no --instance must not delete what it was not told to: %q", readOrEmpty(curlCalls))
	}
}

func TestVultrBoostDownWithNoStateRemovesThePeerOfABoxVultrHoldsNot(t *testing.T) {
	env, _, enrolCalls := boostDownRig(t, `{"instances":[{"id":"zzz","label":"cloud1","status":"active"}]}`)
	_, errOut, err := runBoost(t, env, "down", "--yes", "--name", "cloud9")
	if err != nil {
		t.Fatalf("down: %v: %s", err, errOut)
	}
	if got := strings.TrimSpace(readOrEmpty(enrolCalls)); got != "--remove cloud9" {
		t.Errorf("expected wg-enrol --remove cloud9, got %q", got)
	}
}

func TestVultrBoostDownWithAnInstanceDeletesItAndRemovesThePeer(t *testing.T) {
	env, curlCalls, enrolCalls := boostDownRig(t, cloud9Listed)
	_, errOut, err := runBoost(t, env, "down", "--yes", "--name", "cloud9", "--instance", "abc-123")
	if err != nil {
		t.Fatalf("down: %v: %s", err, errOut)
	}
	if !strings.Contains(readOrEmpty(curlCalls), "DELETE https://api.vultr.com/v2/instances/abc-123") {
		t.Errorf("expected the instance deleted, curl calls: %q", readOrEmpty(curlCalls))
	}
	if got := strings.TrimSpace(readOrEmpty(enrolCalls)); got != "--remove cloud9" {
		t.Errorf("expected wg-enrol --remove cloud9, got %q", got)
	}
}

func TestVultrBoostDownWithNoStateAndNoListLeavesThePeerAndFails(t *testing.T) {
	env, _, enrolCalls := boostDownRig(t, cloud9Listed)
	// Vultr's list cannot be read: curl fails.
	for i, e := range env {
		if v, ok := strings.CutPrefix(e, "PATH="); ok {
			bin := t.TempDir()
			if err := os.WriteFile(filepath.Join(bin, "curl"), []byte("#!/bin/bash\nexit 7\n"), 0o755); err != nil {
				t.Fatal(err)
			}
			env[i] = "PATH=" + bin + ":" + v
		}
	}
	_, errOut, err := runBoost(t, env, "down", "--yes", "--name", "cloud9")
	if err == nil {
		t.Fatalf("expected down to fail when it cannot tell whether Vultr holds cloud9")
	}
	if got := readOrEmpty(enrolCalls); got != "" {
		t.Errorf("the peer was taken off the hub: wg-enrol was called with %q", got)
	}
	if !strings.Contains(errOut, "cloud9") {
		t.Errorf("expected the refusal to name cloud9, got %q", errOut)
	}
}
