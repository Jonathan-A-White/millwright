package application

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// The To do board's hand edits (postern's docs/protocol.md section 13), for
// when he would rather change a card with a thumb than talk to the Mayor. A
// card is an open bead labelled hitl:todo, a list the epic it is filed under
// when that carries hitl:todo too. All three refuse a bead without it.

// todoLabels are the labels a card is filed with: hitl keeps every dispatcher
// off it and hitl:todo makes it an item on a list rather than a need.
var todoLabels = []string{LabelHitl, LabelHitlTodo}

// isToDoList reports whether bead is a list on his board: an epic labelled
// hitl:todo, still open.
func isToDoList(bead StoryDetail) bool {
	return bead.IsEpic && bead.Todo() && !bead.Closed()
}

// refuseNotACard is why a todo_edit or todo_move on bead is refused, or "" when
// bead is a card: an open item on a list.
func refuseNotACard(bead StoryDetail) string {
	switch {
	case bead.IsEpic || !bead.Todo():
		return "it is not an item on a to-do list (no hitl:todo label)"
	case bead.Closed():
		return "it is already closed"
	}
	return ""
}

// applyTodoAdd files a new open card, labelled hitl and hitl:todo, at the end
// of list: the bead the action names, which must be a to-do list. The comment
// saying so goes on the new card, and the Mayor is mailed its title.
func (i PosternInbox) applyTodoAdd(ctx context.Context, m PosternInboxMessage, action PosternAction, list StoryDetail,
	refuse func(string) (posternApplied, error), result posternApplied) (posternApplied, error) {
	if !isToDoList(list) {
		return refuse("it is not a to-do list (an open epic labelled hitl:todo)")
	}
	title := ""
	if action.Title != nil {
		title = strings.TrimSpace(*action.Title)
	}
	if title == "" {
		return refuse("it names no title")
	}
	story := NewStory{EpicID: list.Story.ID, Title: title, Priority: DefaultPriority, Labels: todoLabels}
	if action.Notes != nil {
		story.Description = strings.TrimSpace(*action.Notes)
	}
	id, err := i.Tracker.CreateStory(ctx, story)
	if err != nil {
		return refuse(fmt.Sprintf("filing the card failed: %v", err))
	}
	// CreateStory files held; a card is open the moment it is on the list.
	if err := i.Tracker.ReleaseStory(ctx, id); err != nil {
		return refuse(fmt.Sprintf("filed %s but opening it failed: %v", id, err))
	}
	comment := fmt.Sprintf("ADDED by the Governor via postern, txid %s, to %s", m.Txid, list.Story.ID)
	if err := i.Tracker.CommentOnStory(ctx, id, comment); err != nil {
		return posternApplied{}, fmt.Errorf("recording the Governor's todo_add on %s: %w", id, err)
	}
	result.Detail = "filed " + id
	return result, i.mail(ctx, "Added: "+title, fmt.Sprintf("%s: %s", comment, title))
}

// applyTodoEdit changes the title, the notes (the bead's description), or both
// of a card, as the action names them.
func (i PosternInbox) applyTodoEdit(ctx context.Context, action PosternAction, card StoryDetail,
	refuse func(string) (posternApplied, error), done func(subject, comment string) (posternApplied, error)) (posternApplied, error) {
	if why := refuseNotACard(card); why != "" {
		return refuse(why)
	}
	if action.Title == nil && action.Notes == nil {
		return refuse("it names neither a title nor notes")
	}
	edit := StoryEdit{}
	var changed []string
	if action.Title != nil {
		title := strings.TrimSpace(*action.Title)
		if title == "" {
			return refuse("its new title is blank")
		}
		edit.Title = &title
		changed = append(changed, "title")
	}
	if action.Notes != nil {
		notes := strings.TrimSpace(*action.Notes)
		edit.Description = &notes
		changed = append(changed, "notes")
	}
	if err := i.Tracker.EditStory(ctx, card.Story.ID, edit); err != nil {
		return refuse(err.Error())
	}
	return done(fmt.Sprintf("Edited: %s", card.Story.ID),
		fmt.Sprintf("EDITED by the Governor via postern: the %s changed", strings.Join(changed, " and ")))
}

// applyTodoMove puts a card in a place on a list: before or after another
// card of it, or — when it names neither — at the end of the list it names. A
// list other than the card's own files it under that one. Its todo_rank is
// set to fall between its new neighbours, and the cards of the list are
// numbered afresh only when no rank falls between them (todoRankFor).
func (i PosternInbox) applyTodoMove(ctx context.Context, action PosternAction, card StoryDetail,
	refuse func(string) (posternApplied, error), done func(subject, comment string) (posternApplied, error)) (posternApplied, error) {
	if why := refuseNotACard(card); why != "" {
		return refuse(why)
	}
	anchor := strings.TrimSpace(action.Before + action.After)
	if action.Before != "" && action.After != "" {
		return refuse("it names both a card to go before and one to go after")
	}
	target := strings.TrimSpace(action.List)
	if target == "" {
		target = card.EpicID
	}
	if anchor == "" && target == card.EpicID {
		return refuse("it names no place: a card to go before or after, or another list")
	}
	list, err := i.Tracker.ShowBeads(ctx, []string{target})
	if err != nil {
		return posternApplied{}, fmt.Errorf("reading %s for the Governor's todo_move: %w", target, err)
	}
	if len(list) == 0 || !isToDoList(list[0]) {
		return refuse(fmt.Sprintf("%s is not a to-do list (an open epic labelled hitl:todo)", target))
	}
	epic, err := i.Tracker.ShowEpic(ctx, target)
	if err != nil {
		return posternApplied{}, fmt.Errorf("reading %s's cards for the Governor's todo_move: %w", target, err)
	}
	var others []StoryDetail
	for _, child := range epic.Stories {
		if child.Story.ID != card.Story.ID && !child.IsEpic && child.Todo() && !child.Closed() {
			others = append(others, child)
		}
	}
	others = InTodoOrder(others)
	index := len(others)
	if anchor != "" {
		at := -1
		for n, other := range others {
			if other.Story.ID == anchor {
				at = n
			}
		}
		if at < 0 {
			return refuse(fmt.Sprintf("%s is not an open card on %s", anchor, target))
		}
		index = at
		if action.After != "" {
			index = at + 1
		}
	}
	rank, renumbers := todoRankFor(others, index)

	if target != card.EpicID {
		if err := i.Tracker.MoveStory(ctx, card.Story.ID, target); err != nil {
			return refuse(err.Error())
		}
	}
	ids := make([]string, 0, len(renumbers))
	for id := range renumbers {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if err := i.Tracker.SetStoryMetadata(ctx, id, map[string]string{TodoRankField: renumbers[id]}); err != nil {
			return refuse(fmt.Sprintf("numbering %s failed: %v", id, err))
		}
	}
	if err := i.Tracker.SetStoryMetadata(ctx, card.Story.ID, map[string]string{TodoRankField: rank}); err != nil {
		return refuse(err.Error())
	}
	return done(fmt.Sprintf("Moved: %s", card.Story.ID),
		fmt.Sprintf("MOVED by the Governor via postern: to %s, todo_rank %s", target, rank))
}
