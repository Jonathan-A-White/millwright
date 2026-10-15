package steps

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/cucumber/godog"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain"
)

// registerPosternTodoSteps wires the steps of the Governor's To do board
// actions: todo_add, todo_edit and todo_move (postern_inbox_apply.feature).
func registerPosternTodoSteps(ctx *godog.ScenarioContext, c *posternInboxContext) {
	ctx.Given(`^the epic "([^"]*)" is a to-do list$`, c.theEpicIsAToDoList)
	ctx.Given(`^the to-do list "([^"]*)" has the ranked cards (.*)$`, c.theToDoListHasTheRankedCards)
	ctx.Given(`^the to-do list "([^"]*)" has the unranked cards (.*)$`, c.theToDoListHasTheUnrankedCards)
	ctx.Given(`^the Governor sends the postern action (\{.*\}) with txid "([^"]*)"$`, c.theGovernorSendsThePosternAction)
	ctx.Then(`^the to-do list "([^"]*)" holds an open card titled "([^"]*)" labelled "([^"]*)" with the notes "([^"]*)"$`,
		c.theToDoListHoldsAnOpenCard)
	ctx.Then(`^the tracker took only the mark that the tap was applied$`, c.nothingWasWrittenToTheTracker)
	ctx.Then(`^bead "([^"]*)" now has the title "([^"]*)"$`, c.beadNowHasTheTitle)
	ctx.Then(`^bead "([^"]*)" now has the notes "([^"]*)"$`, c.beadNowHasTheNotes)
	ctx.Then(`^bead "([^"]*)" now stands under "([^"]*)"$`, c.beadNowStandsUnder)
	ctx.Then(`^bead "([^"]*)" has the todo_rank "([^"]*)"$`, c.beadHasTheTodoRank)
	ctx.Then(`^bead "([^"]*)" has no todo_rank$`, c.beadHasNoTodoRank)
	ctx.Given(`^bead "([^"]*)" is due "([^"]*)"$`, c.beadIsDue)
	ctx.Then(`^bead "([^"]*)" has the todo_due "([^"]*)"$`, c.beadHasTheTodoDue)
	ctx.Then(`^bead "([^"]*)" has no todo_due$`, func(id string) error { return c.beadHasTheTodoDue(id, "") })
	ctx.Given(`^bead "([^"]*)" is important$`, c.beadIsImportant)
	ctx.Given(`^bead "([^"]*)" is sized "([^"]*)" with the Then line "([^"]*)"$`, c.beadIsSizedWithThen)
	ctx.Given(`^bead "([^"]*)" has the Then line "([^"]*)"$`, c.beadHasTheThenLine)
	ctx.Given(`^bead "([^"]*)" has the Open: line "([^"]*)"$`, c.beadHasTheOpenLine)
	ctx.Given(`^the Governor sends a todo_edit tap on "([^"]*)" with a then of (\d+) characters and txid "([^"]*)"$`, c.theGovernorSendsALongThen)
	ctx.Then(`^bead "([^"]*)" has the todo_size "([^"]*)"$`, c.beadHasTheTodoSize)
	ctx.Then(`^bead "([^"]*)" has no todo_size$`, func(id string) error { return c.beadHasTheTodoSize(id, "") })
	ctx.Then(`^bead "([^"]*)" has the todo_then "([^"]*)"$`, c.beadHasTheTodoThen)
	ctx.Then(`^bead "([^"]*)" has no todo_then$`, func(id string) error { return c.beadHasTheTodoThen(id, "") })
	ctx.Then(`^the to-do list "([^"]*)" holds (\d+) open cards? titled "([^"]*)" labelled "([^"]*)"$`, c.theToDoListHoldsNCards)
	ctx.Then(`^bead "([^"]*)" is described by the line "([^"]*)"$`, func(id, line string) error { return c.beadIsDescribedBy(id, line) })
	ctx.Then(`^bead "([^"]*)" is described by the lines "([^"]*)" and "([^"]*)"$`, func(id, a, b string) error { return c.beadIsDescribedBy(id, a, b) })
	ctx.Then(`^bead "([^"]*)" does not exist$`, c.beadDoesNotExist)
	ctx.Then(`^bead "([^"]*)" has the todo_important "([^"]*)"$`, c.beadHasTheTodoImportant)
	ctx.Then(`^bead "([^"]*)" has no todo_important$`, func(id string) error { return c.beadHasTheTodoImportant(id, "") })
}

