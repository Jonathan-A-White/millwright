package steps

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/infrastructure/postern"

	"github.com/cucumber/godog"
)

// talkConn is one open /api/events connection of the fake backend.
type talkConn struct {
	out  chan string
	drop chan struct{}
}

// talkBackend is a postern backend that speaks just enough of docs/api.md for
// mw talk wait: GET /api/challenge, GET /api/events as server-sent events and
// GET /api/messages?since=&limit=, paged. It is the real wire, so the real HTTP adapter runs
// against it.
type talkBackend struct {
	mu        sync.Mutex
	records   []map[string]any
	conns     map[*talkConn]bool
	connected int
	// bareTxid, when set, is the txid the next record indexed goes by, as one
	// that came by chain does; empty gives "direct:<seq>".
	bareTxid string
}

func (b *talkBackend) handler(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/api/challenge":
		fmt.Fprint(w, `{"nonce":"a-nonce"}`)
	case "/api/messages":
		since, _ := strconv.ParseInt(r.URL.Query().Get("since"), 10, 64)
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		b.mu.Lock()
		var found []map[string]any
		for _, record := range b.records {
			if record["seq"].(int64) > since {
				found = append(found, record)
			}
		}
		b.mu.Unlock()
		if limit <= 0 || len(found) <= limit {
			json.NewEncoder(w).Encode(map[string]any{"records": found})
			return
		}
		// A page: limit records, next the last of them, more still to come.
		page := found[:limit]
		json.NewEncoder(w).Encode(map[string]any{"records": page, "next": page[limit-1]["seq"], "more": true})
	case "/api/events":
		b.stream(w, r)
	default:
		http.NotFound(w, r)
	}
}

// stream is GET /api/events: it subscribes before it says hello, as the real
// backend does, so that nothing indexed in between is missed.
func (b *talkBackend) stream(w http.ResponseWriter, r *http.Request) {
	flusher := w.(http.Flusher)
	conn := &talkConn{out: make(chan string, 64), drop: make(chan struct{})}
	b.mu.Lock()
	b.conns[conn] = true
	b.connected++
	head := len(b.records)
	b.mu.Unlock()
	defer func() {
		b.mu.Lock()
		delete(b.conns, conn)
		b.mu.Unlock()
	}()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "event: hello\ndata: {\"head\":%d,\"view\":\"\"}\n\n", head)
	flusher.Flush()
	for {
		select {
		case frame := <-conn.out:
			fmt.Fprint(w, frame)
			flusher.Flush()
		case <-conn.drop:
			return
		case <-r.Context().Done():
			return
		}
	}
}

// index adds a record to the index and, when announce is true, tells every
// open stream a message was indexed.
func (b *talkBackend) index(scriptHex string, announce bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	seq := int64(len(b.records) + 1)
	txid := fmt.Sprintf("direct:%d", seq)
	if b.bareTxid != "" {
		txid, b.bareTxid = b.bareTxid, ""
	}
	b.records = append(b.records, map[string]any{
		"seq": seq, "txid": txid, "scriptHex": scriptHex,
	})
	if !announce {
		return
	}
	for conn := range b.conns {
		conn.out <- fmt.Sprintf("event: message\ndata: {\"seq\":%d}\n\n", seq)
	}
}

func (b *talkBackend) connections() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.connected
}

func (b *talkBackend) dropAll() {
	b.mu.Lock()
	defer b.mu.Unlock()
	for conn := range b.conns {
		close(conn.drop)
		delete(b.conns, conn)
	}
}

// talkWaitRun is one run of mw talk wait.
type talkWaitRun struct {
	done    chan struct{}
	out     bytes.Buffer
	errOut  bytes.Buffer
	armedAt time.Time
	err     error
}

