package steps

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"

	"github.com/cucumber/godog"
)

// registerPosternMoveHomeSteps registers the steps of
// features/postern_move_home.feature on the postern inbox's own world.
func registerPosternMoveHomeSteps(ctx *godog.ScenarioContext, c *posternInboxContext) {
	ctx.Given(`^this host is "([^"]*)" and the vault's home file names "([^"]*)"$`, c.thisHostIsAndTheHomeFileNames)
	ctx.Given(`^the home move is written on bead "([^"]*)"$`, c.theHomeMoveIsWrittenOnBead)
	ctx.Given(`^the old home answers ssh$`, c.theOldHomeAnswers)
	ctx.Given(`^the old home does not answer ssh$`, c.theOldHomeDoesNotAnswer)
	ctx.Given(`^the tracker cannot be reached$`, c.theTrackerCannotBeReached)
	ctx.Given(`^a move-home to "([^"]*)" signed by "([^"]*)", sent (\d+) minutes ago, with txid "([^"]*)"$`, c.aMoveHomeSignedBy)
	ctx.Given(`^an unsigned move-home to "([^"]*)", sent (\d+) minutes ago, with txid "([^"]*)"$`, c.anUnsignedMoveHome)
	ctx.Then(`^mw home move ran once, as "([^"]*)"$`, c.mwHomeMoveRanOnceAs)
	ctx.Then(`^mw home move did not run$`, c.mwHomeMoveDidNotRun)
	ctx.Then(`^bead "([^"]*)"'s first comment starts "([^"]*)"$`, c.beadsFirstCommentStarts)
	ctx.Then(`^the txid "([^"]*)" is marked refused, saying "([^"]*)"$`, c.theTxidIsMarkedRefused)
	ctx.Then(`^the txid "([^"]*)" is not marked$`, c.theTxidIsNotMarked)
}

// posternMoveHomeNow is the clock every move-home scenario reads.
var posternMoveHomeNow = posternReplyStamp.Add(time.Hour)

func (c *posternInboxContext) moveHomeWorld() {
	if c.mover == nil {
		c.mover = &apptest.FakeHomeMover{}
	}
	c.now = posternMoveHomeNow
}

func (c *posternInboxContext) thisHostIsAndTheHomeFileNames(host, home string) error {
	c.moveHomeWorld()
	c.host = host
	c.homeFile = &apptest.FakeHomeFile{Text: domain.HomeRecord{Host: home, At: posternReplyStamp, By: "mw@" + home}.String()}
	return nil
}

func (c *posternInboxContext) theHomeMoveIsWrittenOnBead(bead string) error {
	c.memory.AddEpic(bead, domain.Path{})
	c.moveBead = bead
	return nil
}

func (c *posternInboxContext) theOldHomeAnswers() error {
	c.moveHomeWorld()
	c.mover.Answers = true
	return nil
}

func (c *posternInboxContext) theOldHomeDoesNotAnswer() error {
	c.moveHomeWorld()
	c.mover.Answers = false
	return nil
}

func (c *posternInboxContext) theTrackerCannotBeReached() error {
	c.memory.Err = errors.New("dial tcp laptop.mw:3307: connect: no route to host")
	return nil
}

// addMoveHome adds a move-home record (postern's docs/protocol.md section 18)
// whose envelope is the Governor's and whose signer, the key the backend
// vouches for, is signer: "" is a record the backend could not vouch for.
func (c *posternInboxContext) addMoveHome(host, signer string, minutes int, txid string) error {
	text, err := json.Marshal(map[string]string{"host": host})
	if err != nil {
		return err
	}
	c.cipher.From = c.governorKey
	ciphertext, err := c.cipher.Encrypt(c.pubKey, string(text))
	if err != nil {
		return err
	}
	c.backend.AddRecord(application.PosternRecord{
		Txid: txid, Class: application.PosternClassMoveHome, From: c.governorKey, To: c.pubKey, Signer: signer,
		Ts: posternMoveHomeNow.Add(-time.Duration(minutes) * time.Minute), Ciphertext: ciphertext,
	})
	return nil
}

func (c *posternInboxContext) aMoveHomeSignedBy(host, signer string, minutes int, txid string) error {
	return c.addMoveHome(host, signer, minutes, txid)
}

func (c *posternInboxContext) anUnsignedMoveHome(host string, minutes int, txid string) error {
	return c.addMoveHome(host, "", minutes, txid)
}

func (c *posternInboxContext) mwHomeMoveRanOnceAs(args string) error {
	if err := c.itSucceeds(); err != nil {
		return err
	}
	if moves := c.mover.Moves(); len(moves) != 1 || moves[0] != args {
		return fmt.Errorf("expected mw home move to run once, as %q; it ran %q\n%s", args, moves, c.out.String())
	}
	return nil
}

func (c *posternInboxContext) mwHomeMoveDidNotRun() error {
	if err := c.itSucceeds(); err != nil {
		return err
	}
	if moves := c.mover.Moves(); len(moves) != 0 {
		return fmt.Errorf("expected mw home move not to run; it ran %q", moves)
	}
	return nil
}

func (c *posternInboxContext) beadsFirstCommentStarts(bead, want string) error {
	comments, err := c.memory.StoryComments(context.Background(), bead)
	if err != nil {
		return err
	}
	if len(comments) == 0 || !strings.HasPrefix(comments[0].Text, want) {
		return fmt.Errorf("expected %s's first comment to start %q, got %+v", bead, want, comments)
	}
	return nil
}

func (c *posternInboxContext) theTxidIsMarkedRefused(txid, why string) error {
	note, err := c.memory.Note(context.Background(), application.PosternAppliedKey(txid))
	if err != nil {
		return err
	}
	if !strings.HasPrefix(note, "refused ") || !strings.Contains(note, why) {
		return fmt.Errorf("expected %s marked refused, saying %q; got %q", txid, why, note)
	}
	return nil
}

func (c *posternInboxContext) theTxidIsNotMarked(txid string) error {
	note, err := c.memory.Note(context.Background(), application.PosternAppliedKey(txid))
	if err != nil {
		return err
	}
	if note != "" {
		return fmt.Errorf("expected %s not marked, it reads %q", txid, note)
	}
	return nil
}