func (c *posternInboxContext) theEpicIsAToDoList(id string) error {
	c.memory.AddEpic(id, domain.Path{})
	for _, label := range []string{application.LabelHitl, application.LabelHitlTodo} {
		if err := c.memory.AddLabel(context.Background(), id, label); err != nil {
			return err
		}
	}
	return nil
}

func (c *posternInboxContext) fileToDoCard(list, id string) error {
	c.memory.AddStory(list, domain.Story{ID: id, Title: "Story " + id})
	return c.memory.SetLabels(id, application.LabelHitl, application.LabelHitlTodo)
}

var rankedCard = regexp.MustCompile(`"([^"]*)" at ([0-9.]+)`)

func (c *posternInboxContext) theToDoListHasTheRankedCards(list, cards string) error {
	if err := c.theEpicIsAToDoList(list); err != nil {
		return err
	}
	for _, m := range rankedCard.FindAllStringSubmatch(cards, -1) {
		if err := c.fileToDoCard(list, m[1]); err != nil {
			return err
		}
		if err := c.memory.SetStoryMetadata(context.Background(), m[1], map[string]string{application.TodoRankField: m[2]}); err != nil {
			return err
		}
	}
	return nil
}

func (c *posternInboxContext) theToDoListHasTheUnrankedCards(list, cards string) error {
	if err := c.theEpicIsAToDoList(list); err != nil {
		return err
	}
	for _, id := range regexp.MustCompile(`"([^"]*)"`).FindAllStringSubmatch(cards, -1) {
		if err := c.fileToDoCard(list, id[1]); err != nil {
			return err
		}
	}
	return nil
}

func (c *posternInboxContext) theGovernorSendsThePosternAction(text, txid string) error {
	return c.addActionText(c.governorKey, txid, text)
}

func (c *posternInboxContext) theToDoListHoldsAnOpenCard(list, title, labels, notes string) error {
	if err := c.itSucceeds(); err != nil {
		return err
	}
	epic, err := c.memory.ShowEpic(context.Background(), list)
	if err != nil {
		return err
	}
	for _, card := range epic.Stories {
		if card.Story.Title != title {
			continue
		}
		if card.Status != application.StatusOpen {
			return fmt.Errorf("expected the card %q to be open, it stands %s", title, card.Status)
		}
		if got := strings.Join(card.Labels, ", "); got != labels {
			return fmt.Errorf("expected the card %q labelled %q, got %q", title, labels, got)
		}
		if card.Description != notes {
			return fmt.Errorf("expected the card %q to have the notes %q, got %q", title, notes, card.Description)
		}
		return nil
	}
	return fmt.Errorf("expected %s to hold a card titled %q, it holds %+v", list, title, epic.Stories)
}

// nothingWasWrittenToTheTracker checks the pass wrote no bead: its one write
// is the note that marks the txid applied.
func (c *posternInboxContext) nothingWasWrittenToTheTracker() error {
	if wrote := c.memory.Writes() - c.writesBefore; wrote != 1 {
		return fmt.Errorf("expected only the txid's mark written to the tracker, it took %d writes", wrote)
	}
	return nil
}

func (c *posternInboxContext) bead(id string) (application.StoryDetail, error) {
	found, err := c.memory.ShowBeads(context.Background(), []string{id})
	if err != nil {
		return application.StoryDetail{}, err
	}
	if len(found) == 0 {
		return application.StoryDetail{}, fmt.Errorf("no bead %q", id)
	}
	return found[0], nil
}

func (c *posternInboxContext) beadNowHasTheTitle(id, title string) error {
	bead, err := c.bead(id)
	if err != nil {
		return err
	}
	if bead.Story.Title != title {
		return fmt.Errorf("expected %s titled %q, got %q", id, title, bead.Story.Title)
	}
	return nil
}

func (c *posternInboxContext) beadNowHasTheNotes(id, notes string) error {
	bead, err := c.bead(id)
	if err != nil {
		return err
	}
	if bead.Description != notes {
		return fmt.Errorf("expected %s to have the notes %q, got %q", id, notes, bead.Description)
	}
	return nil
}

func (c *posternInboxContext) beadNowStandsUnder(id, parent string) error {
	bead, err := c.bead(id)
	if err != nil {
		return err
	}
	if bead.EpicID != parent {
		return fmt.Errorf("expected %s under %s, got %q", id, parent, bead.EpicID)
	}
	return nil
}