type talkWaitContext struct {
	home    string
	server  *httptest.Server
	backend *talkBackend
	keys    *postern.KeyFile
	client  *postern.HTTP
	mayor   string

	governorKey string
	memory      *apptest.FakeTracker
	mailbox     *apptest.FakeMailbox
	limit       time.Duration

	run       *talkWaitRun
	lastEvent time.Time

	inboxOut   bytes.Buffer
	inboxCount int
}

// talkWaitSeq numbers the txids' payload ts so no two records are alike.
func (c *talkWaitContext) record(class, to, from, plaintext string, announce bool) error {
	cipher := apptest.NewFakeCipher()
	cipher.From = from
	ct, err := cipher.Encrypt(to, plaintext)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(application.PosternPayload{
		V: 1, Kind: application.PosternMessageKind, Class: class, To: to, From: from, Ts: time.Now().Unix(), Ct: ct,
	})
	if err != nil {
		return err
	}
	script, err := postern.RecordScript(payload)
	if err != nil {
		return err
	}
	c.backend.index(hex.EncodeToString(*script), announce)
	if announce {
		c.lastEvent = time.Now()
	}
	return nil
}

func talkTurnPlaintext(id string, turn int, role, text, model string, cut bool) string {
	plain := map[string]any{"talk": map[string]any{"id": id, "turn": turn}, "text": text, "role": role}
	if model != "" {
		plain["model"] = model
	}
	if cut {
		plain["cut"] = true
	}
	raw, _ := json.Marshal(plain)
	return string(raw)
}

// callPlaintext is a section 21 call plaintext: a request or a ring carries
// text and a time, a later names the ring it puts off.
func callPlaintext(role, text, ringTxid string) string {
	plain := map[string]any{"role": role}
	if role == "later" {
		plain["ring_txid"] = ringTxid
	} else {
		plain["text"], plain["at"] = text, time.Now().Unix()
	}
	raw, _ := json.Marshal(plain)
	return string(raw)
}

// governorTurnAbout is a turn whose record carries an about field: what the
// talk was opened from.
func (c *talkWaitContext) governorTurnAbout(turn int, id, text, kind, aboutID, title string) error {
	var plain map[string]any
	_ = json.Unmarshal([]byte(talkTurnPlaintext(id, turn, "turn", text, "", false)), &plain)
	plain["about"] = map[string]any{"kind": kind, "id": aboutID, "title": title}
	raw, _ := json.Marshal(plain)
	return c.record("talk", c.mayor, c.governorKey, string(raw), true)
}

func (c *talkWaitContext) governorTurn(turn int, id, text, model string, cut, announce bool) error {
	return c.record("talk", c.mayor, c.governorKey, talkTurnPlaintext(id, turn, "turn", text, model, cut), announce)
}

