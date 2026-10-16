package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
)

// TodoRankField is the metadata field that orders a hitl:todo card among the
// cards of its list: a number, written as text, that sorts the card ahead of
// those with a higher one. The Governor's phone moves a card by todo_move
// (postern's docs/protocol.md section 13), which sets it; a card the Mayor
// filed has none and follows the ranked ones, in the order filed.
const TodoRankField = "todo_rank"

// todoRank reads a rank as a number, false for none or for text that is not
// one.
func todoRank(text string) (float64, bool) {
	text = strings.TrimSpace(text)
	if text == "" {
		return 0, false
	}
	rank, err := strconv.ParseFloat(text, 64)
	return rank, err == nil
}

// formatTodoRank writes a rank as todo_rank holds it.
func formatTodoRank(rank float64) string { return strconv.FormatFloat(rank, 'g', -1, 64) }

// InTodoOrder is cards in the order a list shows them: those with a todo_rank
// by rank, then the rest as they came, which is the order filed. It leaves
// cards as it was.
func InTodoOrder(cards []StoryDetail) []StoryDetail {
	ordered := slices.Clone(cards)
	sort.SliceStable(ordered, func(i, j int) bool {
		a, aRanked := todoRank(ordered[i].TodoRank)
		b, bRanked := todoRank(ordered[j].TodoRank)
		if aRanked != bRanked {
			return aRanked
		}
		return aRanked && a < b
	})
	return ordered
}

// todoRankFor is the rank to give a card put at index among cards, the cards
// of its list in order and without it. It is a rank between its neighbours
// when there is one, which changes no other card. When there is none — the
// card before it has no rank, or its neighbours' ranks leave no number between
// them — renumbers is every card of the list numbered 1, 2, 3 in the order it
// then stands, the moved card's number being rank; the cards' ids are its keys.
func todoRankFor(cards []StoryDetail, index int) (rank string, renumbers map[string]string) {
	var prev, next *float64
	prevRanked := true
	if index > 0 {
		r, has := todoRank(cards[index-1].TodoRank)
		prevRanked = has
		if has {
			prev = &r
		}
	}
	if index < len(cards) {
		if r, has := todoRank(cards[index].TodoRank); has {
			next = &r
		}
	}
	switch {
	case !prevRanked:
	case prev == nil && next == nil:
		return formatTodoRank(1), nil
	case prev == nil:
		return formatTodoRank(*next - 1), nil
	case next == nil:
		return formatTodoRank(*prev + 1), nil
	default:
		if mid := (*prev + *next) / 2; mid > *prev && mid < *next {
			return formatTodoRank(mid), nil
		}
	}
	renumbers = map[string]string{}
	for i, card := range cards {
		n := i + 1
		if i >= index {
			n++
		}
		renumbers[card.Story.ID] = formatTodoRank(float64(n))
	}
	return formatTodoRank(float64(index + 1)), renumbers
}

// ClosedTodoCards is a tracker that can list the beads carrying a label that
// were closed at since or later: the cards the Governor finished, which the
// ranking pass reads to see when and what he gets done.
type ClosedTodoCards interface {
	ClosedWithLabel(ctx context.Context, label string, since time.Time) ([]StoryDetail, error)
}

// TodoRankRuns keeps the ranking pass's logs, one file a day under
// runs/TodoRankRunDir in the vault: the vault's PutRunFile.
type TodoRankRuns interface {
	PutRunFile(ctx context.Context, storyID, name, contents string) (string, error)
}

// TodoRankRunDir is the directory under runs/ the pass logs go in, each
// named for the date in his zone, <date>.json.
const TodoRankRunDir = "todo-rank"

// TodoAboutFile is the vault's file of his own words on his hours and days,
// which the Mayor writes from his answers and the pass reads.
const TodoAboutFile = "seats/mayor/todo-about.md"

// The pass's session: it needs one turn to answer and one to say so, and is
// short.
const (
	todoRankMaxTurns = 5
	todoRankTimeout  = 5 * time.Minute
)

// TodoRankLog is the pass log: when it ran, on what, every card's row and why,
// the model's tokens and how long the pass took.
type TodoRankLog struct {
	At      string            `json:"at"`
	Model   string            `json:"model"`
	Effort  string            `json:"effort"`
	Elapsed string            `json:"elapsed"`
	Tokens  TodoRankTokens    `json:"tokens"`
	Cards   []TodoRankLogCard `json:"cards"`
}

