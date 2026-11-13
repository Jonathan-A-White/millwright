package doctor_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/configfile"
	"github.com/Jonathan-A-White/millwright/infrastructure/doctor"
)

// pcHome points HOME at a temp dir whose config.toml holds text (no file at
// all when text is empty), and clears the environment override, so what the
// check reads is only what the test says.
func pcHome(t *testing.T, text string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(config.PosternChannelEnv, "")
	if text == "" {
		return
	}
	path := filepath.Join(home, config.File)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("making the config dir: %v", err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatalf("writing the config: %v", err)
	}
}

var (
	pcThisHost  = &apptest.FakeHomeFile{Text: "laptop 2026-09-29T00:10:00Z mw@laptop"}
	pcOtherHost = &apptest.FakeHomeFile{Text: "desktop 2026-09-29T00:10:00Z mw@desktop"}
)

func TestThePosternChannelProbeIsFaultyOnTheHomeWithTheDefaultChain(t *testing.T) {
	pcHome(t, "")
	check := &doctor.PosternChannel{Home: pcThisHost, Host: "laptop"}

	verdict, reason := check.Probe(context.Background())
	if verdict != application.DoctorFaulty {
		t.Fatalf("expected faulty, got %s (%s)", verdict, reason)
	}
	for _, want := range []string{"postern_channel", "chain", "config.toml", `postern_channel = "direct"`} {
		if !strings.Contains(reason, want) {
			t.Fatalf("expected the reason to name %q, got %q", want, reason)
		}
	}
}

func TestThePosternChannelProbeIsFaultyOnTheHomeWithChainSetInTheConfig(t *testing.T) {
	pcHome(t, "postern_channel = \"chain\"\n")
	check := &doctor.PosternChannel{Home: pcThisHost, Host: "laptop"}

	if verdict, reason := check.Probe(context.Background()); verdict != application.DoctorFaulty {
		t.Fatalf("expected faulty, got %s (%s)", verdict, reason)
	}
}

func TestThePosternChannelProbeIsOKOnTheHomeWithDirect(t *testing.T) {
	pcHome(t, "postern_channel = \"direct\"\n")
	check := &doctor.PosternChannel{Home: pcThisHost, Host: "laptop"}

	if verdict, reason := check.Probe(context.Background()); verdict != application.DoctorOK {
		t.Fatalf("expected ok, got %s (%s)", verdict, reason)
	}
}

func TestThePosternChannelProbeReadsTheEnvironmentAsDirect(t *testing.T) {
	pcHome(t, "")
	t.Setenv(config.PosternChannelEnv, "direct")
	check := &doctor.PosternChannel{Home: pcThisHost, Host: "laptop"}

	if verdict, reason := check.Probe(context.Background()); verdict != application.DoctorOK {
		t.Fatalf("expected %s=direct to count as direct, got %s (%s)", config.PosternChannelEnv, verdict, reason)
	}
}

func TestThePosternChannelProbeIsOKOnAHostThatIsNotHome(t *testing.T) {
	pcHome(t, "")
	check := &doctor.PosternChannel{Home: pcOtherHost, Host: "laptop"}

	if verdict, reason := check.Probe(context.Background()); verdict != application.DoctorOK {
		t.Fatalf("expected ok on a host that is not home, got %s (%s)", verdict, reason)
	}
}

func TestThePosternChannelProbeStillJudgesWhenTheHomeCannotBeTold(t *testing.T) {
	pcHome(t, "")
	for name, home := range map[string]application.HomeFile{
		"no home file": &apptest.FakeHomeFile{Missing: true},
		"no home read": nil,
	} {
		t.Run(name, func(t *testing.T) {
			check := &doctor.PosternChannel{Home: home, Host: "laptop"}
			if verdict, reason := check.Probe(context.Background()); verdict != application.DoctorFaulty {
				t.Fatalf("expected faulty, got %s (%s)", verdict, reason)
			}
		})
	}
}