// InitializeTalkWaitScenario registers the steps of features/talk_wait.feature.
func InitializeTalkWaitScenario(ctx *godog.ScenarioContext) {
	c := &talkWaitContext{}

	ctx.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
		*c = talkWaitContext{}
		return ctx, nil
	})
	ctx.After(func(ctx context.Context, sc *godog.Scenario, err error) (context.Context, error) {
		if c.backend != nil {
			c.backend.dropAll()
		}
		if c.server != nil {
			c.server.CloseClientConnections()
			c.server.Close()
		}
		if c.home != "" {
			os.RemoveAll(c.home)
		}
		return ctx, nil
	})

	ctx.Given(`^a postern backend that streams its events$`, c.aBackendThatStreams)
	ctx.Given(`^mw talk wait trusts "([^"]*)" as the Governor's key$`, func(key string) error {
		c.governorKey = key
		return nil
	})
	ctx.Given(`^mw talk wait has a time limit of (\d+) milliseconds$`, func(ms int) error {
		c.limit = time.Duration(ms) * time.Millisecond
		return nil
	})
	ctx.Given(`^mw talk wait's cursor is at the start of the index$`, func() error {
		return c.memory.SetNote(context.Background(), application.TalkWaitCursorKey, "0")
	})
	ctx.Given(`^mw talk wait is armed$`, c.arm)
	ctx.When(`^mw talk wait is armed again$`, c.arm)
	ctx.When(`^mw talk wait is armed$`, c.arm)

	ctx.When(`^the Governor's turn (\d+) of talk "([^"]*)" saying "([^"]*)" is indexed$`, func(turn int, id, text string) error {
		return c.governorTurn(turn, id, text, "", false, true)
	})
	ctx.When(`^the Governor's turn (\d+) of talk "([^"]*)" saying "([^"]*)" is indexed, on model "([^"]*)", cutting the last answer$`,
		func(turn int, id, text, model string) error {
			return c.governorTurn(turn, id, text, model, true, true)
		})
	ctx.When(`^the Governor's turn (\d+) of talk "([^"]*)" saying "([^"]*)" is indexed, about the (\w+) "([^"]*)" titled "([^"]*)"$`,
		func(turn int, id, text, kind, aboutID, title string) error {
			return c.governorTurnAbout(turn, id, text, kind, aboutID, title)
		})
	ctx.Given(`^the Governor's turn (\d+) of talk "([^"]*)" saying "([^"]*)" has been indexed$`, func(turn int, id, text string) error {
		return c.governorTurn(turn, id, text, "", false, false)
	})
	ctx.When(`^the Governor ends talk "([^"]*)"$`, func(id string) error {
		return c.record("talk", c.mayor, c.governorKey, talkTurnPlaintext(id, 9, "end", "", "", false), true)
	})
	ctx.When(`^a "([^"]*)" record to the Mayor is indexed$`, func(class string) error {
		return c.record(class, c.mayor, c.governorKey, class+" text", true)
	})
	ctx.Given(`^a "([^"]*)" record to the Mayor has been indexed$`, func(class string) error {
		return c.record(class, c.mayor, c.governorKey, class+" text", false)
	})
	ctx.Given(`^(\d+) talk turns to another key have been indexed$`, func(n int) error {
		for i := 0; i < n; i++ {
			if err := c.record("talk", "another-key", c.governorKey, talkTurnPlaintext("talk-x", 1, "turn", "not for the Mayor", "", false), false); err != nil {
				return err
			}
		}
		return nil
	})
	ctx.When(`^a talk turn to another key is indexed$`, func() error {
		return c.record("talk", "another-key", c.governorKey, talkTurnPlaintext("talk-x", 1, "turn", "not for the Mayor", "", false), true)
	})
	ctx.When(`^a talk turn from "([^"]*)" to the Mayor is indexed$`, func(from string) error {
		return c.record("talk", c.mayor, from, talkTurnPlaintext("talk-x", 1, "turn", "not the Governor's", "", false), true)
	})
	ctx.When(`^a talk record with role "([^"]*)" from the Governor to the Mayor is indexed$`, func(role string) error {
		return c.record("talk", c.mayor, c.governorKey, talkTurnPlaintext("talk-x", 1, role, "not a turn", "", false), true)
	})
	ctx.When(`^the Governor's call request "([^"]*)" is indexed$`, func(text string) error {
		return c.record("call", c.mayor, c.governorKey, callPlaintext("request", text, ""), true)
	})
	ctx.When(`^the Governor's call request "([^"]*)" is indexed with the bare txid "([^"]*)"$`, func(text, txid string) error {
		c.backend.mu.Lock()
		c.backend.bareTxid = txid
		c.backend.mu.Unlock()
		return c.record("call", c.mayor, c.governorKey, callPlaintext("request", text, ""), true)
	})
	ctx.Then(`^mw talk wait remembered the Governor's newest record came by "([^"]*)"$`, func(channel string) error {
		got, err := c.memory.Note(context.Background(), application.TalkWaitChannelKey)
		if err != nil {
			return err
		}
		if got != channel {
			return fmt.Errorf("expected the remembered channel %q, got %q", channel, got)
		}
		return nil
	})
	ctx.Given(`^the Governor's call request "([^"]*)" has been indexed$`, func(text string) error {
		return c.record("call", c.mayor, c.governorKey, callPlaintext("request", text, ""), false)
	})
	ctx.When(`^the Governor's later on the ring "([^"]*)" is indexed$`, func(ring string) error {
		return c.record("call", c.mayor, c.governorKey, callPlaintext("later", "", ring), true)
	})
	ctx.When(`^a call request to another key is indexed$`, func() error {
		return c.record("call", "another-key", c.governorKey, callPlaintext("request", "not for the Mayor", ""), true)
	})
	ctx.When(`^a call request from "([^"]*)" to the Mayor is indexed$`, func(from string) error {
		return c.record("call", c.mayor, from, callPlaintext("request", "not the Governor's", ""), true)
	})
	ctx.When(`^the stream drops$`, func() error {
		before := c.backend.connections()
		c.backend.dropAll()
		return c.waitFor("the wait to open the stream again", func() bool { return c.backend.connections() > before })
	})
	ctx.Given(`^the Deputy has mailed the Mayor "([^"]*)"$`, func(subject string) error {
		return c.mail("deputy@desktop", subject)
	})
	ctx.Given(`^the Builder has mailed the Mayor "([^"]*)"$`, func(subject string) error {
		return c.mail("builder@desktop", subject)
	})

	ctx.Then(`^mw talk wait ends within (\d+) seconds?$`, c.endsWithin)
	ctx.Then(`^mw talk wait is still waiting$`, c.stillWaiting)
	ctx.Then(`^the wait printed "([^"]*)"$`, func(text string) error {
		return c.printed(text, true)
	})
	ctx.Then(`^the wait did not print "([^"]*)"$`, func(text string) error {
		return c.printed(text, false)
	})
	ctx.Then(`^the wait printed an index-to-print time in milliseconds$`, func() error {
		if !regexp.MustCompile(`index-to-print \d+ ms`).MatchString(c.run.out.String()) {
			return fmt.Errorf("expected an index-to-print time in ms, it printed:\n%s", c.run.out.String())
		}
		return nil
	})

	ctx.When(`^mw postern inbox --unread-count is run against the backend$`, func() error {
		out, err := c.inbox(true)
		if err != nil {
			return err
		}
		n, err := strconv.Atoi(strings.TrimSpace(out))
		if err != nil {
			return fmt.Errorf("the unread count %q is not a number: %w", out, err)
		}
		c.inboxCount = n
		return nil
	})
	ctx.When(`^mw postern inbox is run against the backend$`, func() error {
		out, err := c.inbox(false)
		c.inboxOut.WriteString(out)
		return err
	})
	ctx.Then(`^the backend inbox counts (\d+) unread$`, func(want int) error {
		if c.inboxCount != want {
			return fmt.Errorf("expected %d unread, the inbox counted %d", want, c.inboxCount)
		}
		return nil
	})
	ctx.Then(`^the backend inbox printed "([^"]*)"$`, func(text string) error {
		if !strings.Contains(c.inboxOut.String(), text) {
			return fmt.Errorf("expected the inbox to print %q, it printed:\n%s", text, c.inboxOut.String())
		}
		return nil
	})
	ctx.Then(`^the backend inbox did not print "([^"]*)"$`, func(text string) error {
		if strings.Contains(c.inboxOut.String(), text) {
			return fmt.Errorf("expected the inbox not to print %q, it printed:\n%s", text, c.inboxOut.String())
		}
		return nil
	})
}

