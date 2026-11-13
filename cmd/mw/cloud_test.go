package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
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

// The plan the check reads carries retired_hosts, so a retired host's line in
// [cloud.caps] counts no free sessions (mw-gq6.1111).
func TestCloudPlanCarriesTheRetiredHostsOfTheConfig(t *testing.T) {
	mwConfig(t, "host = \"laptop\"\nretired_hosts = [\"desktop\"]\n\n[cloud]\ncommand = \"/opt/millwright/contrib/vultr-boost\"\nmax_boxes = 2\n\n[cloud.caps]\nlaptop = 2\ndesktop = 6\n")
	settings, ok, err := config.Cloud()
	if err != nil || !ok {
		t.Fatalf("expected a [cloud] table, got %v, %v", ok, err)
	}
	plan, err := cloudPlan(settings, "laptop")
	if err != nil {
		t.Fatal(err)
	}
	if !application.IsRetired(plan.Retired, "desktop") || application.IsRetired(plan.Retired, "laptop") {
		t.Errorf("expected the plan to retire desktop alone, got %v", plan.Retired)
	}
	if plan.HostCaps["desktop"] != 6 {
		t.Errorf("expected desktop's line kept in the plan, got %v", plan.HostCaps)
	}
}

// The plan the check reads carries the [cloud] prefix, so the boxes a home
// names and gates are the ones its config says; with no prefix key they are
// still cloud1, cloud2 (mw-gq6.1250).
func TestCloudPlanCarriesThePrefixOfTheConfig(t *testing.T) {
	mwConfig(t, "host = \"laptop\"\n\n[cloud]\ncommand = \"/opt/millwright/contrib/vultr-boost\"\nprefix = \"mill\"\n")
	settings, ok, err := config.Cloud()
	if err != nil || !ok {
		t.Fatalf("expected a [cloud] table, got %v, %v", ok, err)
	}
	plan, err := cloudPlan(settings, "laptop")
	if err != nil {
		t.Fatal(err)
	}
	if plan.Prefix != "mill" || !plan.IsBox("mill3") || plan.IsBox("cloud3") {
		t.Errorf("expected the plan to name boxes mill1, mill2, ..., got prefix %q", plan.Prefix)
	}

	mwConfig(t, "host = \"laptop\"\n\n[cloud]\ncommand = \"/opt/millwright/contrib/vultr-boost\"\n")
	settings, _, err = config.Cloud()
	if err != nil {
		t.Fatal(err)
	}
	plan, err = cloudPlan(settings, "laptop")
	if err != nil {
		t.Fatal(err)
	}
	if !plan.IsBox("cloud3") || plan.IsBox("mill3") {
		t.Errorf("expected a [cloud] with no prefix to name boxes cloud1, cloud2, ..., got prefix %q", plan.Prefix)
	}
}

// A prefix a box's number cannot follow is refused, saying which key.
func TestACloudPrefixThatIsNotLettersIsRefused(t *testing.T) {
	mwConfig(t, "host = \"laptop\"\n\n[cloud]\ncommand = \"/opt/millwright/contrib/vultr-boost\"\nprefix = \"Mill 2\"\n")
	if _, _, err := config.Cloud(); err == nil || !strings.Contains(err.Error(), "prefix") {
		t.Fatalf("expected a refusal naming prefix, got %v", err)
	}
}
