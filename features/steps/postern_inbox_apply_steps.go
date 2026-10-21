package steps

import (
	"context"
	"fmt"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"

	"github.com/cucumber/godog"
)

// registerPosternInboxApplySteps adds the steps of the apply pass saving the
// files the Governor sends on any thread.
func registerPosternInboxApplySteps(ctx *godog.ScenarioContext, c *posternInboxContext) {
	ctx.Given(`^a postern message from "([^"]*)" with text "([^"]*)" and txid "([^"]*)" carrying a screenshot$`,
		func(from, text, txid string) error {
			return c.aPosternMessageCarryingAScreenshot(from, application.PosternThread{}, text, txid)
		})
	ctx.Given(`^a postern message from "([^"]*)" in channel "([^"]*)" with text "([^"]*)" and txid "([^"]*)" carrying a screenshot$`,
		func(from, topic, text, txid string) error {
			return c.aPosternMessageCarryingAScreenshot(from, application.PosternThread{Topic: topic}, text, txid)
		})
	ctx.Then(`^the txid "([^"]*)" is marked applied naming the decrypted image's path$`, func(txid string) error {
		note, err := c.memory.Note(context.Background(), application.PosternAppliedKey(txid))
		if err != nil {
			return err
		}
		if !strings.HasPrefix(note, "applied ") || !strings.Contains(note, c.attachmentPath()) {
			return fmt.Errorf("expected %s marked applied naming %s, got %q", txid, c.attachmentPath(), note)
		}
		return nil
	})
}
