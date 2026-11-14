package main

import (
	"context"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/rig"
	"github.com/Jonathan-A-White/millwright/infrastructure/vault"
)

// gateCommands says which of the rig's gate commands a path runs. [tests] and
// [changed_specs] belong to every landing gate; the related-tests line (the
// landing gate of mw next and mw land, never mw check) and the parts (mw check
// alone) are asked for.
type gateCommands struct {
	related bool
	parts   bool

	// reread makes the commands be read from the config file again each time the
	// gate asks for one, not once when the batch began (mw-gq6.1006): mw land's.
	// [always_run], [named_tests], builder_nice, builder_jobs and the vault's
	// timing tests stay as the batch began with them.
	reread bool
}

// gateLinesNow reads the gate's commands from the config file as which asks for
// them: the lines landingChecks gives a Checks, again.
func gateLinesNow(which gateCommands) (rig.GateLines, error) {
	var lines rig.GateLines
	var err error
	if lines.Tests, err = config.Tests(); err != nil {
		return lines, err
	}
	if lines.Specs, err = config.ChangedSpecs(); err != nil {
		return lines, err
	}
	if which.related {
		if lines.Related, err = config.RelatedTests(); err != nil {
			return lines, err
		}
	}
	if which.parts {
		configured, err := config.TestParts()
		if err != nil {
			return lines, err
		}
		lines.Parts = gateParts(configured)
	}
	return lines, nil
}

// gateParts is the parts of the config file as the application names them.
func gateParts(configured map[string][]config.TestPart) map[string][]application.GatePart {
	parts := map[string][]application.GatePart{}
	for rigName, named := range configured {
		for _, p := range named {
			parts[rigName] = append(parts[rigName], application.GatePart{Name: p.Name, Command: p.Command})
		}
	}
	return parts
}

// landingChecks (vaultDir is where the rigs' files are) is the one place the gate's commands are read from the config
// file and put into the rig's Checks, so that mw next, mw land and mw check
// cannot drift apart on which of [tests], [test_parts] and [changed_specs] a
// landing runs (mw-gq6.627: mw land was built without [changed_specs] and ran
// none). The Checks is also the SpecChecks of the same rigs.
func landingChecks(vaultDir string, which gateCommands) (*rig.Checks, error) {
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
	testRuns, err := hostTestRuns()
	if err != nil {
		return nil, err
	}
	named, err := config.NamedTests()
	if err != nil {
		return nil, err
	}
	options := []rig.CheckOption{rig.WithCommands(tests), rig.WithSpecCommands(changedSpecs), rig.WithNamedCommands(named), rig.WithNice(nice), rig.WithJobs(jobs), rig.WithTestRuns(testRuns), rig.WithTimingTests(vault.New(vaultDir).TimingTests(context.Background()))}
	if which.related {
		related, err := config.RelatedTests()
		if err != nil {
			return nil, err
		}
		always, err := config.AlwaysRun()
		if err != nil {
			return nil, err
		}
		options = append(options, rig.WithRelatedCommands(related), rig.WithAlwaysRun(always))
	}
	if which.parts {
		configured, err := config.TestParts()
		if err != nil {
			return nil, err
		}
		options = append(options, rig.WithParts(gateParts(configured)))
	}
	if which.reread {
		options = append(options, rig.WithGateLinesFrom(func() (rig.GateLines, error) { return gateLinesNow(which) }, time.Now))
	}
	return rig.NewChecks(options...), nil
}
