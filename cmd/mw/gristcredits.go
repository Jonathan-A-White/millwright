package main

import (
	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/grist"
)

// newGristCredits is what the mill bills its grinds to on this host: the
// Anthropic API key at [grist] api_key_file while [grist] api_credits is on,
// else nil, and every grind is billed to the subscription login. The key is
// read for each grind, not here.
func newGristCredits(host, stateDir string, settings config.GristSettings) *application.GristCredits {
	if !settings.APICredits {
		return nil
	}
	return &application.GristCredits{
		On:     true,
		Key:    grist.CreditKeyFile{Path: settings.APIKeyFile},
		State:  &grist.CreditState{Dir: stateDir},
		Events: homeEventLog(),
		Host:   host,
	}
}