func (c *posternInboxContext) beadHasTheTodoRank(id, rank string) error {
	if got := c.memory.Metadata(id)[application.TodoRankField]; got != rank {
		return fmt.Errorf("expected %s at todo_rank %q, got %q", id, rank, got)
	}
	return nil
}

func (c *posternInboxContext) beadHasNoTodoRank(id string) error {
	if got, set := c.memory.Metadata(id)[application.TodoRankField]; set {
		return fmt.Errorf("expected %s to have no todo_rank, got %q", id, got)
	}
	return nil
}

func (c *posternInboxContext) beadIsDue(id, due string) error {
	return c.memory.SetStoryMetadata(context.Background(), id, map[string]string{application.TodoDueField: due})
}

func (c *posternInboxContext) beadHasTheTodoDue(id, due string) error {
	if got := c.memory.Metadata(id)[application.TodoDueField]; got != due {
		return fmt.Errorf("expected %s at todo_due %q, got %q", id, due, got)
	}
	return nil
}

func (c *posternInboxContext) beadIsImportant(id string) error {
	return c.memory.SetStoryMetadata(context.Background(), id, map[string]string{application.TodoImportantField: "true"})
}

func (c *posternInboxContext) beadHasTheTodoImportant(id, important string) error {
	if got := c.memory.Metadata(id)[application.TodoImportantField]; got != important {
		return fmt.Errorf("expected %s at todo_important %q, got %q", id, important, got)
	}
	return nil
}

func (c *posternInboxContext) beadIsSizedWithThen(id, size, then string) error {
	return c.memory.SetStoryMetadata(context.Background(), id,
		map[string]string{application.TodoSizeField: size, application.TodoThenField: then})
}

func (c *posternInboxContext) beadHasTheThenLine(id, then string) error {
	return c.memory.SetStoryMetadata(context.Background(), id, map[string]string{application.TodoThenField: then})
}

func (c *posternInboxContext) beadHasTheOpenLine(id, target string) error {
	bead, err := c.bead(id)
	if err != nil {
		return err
	}
	return c.memory.SetDescription(id, strings.TrimSpace(bead.Description+"\nOpen: "+target))
}

func (c *posternInboxContext) theGovernorSendsALongThen(id string, n int, txid string) error {
	return c.addActionText(c.governorKey, txid,
		fmt.Sprintf(`{"action":"todo_edit","bead":%q,"then":%q}`, id, strings.Repeat("x", n)))
}

func (c *posternInboxContext) beadHasTheTodoSize(id, size string) error {
	if got := c.memory.Metadata(id)[application.TodoSizeField]; got != size {
		return fmt.Errorf("expected %s at todo_size %q, got %q", id, size, got)
	}
	return nil
}

func (c *posternInboxContext) beadHasTheTodoThen(id, then string) error {
	if got := c.memory.Metadata(id)[application.TodoThenField]; got != then {
		return fmt.Errorf("expected %s at todo_then %q, got %q", id, then, got)
	}
	return nil
}

// theToDoListHoldsNCards checks list holds exactly n open cards titled title,
// each labelled labels.
func (c *posternInboxContext) theToDoListHoldsNCards(list string, n int, title, labels string) error {
	epic, err := c.memory.ShowEpic(context.Background(), list)
	if err != nil {
		return err
	}
	found := 0
	for _, card := range epic.Stories {
		if card.Story.Title != title || card.Status != application.StatusOpen {
			continue
		}
		if got := strings.Join(card.Labels, ", "); got != labels {
			return fmt.Errorf("expected the card %q labelled %q, got %q", title, labels, got)
		}
		found++
	}
	if found != n {
		return fmt.Errorf("expected %s to hold %d open cards titled %q, it holds %d: %+v", list, n, title, found, epic.Stories)
	}
	return nil
}

func (c *posternInboxContext) beadIsDescribedBy(id string, lines ...string) error {
	bead, err := c.bead(id)
	if err != nil {
		return err
	}
	if want := strings.Join(lines, "\n"); bead.Description != want {
		return fmt.Errorf("expected %s described as %q, got %q", id, want, bead.Description)
	}
	return nil
}

func (c *posternInboxContext) beadDoesNotExist(id string) error {
	found, err := c.memory.ShowBeads(context.Background(), []string{id})
	if err == nil && len(found) > 0 {
		return fmt.Errorf("expected no bead %s, found %+v", id, found[0])
	}
	return nil
}