func (c *talkWaitContext) aBackendThatStreams() error {
	home, err := os.MkdirTemp("", "mw-talk-wait-")
	if err != nil {
		return err
	}
	c.home = home
	c.keys = postern.New(filepath.Join(home, "postern.key"))
	if err := c.keys.Generate(); err != nil {
		return err
	}
	if c.mayor, _, err = c.keys.PublicKey(); err != nil {
		return err
	}
	c.backend = &talkBackend{conns: map[*talkConn]bool{}}
	c.server = httptest.NewServer(http.HandlerFunc(c.backend.handler))
	c.client = postern.NewHTTP(c.server.URL, c.keys)
	c.memory = apptest.NewFakeTracker()
	c.mailbox = apptest.NewFakeMailbox()
	return nil
}

func (c *talkWaitContext) mail(from, subject string) error {
	_, err := c.mailbox.Send(context.Background(), application.NewMessage{From: from, To: "mayor", Subject: subject, Body: subject + " body"})
	return err
}

// arm starts mw talk wait and waits for it to be on the stream, so that what a
// scenario indexes next is an event it is there to hear.
func (c *talkWaitContext) arm() error {
	before := c.backend.connections()
	run := &talkWaitRun{done: make(chan struct{}), armedAt: time.Now()}
	c.run = run
	cipher := apptest.NewFakeCipher()
	wait := application.TalkWait{
		Stream:      c.client,
		Postern:     c.client,
		Cipher:      cipher,
		Keys:        c.keys,
		Memory:      c.memory,
		Mailbox:     c.mailbox,
		GovernorKey: c.governorKey,
		Limit:       c.limit,
		MinBackoff:  5 * time.Millisecond,
		MaxBackoff:  20 * time.Millisecond,
		Out:         &run.out,
		Err:         &run.errOut,
	}
	if wait.Limit == 0 {
		wait.Limit = time.Minute
	}
	go func() {
		defer close(run.done)
		_, run.err = wait.Run(context.Background())
	}()
	return c.waitFor("the wait to open the stream", func() bool {
		select {
		case <-run.done:
			return true
		default:
		}
		return c.backend.connections() > before
	})
}

