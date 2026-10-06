package main

import (
	"os"
	"path/filepath"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/network"
)

// hostNetwork is how this host tells whether its network is metered: config
// `metered`, or on a WSL host Windows' own answer, kept in the state directory
// beside the sync lock for about a minute. A host with no PowerShell mounted
// has none to ask and is unmetered unless config says so. quiet reads without
// writing, for mw status and a dry run: no kept answer and no event.
func hostNetwork(quiet bool) (application.NetworkReader, error) {
	said, err := config.Metered()
	if err != nil {
		return nil, err
	}
	setting, err := application.ParseMeteredSetting(said)
	if err != nil {
		return nil, err
	}
	reader := &application.Network{Setting: setting, Quiet: quiet, Events: homeEventLog()}
	if host, err := config.Host(); err == nil {
		reader.Host = host
	}
	if _, err := os.Stat(network.PowerShell); err == nil {
		reader.Probe = network.Windows{}
	}
	if home, err := os.UserHomeDir(); err == nil {
		reader.Store = network.NewStore(filepath.Join(home, SyncHaltStateDir))
	}
	return reader, nil
}
