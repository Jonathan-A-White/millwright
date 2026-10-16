package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
)

// TodoRankJobName is the name of the follower's job that ranks the To do board
// each night: its events' actor is todo-rank@<host>.
const TodoRankJobName = "todo-rank"

// DefaultTodoRankHour is the UTC hour from which the nightly pass is due when
// the config does not say: about three in the morning in his zone.
const DefaultTodoRankHour = 7

// The kv notes the job keeps between runs: the UTC day of its last pass (one a
// day, whether or not it worked), the day, in his zone, the weekly card was last
// sent, and under TodoLetGoKeyPrefix the cards the weekly card of a list asked
// about, a JSON list of bead ids, for the answer to find.
const (
	TodoRankDayKey     = "todo.rank.last"
	TodoRankWeeklyKey  = "todo.rank.weekly"
	TodoLetGoKeyPrefix = "todo.letgo."
)

// TodoLetGoKey is the note the weekly Let go card of the list epic holds the
// cards it listed under.
func TodoLetGoKey(epic string) string { return TodoLetGoKeyPrefix + epic }

// The two answers the weekly Let go card offers. A tap on a card's own Keep or
// Drop is finer; these two answer for every card the card listed.
const (
	TodoKeepAllOption = "A: Keep them all"
	TodoDropAllOption = "B: Drop them all"
	// TodoLetGoReason is the reason a card dropped by the weekly card is closed
	// with.
	TodoLetGoReason = "let-go"
)

// TodoNightly is the follower's todo-rank job: once a UTC day, from Hour on and
// on the home only, one ranking pass (Rank). After a pass that worked it tells
// the Governor once, in the card's own thread, of each starred card the pass
// marked slipped, and on the first pass of the week (WeekStart, in his Zone) it
// posts the weekly Let go card.
//
// The day is marked before the pass begins, so a pass that fails is tried the
// next night and never twice in one.
type TodoNightly struct {
	// Hour is the UTC hour of the day from which the pass is due.
	Hour int
	// WeekStart is the weekday, in Zone, the weekly card is posted on.
	WeekStart time.Weekday
	// Zone is his: the week is read in it. Nil is UTC.
	Zone *time.Location

	// Rank makes one ranking pass (mw todo rank).
	Rank    func(ctx context.Context) error
	Tracker WorkTracker
	Notes   TodoNightlyNotes
	Sender  PosternSender
	// Home says whether this host is the home: only the home ranks.
	Home func(ctx context.Context) (bool, error)
	// Now is the clock; nil is time.Now.
	Now func() time.Time
	// Err is where a card passed over is said. A nil Err says nothing.
	Err io.Writer

	mu    sync.Mutex
	tried string // the UTC day the pass was last begun
}

func (n *TodoNightly) now() time.Time {
	if n.Now != nil {
		return n.Now()
	}
	return time.Now()
}

func (n *TodoNightly) zone() *time.Location {
	if n.Zone != nil {
		return n.Zone
	}
	return time.UTC
}

// Job is the follower's job: no event springs it; every pass of the follower
// asks whether the night's pass is due.
func (n *TodoNightly) Job() SpringJob {
	return SpringJob{
		Name: TodoRankJobName,
		Probe: func(ctx context.Context) (string, bool) {
			if n.due(ctx) {
				return ClockReason, true
			}
			return "", false
		},
		Run: n.run,
	}
}

// due reports whether the pass is to be made now: it is the hour, none has been
// begun today, and this is the home. The home is asked last, for it reads a file
// in the vault.
func (n *TodoNightly) due(ctx context.Context) bool {
	now := n.now().UTC()
	if now.Hour() < n.Hour {
		return false
	}
	day := now.Format(time.DateOnly)
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.tried == day {
		return false
	}
	last, err := n.Notes.Note(ctx, TodoRankDayKey)
	if err != nil {
		n.say("mw events follow: reading the day of the last To do pass: %v\n", err)
		return false
	}
	if strings.TrimSpace(last) == day {
		n.tried = day
		return false
	}
	at, err := n.Home(ctx)
	return err == nil && at
}

// run makes the night's pass, then tells him of what it found.
func (n *TodoNightly) run(ctx context.Context) error {
	if !n.due(ctx) {
		return nil
	}
	day := n.now().UTC().Format(time.DateOnly)
	n.mu.Lock()
	n.tried = day
	n.mu.Unlock()
	if err := n.Notes.SetNote(ctx, TodoRankDayKey, day); err != nil {
		return fmt.Errorf("marking the day of the To do pass: %w", err)
	}
	if err := n.Rank(ctx); err != nil {
		return err
	}
	return errors.Join(n.tellSlipped(ctx), n.weeklyLetGo(ctx))
}

