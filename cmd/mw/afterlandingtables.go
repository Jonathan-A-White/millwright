package main

import (
	"strconv"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
)

// afterLandingTables is this host's per-rig [after_landing_limit] and
// [after_landing_deploy_step] values, by table then rig, each as the text its
// fingerprint is taken of: a limit as the duration prints (so "20m" and "1200s"
// agree), a step as its number. A rig this host has an [after_landing] line or a
// value in either table for, and no value in a table, is named in it with an
// empty value, which is left as an explicit "none" (mw-gq6.1768). mw sync leaves
// a fingerprint of each and mw doctor's after-landing-agrees check compares them
// (mw-gq6.1766).
func afterLandingTables() (map[string]map[string]string, error) {
	limits, err := config.AfterLandingLimits()
	if err != nil {
		return nil, err
	}
	steps, err := config.AfterLandingDeploySteps()
	if err != nil {
		return nil, err
	}
	tables := map[string]map[string]string{
		application.AfterLandingLimitTable:      {},
		application.AfterLandingDeployStepTable: {},
	}
	for rig, limit := range limits {
		tables[application.AfterLandingLimitTable][rig] = limit.String()
	}
	for rig, step := range steps {
		tables[application.AfterLandingDeployStepTable][rig] = strconv.Itoa(step)
	}
	lines, err := config.AfterLanding()
	if err != nil {
		return nil, err
	}
	rigs := map[string]bool{}
	for rig, line := range lines {
		if strings.TrimSpace(line) != "" {
			rigs[rig] = true
		}
	}
	for _, table := range tables {
		for rig := range table {
			rigs[rig] = true
		}
	}
	for rig := range rigs {
		for _, table := range application.AfterLandingTableNames {
			if _, has := tables[table][rig]; !has {
				tables[table][rig] = ""
			}
		}
	}
	return tables, nil
}
