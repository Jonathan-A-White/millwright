// Package netfault reads what git and bd say when this host's network is not
// there. It is the one place in the factory that knows those words; the
// application is handed a typed error (application.NameNotResolved) and never
// reads a tool's output.
package netfault

import "strings"

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
