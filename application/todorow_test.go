package application

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/domain"
)

func rowCards(ids ...string) []StoryDetail {
	cards := make([]StoryDetail, len(ids))
	for n, id := range ids {
		cards[n] = StoryDetail{Story: domain.Story{ID: id, Title: "Card " + id}, Labels: []string{LabelHitl, LabelHitlTodo}}
	}
	return cards
}

func rowAnswer(t *testing.T, rows ...TodoRankRow) json.RawMessage {
	t.Helper()
	answer, err := json.Marshal(map[string]any{"cards": rows})
	if err != nil {
		t.Fatal(err)
	}
	return answer
}

// The rows come back in the order the cards were given, whatever order the
// model answered in, each trimmed.
func TestTodoRowsComeBackInTheCardsOrderTrimmed(t *testing.T) {
	rows, refused := judgeTodoRows(rowCards("a", "b"), rowAnswer(t,
		TodoRankRow{Bead: " b ", Row: " Later", Why: " Nothing presses "},
		TodoRankRow{Bead: "a", Row: "now", Why: "Due tomorrow"}))
	if len(refused) > 0 {
		t.Fatalf("refused: %v", refused)
	}
	want := []TodoRankRow{{Bead: "a", Row: "now", Why: "Due tomorrow"}, {Bead: "b", Row: "later", Why: "Nothing presses"}}
	if len(rows) != 2 || rows[0] != want[0] || rows[1] != want[1] {
		t.Fatalf("expected %+v, got %+v", want, rows)
	}
}

// Every way an answer can be wrong is said, all at once, and no row is kept.
func TestABadAnswerIsRefusedForEveryReason(t *testing.T) {
	cards := rowCards("a", "b", "c", "d")
	cards[3].TodoImportant = true
	long := strings.Repeat("é", TodoRowWhyMax)
	rows, refused := judgeTodoRows(cards, rowAnswer(t,
		TodoRankRow{Bead: "a", Row: "now", Why: "One"},
		TodoRankRow{Bead: "a", Row: "now", Why: "Twice"},
		TodoRankRow{Bead: "b", Row: "later", Why: "  "},
		TodoRankRow{Bead: "c", Row: "later", Why: long},
		TodoRankRow{Bead: "d", Row: "let-go", Why: "Old"},
		TodoRankRow{Bead: "z", Row: "later", Why: "Who?"}))
	if rows != nil {
		t.Fatalf("expected no rows from a refused answer, got %+v", rows)
	}
	said := strings.Join(refused, "\n")
	for _, want := range []string{
		"a is ranked twice", "b has no why", "the why of c is 80 characters, it must be under 80",
		"d is starred", "z is not a card it was given",
	} {
		if !strings.Contains(said, want) {
			t.Errorf("expected the refusal to say %q, it says:\n%s", want, said)
		}
	}
}

func TestAnAnswerThatIsNotTheSchemaIsRefused(t *testing.T) {
	for _, answer := range []string{``, `null`, `[]`, `{"cards":"no"}`, `not json`} {
		if _, refused := judgeTodoRows(rowCards("a"), json.RawMessage(answer)); len(refused) == 0 {
			t.Errorf("expected %q refused", answer)
		}
	}
}

func TestMoreThanThreeInMakeTimeIsRefused(t *testing.T) {
	_, refused := judgeTodoRows(rowCards("a", "b", "c", "d"), rowAnswer(t,
		TodoRankRow{Bead: "a", Row: "make-time", Why: "x"}, TodoRankRow{Bead: "b", Row: "make-time", Why: "x"},
		TodoRankRow{Bead: "c", Row: "make-time", Why: "x"}, TodoRankRow{Bead: "d", Row: "make-time", Why: "x"}))
	if !strings.Contains(strings.Join(refused, "\n"), "4 cards in make-time, at most 3") {
		t.Fatalf("expected four in make-time refused, got %v", refused)
	}
}

// The schema the model answers in names the four rows, and its why's limit is
// the one the answer is judged by.
func TestTheSchemaNamesTheFourRows(t *testing.T) {
	var schema struct {
		Properties struct {
			Cards struct {
				Items struct {
					Properties struct {
						Row struct {
							Enum []string `json:"enum"`
						} `json:"row"`
						Why struct {
							MaxLength int `json:"maxLength"`
						} `json:"why"`
					} `json:"properties"`
				} `json:"items"`
			} `json:"cards"`
		} `json:"properties"`
	}
	if err := json.Unmarshal([]byte(TodoRankSchema), &schema); err != nil {
		t.Fatalf("the schema is not JSON: %v", err)
	}
	item := schema.Properties.Cards.Items.Properties
	if strings.Join(item.Row.Enum, ",") != strings.Join(TodoRows, ",") || item.Why.MaxLength != TodoRowWhyMax-1 {
		t.Fatalf("expected the rows %v and a why under %d, got %+v", TodoRows, TodoRowWhyMax, item)
	}
}

// The message carries the cards' facts in his zone, and the system prompt is
// the same bytes whatever the board holds, so the harness's prompt cache can
// serve one night's pass from the last's.
func TestTheMessageCarriesTheBoardAndTheSystemPromptDoesNot(t *testing.T) {
	zone := time.FixedZone("EDT", -4*3600)
	now := time.Date(2026, 10, 16, 13, 0, 0, 0, time.UTC)
	open := rowCards("mw-todo.1")
	open[0].TodoDue = "2026-10-17T13:00:00Z"
	open[0].TodoImportant = true
	open[0].TodoSize = "hour"
	open[0].Created = now.AddDate(0, 0, -12)
	open[0].Updated = now.AddDate(0, 0, -31)
	open[0].TodoRow = "later"
	done := rowCards("mw-done.1")
	done[0].Created = now.AddDate(0, 0, -9)
	done[0].ClosedAt = time.Date(2026, 10, 14, 23, 30, 0, 0, time.UTC) // Wednesday 19:30 his time
	message := todoRankMessage(now, zone, "Train days are for big things", open, done, map[string]string{})

	for _, want := range []string{
		`"bead": "mw-todo.1"`, `"starred": true`, `"due": "2026-10-17T09:00:00-04:00"`, `"days_to_due": 1`,
		`"size": "hour"`, `"days_waiting": 12`, `"days_untouched": 31`, `"row": "later"`,
		`"closed": "2026-10-14 19:30"`, `"weekday": "Wednesday"`, `"days_open": 7`,
		"Train days are for big things", "Friday 2026-10-16",
	} {
		if !strings.Contains(message, want) {
			t.Errorf("expected the message to hold %s:\n%s", want, message)
		}
	}
	if a, b := todoRankSystem("Rules."), todoRankSystem("Rules."); a != b || strings.Contains(a, "mw-todo") {
		t.Fatalf("the system prompt is not fixed:\n%s", a)
	}
}
