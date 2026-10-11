package steps

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
	"github.com/Jonathan-A-White/millwright/infrastructure/postern"
	"github.com/Jonathan-A-White/millwright/infrastructure/vault"

	"github.com/cucumber/godog"
)

// reportSendContext holds what mw report send reads and writes, all in memory
// or in a temp directory: the tracker and vault the report reads, the home
// file, a fake postern backend and cipher, and the list of days sent.
type reportSendContext struct {
	dir        string
	tracker    *apptest.FakeTracker
	files      *vault.Vault
	benchmarks *apptest.FakeBenchmarks
	home       *apptest.FakeHomeFile
	backend    *apptest.FakePostern
	cipher     *apptest.FakeCipher
	keys       *postern.KeyFile
	sent       *apptest.FakeReportSentLog

	host string
	now  time.Time

	printed bytes.Buffer
	err     error
}

// InitializeReportSendScenario registers the steps of features/report_send.feature.
func InitializeReportSendScenario(ctx *godog.ScenarioContext) {
	c := &reportSendContext{}

	ctx.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
		*c = reportSendContext{
			tracker:    apptest.NewFakeTracker(),
			benchmarks: &apptest.FakeBenchmarks{},
			home:       &apptest.FakeHomeFile{},
			backend:    apptest.NewFakePostern(),
			cipher:     apptest.NewFakeCipher(),
			sent:       &apptest.FakeReportSentLog{},
		}
		return ctx, nil
	})
	ctx.After(func(ctx context.Context, sc *godog.Scenario, err error) (context.Context, error) {
		if c.dir != "" {
			os.RemoveAll(c.dir)
		}
		return ctx, nil
	})

	ctx.Given(`^a summary factory whose clock reads "([^"]*)"$`, c.aFactory)
	ctx.Given(`^the summary home file says "([^"]*)"$`, c.theHomeFileSays)
	ctx.Given(`^this summary host is "([^"]*)"$`, c.thisHostIs)
	ctx.Given(`^the summary ledger holds the rows:$`, c.theLedgerRows)
	ctx.Given(`^the summary benchmarks:$`, c.theBenchmarks)
	ctx.Given(`^the postern backend will report the summary txid "([^"]*)"$`, c.theBackendWillReport)
	ctx.Given(`^the summary clock reads "([^"]*)"$`, c.theClockReads)
	ctx.Given(`^mw report send has run$`, c.sendRuns(false, false))
	ctx.When(`^mw report send runs$`, c.sendRuns(false, false))
	ctx.When(`^mw report send runs with --force$`, c.sendRuns(true, false))
	ctx.When(`^mw report send runs with --dry-run$`, c.sendRuns(false, true))
	ctx.Then(`^the summary was posted once, as a plain message with no class and no bead$`, c.postedOnceAsAPlainMessage)
	ctx.Then(`^the summary was posted (\d+) times$`, c.postedTimes)
	ctx.Then(`^nothing was posted$`, c.nothingPosted)
	ctx.Then(`^the posted text starts with "([^"]*)"$`, c.postedStartsWith)
	ctx.Then(`^the posted text holds QUALITY, SPEED, FUEL and CHEAP WINS in that order$`, c.postedHoldsPartsInOrder)
	ctx.Then(`^the posted text has the line "([^"]*)"$`, c.postedHasTheLine)
	ctx.Then(`^the posted text is at most (\d+) lines of (\d+) columns$`, c.postedIsAtMost)
	ctx.Then(`^the summary prints the txid "([^"]*)"$`, c.printsTheTxid)
	ctx.Then(`^the summary says "([^"]*)"$`, c.says)
	ctx.Then(`^the summary remembers no day$`, c.remembersNoDay)
}

func (c *reportSendContext) aFactory(now string) error {
	at, err := time.Parse(time.RFC3339, now)
	if err != nil {
		return err
	}
	c.now = at
	if c.dir, err = os.MkdirTemp("", "mw-report-send-"); err != nil {
		return err
	}
	c.files = vault.New(filepath.Join(c.dir, "vault"))
	c.keys = postern.New(filepath.Join(c.dir, "postern.key"))
	return c.keys.Generate()
}

