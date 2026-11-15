package main

import (
	"encoding/binary"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"
)

// The MySQL commands the tap reads. Every other command ends the statement
// before it and is not listed itself.
const (
	comQuit        = 0x01
	comQuery       = 0x03
	comStmtPrepare = 0x16
	comStmtExecute = 0x17
)

// textKept is the most of one statement's text the tap keeps.
const textKept = 64 << 10

// stopWait is how long Stop waits for the connections still open to end by
// themselves before it closes them.
const stopWait = 2 * time.Second

// Statement is one statement a client sent through the tap.
type Statement struct {
	// Conn numbers the connection it was sent on, from 1, in the order opened.
	Conn int
	// Text is the statement on one line. A prepared statement reads
	// "PREPARE <text>" and each use of it "EXECUTE <text>".
	Text string
	// Took is the time from the statement's last byte to the server's last
	// reply byte before the client's next command: the server's work plus the
	// sending of the rows.
	Took time.Duration
	// Bytes is the size of the reply.
	Bytes int
}

// Traced is what a tap saw, the statements in the order they were sent.
type Traced struct {
	Connections int
	Statements  []Statement
}

// tap stands between a client and the served beads: it passes every byte both
// ways untouched and reads the client's side for the statements. It reads no
// reply beyond its size and a prepared statement's number, and a connection
// that turns to TLS is passed on unread.
type tap struct {
	listener net.Listener
	server   string

	mu     sync.Mutex
	traced Traced
	open   map[net.Conn]struct{}
	serves sync.WaitGroup
}

// startTap listens on a free port of this host and passes each connection on
// to the server at the address given.
func startTap(server string) (*tap, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("listening for the command's bd: %w", err)
	}
	t := &tap{listener: l, server: server, open: map[net.Conn]struct{}{}}
	go t.accept()
	return t, nil
}

// Addr is the address the tap listens on.
func (t *tap) Addr() string { return t.listener.Addr().String() }

// Stop ends the tap and returns what it saw. Connections still open are given
// stopWait to end, then closed.
func (t *tap) Stop() Traced {
	t.listener.Close()
	done := make(chan struct{})
	go func() { t.serves.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(stopWait):
		t.mu.Lock()
		for c := range t.open {
			c.Close()
		}
		t.mu.Unlock()
		<-done
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.traced
}

func (t *tap) accept() {
	for {
		c, err := t.listener.Accept()
		if err != nil {
			return
		}
		t.mu.Lock()
		t.traced.Connections++
		n := t.traced.Connections
		t.open[c] = struct{}{}
		t.serves.Add(1)
		t.mu.Unlock()
		go func() {
			defer t.serves.Done()
			t.serve(n, c)
			t.mu.Lock()
			delete(t.open, c)
			t.mu.Unlock()
		}()
	}
}

// conn is one connection's reading of the client's side.
type conn struct {
	n int
	// unread holds the client's bytes not yet a whole packet.
	unread []byte
	// greeted is set once the client's answer to the server's greeting passed;
	// unreadable once the connection turned to TLS.
	greeted, unreadable bool

	mu sync.Mutex
	// current is the statement the server is answering, nil between statements.
	current *Statement
	sent    time.Time
	// preparing is set while current is a PREPARE whose number is not yet read.
	preparing bool
	prepared  map[uint32]string
}

func (t *tap) serve(n int, client net.Conn) {
	defer client.Close()
	server, err := net.Dial("tcp", t.server)
	if err != nil {
		return
	}
	defer server.Close()
	c := &conn{n: n, prepared: map[uint32]string{}}

	replies := make(chan struct{})
	go func() {
		defer close(replies)
		buf := make([]byte, 1<<16)
		for {
			k, err := server.Read(buf)
			if k > 0 {
				c.replied(buf[:k])
				if _, werr := client.Write(buf[:k]); werr != nil {
					return
				}
			}
			if err != nil {
				client.Close()
				return
			}
		}
	}()

	buf := make([]byte, 1<<16)
	for {
		k, err := client.Read(buf)
		if k > 0 {
			t.asked(c, buf[:k])
			if _, werr := server.Write(buf[:k]); werr != nil {
				break
			}
		}
		if err != nil {
			break
		}
	}
	server.Close()
	<-replies
	t.keep(c)
}

// replied counts a piece of the server's reply to the current statement.
func (c *conn) replied(b []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.current == nil {
		return
	}
	took := time.Since(c.sent)
	// A PREPARE is answered with 0x00 and the statement's number.
	if c.preparing && len(b) >= 9 && b[4] == 0x00 {
		c.prepared[binary.LittleEndian.Uint32(b[5:9])] = strings.TrimPrefix(c.current.Text, "PREPARE ")
	}
	c.preparing = false
	c.current.Took = took
	c.current.Bytes += len(b)
}

// asked reads the client's bytes for whole packets. A packet numbered 0 is a
// command: it ends the statement before it and, when it is a statement itself,
// starts the next. The clock of a statement starts when its last byte is read
// here, just before it is passed on.
func (t *tap) asked(c *conn, b []byte) {
	if c.unreadable {
		return
	}
	c.unread = append(c.unread, b...)
	for len(c.unread) >= 4 {
		size := int(c.unread[0]) | int(c.unread[1])<<8 | int(c.unread[2])<<16
		if len(c.unread) < 4+size {
			return
		}
		seq, payload := c.unread[3], c.unread[4:4+size]
		c.unread = c.unread[4+size:]
		if !c.greeted {
			c.greeted = true
			// A 32-byte answer to the greeting asks for TLS: nothing after it can be read.
			if size == 32 {
				c.unreadable, c.unread = true, nil
				return
			}
			continue
		}
		if seq != 0 || size == 0 {
			continue // more of a sign-in, or of a packet too long for one
		}
		t.keep(c)
		text, ok := c.statement(payload)
		if !ok {
			continue
		}
		c.mu.Lock()
		c.current = &Statement{Conn: c.n, Text: text}
		c.preparing = payload[0] == comStmtPrepare
		c.sent = time.Now()
		c.mu.Unlock()
	}
}

// statement is the text of a command that is a statement.
func (c *conn) statement(payload []byte) (string, bool) {
	switch payload[0] {
	case comQuery:
		return oneLine(payload[1:]), true
	case comStmtPrepare:
		return "PREPARE " + oneLine(payload[1:]), true
	case comStmtExecute:
		if len(payload) < 5 {
			return "", false
		}
		id := binary.LittleEndian.Uint32(payload[1:5])
		c.mu.Lock()
		text, known := c.prepared[id]
		c.mu.Unlock()
		if !known {
			text = fmt.Sprintf("prepared statement %d", id)
		}
		return "EXECUTE " + text, true
	}
	return "", false
}

// keep ends the connection's current statement and adds it to what the tap saw.
func (t *tap) keep(c *conn) {
	c.mu.Lock()
	done := c.current
	c.current = nil
	c.mu.Unlock()
	if done == nil {
		return
	}
	t.mu.Lock()
	t.traced.Statements = append(t.traced.Statements, *done)
	t.mu.Unlock()
}

// oneLine is a statement's text with every run of white space made one space.
func oneLine(b []byte) string {
	if len(b) > textKept {
		b = b[:textKept]
	}
	return strings.Join(strings.Fields(string(b)), " ")
}
