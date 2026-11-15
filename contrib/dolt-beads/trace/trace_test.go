package main

// These tests drive the trace of the served beads (mw-gq6.1310): a tap between
// one command's bd and the server that says which statements the server was
// asked and how long it took over each. The server here is a stand-in that
// answers each statement after a wait the test names, so nothing reads the
// vault or a real dolt. A time is held only from below (the stand-in slept at
// least that long), so a loaded host cannot fail them.

import (
	"bytes"
	"io"
	"net"
	"os"
	"strings"
	"testing"
	"time"
)

// packet is one MySQL packet: a three-byte length, a sequence number, the payload.
func packet(seq byte, payload []byte) []byte {
	n := len(payload)
	return append([]byte{byte(n), byte(n >> 8), byte(n >> 16), seq}, payload...)
}

// query is a COM_QUERY packet holding the statement.
func query(sql string) []byte {
	return packet(0, append([]byte{comQuery}, sql...))
}

// standIn is a server that greets, then answers every client packet with reply
// after the wait slow names for it (none when slow is nil or names none).
func standIn(t *testing.T, reply []byte, slow func(request []byte) time.Duration) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				c.Write(packet(0, []byte("greeting")))
				buf := make([]byte, 1<<16)
				for {
					n, err := c.Read(buf)
					if err != nil {
						return
					}
					if slow != nil {
						time.Sleep(slow(buf[:n]))
					}
					c.Write(reply)
				}
			}()
		}
	}()
	return l.Addr().String()
}

// exchange writes one request to the tap and reads the reply through it.
func exchange(t *testing.T, c net.Conn, request []byte, replyLen int) {
	t.Helper()
	if _, err := c.Write(request); err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadFull(c, make([]byte, replyLen)); err != nil {
		t.Fatalf("reading the reply through the tap: %v", err)
	}
}

func TestTheTapNamesEachStatementAndHowLongTheServerTook(t *testing.T) {
	reply := packet(1, []byte("ok-ok-ok"))
	server := standIn(t, reply, func(request []byte) time.Duration {
		if bytes.Contains(request, []byte("FROM dependencies")) {
			return 60 * time.Millisecond
		}
		return 0
	})
	tap, err := startTap(server)
	if err != nil {
		t.Fatal(err)
	}
	c, err := net.Dial("tcp", tap.Addr())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadFull(c, make([]byte, len(packet(0, []byte("greeting"))))); err != nil {
		t.Fatal(err)
	}
	exchange(t, c, packet(1, []byte("the client's answer to the greeting")), len(reply))
	exchange(t, c, query("SELECT 1"), len(reply))
	exchange(t, c, query("SELECT issue_id\n  FROM dependencies ORDER BY issue_id"), len(reply))
	c.Write(packet(0, []byte{comQuit}))
	c.Close()

	got := tap.Stop()
	if got.Connections != 1 {
		t.Errorf("connections = %d, want 1", got.Connections)
	}
	if len(got.Statements) != 2 {
		t.Fatalf("statements = %+v, want the two queries and neither the greeting's answer nor the quit", got.Statements)
	}
	fast, slow := got.Statements[0], got.Statements[1]
	if fast.Text != "SELECT 1" {
		t.Errorf("first statement = %q, want SELECT 1", fast.Text)
	}
	if slow.Text != "SELECT issue_id FROM dependencies ORDER BY issue_id" {
		t.Errorf("second statement = %q, want the dependencies read on one line", slow.Text)
	}
	if slow.Took < 60*time.Millisecond {
		t.Errorf("the slow statement took %v, want at least the 60ms the server slept", slow.Took)
	}
	if slow.Bytes != len(reply) {
		t.Errorf("the slow statement's reply = %d bytes, want %d", slow.Bytes, len(reply))
	}
}

