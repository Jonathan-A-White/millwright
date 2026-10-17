package application

import (
	"context"
	"testing"
)

type markedBuilt map[string]string

func (m markedBuilt) Built(_ context.Context, rig string) (string, error) { return m[rig], nil }
func (m markedBuilt) MarkBuilt(_ context.Context, rig, commit string) error {
	m[rig] = commit
	return nil
}

// mw-gq6.564: a landing's own deploy that passes sets the rig's deployed mark on
// this host, so that the home's tick does not deploy the commit again.
func TestALandingsPassingDeploySetsTheDeployedMark(t *testing.T) {
	marks := markedBuilt{}
	deploy := &fakeDeploy{}
	n := Next{AfterLanding: deploy, Tracker: &smokeComments{}, Host: "desktop", Deployed: marks}
	c := &closeOut{id: "mw-1.1", target: "main", rigDir: "/rigs/lampas"}
	c.path.Rig = "lampas"
	report := &NextReport{Rig: Advanced{Moved: true}, Landed: true}
	report.How.Commit = "abc1234def"

	n.afterLanding(context.Background(), c, report)

	if deploy.ran != 1 || marks["lampas.deployed"] != "abc1234def" {
		t.Errorf("expected one run and the mark set to the landed commit, ran %d, marks %v", deploy.ran, marks)
	}
}
