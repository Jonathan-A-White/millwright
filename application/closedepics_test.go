package application_test

import (
	"context"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// mw-gq6.1720: open children of a closed epic drop out of mw brief and the
// weighing by vision, so mw doctor names each such epic.
func TestDoctorNamesAClosedEpicWithOpenChildren(t *testing.T) {
	ctx := context.Background()
	tracker := apptest.NewFakeTracker()
	lampas := domain.Path{Rig: "lampas", Branch: "main"}
	tracker.AddEpic("mw-old", lampas)
	tracker.AddEpic("mw-live", lampas)
	tracker.AddEpic("mw-done", lampas)
	tracker.AddStory("mw-old", domain.Story{ID: "mw-old.1", Title: "A bug"})
	tracker.AddStory("mw-old", domain.Story{ID: "mw-old.2", Title: "Another bug"})
	tracker.AddStory("mw-old", domain.Story{ID: "mw-old.3", Title: "Finished"})
	mustDo(t, tracker.SetStatus("mw-old.3", apptest.StatusClosed))
	tracker.AddStory("mw-live", domain.Story{ID: "mw-live.1", Title: "Live work"})
	tracker.AddStory("mw-done", domain.Story{ID: "mw-done.1", Title: "Finished"})
	mustDo(t, tracker.SetStatus("mw-done.1", apptest.StatusClosed))
	tracker.ShutEpic("mw-old", "first demo accepted, open work moved to mw-live")
	tracker.ShutEpic("mw-done", "done")
	mailbox := apptest.NewFakeMailbox()
	check := &application.ClosedEpicsCheck{
		Host: "laptop", Stories: tracker, Tracker: tracker,
		Mail: func(ctx context.Context, message application.NewMessage) error {
			_, err := mailbox.Send(ctx, message)
			return err
		},
	}

	verdict, why := check.Probe(ctx)

	if verdict != application.DoctorFaulty {
		t.Fatalf("expected faulty, got %v: %s", verdict, why)
	}
	if !strings.Contains(why, "mw-old (2 open children)") || !strings.Contains(why, "moved to mw-live") {
		t.Errorf("expected the closed epic named with its open count and reason, got %q", why)
	}
	if strings.Contains(why, "mw-done") || strings.Contains(why, "mw-live ") {
		t.Errorf("expected only the closed epic with open children named, got %q", why)
	}
	if err := check.Cure(ctx); err != nil {
		t.Fatalf("curing: %v", err)
	}
	mail, _ := mailbox.Inbox(ctx, application.MayorMailbox)
	if len(mail) != 1 || !strings.Contains(mail[0].Body, "mw-old (2 open children)") {
		t.Errorf("expected one mail to the Mayor naming mw-old, got %+v", mail)
	}

	mustDo(t, tracker.SetStatus("mw-old.1", apptest.StatusClosed))
	mustDo(t, tracker.SetStatus("mw-old.2", apptest.StatusClosed))
	if verdict, why := check.Probe(ctx); verdict != application.DoctorOK {
		t.Errorf("expected ok once its children are closed, got %v: %s", verdict, why)
	}
}

// mw-gq6.1725: a closed molecule (a formula run) is not an epic: mw closes its
// steps, the Mayor never moves them, so a step still open at the read is not
// work lost under a closed epic.
func TestAClosedMoleculeIsNotAClosedEpicWithOpenChildren(t *testing.T) {
	ctx := context.Background()
	tracker := apptest.NewFakeTracker()
	lampas := domain.Path{Rig: "lampas", Branch: "main"}
	tracker.AddEpic("mw-old", lampas)
	tracker.AddStory("mw-old", domain.Story{ID: "mw-old.1", Title: "A bug"})
	tracker.ShutEpic("mw-old", "moved on")
	for _, id := range []string{"mw-mol-whole", "mw-mol-step"} {
		tracker.AddStory("", domain.Story{ID: id, Title: "A formula run"})
		mustDo(t, tracker.SetType(id, "molecule"))
		mustDo(t, tracker.SetStatus(id, apptest.StatusClosed))
	}
	tracker.AddStory("mw-mol-whole", domain.Story{ID: "mw-mol-whole.1", Title: "Understand"})
	mustDo(t, tracker.SetStatus("mw-mol-whole.1", apptest.StatusClosed))
	tracker.AddStory("mw-mol-step", domain.Story{ID: "mw-mol-step.1", Title: "Understand"})
	tracker.AddStory("mw-mol-step", domain.Story{ID: "mw-mol-step.2", Title: "Commit"})
	mustDo(t, tracker.SetStatus("mw-mol-step.1", apptest.StatusClosed))

	found, err := application.ClosedEpicsWithOpenChildren(ctx, tracker, tracker)
	if err != nil {
		t.Fatalf("reading: %v", err)
	}

	if len(found) != 1 || found[0].ID != "mw-old" {
		t.Errorf("expected only the closed epic mw-old named, got %+v", found)
	}
}
