package application_test

import (
	"testing"

	. "github.com/Jonathan-A-White/millwright/application"
)

func TestAnOpenTodoBeadIsALoneStoryUnderAnyParent(t *testing.T) {
	todo := StoryDetail{Status: StatusOpen, Labels: []string{LabelHitl, LabelHitlTodo}}
	if !IsLoneStory(todo, "mw-list", "") {
		t.Error("an open hitl:todo bead under an epic should be a lone story")
	}
	closed := StoryDetail{Status: StatusClosed, Labels: []string{LabelHitl, LabelHitlTodo}}
	if IsLoneStory(closed, "mw-list", "") {
		t.Error("a closed hitl:todo bead under an epic should not be a lone story")
	}
	plain := StoryDetail{Status: StatusOpen, Labels: []string{LabelHitl}}
	if IsLoneStory(plain, "mw-list", "millwright") {
		t.Error("a story under an epic should not be a lone story")
	}
	if !IsLoneStory(plain, "", "millwright") {
		t.Error("a story with no parent that names a rig should be a lone story")
	}
	if IsLoneStory(plain, "", "") {
		t.Error("a bead with no parent and no rig should not be a lone story")
	}
}