func (c *talkWaitContext) waitFor(what string, ok func() bool) error {
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if ok() {
			return nil
		}
		time.Sleep(2 * time.Millisecond)
	}
	return fmt.Errorf("timed out waiting for %s", what)
}

func (c *talkWaitContext) endsWithin(seconds int) error {
	base := c.lastEvent
	if c.run.armedAt.After(base) {
		base = c.run.armedAt
	}
	select {
	case <-c.run.done:
		if c.run.err != nil {
			return fmt.Errorf("mw talk wait failed: %w\n%s", c.run.err, c.run.errOut.String())
		}
		return nil
	case <-time.After(time.Until(base.Add(time.Duration(seconds) * time.Second))):
		return fmt.Errorf("mw talk wait had not ended %d s after the event; so far it printed:\n%s%s",
			seconds, c.run.out.String(), c.run.errOut.String())
	}
}

func (c *talkWaitContext) stillWaiting() error {
	select {
	case <-c.run.done:
		return fmt.Errorf("mw talk wait ended, it printed:\n%s%s", c.run.out.String(), c.run.errOut.String())
	case <-time.After(300 * time.Millisecond):
		return nil
	}
}

func (c *talkWaitContext) printed(text string, want bool) error {
	if got := strings.Contains(c.run.out.String(), text); got != want {
		if want {
			return fmt.Errorf("expected the wait to print %q, it printed:\n%s", text, c.run.out.String())
		}
		return fmt.Errorf("expected the wait not to print %q, it printed:\n%s", text, c.run.out.String())
	}
	return nil
}

// inbox runs mw postern inbox, or its --unread-count, against the backend,
// keeping its cursor in the same notes the wait keeps its own in, as the real
// ones do under bd.
func (c *talkWaitContext) inbox(unreadCount bool) (string, error) {
	var out bytes.Buffer
	inbox := application.PosternInbox{
		Postern:     c.client,
		Cipher:      apptest.NewFakeCipher(),
		Keys:        c.keys,
		Memory:      c.memory,
		GovernorKey: c.governorKey,
		Out:         &out,
	}
	var err error
	if unreadCount {
		_, err = inbox.UnreadCount(context.Background())
	} else {
		_, err = inbox.Run(context.Background())
	}
	return out.String(), err
}
