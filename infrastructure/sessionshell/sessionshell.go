// Package sessionshell writes the shell line that runs a story's session, for
// every harness. A headless harness prints its result on stdout, so the
// factory runs it under a shell line that keeps the result, renews the lease
// beside it, gives it a retrying bd, and closes the story out after it; none
// of that is particular to one harness, and each adapter (infrastructure/claude,
// and the others after it) builds its own argv and hands it to this package.
//
// It assembles and nothing else: it starts no session and reaches no network.
package sessionshell

import (
	"strings"

	"github.com/Jonathan-A-White/millwright/application"
)

// ShellLine is an argv written as one line for a shell to read back as the same
// argv.
func ShellLine(argv []string) string {
	words := make([]string, 0, len(argv))
	for _, word := range argv {
		words = append(words, ShellQuote(word))
	}
	return strings.Join(words, " ")
}

// ShellQuote is one word of a shell line. A word a shell would read back
// unchanged is left alone, so that a person attached to the session can read
// what is running; anything else is wrapped in single quotes, inside which
// every character is literal — a quote, a colon, a newline, a dollar sign, a
// backslash — with the one exception of a single quote itself, which is closed,
// escaped and reopened.
func ShellQuote(word string) string {
	if shellSafe(word) {
		return word
	}
	return "'" + strings.ReplaceAll(word, "'", `'\''`) + "'"
}

// shellSafe reports whether a word means itself to a shell as it is written.
// The list is deliberately short: everything not on it gets quoted.
func shellSafe(word string) bool {
	if word == "" {
		return false
	}
	for _, r := range word {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case strings.ContainsRune("-_./:=@,+%", r):
		default:
			return false
		}
	}
	return true
}

// ResultRedirect is argv run with only its stdout kept, in resultFile. What the
// harness says on stderr stays in the session, where a person who attaches to
// it can read it.
//
// The redirect writes to a temp path beside the real one and is renamed into
// place only once the session has fully exited, rather than opening the real
// path directly: a plain `> resultFile` opens (and truncates) that exact path
// the moment this line starts, and keeps the same file descriptor for the
// session's whole run however long that is — so anything else that replaces the
// path while the session is still going (a git operation elsewhere in the vault
// touched it, on the incident this guards against, mw-gq6.89) orphans that
// descriptor, and the session's own completed write lands nowhere anyone can
// read it. The rename is atomic on the same filesystem, so whatever the real
// path held meanwhile is replaced whole, never partially, by this session's own
// result.
func ResultRedirect(argv []string, resultFile string) string {
	tmp := resultFile + ".tmp"
	return ShellLine(argv) + " > " + ShellQuote(tmp) + "; mv " + ShellQuote(tmp) + " " + ShellQuote(resultFile)
}

// WithBdShim runs line with the session's bd retrying a write that meets a
// passing fault of the beads server (mw-gq6.393): a script named bd, in a
// directory of its own put ahead of the real bd on the PATH of line alone, runs
// retry (`mw bd`), and the real bd is named to it by MW_BD_REAL. A host with no
// bd, or no place to put the script, leaves the session the PATH it had. An
// empty retry leaves line as it was.
func WithBdShim(line string, retry []string) string {
	if len(retry) == 0 {
		return line
	}
	return BdShimSetup(retry) + BdShimEnv + line + "; " + BdShimCleanup
}

// BdShimSetup is the shell that makes the directory holding the script that
// stands in for bd, and names the real bd, leaving SHIM and REAL empty when
// either cannot be had. The script is two fixed lines, written by printf from
// words ShellQuote has quoted, so nothing in them is read by a shell twice.
func BdShimSetup(retry []string) string {
	script := ShellQuote("#!/bin/sh") + " " + ShellQuote("exec "+ShellLine(retry)+` "$@"`)
	return `REAL=$(command -v bd); SHIM=$(mktemp -d); ` +
		`if [ -z "$REAL" ] || [ -z "$SHIM" ] || ! { printf '%s\n' ` + script + ` > "$SHIM/bd" && chmod +x "$SHIM/bd"; }; then SHIM=; REAL=; fi; `
}

// BdShimEnv is what is put in front of the harness's command to give it the
// script: assignments that last for that one command.
const BdShimEnv = `MW_BD_REAL="$REAL" PATH="${SHIM:+$SHIM:}$PATH" `

// BdShimCleanup takes the script's directory away once the session is over.
const BdShimCleanup = `if [ -n "$SHIM" ]; then rm -rf "$SHIM"; fi`

