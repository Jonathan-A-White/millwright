package steps

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	millwright "github.com/Jonathan-A-White/millwright"
	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
	"github.com/Jonathan-A-White/millwright/infrastructure/postern"

	"github.com/cucumber/godog"
)

// todoNightlyGovernorKey is the key the answers of these scenarios are signed
// by, and the one the inbox trusts as the Governor's.
const todoNightlyGovernorKey = "governor-pubkey-hex"

// todoNightlyContext holds the fake tracker, grinder and sender the todo-rank
// job runs against, the job itself (kept from one ask to the next, as the
// follower keeps it), and, for the answer scenarios, the postern inbox's fakes.
type todoNightlyContext struct {
	tracker *apptest.FakeTracker
	grinder *apptest.FakeGrinder
	sender  *apptest.FakePosternSender
	runs    *todoRankRuns

	now       time.Time
	home      bool
	hour      int
	weekStart time.Weekday
	job       *application.SpringJob
	err       error
	printed   bytes.Buffer

	// The answer scenarios' postern inbox.
	dir     string
	backend *apptest.FakePostern
	cipher  *apptest.FakeCipher
	mailbox *apptest.FakeMailbox
	keys    *postern.KeyFile
	pubKey  string
	epic    string
	replies int
}

// InitializeTodoNightlyScenario registers the steps of features/todo-nightly.feature.
func InitializeTodoNightlyScenario(ctx *godog.ScenarioContext) {
	c := &todoNightlyContext{}

	ctx.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
		*c = todoNightlyContext{
			tracker: apptest.NewFakeTracker(), grinder: &apptest.FakeGrinder{}, sender: &apptest.FakePosternSender{},
			runs: &todoRankRuns{files: map[string]string{}}, hour: application.DefaultTodoRankHour, weekStart: time.Monday,
			backend: apptest.NewFakePostern(), mailbox: apptest.NewFakeMailbox(),
		}
		c.cipher = apptest.NewFakeCipher()
		c.cipher.From = todoNightlyGovernorKey
		return ctx, nil
	})
	ctx.After(func(ctx context.Context, sc *godog.Scenario, err error) (context.Context, error) {
		if c.dir != "" {
			os.RemoveAll(c.dir)
		}
		return ctx, nil
	})

	clock := `^the nightly clock stands at "([^"]*)"$`
	ctx.Given(clock, c.theClockStandsAt)
	ctx.When(clock, c.theClockStandsAt)
	ctx.Given(`^six To do cards are open on the list "([^"]*)" for the nightly job$`, c.sixCardsAreOpen)
	ctx.Given(`^the nightly host is home$`, func() error { c.home = true; return nil })
	ctx.Given(`^the nightly host is not home$`, func() error { c.home = false; return nil })
	ctx.Given(`^the todo-rank hour is (\d+)$`, func(hour int) error { c.hour = hour; return nil })
	ctx.Given(`^the todo week starts on "([^"]*)"$`, c.theWeekStartsOn)
	ctx.Given(`^the nightly ranking model answers every card "([^"]*)" because "([^"]*)"$`, c.theModelAnswersEveryCard)
	ctx.Given(`^the nightly ranking model answers cards "([^"]*)" and "([^"]*)" "([^"]*)" and the rest "([^"]*)"$`, c.theModelAnswersTwoCards)
	ctx.Given(`^the card "([^"]*)" is starred and due at "([^"]*)" for the nightly job$`, c.theCardIsStarredAndDue)
	ctx.Given(`^a post in a thread fails$`, func() error { c.sender.Err = errors.New("postern is down"); return nil })
	ctx.Given(`^a post in a thread does not fail$`, func() error { c.sender.Err = nil; return nil })
	ctx.Given(`^the weekly card was sent with "([^"]*)" and "([^"]*)" let go$`, c.theWeeklyCardWasSent)
	ctx.Given(`^the card "([^"]*)" is closed by hand for the nightly job$`, c.theCardIsClosedByHand)
	jobAsked := `^the todo-rank job is asked$`
	ctx.Given(jobAsked, c.theJobIsAsked)
	ctx.When(jobAsked, c.theJobIsAsked)
	ctx.When(`^he answers the weekly card "([^"]*)"$`, c.heAnswers)

	ctx.Then(`^the todo-rank job ran (\d+) times?$`, c.ran)
	ctx.Then(`^the todo-rank job failed, saying: (.+)$`, c.failedSaying)
	ctx.Then(`^exactly (\d+) messages? w(?:as|ere) posted in the thread of "([^"]*)"$`, c.postedInThread)
	ctx.Then(`^the message in the thread of "([^"]*)" says it is past due$`, c.threadMessageSaysPastDue)
	ctx.Then(`^a decision card was sent on the list "([^"]*)" with the options "([^"]*)" and "([^"]*)"$`, c.aCardWasSent)
	ctx.Then(`^the decision card lists "([^"]*)" and "([^"]*)" and not "([^"]*)"$`, c.theCardLists)
	ctx.Then(`^the decision card says a tap on a card's own Keep or Drop is finer$`, c.theCardSaysFiner)
	ctx.Then(`^exactly (\d+) decision cards? w(?:as|ere) sent in all$`, c.decisionCardsInAll)
	ctx.Then(`^the card "([^"]*)" is ranked "([^"]*)" for the nightly job$`, c.isRanked)
	ctx.Then(`^the card "([^"]*)" is still open for the nightly job$`, c.isOpen)
	ctx.Then(`^the card "([^"]*)" is still closed for the nightly job$`, c.isClosed)
	ctx.Then(`^the card "([^"]*)" was commented KEPT by the answer$`, c.wasCommentedKept)
	ctx.Then(`^the card "([^"]*)" was dropped for the nightly job with the reason "([^"]*)"$`, c.wasDropped)
}