func (c *reportSendContext) theHomeFileSays(text string) error {
	c.home.Text = text + "\n"
	return nil
}

func (c *reportSendContext) thisHostIs(host string) error {
	c.host = host
	return nil
}

func (c *reportSendContext) theClockReads(now string) error {
	at, err := time.Parse(time.RFC3339, now)
	c.now = at
	return err
}

func (c *reportSendContext) theBackendWillReport(txid string) error {
	c.backend.NextTxid = txid
	return nil
}

// theLedgerRows fills the builder's ledger, and files a lampas story on the
// fake tracker for each row's id.
func (c *reportSendContext) theLedgerRows(table *godog.Table) error {
	cols := columns(table)
	for _, row := range table.Rows[1:] {
		cell := func(name string) string { return strings.TrimSpace(row.Cells[cols[name]].Value) }
		id := cell("id")
		c.tracker.AddStory("", domain.Story{ID: id, Title: "The story " + id,
			Overrides: domain.Path{Rig: "lampas", Host: "laptop", Model: domain.Model("sonnet")}})
		if err := c.tracker.SetUpdated(id, c.now); err != nil {
			return err
		}
		at, err := time.Parse(application.LedgerDate, cell("day"))
		if err != nil {
			return err
		}
		tokens, _ := strconv.Atoi(cell("tokens"))
		outcome := cell("outcome")
		if reason, refused := strings.CutPrefix(outcome, "refused for "); refused {
			outcome = application.NotLanded(application.Reason(reason), "the gate was red")
		}
		line := application.LedgerLine{When: at, StoryID: id, Title: "The story " + id, Outcome: outcome,
			Path:   domain.Path{Model: domain.Model("sonnet"), Effort: domain.Effort("high")},
			Result: application.SessionResult{Fuel: application.Fuel{Input: tokens}}}.String()
		if err := c.files.AppendToLedger(context.Background(), reportSeat, line); err != nil {
			return err
		}
	}
	return nil
}

func (c *reportSendContext) theBenchmarks(table *godog.Table) error {
	cols := columns(table)
	for _, row := range table.Rows[1:] {
		cell := func(name string) string { return strings.TrimSpace(row.Cells[cols[name]].Value) }
		day, err := time.Parse(application.LedgerDate, cell("day"))
		if err != nil {
			return err
		}
		b := application.Benchmark{Story: cell("story"), Rig: "lampas", Host: "laptop", At: day.Add(12 * time.Hour), Landed: true}
		b.ActualS, _ = strconv.ParseFloat(cell("actual"), 64)
		ratio, _ := strconv.ParseFloat(cell("ratio"), 64)
		b.ParS = b.ActualS / ratio
		c.benchmarks.Records = append(c.benchmarks.Records, b)
	}
	return nil
}

func (c *reportSendContext) sendRuns(force, dryRun bool) func() error {
	return func() error {
		c.printed.Reset()
		now := func() time.Time { return c.now }
		_, c.err = application.FactoryReportSend{
			Report: application.FactoryReport{
				Tracker: c.tracker, Files: c.files, Benchmarks: c.benchmarks, Seat: reportSeat,
			},
			Home: c.home,
			Host: c.host,
			Sent: c.sent,
			Post: application.PosternSend{
				Postern: c.backend, Chain: apptest.NewFakeChain(c.backend, c.keys), Cipher: c.cipher, Keys: c.keys,
				GovernorKey: "governor-pubkey-hex", Channel: application.PosternChannelDirect, Now: now,
			},
			Force: force, DryRun: dryRun,
			Now: now,
			Out: &c.printed,
		}.Run(context.Background())
		if c.err != nil {
			return fmt.Errorf("mw report send failed: %w", c.err)
		}
		return nil
	}
}

