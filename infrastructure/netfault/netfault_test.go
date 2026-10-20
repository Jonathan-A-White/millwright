package netfault_test

import (
	"testing"

	"github.com/Jonathan-A-White/millwright/infrastructure/netfault"
)

func TestNameNotResolvedReadsGitsAndTheResolversWording(t *testing.T) {
	for name, tc := range map[string]struct{ said, line string }{
		"git over ssh": {
			"ssh: Could not resolve hostname github.com: Temporary failure in name resolution\nfatal: Could not read from remote repository.\n",
			"ssh: Could not resolve hostname github.com: Temporary failure in name resolution",
		},
		"git over https": {
			"fatal: unable to access 'https://github.com/x/y.git/': Could not resolve host: github.com\n",
			"fatal: unable to access 'https://github.com/x/y.git/': Could not resolve host: github.com",
		},
		"the resolver's own words": {
			"getaddrinfo: Temporary failure in name resolution\n",
			"getaddrinfo: Temporary failure in name resolution",
		},
		"a Go program's lookup": {
			"push: dial tcp: lookup github.com on 127.0.0.53:53: no such host\n",
			"push: dial tcp: lookup github.com on 127.0.0.53:53: no such host",
		},
		"the name or service is not known": {
			"ssh: Could not resolve hostname github.com: Name or service not known\n",
			"ssh: Could not resolve hostname github.com: Name or service not known",
		},
		"shouted, and buried in a longer message": {
			"git pull --rebase in /v: exit status 128: COULD NOT RESOLVE HOST: github.com\n",
			"git pull --rebase in /v: exit status 128: COULD NOT RESOLVE HOST: github.com",
		},
	} {
		t.Run(name, func(t *testing.T) {
			line, found := netfault.NameNotResolved(tc.said)
			if !found {
				t.Fatalf("expected %q to be a name that could not be resolved", tc.said)
			}
			if line != tc.line {
				t.Fatalf("expected the line %q, got %q", tc.line, line)
			}
		})
	}
}

func TestNameNotResolvedLeavesEveryOtherFailureAlone(t *testing.T) {
	for name, said := range map[string]string{
		"nothing at all":                      "",
		"a refused key":                       "git@github.com: Permission denied (publickey).\nfatal: Could not read from remote repository.\n",
		"a merge conflict":                    "CONFLICT (content): Merge conflict in seats/mayor/ledger.md\n",
		"a network that is down but resolves": "ssh: connect to host github.com port 22: Network is unreachable\n",
		"a near miss":                         "fatal: could not resolve reference refs/heads/main\nerror: unable to resolve HEAD\n",
		"a file that is not there":            "open /x: no such file or directory\n",
	} {
		t.Run(name, func(t *testing.T) {
			if line, found := netfault.NameNotResolved(said); found {
				t.Fatalf("expected %q not to be a name that could not be resolved, matched %q", said, line)
			}
		})
	}
}

func TestUnreachableReadsAConnectionThatFailedAndNamesTheHost(t *testing.T) {
	for name, tc := range map[string]struct{ said, remote string }{
		"git over https, the mw-6k44rl failure": {
			"fatal: unable to access 'https://github.com/x/y.git/': Failed to connect to github.com port 443 after 135 ms: Couldn't connect to server\n", "github.com"},
		"a timeout":                 {"ssh: connect to host github.com port 22: Connection timed out\nfatal: Could not read from remote repository.\n", "github.com"},
		"a refused connection":      {"ssh: connect to host git.example.org port 22: Connection refused\n", "git.example.org"},
		"a TLS handshake":           {"fatal: unable to access 'https://github.com/x/y.git/': gnutls_handshake() failed: The TLS connection was non-properly terminated.\n", "github.com"},
		"an HTTP 503":               {"fatal: unable to access 'https://user@github.com/x/y.git/': The requested URL returned error: 503\n", "github.com"},
		"an RPC failure with a 502": {"error: RPC failed; HTTP 502 curl 22 The requested URL returned error: 502\n", netfault.UnknownRemote},
		"no route":                  {"ssh: connect to host github.com port 22: No route to host\n", "github.com"},
	} {
		t.Run(name, func(t *testing.T) {
			line, remote, found := netfault.Unreachable(tc.said)
			if !found || line == "" {
				t.Fatalf("expected %q to be a remote that could not be reached", tc.said)
			}
			if remote != tc.remote {
				t.Errorf("expected the remote %q, got %q", tc.remote, remote)
			}
		})
	}
}

func TestUnreachableLeavesWhatTheRemoteStatesAlone(t *testing.T) {
	for name, said := range map[string]string{
		"a refused key":         "git@github.com: Permission denied (publickey).\nfatal: Could not read from remote repository.\n",
		"a rejected push":       " ! [rejected]        main -> main (non-fast-forward)\n",
		"a merge conflict":      "CONFLICT (content): Merge conflict in ledger.jsonl\n",
		"a 404":                 "fatal: unable to access 'https://github.com/x/y.git/': The requested URL returned error: 404\n",
		"a name not resolved":   "fatal: unable to access 'https://github.com/x/y/': Could not resolve host: github.com\n",
		"a stray 5xx in a path": "error: pathspec 'a 5xx' did not match\n",
	} {
		t.Run(name, func(t *testing.T) {
			if line, _, found := netfault.Unreachable(said); found {
				t.Errorf("expected %q not to be an unreachable remote, matched %q", said, line)
			}
		})
	}
}
