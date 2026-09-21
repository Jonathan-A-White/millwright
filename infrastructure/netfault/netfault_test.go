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
