package main

import (
	"context"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain"
	"github.com/Jonathan-A-White/millwright/infrastructure/beads"
)

// mw-6ww.163.7: the backend stage writes its swap as a hands step the way mw hands
// add does, so a host the config retires (retired_hosts) is refused there too.
func TestBackendStageRefusesAHandsStepForARetiredHost(t *testing.T) {
	mwConfig(t, "retired_hosts = [\"desktop\"]\n\n[rigs]\nbrookfield = \"/nowhere/brookfield\"\n\n"+
		"[backend.brookfield]\nbuild = \"go build -o {out} ./cmd/x\"\nstage = \"/srv/x.new\"\nlive = \"/srv/x\"\nservice = \"x\"\nhealth = \"http://127.0.0.1:1/h\"\n")

	stage := hostBackend(&beads.Gateway{}, nil, "laptop", map[string]string{"brookfield": "/nowhere/brookfield"}, nil)
	if stage.Hands == nil {
		t.Fatal("expected the backend stage to be wired with a hands step writer")
	}
	_, err := stage.Hands.Run(context.Background(), application.HandsAddRequest{Bead: "mw-x.1", Step: domain.HandsStep{ID: "swap", Host: "desktop", As: domain.HandsAsRoot, Run: "true"}})
	if err == nil || !strings.Contains(err.Error(), "the host desktop is retired (retired_hosts = [desktop] in the config file)") {
		t.Fatalf("expected a hands step for a retired host to be refused naming retired_hosts, got %v", err)
	}
}