// WithHeartbeat runs line with the launch's heartbeat beside it, or line as it
// was when the launch has none.
//
// Heartbeat runs beside the harness, not after it: started in the background
// before the harness's own command line and killed the moment that line
// finishes, so the lease it renews is never left running once nothing is left
// to renew it for. Heartbeat's own stop condition — the session no longer
// running — would settle it eventually even without the kill, but not until its
// next tick, and not at all if After never runs (mw-gq6.124). The kill is
// waited for: mw ends on TERM by cancelling what it was doing rather than at
// once, and the close-out chained on after it refuses a story whose heartbeat is
// still in the process table (mw-gq6.394), so After must not start until the
// heartbeat is gone (mw-gq6.487).
func WithHeartbeat(line string, l application.Launch) string {
	if len(l.Heartbeat) == 0 {
		return line
	}
	return HeartbeatLine(l) + line + "; kill $HB 2>/dev/null; wait $HB 2>/dev/null" + HeartbeatEnd(l)
}

// HeartbeatLine starts the heartbeat in the background and leaves its pid in
// $HB: bare, or with its stdout and stderr kept in HeartbeatLog as well as shown
// in the pane (mw-gq6.494). The words are the one trace of why a lease lapsed,
// and mw dispatch closes the pane when it reclaims the story. A pipe into tee
// would make $! tee's pid and the kill would miss the heartbeat, so the
// heartbeat writes to a fifo that a backgrounded tee reads: $HB stays the
// heartbeat itself. tee appends, so a later attempt's heartbeat adds to it.
func HeartbeatLine(l application.Launch) string {
	if l.HeartbeatLog == "" {
		return ShellLine(l.Heartbeat) + " & HB=$!; "
	}
	return `HBD=$(mktemp -d); mkfifo "$HBD/w"; tee -a ` + ShellQuote(l.HeartbeatLog) + ` < "$HBD/w" & HBT=$!; ` +
		ShellLine(l.Heartbeat) + ` > "$HBD/w" 2>&1 & HB=$!; `
}

// HeartbeatEnd is what settles the fifo HeartbeatLine made, once the heartbeat
// is gone: tee ends on its own as the fifo's last writer closes, so it is
// waited for, and the fifo's directory removed.
func HeartbeatEnd(l application.Launch) string {
	if l.HeartbeatLog == "" {
		return ""
	}
	return `; wait $HBT 2>/dev/null; rm -rf "$HBD"`
}

// WithAfter chains the launch's After on line, or leaves line as it was when
// the launch has none. It is chained with `;`, not `&&`: a session that failed,
// ran out of fuel or died is exactly the one whose closing out must still
// happen, and `&&` would skip it.
func WithAfter(line string, l application.Launch) string {
	if len(l.After) == 0 {
		return line
	}
	return line + "; " + AfterLine(l)
}

// AfterLine is After as the shell runs it: alone, or with its stdout and stderr
// kept in AfterLog as well as shown in the pane (mw-gq6.487). A close-out that
// dies says why in its pane and nowhere else, and mw dispatch closes that pane
// when it reclaims the story: the log is what is left to read. tee appends, so
// a close-out run again for the same attempt (RunCloseOutAgain) adds to it.
func AfterLine(l application.Launch) string {
	after := ShellLine(l.After)
	if l.AfterLog == "" {
		return after
	}
	return "{ " + after + "; } 2>&1 | tee -a " + ShellQuote(l.AfterLog)
}

// Env is what a host gives a session ahead of its line, each part left out when
// empty. Nothing is read here: the line holds only the paths, and a file that is
// missing or unreadable is skipped (mw-43v9x.23).
type Env struct {
	// File is the host's beads environment, read first of all.
	File string
	// HarnessFile is the harness's own environment file, read after File. The
	// caller names it only when it exists.
	HarnessFile string
	// ServerHost is exported as BEADS_DOLT_SERVER_HOST, after the files, so it
	// wins over them.
	ServerHost string
}

// Prelude puts the host's environment ahead of line, so that the close-out
// chained on after the harness inherits it too.
func Prelude(line string, e Env) string {
	if e.ServerHost != "" {
		line = "export BEADS_DOLT_SERVER_HOST=" + ShellQuote(e.ServerHost) + "; " + line
	}
	for _, file := range []string{e.HarnessFile, e.File} {
		if file == "" {
			continue
		}
		q := ShellQuote(file)
		line = "if [ -r " + q + " ]; then set -a; . " + q + "; set +a; fi; " + line
	}
	return line
}
