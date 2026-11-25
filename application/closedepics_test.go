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
