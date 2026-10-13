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
