package main

import (
	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/rig"
)

// gateCommands says which of the rig's gate commands a path runs. [tests] and
// [changed_specs] belong to every landing gate; the related-tests line (the
// landing gate of mw next and mw land, never mw check) and the parts (mw check
// alone) are asked for.
type gateCommands struct {
	related bool
	parts   bool
}

// landingChecks is the one place the gate's commands are read from the config
// file and put into the rig's Checks, so that mw next, mw land and mw check
// cannot drift apart on which of [tests], [test_parts] and [changed_specs] a
// landing runs (mw-gq6.627: mw land was built without [changed_specs] and ran
// none). The Checks is also the SpecChecks of the same rigs.
func landingChecks(which gateCommands) (*rig.Checks, error) {
	tests, err := config.Tests()
	if err != nil {
		return nil, err
	}
	changedSpecs, err := config.ChangedSpecs()
	if err != nil {
		return nil, err
	}
	nice, err := config.BuilderNice()
	if err != nil {
		return nil, err
	}
	jobs, err := config.BuilderJobs()
	if err != nil {
		return nil, err
	}
	options := []rig.CheckOption{rig.WithCommands(tests), rig.WithSpecCommands(changedSpecs), rig.WithNice(nice), rig.WithJobs(jobs)}
	if which.related {
		related, err := config.RelatedTests()
		if err != nil {
			return nil, err
		}
		options = append(options, rig.WithRelatedCommands(related))
	}
	if which.parts {
		configured, err := config.TestParts()
		if err != nil {
			return nil, err
		}
		parts := map[string][]application.GatePart{}
		for rigName, named := range configured {
			for _, p := range named {
				parts[rigName] = append(parts[rigName], application.GatePart{Name: p.Name, Command: p.Command})
			}
		}
		options = append(options, rig.WithParts(parts))
	}
	return rig.NewChecks(options...), nil
}