func (c *todoNightlyContext) theClockStandsAt(at string) error {
	now, err := time.Parse(time.RFC3339, at)
	c.now = now
	return err
}

func (c *todoNightlyContext) theWeekStartsOn(day string) error {
	for d := time.Sunday; d <= time.Saturday; d++ {
		if strings.EqualFold(d.String(), day) {
			c.weekStart = d
			return nil
		}
	}
	return fmt.Errorf("%q is no weekday", day)
}

func (c *todoNightlyContext) sixCardsAreOpen(list string) error {
	c.epic = "mw-todo"
	c.tracker.AddEpic(c.epic, domain.Path{})
	c.tracker.DescribeEpic(c.epic, list, application.StatusOpen, application.DefaultPriority)
	for n := 1; n <= 6; n++ {
		id := fmt.Sprintf("mw-todo.%d", n)
		c.tracker.AddStory(c.epic, domain.Story{ID: id, Title: "Card " + id})
		if err := c.tracker.SetLabels(id, application.LabelHitl, application.LabelHitlTodo); err != nil {
			return err
		}
	}
	return nil
}

func (c *todoNightlyContext) answer(rows []application.TodoRankRow) error {
	answer, err := json.Marshal(map[string]any{"cards": rows})
	if err != nil {
		return err
	}
	c.grinder.Result = application.SessionResult{
		Subtype: "success", Answer: answer, Model: "sonnet", Duration: 4 * time.Second,
		Fuel: application.Fuel{Input: 1200, Output: 300, CacheRead: 5000},
	}
	return nil
}

func (c *todoNightlyContext) theModelAnswersEveryCard(row, why string) error {
	return c.modelAnswers("", "", row, row, why)
}

func (c *todoNightlyContext) theModelAnswersTwoCards(a, b, row, rest string) error {
	return c.modelAnswers(a, b, row, rest, "Nothing presses")
}

// modelAnswers has the model put the two named cards in row and every other
// card in rest; a card named "" is none.
func (c *todoNightlyContext) modelAnswers(a, b, row, rest, why string) error {
	var rows []application.TodoRankRow
	for n := 1; n <= 6; n++ {
		id := fmt.Sprintf("mw-todo.%d", n)
		r := rest
		if id == a || id == b {
			r = row
		}
		rows = append(rows, application.TodoRankRow{Bead: id, Row: r, Why: why})
	}
	return c.answer(rows)
}

func (c *todoNightlyContext) theCardIsStarredAndDue(id, due string) error {
	return c.tracker.SetStoryMetadata(context.Background(), id, map[string]string{
		application.TodoImportantField: "true", application.TodoDueField: due,
	})
}

