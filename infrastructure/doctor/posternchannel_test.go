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

func TestThePosternChannelHasNoCure(t *testing.T) {
	check := doctor.NewPosternChannel(pcThisHost, "laptop")

	if check.Name() != "postern-channel" {
		t.Fatalf("expected the name postern-channel, got %q", check.Name())
	}
	if err := check.Cure(context.Background()); err == nil || !strings.Contains(err.Error(), "no cure") {
		t.Fatalf("expected a no-cure error, got %v", err)
	}
	if !strings.Contains(check.WayBack(), "config.toml") {
		t.Fatalf("expected the way back to say a person edits the file, got %q", check.WayBack())
	}
	if _, cap := check.Damper(); cap != 1 {
		t.Fatalf("expected a cap of 1, got %d", cap)
	}
}
