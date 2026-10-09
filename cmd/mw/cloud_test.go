package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/hostlock"
)

func TestHomeSpringHasACloudJobOnlyWhereTheConfigHasACloudTable(t *testing.T) {
	defer func(was userUnits) { springUnits = was }(springUnits)
	springUnits = &fakeUnits{installed: map[string]bool{}}
	for _, c := range []struct {
		name, config string
		want         bool
	}{
		{"no [cloud] table", "host = \"desktop\"\n", false},
		{"a [cloud] table", "host = \"desktop\"\n\n[cloud]\ncommand = \"/opt/millwright/contrib/vultr-boost\"\n", true},
	} {
		t.Run(c.name, func(t *testing.T) {
			mwConfig(t, c.config)
			spring, err := homeSpring(t.TempDir()+"/log.jsonl", "desktop", &bytes.Buffer{})
			if err != nil {
				t.Fatal(err)
			}
			has := false
			for _, j := range spring.Jobs {
				if j.Name == CloudJobName {
					has = true
					if j.Every != cloudEvery || j.Wants != nil {
						t.Fatalf("the cloud job is %+v: it runs on the clock alone, every minute", j)
					}
				}
			}
			if has != c.want {
				t.Fatalf("a cloud job: %v, want %v", has, c.want)
			}
		})
	}
}

func TestCloudCheckDoesNothingWithoutACloudTable(t *testing.T) {
	mwConfig(t, "host = \"desktop\"\n")
	_, ok, err := cloudCheck(context.Background(), &bytes.Buffer{})
	if err != nil || ok {
		t.Fatalf("expected no check without a [cloud] table, got %v, %v", ok, err)
	}
	cmd := newCloudCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"check"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "nothing to do") {
		t.Fatalf("expected mw cloud check to say there is nothing to do, got %q", out.String())
	}
}

func TestACloudCheckFindingTheLockHeldDoesNothing(t *testing.T) {
	mwConfig(t, "host = \"desktop\"\n")
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	release, taken, err := hostlock.NewTry(filepath.Join(home, DispatchStateDir), cloudLockFile).TryTake(context.Background())
	if err != nil || !taken {
		t.Fatalf("taking the cloud lock: %v, %v", taken, err)
	}
	defer release()
	// A check with nothing in it would fail if it ran.
	_, ran, err := runCloudCheck(context.Background(), application.CloudCheck{})
	if ran || err != nil {
		t.Fatalf("expected a check to find the lock held and do nothing, got ran %v, %v", ran, err)
	}
}
