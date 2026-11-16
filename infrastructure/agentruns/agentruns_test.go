package agentruns_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/agentruns"
)

func TestARunBegunIsLiveWhileItsProcessIsThereAndGoneWhenItIsNot(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "agent-runs")
	alive := true
	marks := agentruns.Marks{Dir: dir, Alive: func(int) bool { return alive }}
	ctx := context.Background()
	run := application.AgentRun{Job: "sleep", Host: "laptop", Began: time.Date(2026, 11, 15, 7, 34, 50, 0, time.UTC)}
	if err := marks.Begin(ctx, run); err != nil {
		t.Fatal(err)
	}
	live, gone, err := marks.Open(ctx)
	if err != nil || len(live) != 1 || len(gone) != 0 || live[0].Job != "sleep" || live[0].Pid != os.Getpid() || !live[0].Began.Equal(run.Began) {
		t.Fatalf("live %v gone %v err %v", live, gone, err)
	}
	alive = false
	if live, gone, _ = marks.Open(ctx); len(live) != 0 || len(gone) != 1 {
		t.Fatalf("a mark of a process that is not there: live %v gone %v", live, gone)
	}
	if err := marks.Forget(ctx, run); err != nil {
		t.Fatal(err)
	}
	if live, gone, _ = marks.Open(ctx); len(live)+len(gone) != 0 {
		t.Fatalf("a forgotten mark is left: %v %v", live, gone)
	}
}

func TestEndTakesTheMarkAwayAndNoDirectoryIsNoMarks(t *testing.T) {
	marks := agentruns.New(filepath.Join(t.TempDir(), "none"))
	ctx := context.Background()
	if live, gone, err := marks.Open(ctx); err != nil || live != nil || gone != nil {
		t.Fatalf("no directory: %v %v %v", live, gone, err)
	}
	run := application.AgentRun{Job: "todo-rank", Host: "laptop"}
	if err := marks.Begin(ctx, run); err != nil {
		t.Fatal(err)
	}
	if err := marks.End(ctx, run); err != nil {
		t.Fatal(err)
	}
	if err := marks.End(ctx, run); err != nil {
		t.Fatalf("ending a run twice: %v", err)
	}
	if live, gone, _ := marks.Open(ctx); len(live)+len(gone) != 0 {
		t.Fatalf("an ended run is left: %v %v", live, gone)
	}
}

func TestThisProcessIsAliveToTheRealCheck(t *testing.T) {
	marks := agentruns.New(t.TempDir())
	ctx := context.Background()
	if err := marks.Begin(ctx, application.AgentRun{Job: "grist", Host: "laptop"}); err != nil {
		t.Fatal(err)
	}
	if live, _, _ := marks.Open(ctx); len(live) != 1 {
		t.Fatalf("a run of this very process is not live: %v", live)
	}
	if err := marks.Begin(ctx, application.AgentRun{Job: "../x", Host: "laptop"}); err == nil {
		t.Fatal("a job name that climbs out of the directory was taken")
	}
}
