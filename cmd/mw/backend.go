package main

import (
	"fmt"
	"io"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/beads"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/rig"
)

// hostBackend is how this host stages the backend of a landing that changed it
// (application.BackendStage, mw-gq6.185): the [backend.<rig>] tables of the
// config file say how each rig's backend is built and swapped, the hands step is
// written the way mw hands add writes one, and the home is read from the vault's
// home file. A host with no such table gets a stage that does nothing; a table
// that cannot be read is said on errs and costs the landing or the tick nothing.
func hostBackend(gateway *beads.Gateway, files application.HomeFile, host string, rigs map[string]string, errs io.Writer) application.BackendStage {
	settings, err := config.Backends()
	if err != nil {
		if errs != nil {
			fmt.Fprintf(errs, "no backend staging: %v\n", err)
		}
		return application.BackendStage{}
	}
	if len(settings) == 0 {
		return application.BackendStage{}
	}
	stage := application.BackendStage{
		Rigs:     rigs,
		Settings: map[string]application.BackendRig{},
		Builds:   rig.New(),
		Home:     files,
		Host:     host,
		Tracker:  gateway,
		Notes:    gateway,
		Hands: application.HandsAdd{
			Tracker: gateway,
			Notes:   gateway,
			Now:     handsClock,
			Push:    newHandsPush(gateway),
			Err:     errs,

			View:     newHandsView(gateway, host),
			ViewLock: newHandsViewLock(),
		},
	}
	for name, b := range settings {
		stage.Settings[name] = application.BackendRig{
			Dir: b.Dir, Build: b.Build, Stage: b.Stage, Live: b.Live, Service: b.Service, Health: b.Health, Check: b.Check,
		}
	}
	return stage
}
