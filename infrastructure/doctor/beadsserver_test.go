package doctor_test

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/doctor"
)

// dialing stands in for the TCP dial, answering as told and recording what
// it was asked to reach.
func dialing(answers bool, asked *string) func(context.Context, string) bool {
	return func(_ context.Context, address string) bool {
		*asked = address
		return answers
	}
}

func TestBeadsServerIsOKAndInertOnAHostThatKeepsItsOwnDatabase(t *testing.T) {
	for name, check := range map[string]*doctor.BeadsServer{
		"remote":                 {Mode: "remote", Address: "10.88.0.3:3307"},
		"backup with no address": {Mode: "backup"},
		"remote with no address": {Mode: "remote"},
	} {
		asked := ""
		check.Dial = dialing(false, &asked)

		verdict, reason := check.Probe(context.Background())
		if verdict != application.DoctorOK || !strings.Contains(reason, check.Mode) {
			t.Errorf("%s: expected ok, saying why there is nothing to reach, got %s (%s)", name, verdict, reason)
		}
		if asked != "" {
			t.Errorf("%s: expected no dial at all, got one to %s", name, asked)
		}
	}
}

func TestBeadsServerIsOKWhenTheHomesOwnDatabaseAnswers(t *testing.T) {
	asked := ""
	check := &doctor.BeadsServer{Mode: "backup", Why: "auto: home", Address: "127.0.0.1:3307", Dial: dialing(true, &asked)}

	if verdict, reason := check.Probe(context.Background()); verdict != application.DoctorOK || reason != "" {
		t.Fatalf("expected a plain ok, got %s (%s)", verdict, reason)
	}
	if asked != "127.0.0.1:3307" {
		t.Fatalf("expected a dial of 127.0.0.1:3307, got %q", asked)
	}
}

func TestBeadsServerIsFaultyWhenTheHomesOwnDatabaseDoesNotAnswer(t *testing.T) {
	asked := ""
	check := &doctor.BeadsServer{Mode: "backup", Why: "auto: home", Address: "127.0.0.1:3307", Dial: dialing(false, &asked)}

	verdict, reason := check.Probe(context.Background())
	if verdict != application.DoctorFaulty {
		t.Fatalf("expected faulty, got %s (%s)", verdict, reason)
	}
	for _, want := range []string{"127.0.0.1:3307", "dolt-beads", "this host"} {
		if !strings.Contains(reason, want) {
			t.Errorf("expected the reason to name %q, got %q", want, reason)
		}
	}
	err := check.Cure(context.Background())
	if err == nil || !strings.Contains(err.Error(), "dolt-beads") || strings.Contains(err.Error(), "wg") {
		t.Fatalf("expected no cure, naming dolt-beads and not wg, got %v", err)
	}
	if wait, cap := check.Damper(); wait != 0 || cap != 1 {
		t.Fatalf("expected one try an episode, got %s and %d", wait, cap)
	}
}

func TestBeadsServerIsOKWhenTheSharedDatabaseAnswers(t *testing.T) {
	asked := ""
	check := &doctor.BeadsServer{Mode: "shared", Address: "10.88.0.3:3307", Dial: dialing(true, &asked)}

	if verdict, reason := check.Probe(context.Background()); verdict != application.DoctorOK || reason != "" {
		t.Fatalf("expected a plain ok, got %s (%s)", verdict, reason)
	}
	if asked != "10.88.0.3:3307" {
		t.Fatalf("expected a dial of 10.88.0.3:3307, got %q", asked)
	}
}

func TestBeadsServerIsFaultyWhenTheSharedDatabaseDoesNotAnswerAndHasNoCure(t *testing.T) {
	asked := ""
	check := &doctor.BeadsServer{Mode: "shared", Address: "10.88.0.3:3307", Dial: dialing(false, &asked)}

	verdict, reason := check.Probe(context.Background())
	if verdict != application.DoctorFaulty || !strings.Contains(reason, "10.88.0.3:3307") {
		t.Fatalf("expected faulty, naming the address, got %s (%s)", verdict, reason)
	}
	if err := check.Cure(context.Background()); err == nil || !strings.Contains(err.Error(), "wg") {
		t.Fatalf("expected no cure, pointing at the wg check, got %v", err)
	}
	if wait, cap := check.Damper(); wait != 0 || cap != 1 {
		t.Fatalf("expected one try an episode, got %s and %d", wait, cap)
	}
	if !strings.Contains(check.WayBack(), "none") {
		t.Fatalf("expected no way back to name, got %q", check.WayBack())
	}
}

func TestBeadsServerCannotTellWithNoAddressOrASettingItCannotRead(t *testing.T) {
	for name, check := range map[string]*doctor.BeadsServer{
		"no host":                {Mode: "shared"},
		"a bad port":             {Mode: "shared", AddressErr: errors.New("BEADS_DOLT_SERVER_PORT is \"x\"")},
		"a home with a bad port": {Mode: "backup", AddressErr: errors.New("BEADS_DOLT_SERVER_PORT is \"x\"")},
		"an unknown mode":        {ModeErr: errors.New("beads_sync is \"server\"")},
	} {
		verdict, reason := check.Probe(context.Background())
		if verdict != application.DoctorCannotTell || reason == "" {
			t.Errorf("%s: expected cannot-tell with a reason, got %s (%s)", name, verdict, reason)
		}
	}
}

func TestBeadsServerDialsForRealWithAShortTimeout(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("no loopback listener: %v", err)
	}
	defer listener.Close()

	check := doctor.NewBeadsServer("shared", listener.Addr().String(), nil, nil)
	if verdict, reason := check.Probe(context.Background()); verdict != application.DoctorOK {
		t.Fatalf("expected a listening address to answer, got %s (%s)", verdict, reason)
	}
	listener.Close()
	if verdict, _ := check.Probe(context.Background()); verdict != application.DoctorFaulty {
		t.Fatalf("expected a closed address to be faulty, got %s", verdict)
	}
	if check.Name() != "beads-server" {
		t.Fatalf("expected the check named beads-server, got %q", check.Name())
	}
}

func TestBeadsServerNamesWhyAutoChoseTheMode(t *testing.T) {
	var asked string
	check := &doctor.BeadsServer{Mode: "shared", Why: "auto: boost of laptop", Address: "laptop.mw:3307", Dial: dialing(false, &asked)}
	if _, why := check.Probe(context.Background()); !strings.Contains(why, "laptop.mw:3307") {
		t.Errorf("expected the address named, got %q", why)
	}
	inert := &doctor.BeadsServer{Mode: "backup", Why: "auto: home"}
	if verdict, why := inert.Probe(context.Background()); verdict != application.DoctorOK || !strings.Contains(why, "backup (auto: home)") {
		t.Errorf("expected ok naming backup (auto: home), got %v %q", verdict, why)
	}
}
