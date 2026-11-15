// Command trace says what the served beads' server spends its time on while one
// command runs (mw-gq6.1310). It runs the command with its bd pointed at a tap
// on this host, which passes every byte on to the server and reads the
// statements the command's bd (and mw's own kept connection) sends, then lists
// the ones the server was slow over, slowest first:
//
//	go run ./contrib/dolt-beads/trace bd list --limit 1
//	go run ./contrib/dolt-beads/trace --over 0 --full mw brief <epic>
//
// The beads user may not read SHOW PROCESSLIST, and the server's own log names
// connections, never statements; the tap needs neither, and nothing on the
// server is changed. It only reads what passes: a command that writes still
// writes. The server is read from BEADS_DOLT_SERVER_HOST and
// BEADS_DOLT_SERVER_PORT, as bd reads them (127.0.0.1 and 3307 when unset).
//
// What it found on the Laptop is in contrib/dolt-beads/priority.conf.
package main

import (
	"cmp"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"
)

func main() {
	over := flag.Duration("over", 100*time.Millisecond, "list the statements the server took this long or longer over (0 lists all)")
	full := flag.Bool("full", false, "print each statement whole, not its first 200 characters")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: go run ./contrib/dolt-beads/trace [--over 100ms] [--full] <command> [args...]")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() == 0 {
		flag.Usage()
		os.Exit(2)
	}
	width := 200
	if *full {
		width = 0
	}
	os.Exit(trace(options{
		Server: net.JoinHostPort(envOr("BEADS_DOLT_SERVER_HOST", "127.0.0.1"), envOr("BEADS_DOLT_SERVER_PORT", "3307")),
		Over:   *over,
		Width:  width,
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
		Env:    os.Environ(),
	}, flag.Args()))
}

func envOr(name, otherwise string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return otherwise
}

// options is what one trace is given.
type options struct {
	// Server is the served beads' address.
	Server string
	// Over is the least time a listed statement took, and Width the most of its
	// text printed (0 prints it whole).
	Over  time.Duration
	Width int
	// Stdin and Stdout are the command's own; Stderr takes the command's and,
	// after it, the report.
	Stdin          io.Reader
	Stdout, Stderr io.Writer
	// Env is the command's environment before its bd is pointed at the tap.
	Env []string
}

// trace runs the command with its bd pointed at a tap before the server, writes
// the report to Stderr and returns the command's exit status (127 when it could
// not be started, 1 when the tap could not).
func trace(o options, argv []string) int {
	tap, err := startTap(o.Server)
	if err != nil {
		fmt.Fprintf(o.Stderr, "trace: %v\n", err)
		return 1
	}
	host, port, _ := net.SplitHostPort(tap.Addr())
	env := slices.DeleteFunc(slices.Clone(o.Env), func(kv string) bool {
		return strings.HasPrefix(kv, "BEADS_DOLT_SERVER_HOST=") || strings.HasPrefix(kv, "BEADS_DOLT_SERVER_PORT=")
	})
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Env = append(env, "BEADS_DOLT_SERVER_HOST="+host, "BEADS_DOLT_SERVER_PORT="+port)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = o.Stdin, o.Stdout, o.Stderr

	before := load()
	start := time.Now()
	status := 0
	if err := cmd.Run(); err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) {
			tap.Stop()
			fmt.Fprintf(o.Stderr, "trace: %v\n", err)
			return 127
		}
		status = exit.ExitCode()
	}
	took := time.Since(start)
	traced := tap.Stop()

	fmt.Fprintf(o.Stderr, "%s: %.2f s, exit %d", strings.Join(argv, " "), took.Seconds(), status)
	if after := load(); before != "" && after != "" {
		fmt.Fprintf(o.Stderr, ", load %s -> %s", before, after)
	}
	fmt.Fprintf(o.Stderr, "\n%s", report(traced, o.Over, o.Width))
	return status
}

// load is this host's one-minute load, or nothing where it cannot be read.
func load() string {
	b, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return ""
	}
	first, _, _ := strings.Cut(string(b), " ")
	return first
}

// report is what a trace says: the totals, then each statement the server took
// over or longer over, slowest first, with its time, its reply's size and the
// first width characters of its text (all of it when width is 0).
func report(t Traced, over time.Duration, width int) string {
	var total time.Duration
	var slow []Statement
	for _, s := range t.Statements {
		total += s.Took
		if s.Took >= over {
			slow = append(slow, s)
		}
	}
	slices.SortStableFunc(slow, func(a, b Statement) int { return cmp.Compare(b.Took, a.Took) })

	var b strings.Builder
	fmt.Fprintf(&b, "%s, %s, %d ms of the server's time\n",
		counted(t.Connections, "connection"), counted(len(t.Statements), "statement"), total.Milliseconds())
	if len(slow) == 0 {
		fmt.Fprintf(&b, "no statement took %d ms or more\n", over.Milliseconds())
		return b.String()
	}
	fmt.Fprintf(&b, "%s took %d ms or more:\n", counted(len(slow), "statement"), over.Milliseconds())
	for _, s := range slow {
		text := []rune(s.Text)
		if width > 0 && len(text) > width {
			text = append(text[:width:width], '…')
		}
		fmt.Fprintf(&b, "%8d ms  %9s  %s\n", s.Took.Milliseconds(), size(s.Bytes), string(text))
	}
	return b.String()
}

func counted(n int, what string) string {
	if n == 1 {
		return "1 " + what
	}
	return fmt.Sprintf("%d %ss", n, what)
}

// size is a count of bytes as a person reads it.
func size(n int) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}
