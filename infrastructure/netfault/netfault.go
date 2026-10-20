// Package netfault reads what git and bd say when this host's network is not
// there. It is the one place in the factory that knows those words; the
// application is handed a typed error (application.NameNotResolved) and never
// reads a tool's output.
package netfault

import (
	"regexp"
	"strings"
)

// unresolved is what a name that could not be resolved sounds like, lower case.
// git over ssh and over https say "could not resolve host" (and "hostname"),
// the resolver says "temporary failure in name resolution" and, when it has an
// answer that is no, "name or service not known", and a Go program (bd and its
// Dolt) says "no such host".
//
// A failure that only sounds like these is not one: "could not resolve
// reference", a refused key, a network that is unreachable, a file that is not
// there.
var unresolved = []string{
	"could not resolve host",
	"temporary failure in name resolution",
	"name or service not known",
	"no such host",
}

// NameNotResolved reports whether a tool's output says a name could not be
// resolved, and returns the first line that says so.
func NameNotResolved(said string) (string, bool) {
	for _, line := range strings.Split(said, "\n") {
		lower := strings.ToLower(line)
		for _, words := range unresolved {
			if strings.Contains(lower, words) {
				return strings.TrimSpace(line), true
			}
		}
	}
	return "", false
}

// unreachable is what a remote that could not be reached sounds like, lower
// case, where a name was resolved but nothing answered: a connection that
// failed, timed out, was refused or reset, a TLS handshake that did not
// finish, a host or network with no route, and a server that answered with an
// error of its own. It is the one list of those words (mw-gq6.648).
//
// A failure that only sounds like these is not one: a key refused, a push
// rejected, a merge conflict.
var unreachable = []string{
	"failed to connect to",
	"couldn't connect to server",
	"connection timed out",
	"operation timed out",
	"connection refused",
	"connection reset",
	"network is unreachable",
	"no route to host",
	"tls handshake",
	"ssl_connect",
	"ssl connection timeout",
	"gnutls_handshake",
	"kex_exchange_identification",
	"the remote end hung up unexpectedly",
	"early eof",
}

// serverError is an HTTP 5xx from the remote's own host: git says "The requested
// URL returned error: 503" and, from a smart push, "RPC failed; HTTP 502".
var serverError = regexp.MustCompile(`(?i)(returned error: 5\d\d|\bhttp/?[0-9.]* 5\d\d\b)`)

var (
	// connectedTo reads the host out of "Failed to connect to github.com port
	// 443" and "ssh: connect to host github.com port 22".
	connectedTo = regexp.MustCompile(`(?i)connect to (?:host )?([^\s:]+)`)
	// accessedAt reads it out of "unable to access 'https://github.com/x/y/'".
	accessedAt = regexp.MustCompile(`(?i)unable to access '[a-z+]+://(?:[^@/']*@)?([^/:']+)`)
)

// UnknownRemote is the name given to a remote whose words do not name its host.
const UnknownRemote = "the remote"

// Unreachable reports whether a tool's output says a remote could not be
// reached, and returns the first line that says so, with the host it names, or
// UnknownRemote when it names none.
func Unreachable(said string) (line, remote string, ok bool) {
	for _, l := range strings.Split(said, "\n") {
		lower := strings.ToLower(l)
		found := serverError.MatchString(l)
		for _, words := range unreachable {
			if strings.Contains(lower, words) {
				found = true
				break
			}
		}
		if found {
			return strings.TrimSpace(l), hostIn(said, l), true
		}
	}
	return "", "", false
}

// hostIn is the host a failure names, read from the line that says so or, when
// that does not, from any other line of the output.
func hostIn(said, line string) string {
	for _, text := range []string{line, said} {
		for _, re := range []*regexp.Regexp{connectedTo, accessedAt} {
			if m := re.FindStringSubmatch(text); m != nil {
				return m[1]
			}
		}
	}
	return UnknownRemote
}