func TestThePosternChannelProbeCannotTellWhenTheConfigIsRefused(t *testing.T) {
	pcHome(t, "postern_channel = \"smoke\"\n")
	check := &doctor.PosternChannel{Home: pcThisHost, Host: "laptop"}

	verdict, reason := check.Probe(context.Background())
	if verdict != application.DoctorCannotTell {
		t.Fatalf("expected cannot-tell, got %s (%s)", verdict, reason)
	}
	if !strings.Contains(reason, "smoke") {
		t.Fatalf("expected the reason to carry the refusal, got %q", reason)
	}
}

// pcCheck is the check on the home, with the config file under HOME as the
// file its cure edits.
func pcCheck(t *testing.T) *doctor.PosternChannel {
	t.Helper()
	path, err := config.FilePath()
	if err != nil {
		t.Fatalf("finding the config file: %v", err)
	}
	check := doctor.NewPosternChannel(pcThisHost, "laptop")
	check.Config = configfile.New(path)
	check.Out = &strings.Builder{}
	return check
}

func TestThePosternChannelCureAddsDirectToAConfigThatLacksTheKey(t *testing.T) {
	pcHome(t, "cap = 2\n")
	check := pcCheck(t)

	if verdict, reason := check.Probe(context.Background()); verdict != application.DoctorFaulty {
		t.Fatalf("expected faulty before the cure, got %s (%s)", verdict, reason)
	}
	if err := check.Cure(context.Background()); err != nil {
		t.Fatalf("expected the cure to work, got %v", err)
	}
	path, _ := config.FilePath()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the config: %v", err)
	}
	if !strings.Contains(string(data), `postern_channel = "direct"`) {
		t.Fatalf("expected the config to hold postern_channel = \"direct\", got %q", data)
	}
	if !strings.Contains(string(data), "cap = 2") {
		t.Fatalf("expected the cure to leave the other lines alone, got %q", data)
	}
	if verdict, reason := check.Probe(context.Background()); verdict != application.DoctorOK {
		t.Fatalf("expected ok after the cure, got %s (%s)", verdict, reason)
	}
}

func TestThePosternChannelCureMakesTheConfigWhenThereIsNone(t *testing.T) {
	pcHome(t, "")
	check := pcCheck(t)

	if err := check.Cure(context.Background()); err != nil {
		t.Fatalf("expected the cure to work, got %v", err)
	}
	if verdict, reason := check.Probe(context.Background()); verdict != application.DoctorOK {
		t.Fatalf("expected ok after the cure, got %s (%s)", verdict, reason)
	}
}

func TestThePosternChannelCureSetsDirectOverChain(t *testing.T) {
	pcHome(t, "postern_channel = \"chain\"\n")
	check := pcCheck(t)

	if err := check.Cure(context.Background()); err != nil {
		t.Fatalf("expected the cure to work, got %v", err)
	}
	if verdict, reason := check.Probe(context.Background()); verdict != application.DoctorOK {
		t.Fatalf("expected ok after the cure, got %s (%s)", verdict, reason)
	}
}

func TestThePosternChannelWithNoConfigFileToEditHasNoCure(t *testing.T) {
	check := doctor.NewPosternChannel(pcThisHost, "laptop")

	if check.Name() != "postern-channel" {
		t.Fatalf("expected the name postern-channel, got %q", check.Name())
	}
	if err := check.Cure(context.Background()); err == nil || !strings.Contains(err.Error(), "no cure") {
		t.Fatalf("expected a no-cure error, got %v", err)
	}
}

func TestThePosternChannelWayBackAndDamper(t *testing.T) {
	check := pcCheck(t)

	if !strings.Contains(check.WayBack(), "config.toml") {
		t.Fatalf("expected the way back to name the config file, got %q", check.WayBack())
	}
	if _, cap := check.Damper(); cap != 1 {
		t.Fatalf("expected a cap of 1, got %d", cap)
	}
}