// posted is the plaintext of the last message delivered, and the record that
// carried it.
func (c *reportSendContext) posted() (string, application.PosternPayload, error) {
	delivered := c.backend.Delivered()
	if len(delivered) == 0 {
		return "", application.PosternPayload{}, fmt.Errorf("nothing was posted")
	}
	var record application.PosternPayload
	if err := json.Unmarshal(delivered[len(delivered)-1], &record); err != nil {
		return "", record, err
	}
	privKey, err := c.keys.PrivateKeyWIF()
	if err != nil {
		return "", record, err
	}
	text, _, err := c.cipher.Decrypt(privKey, record.Ct)
	return text, record, err
}

func (c *reportSendContext) postedOnceAsAPlainMessage() error {
	if err := c.postedTimes(1); err != nil {
		return err
	}
	text, record, err := c.posted()
	if err != nil {
		return err
	}
	var threaded application.PosternThreadedMessage
	if json.Unmarshal([]byte(text), &threaded) == nil && (threaded.Thread != application.PosternThread{}) {
		return fmt.Errorf("expected a message in the Factory channel, got a threaded one: %s", text)
	}
	if record.Class != "" && record.Class != "message" {
		return fmt.Errorf("expected a plain message, got class %q", record.Class)
	}
	return nil
}

func (c *reportSendContext) postedTimes(want int) error {
	if got := len(c.backend.Delivered()); got != want {
		return fmt.Errorf("expected %d messages posted, got %d; the summary printed:\n%s", want, got, c.printed.String())
	}
	return nil
}

func (c *reportSendContext) nothingPosted() error {
	return c.postedTimes(0)
}

func (c *reportSendContext) postedStartsWith(want string) error {
	text, _, err := c.posted()
	if err != nil {
		return err
	}
	if !strings.HasPrefix(text, want) {
		return fmt.Errorf("expected the posted text to start %q, got:\n%s", want, text)
	}
	return nil
}

func (c *reportSendContext) postedHoldsPartsInOrder() error {
	text, _, err := c.posted()
	if err != nil {
		return err
	}
	at := 0
	for _, part := range []string{"QUALITY", "SPEED", "FUEL", "CHEAP WINS"} {
		i := strings.Index(text[at:], part)
		if i < 0 {
			return fmt.Errorf("expected %s after offset %d in the posted text:\n%s", part, at, text)
		}
		at += i + len(part)
	}
	return nil
}

func (c *reportSendContext) postedHasTheLine(want string) error {
	text, _, err := c.posted()
	if err != nil {
		return err
	}
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), want) {
			return nil
		}
	}
	return fmt.Errorf("expected a line starting %q in the posted text:\n%s", want, text)
}

func (c *reportSendContext) postedIsAtMost(lines, columns int) error {
	text, _, err := c.posted()
	if err != nil {
		return err
	}
	all := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if len(all) > lines {
		return fmt.Errorf("expected at most %d lines, got %d:\n%s", lines, len(all), text)
	}
	for _, line := range all {
		if n := utf8.RuneCountInString(line); n > columns {
			return fmt.Errorf("a line is %d columns wide, over %d: %q", n, columns, line)
		}
	}
	return nil
}

func (c *reportSendContext) printsTheTxid(want string) error {
	for _, line := range strings.Split(c.printed.String(), "\n") {
		if strings.TrimSpace(line) == want {
			return nil
		}
	}
	return fmt.Errorf("expected the txid %q on a line of its own, got:\n%s", want, c.printed.String())
}

func (c *reportSendContext) says(want string) error {
	if !strings.Contains(c.printed.String(), want) {
		return fmt.Errorf("expected the output to say %q, got:\n%s", want, c.printed.String())
	}
	return nil
}

func (c *reportSendContext) remembersNoDay() error {
	if got := c.sent.All(); len(got) != 0 {
		return fmt.Errorf("expected no day remembered, got %v", got)
	}
	return nil
}
