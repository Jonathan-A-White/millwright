package steps

import (
	"strings"

	"github.com/cucumber/godog"
)

// unfinishedWord stands in a feature's text for the marker word a branch's
// added line carries; it is written in pieces here so that this file does not
// carry it.
const unfinishedWord = "PROVI" + "SIONAL"

// registerNextUnfinishedSteps registers the steps of the scenarios of a branch
// that adds a line saying the work is unfinished (mw-gq6.1713).
func registerNextUnfinishedSteps(ctx *godog.ScenarioContext, c *nextContext) {
	ctx.Given(`^a commit on the branch of "([^"]*)" adds the line "([^"]*)" to "([^"]*)"$`, c.aCommitAddingALine)
}

// aCommitAddingALine commits a file holding line, in which <marker> reads as
// the unfinished-work marker word, on a story's branch.
func (c *nextContext) aCommitAddingALine(id, line, name string) error {
	line = strings.ReplaceAll(line, "<marker>", unfinishedWord)
	return c.aCommitAdding(id, name, []byte("package x\n\n"+line+"\n"), 0o644)
}