// TodoRankTokens is the model's tokens as the pass log keeps them.
type TodoRankTokens struct {
	Input      int `json:"input"`
	Output     int `json:"output"`
	CacheRead  int `json:"cache_read"`
	CacheWrite int `json:"cache_write"`
	Total      int `json:"total"`
}

// TodoRankLogCard is one card in the pass log: its row and why, whether the
// pass wrote them (false when they were the card's already), and the due time
// it marked slipped, if it did.
type TodoRankLogCard struct {
	TodoRankRow
	Written bool   `json:"written"`
	Slipped string `json:"slipped,omitempty"`
}

// TodoRankPass is mw todo rank: one short model pass that puts every open
// hitl:todo card in a row (TodoRows) with a one-line why. The model is given
// the open cards, the cards closed in the last TodoRankDoneDays and his about
// file; its answer is judged whole (judgeTodoRows), and a refused answer
// writes nothing.
type TodoRankPass struct {
	Tracker WorkTracker
	Closed  ClosedTodoCards
	Grinder Grinder
	Runs    TodoRankRuns

	// Instructions is grinds/todo-rank.md, the rule the model is given.
	Instructions string
	// AboutFile is the path of his about file; one that is not there is no
	// about text, unless AboutRequired.
	AboutFile     string
	AboutRequired bool

	Model, Effort string
	// Zone is his: the days and hours the model is given are in it.
	Zone *time.Location

	// Now is the clock; the zero value reads the real one.
	Now func() time.Time
	// Out is where the rows are printed. A nil Out prints nothing.
	Out io.Writer
}

// Run makes one pass; with dryRun it prints the rows and writes nothing. It
// writes todo_row and todo_row_why only on a card whose row or why changed,
// todo_slipped on a starred card past due once for each due time, then the
// pass log. It reports the rows in the order the cards were read.
func (p TodoRankPass) Run(ctx context.Context, dryRun bool) ([]TodoRankRow, error) {
	started := time.Now()
	now := started
	if p.Now != nil {
		now = p.Now()
	}
	zone := p.Zone
	if zone == nil {
		zone = time.UTC
	}
	about, err := p.about()
	if err != nil {
		return nil, fmt.Errorf("mw todo rank: %w", err)
	}
	open, done, lists, err := p.read(ctx, now)
	if err != nil {
		return nil, fmt.Errorf("mw todo rank: %w", err)
	}
	if len(open) == 0 {
		p.say("mw todo rank: no open To do card to rank\n")
		return nil, nil
	}

	dir, err := os.MkdirTemp("", "mw-todo-rank-")
	if err != nil {
		return nil, fmt.Errorf("mw todo rank: %w", err)
	}
	defer os.RemoveAll(dir)
	result, err := p.Grinder.Grind(ctx, GrindCall{
		Dir: dir, Model: p.Model, Effort: p.Effort, System: todoRankSystem(p.Instructions), Schema: TodoRankSchema,
		Prompt: todoRankMessage(now, zone, about, open, done, lists), Timeout: todoRankTimeout, MaxTurns: todoRankMaxTurns,
	})
	if err != nil {
		return nil, fmt.Errorf("mw todo rank: the model's pass failed, nothing was written: %w", err)
	}
	if result.IsError || len(result.Answer) == 0 {
		return nil, fmt.Errorf("mw todo rank: the model gave no answer (%s), nothing was written: %s", result.Subtype, strings.TrimSpace(result.Said))
	}
	rows, refused := judgeTodoRows(open, result.Answer)
	if len(refused) > 0 {
		return nil, fmt.Errorf("mw todo rank: the model's answer is refused, nothing was written: %s", strings.Join(refused, "; "))
	}

	titles := make(map[string]string, len(open))
	for _, card := range open {
		titles[card.Story.ID] = strings.TrimSpace(card.Story.Title)
	}
	if dryRun {
		p.say("mw todo rank: %d cards ranked (a dry run: nothing written)\n", len(rows))
		p.printRows(rows, titles)
		return rows, nil
	}

	logged := make([]TodoRankLogCard, len(rows))
	written := 0
	for n, card := range open {
		row := rows[n]
		logged[n] = TodoRankLogCard{TodoRankRow: row}
		fields := map[string]string{}
		if row.Row != card.TodoRow || row.Why != card.TodoRowWhy {
			fields[TodoRowField], fields[TodoRowWhyField] = row.Row, row.Why
			logged[n].Written = true
		}
		if due := slippedStar(card, now); due != "" {
			fields[TodoSlippedField] = due
			logged[n].Slipped = due
		}
		if len(fields) == 0 {
			continue
		}
		if err := p.Tracker.SetStoryMetadata(ctx, card.Story.ID, fields); err != nil {
			return nil, fmt.Errorf("mw todo rank: writing %s, after %d cards written: %w", card.Story.ID, written, err)
		}
		written++
	}

	fuel := result.Fuel
	entry := TodoRankLog{
		At: now.UTC().Format(time.RFC3339), Model: p.Model, Effort: p.Effort,
		Elapsed: time.Since(started).Round(time.Millisecond).String(),
		Tokens: TodoRankTokens{
			Input: fuel.Input, Output: fuel.Output, CacheRead: fuel.CacheRead, CacheWrite: fuel.CacheWrite, Total: fuel.Total(),
		},
		Cards: logged,
	}
	body, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("mw todo rank: %w", err)
	}
	where, err := p.Runs.PutRunFile(ctx, TodoRankRunDir, now.In(zone).Format(time.DateOnly)+".json", string(body)+"\n")
	if err != nil {
		return nil, fmt.Errorf("mw todo rank: %d cards written, but the pass log was not: %w", written, err)
	}
	p.say("mw todo rank: %d cards ranked, %d written; the log is %s\n", len(rows), written, where)
	p.printRows(rows, titles)
	return rows, nil
}

