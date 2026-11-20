package main

import (
	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/grist"
)

// newGristCredits is the Anthropic API key at [grist] api_key_file, as this
// host's mill bills its grinds to it: a person marked API only's always, and
// the factory's own while [grist] api_credits is on (On), else on the
// subscription login (mw-gq6.1518). The key is read for each grind, not here.
func newGristCredits(host, stateDir string, settings config.GristSettings) *application.GristCredits {
	return &application.GristCredits{
		On:     settings.APICredits,
		Key:    grist.CreditKeyFile{Path: settings.APIKeyFile},
		State:  &grist.CreditState{Dir: stateDir},
		Events: homeEventLog(),
		Host:   host,
	}
}