func TestTheTapReadsAStatementSentInPieces(t *testing.T) {
	reply := packet(1, []byte("ok"))
	server := standIn(t, reply, nil)
	tap, err := startTap(server)
	if err != nil {
		t.Fatal(err)
	}
	c, err := net.Dial("tcp", tap.Addr())
	if err != nil {
		t.Fatal(err)
	}
	io.ReadFull(c, make([]byte, len(packet(0, []byte("greeting")))))
	exchange(t, c, packet(1, []byte("answer")), len(reply))
	whole := query("SELECT title FROM issues WHERE id = 'mw-1'")
	c.Write(whole[:9])
	io.ReadFull(c, make([]byte, len(reply))) // the stand-in answers every piece
	exchange(t, c, whole[9:], len(reply))
	c.Close()

	got := tap.Stop()
	if len(got.Statements) != 1 || got.Statements[0].Text != "SELECT title FROM issues WHERE id = 'mw-1'" {
		t.Fatalf("statements = %+v, want the one statement whole", got.Statements)
	}
}

func TestTheReportListsTheSlowStatementsSlowestFirstWithTheTotals(t *testing.T) {
	traced := Traced{
		Connections: 3,
		Statements: []Statement{
			{Text: "SELECT 1", Took: 2 * time.Millisecond, Bytes: 11},
			{Text: "SELECT issue_id FROM dependencies ORDER BY issue_id", Took: 1492 * time.Millisecond, Bytes: 6841797},
			{Text: "SELECT i.id FROM issues i LEFT JOIN (SELECT issue_id FROM dependencies GROUP BY issue_id) d ON d.issue_id = i.id LIMIT 2", Took: 2466 * time.Millisecond, Bytes: 17517},
		},
	}
	got := report(traced, 100*time.Millisecond, 60)
	want := strings.Join([]string{
		"3 connections, 3 statements, 3960 ms of the server's time",
		"2 statements took 100 ms or more:",
		"    2466 ms    17.1 KB  SELECT i.id FROM issues i LEFT JOIN (SELECT issue_id FROM de…",
		"    1492 ms     6.5 MB  SELECT issue_id FROM dependencies ORDER BY issue_id",
		"",
	}, "\n")
	if got != want {
		t.Errorf("report =\n%s\nwant\n%s", got, want)
	}

	none := report(Traced{Connections: 1, Statements: traced.Statements[:1]}, 100*time.Millisecond, 60)
	if !strings.Contains(none, "no statement took 100 ms or more") {
		t.Errorf("report with nothing slow =\n%s\nwant it to say no statement took 100 ms or more", none)
	}
}

func TestTraceRunsTheCommandWithItsBdPointedAtTheTap(t *testing.T) {
	server := standIn(t, packet(1, []byte("ok")), nil)
	out := &bytes.Buffer{}
	said := &bytes.Buffer{}
	status := trace(options{
		Server: server,
		Over:   100 * time.Millisecond,
		Width:  200,
		Stdout: out,
		Stderr: said,
		Env:    append(os.Environ(), "BEADS_DOLT_SERVER_PORT=1", "BEADS_DOLT_SERVER_HOST=elsewhere"),
	}, []string{"sh", "-c", `echo "to $BEADS_DOLT_SERVER_HOST:$BEADS_DOLT_SERVER_PORT"; exit 3`})
	if status != 3 {
		t.Errorf("exit status = %d, want the command's own 3", status)
	}
	line := strings.TrimSpace(out.String())
	host, port, err := net.SplitHostPort(strings.TrimPrefix(line, "to "))
	if err != nil || host != "127.0.0.1" || port == "1" || port == "" {
		t.Errorf("the command saw %q, want its bd pointed at the tap on 127.0.0.1 and a port of the tap's own", line)
	}
	for _, want := range []string{"sh -c", "exit 3", "0 connections, 0 statements", "no statement took 100 ms or more"} {
		if !strings.Contains(said.String(), want) {
			t.Errorf("the report on stderr =\n%s\nwant it to hold %q", said.String(), want)
		}
	}
}