// build makes the job once, on the first ask: the follower keeps one job for
// as long as it runs, and its day of the last pass with it.
func (c *todoNightlyContext) build() error {
	if c.job != nil {
		return nil
	}
	zone, err := time.LoadLocation("America/New_York")
	if err != nil {
		return err
	}
	pass := application.TodoRankPass{
		Tracker: c.tracker, Closed: c.tracker, Grinder: c.grinder, Runs: c.runs,
		Instructions: millwright.TodoRankInstructions, Model: "sonnet", Effort: "medium", Zone: zone,
		Now: func() time.Time { return c.now }, Out: &c.printed,
	}
	nightly := &application.TodoNightly{
		Hour: c.hour, WeekStart: c.weekStart, Zone: zone,
		Rank: func(ctx context.Context) error {
			_, err := pass.Run(ctx, false)
			return err
		},
		Tracker: c.tracker, Notes: c.tracker, Sender: c.sender,
		Home: func(context.Context) (bool, error) { return c.home, nil },
		Now:  func() time.Time { return c.now },
		Err:  &c.printed,
	}
	job := nightly.Job()
	c.job = &job
	return nil
}

func (c *todoNightlyContext) theJobIsAsked() error {
	if err := c.build(); err != nil {
		return err
	}
	c.err = nil
	ctx := context.Background()
	if _, ok := c.job.Probe(ctx); ok {
		c.err = c.job.Run(ctx)
	}
	return nil
}

func (c *todoNightlyContext) ran(want int) error {
	if got := len(c.grinder.Calls()); got != want {
		return fmt.Errorf("expected the job to have ranked %d times, the model was asked %d times\n%s", want, got, c.printed.String())
	}
	return nil
}

func (c *todoNightlyContext) failedSaying(reason string) error {
	if c.err == nil {
		return fmt.Errorf("expected the job to fail, saying %q; it did not", reason)
	}
	if !strings.Contains(c.err.Error(), reason) {
		return fmt.Errorf("expected the failure to say %q, got:\n%s", reason, c.err)
	}
	return nil
}

func (c *todoNightlyContext) inThread(id string) []application.PosternSendRequest {
	var posts []application.PosternSendRequest
	for _, req := range c.sender.Sent() {
		if req.Thread == id && req.Class == "message" {
			posts = append(posts, req)
		}
	}
	return posts
}

func (c *todoNightlyContext) postedInThread(want int, id string) error {
	if got := len(c.inThread(id)); got != want {
		return fmt.Errorf("expected %d messages in the thread of %s, got %d: %+v", want, id, got, c.sender.Sent())
	}
	return nil
}

func (c *todoNightlyContext) threadMessageSaysPastDue(id string) error {
	posts := c.inThread(id)
	if len(posts) == 0 || !strings.Contains(posts[0].Text, "past due") {
		return fmt.Errorf("expected the message in the thread of %s to say it is past due, got %+v", id, posts)
	}
	return nil
}

func (c *todoNightlyContext) decisionCards() []application.PosternSendRequest {
	var cards []application.PosternSendRequest
	for _, req := range c.sender.Sent() {
		if req.Class == "decision-needed" {
			cards = append(cards, req)
		}
	}
	return cards
}

func (c *todoNightlyContext) decisionCardsInAll(want int) error {
	if got := len(c.decisionCards()); got != want {
		return fmt.Errorf("expected %d decision cards, got %d: %+v", want, got, c.sender.Sent())
	}
	return nil
}

func (c *todoNightlyContext) lastCard() (application.PosternSendRequest, error) {
	cards := c.decisionCards()
	if len(cards) == 0 {
		return application.PosternSendRequest{}, fmt.Errorf("no decision card was sent; the job said:\n%s\n%v", c.printed.String(), c.err)
	}
	return cards[len(cards)-1], nil
}

func (c *todoNightlyContext) aCardWasSent(list, a, b string) error {
	card, err := c.lastCard()
	if err != nil {
		return err
	}
	if card.Bead != c.epic {
		return fmt.Errorf("expected the card on the list %q (%s), it is on %q", list, c.epic, card.Bead)
	}
	if len(card.Options) != 2 || card.Options[0] != a || card.Options[1] != b {
		return fmt.Errorf("expected the options %q and %q, got %q", a, b, card.Options)
	}
	return nil
}

func (c *todoNightlyContext) theCardLists(a, b, not string) error {
	card, err := c.lastCard()
	if err != nil {
		return err
	}
	for _, id := range []string{a, b} {
		if !strings.Contains(card.Text, id) {
			return fmt.Errorf("expected the card to list %s:\n%s", id, card.Text)
		}
	}
	if strings.Contains(card.Text, not) {
		return fmt.Errorf("expected the card not to list %s:\n%s", not, card.Text)
	}
	return nil
}