// about is the text of his about file, "" when there is none.
func (p TodoRankPass) about() (string, error) {
	if strings.TrimSpace(p.AboutFile) == "" {
		return "", nil
	}
	text, err := os.ReadFile(p.AboutFile)
	if errors.Is(err, fs.ErrNotExist) && !p.AboutRequired {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("reading the about file: %w", err)
	}
	return string(text), nil
}

// read is the open cards, the cards closed in the last TodoRankDoneDays, and
// the titles of the lists they are on, by id.
func (p TodoRankPass) read(ctx context.Context, now time.Time) (open, done []StoryDetail, lists map[string]string, err error) {
	ready, err := p.Tracker.ReadyWithLabel(ctx, LabelHitlTodo)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("reading the To do cards: %w", err)
	}
	for _, card := range ready {
		if refuseNotACard(card) == "" {
			open = append(open, card)
		}
	}
	closed, err := p.Closed.ClosedWithLabel(ctx, LabelHitlTodo, now.AddDate(0, 0, -TodoRankDoneDays))
	if err != nil {
		return nil, nil, nil, fmt.Errorf("reading the cards closed in the last %d days: %w", TodoRankDoneDays, err)
	}
	for _, card := range closed {
		if !card.IsEpic && card.Todo() && card.Closed() {
			done = append(done, card)
		}
	}
	var ids []string
	seen := map[string]bool{}
	for _, card := range append(slices.Clone(open), done...) {
		if id := card.EpicID; id != "" && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	lists = map[string]string{}
	if len(ids) > 0 {
		found, err := p.Tracker.ShowBeads(ctx, ids)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("reading the lists %s: %w", strings.Join(ids, ", "), err)
		}
		for _, list := range found {
			lists[list.Story.ID] = list.Story.Title
		}
	}
	return open, done, lists, nil
}

// slippedStar is the due time of card when it is starred, past due and not
// yet marked slipped for that due time; "" otherwise.
func slippedStar(card StoryDetail, now time.Time) string {
	due := strings.TrimSpace(card.TodoDue)
	if !card.TodoImportant || due == "" || strings.TrimSpace(card.TodoSlipped) == due {
		return ""
	}
	at, err := parseTodoDue(due)
	if err != nil || !at.Before(now) {
		return ""
	}
	return due
}

// printRows prints the rows a row at a time, now first.
func (p TodoRankPass) printRows(rows []TodoRankRow, titles map[string]string) {
	for _, want := range TodoRows {
		for _, row := range rows {
			if row.Row == want {
				p.say("%-10s %-10s %s — %s\n", row.Row, row.Bead, titles[row.Bead], row.Why)
			}
		}
	}
}

func (p TodoRankPass) say(format string, args ...any) {
	if p.Out != nil {
		fmt.Fprintf(p.Out, format, args...)
	}
}