// tellSlipped posts, in the thread of each open card the pass marked slipped
// and he has not been told of, that it is past due. A card is marked told before
// the post, so a pass that dies half-way never posts twice; a post that fails
// takes the mark back, so the next night tries it again.
func (n *TodoNightly) tellSlipped(ctx context.Context) error {
	cards, err := n.Tracker.ReadyWithLabel(ctx, LabelHitlTodo)
	if err != nil {
		return fmt.Errorf("reading the To do cards to tell of slipped ones: %w", err)
	}
	var failed []string
	for _, card := range cards {
		slipped := strings.TrimSpace(card.TodoSlipped)
		if card.IsEpic || card.Closed() || slipped == "" || strings.TrimSpace(card.TodoSlippedTold) == slipped {
			continue
		}
		if err := n.tell(ctx, card, slipped); err != nil {
			failed = append(failed, fmt.Sprintf("%s: %v", card.Story.ID, err))
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("a slipped card was not told: %s", strings.Join(failed, "; "))
	}
	return nil
}

func (n *TodoNightly) tell(ctx context.Context, card StoryDetail, slipped string) error {
	id := card.Story.ID
	if err := n.Tracker.SetStoryMetadata(ctx, id, map[string]string{TodoSlippedToldField: slipped}); err != nil {
		return fmt.Errorf("marking it told: %w", err)
	}
	title := strings.TrimSpace(card.Story.Title)
	text := fmt.Sprintf("%s is past due. You starred it, so it has not been moved off your list.", title)
	if at, err := parseTodoDue(slipped); err == nil {
		text = fmt.Sprintf("%s was due %s and is past due. You starred it, so it has not been moved off your list.",
			title, at.In(n.zone()).Format("Mon 2 Jan 15:04"))
	}
	_, err := n.Sender.Run(ctx, PosternSendRequest{Class: "message", Text: text, Thread: id, Recorded: true})
	if err == nil {
		return nil
	}
	if clearErr := n.Tracker.SetStoryMetadata(ctx, id, map[string]string{TodoSlippedToldField: card.TodoSlippedTold}); clearErr != nil {
		return fmt.Errorf("%w (and the mark could not be taken off: %v)", err, clearErr)
	}
	return err
}

// weeklyLetGo posts the weekly Let go card on the first pass of the week: one
// decision card on the list of the first let-go card, listing every let-go card
// and offering Keep them all and Drop them all. It is posted once for each week,
// and not at all when no card is let go.
func (n *TodoNightly) weeklyLetGo(ctx context.Context) error {
	local := n.now().In(n.zone())
	if local.Weekday() != n.WeekStart {
		return nil
	}
	week := local.Format(time.DateOnly)
	if sent, err := n.Notes.Note(ctx, TodoRankWeeklyKey); err != nil {
		return fmt.Errorf("reading the day of the last Let go card: %w", err)
	} else if strings.TrimSpace(sent) == week {
		return nil
	}
	cards, err := n.Tracker.ReadyWithLabel(ctx, LabelHitlTodo)
	if err != nil {
		return fmt.Errorf("reading the To do cards for the Let go card: %w", err)
	}
	var letGo []StoryDetail
	for _, card := range InTodoOrder(cards) {
		if !card.IsEpic && !card.Closed() && card.TodoRow == TodoRowLetGo {
			letGo = append(letGo, card)
		}
	}
	if len(letGo) == 0 {
		return n.Notes.SetNote(ctx, TodoRankWeeklyKey, week)
	}
	epic := ""
	var ids []string
	var lines []string
	for _, card := range letGo {
		if epic == "" {
			epic = card.EpicID
		}
		ids = append(ids, card.Story.ID)
		line := fmt.Sprintf("- %s: %s", card.Story.ID, strings.TrimSpace(card.Story.Title))
		if why := strings.TrimSpace(card.TodoRowWhy); why != "" {
			line += " (" + why + ")"
		}
		lines = append(lines, line)
	}
	if epic == "" {
		return fmt.Errorf("the Let go card is not sent: %s is on no list", letGo[0].Story.ID)
	}
	listed, err := json.Marshal(ids)
	if err != nil {
		return err
	}
	// The cards first: an answer that arrives finds what it was asked about.
	if err := n.Notes.SetNote(ctx, TodoLetGoKey(epic), string(listed)); err != nil {
		return fmt.Errorf("keeping the cards of the Let go card: %w", err)
	}
	text := fmt.Sprintf("Let go: %d cards look ready to go.\n%s\nA tap on a card's own Keep or Drop is finer; these two answer for all of them.",
		len(letGo), strings.Join(lines, "\n"))
	if _, err := n.Sender.Run(ctx, PosternSendRequest{
		Class: "decision-needed", Text: text, Bead: epic, Options: []string{TodoKeepAllOption, TodoDropAllOption},
	}); err != nil {
		return fmt.Errorf("sending the Let go card: %w", err)
	}
	return n.Notes.SetNote(ctx, TodoRankWeeklyKey, week)
}

func (n *TodoNightly) say(format string, args ...any) {
	if n.Err != nil {
		fmt.Fprintf(n.Err, format, args...)
	}
}