func (c *todoNightlyContext) theCardSaysFiner() error {
	card, err := c.lastCard()
	if err != nil {
		return err
	}
	if !strings.Contains(card.Text, "A tap on a card's own Keep or Drop is finer") {
		return fmt.Errorf("expected the card to say a tap on a card's own Keep or Drop is finer:\n%s", card.Text)
	}
	return nil
}

// theWeeklyCardWasSent has the job send the weekly card, on a Monday, with the
// two cards let go; the question note the card leaves is the one mw postern send
// writes.
func (c *todoNightlyContext) theWeeklyCardWasSent(a, b string) error {
	c.now = time.Date(2026, time.October, 19, 7, 0, 0, 0, time.UTC)
	if err := c.theModelAnswersTwoCards(a, b, "let-go", "later"); err != nil {
		return err
	}
	if err := c.theJobIsAsked(); err != nil {
		return err
	}
	if c.err != nil {
		return fmt.Errorf("the job failed: %w", c.err)
	}
	card, err := c.lastCard()
	if err != nil {
		return err
	}
	note, err := json.Marshal(struct {
		Txid    string   `json:"txid"`
		Options []string `json:"options"`
	}{Txid: "tx-weekly", Options: card.Options})
	if err != nil {
		return err
	}
	return c.tracker.SetNote(context.Background(), application.PosternQuestionKey(card.Bead), string(note))
}

func (c *todoNightlyContext) theCardIsClosedByHand(id string) error {
	return c.tracker.CloseStory(context.Background(), id, "dropped by hand")
}

// heAnswers sends the Governor's tap on the weekly card to a postern inbox and
// applies it.
func (c *todoNightlyContext) heAnswers(answer string) error {
	dir, err := os.MkdirTemp("", "mw-todo-nightly-")
	if err != nil {
		return err
	}
	c.dir = dir
	c.keys = postern.New(filepath.Join(dir, "postern.key"))
	if err := c.keys.Generate(); err != nil {
		return err
	}
	if c.pubKey, _, err = c.keys.PublicKey(); err != nil {
		return err
	}
	text, err := json.Marshal(application.PosternReply{Bead: c.epic, Answer: answer})
	if err != nil {
		return err
	}
	ciphertext, err := c.cipher.Encrypt(c.pubKey, string(text))
	if err != nil {
		return err
	}
	c.replies++
	c.backend.AddRecord(application.PosternRecord{
		Txid: fmt.Sprintf("tx-answer-%d", c.replies), Class: "message", From: c.cipher.From, To: c.pubKey,
		Ts: c.now, Ciphertext: ciphertext,
	})
	inbox := application.PosternInbox{
		Postern: c.backend, Cipher: c.cipher, Keys: c.keys, Memory: c.tracker, Tracker: c.tracker,
		Mailbox: c.mailbox, GovernorKey: todoNightlyGovernorKey, Out: &c.printed,
	}
	_, err = inbox.Apply(context.Background())
	return err
}

func (c *todoNightlyContext) isRanked(id, row string) error {
	if got := c.tracker.Metadata(id)[application.TodoRowField]; got != row {
		return fmt.Errorf("expected %s ranked %q, got %q\n%s", id, row, got, c.printed.String())
	}
	return nil
}

func (c *todoNightlyContext) isOpen(id string) error {
	if reason := c.tracker.CloseReason(id); reason != "" {
		return fmt.Errorf("expected %s still open, it was closed: %s", id, reason)
	}
	return nil
}

func (c *todoNightlyContext) isClosed(id string) error {
	if reason := c.tracker.CloseReason(id); reason != "dropped by hand" {
		return fmt.Errorf("expected %s still closed by hand, its close reason is %q", id, reason)
	}
	return nil
}

func (c *todoNightlyContext) wasCommentedKept(id string) error {
	for _, comment := range c.tracker.Comments(id) {
		if strings.HasPrefix(comment, "KEPT by the Governor via postern") {
			return nil
		}
	}
	return fmt.Errorf("expected a KEPT comment on %s, got %v", id, c.tracker.Comments(id))
}

func (c *todoNightlyContext) wasDropped(id, reason string) error {
	if got := c.tracker.CloseReason(id); got != reason {
		return fmt.Errorf("expected %s closed with the reason %q, got %q\n%s", id, reason, got, c.printed.String())
	}
	return nil
}
