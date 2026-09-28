package steps

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
	"github.com/Jonathan-A-White/millwright/infrastructure/postern"

	"github.com/cucumber/godog"
)

// posternReplyStamp is when every fake reply record in this feature's
// scenarios is stamped, so the ANSWER comment's timestamp is predictable.
var posternReplyStamp = time.Date(2026, time.September, 25, 10, 0, 0, 0, time.UTC)

// posternInboxContext holds a throwaway postern key, a fake backend and
// cipher, and a fake tracker standing in for the note store, so that mw
// postern inbox can be exercised with no network and no real bd.
type posternInboxContext struct {
	home        string
	keys        *postern.KeyFile
	pubKey      string
	backend     *apptest.FakePostern
	cipher      *apptest.FakeCipher
	memory      *apptest.FakeTracker
	mailbox     *apptest.FakeMailbox
	out         *bytes.Buffer
	governorKey string
	attachDir   string
	transcriber *apptest.FakeTranscriber

	// attachImage and attachTxid are what the last "carrying a screenshot"
	// step built, so a later Then step can compute the path mw postern
	// inbox should have written it to, without hardcoding it in the feature.
	attachImage []byte
	attachTxid  string

	messages    []application.PosternInboxMessage
	unreadCount int
	err         error
}

// InitializePosternInboxScenario registers the steps of
// features/postern_inbox.feature.
func InitializePosternInboxScenario(ctx *godog.ScenarioContext) {
	c := &posternInboxContext{}

	ctx.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
		cipher := apptest.NewFakeCipher()
		// The envelope's sender key every fake record is really encrypted
		// under, standing in for BRC-78's embedded sender public key: the
		// same placeholder these scenarios have always used as the sender's
		// hex, so a record built without overriding From or Signer is a
		// genuine, verified one.
		cipher.From = "governor-pubkey-hex"
		*c = posternInboxContext{
			backend: apptest.NewFakePostern(),
			cipher:  cipher,
			memory:  apptest.NewFakeTracker(),
			mailbox: apptest.NewFakeMailbox(),
			out:     &bytes.Buffer{},
		}
		return ctx, nil
	})
	ctx.After(func(ctx context.Context, sc *godog.Scenario, err error) (context.Context, error) {
		if c.home != "" {
			os.RemoveAll(c.home)
		}
		return ctx, nil
	})

	ctx.Given(`^a throwaway postern key$`, c.aThrowawayPosternKey)
	ctx.Given(`^a postern record of class "([^"]*)" addressed to this key$`, c.aPosternRecordAddressedToThisKey)
	ctx.Given(`^a postern record of class "([^"]*)" addressed to this key with txid "([^"]*)"$`, c.aPosternRecordAddressedToThisKeyWithTxid)
	ctx.Given(`^a postern record of class "([^"]*)" addressed to another key$`, c.aPosternRecordAddressedToAnotherKey)
	ctx.Given(`^bead "([^"]*)" is known to the tracker$`, c.beadIsKnownToTheTracker)
	ctx.Given(`^bead "([^"]*)" has an open question, txid "([^"]*)"$`, c.beadHasAnOpenQuestion)
	ctx.Given(`^epic "([^"]*)" has (\d+) held stories$`, c.epicHasNHeldStories)
	ctx.Given(`^epic "([^"]*)" has an open question offering "([^"]*)", txid "([^"]*)"$`, c.epicHasAnOpenQuestionOffering)
	ctx.Given(`^a postern reply for bead "([^"]*)" with answer "([^"]*)" and txid "([^"]*)" addressed to this key$`,
		c.aPosternReplyAddressedToThisKey)
	ctx.Given(`^a postern reply from "([^"]*)" for bead "([^"]*)" with answer "([^"]*)" and txid "([^"]*)" addressed to this key$`,
		c.aPosternReplyFromAddressedToThisKey)
	ctx.Given(`^a plain text postern record with text "([^"]*)" addressed to this key$`, c.aPlainTextRecordAddressedToThisKey)
	ctx.Given(`^a postern record of class "([^"]*)" addressed to this key, threaded on bead "([^"]*)"$`, c.aPosternRecordAddressedToThisKeyThreadedOnBead)
	ctx.Given(`^a postern record of class "([^"]*)" addressed to this key, on topic "([^"]*)"$`, c.aPosternRecordAddressedToThisKeyOnTopic)
	ctx.Given(`^a postern question for bead "([^"]*)" addressed to this key$`, c.aPosternQuestionForBeadAddressedToThisKey)
	ctx.Given(`^a postern record of class "([^"]*)" addressed to this key signed by "([^"]*)"$`, c.aPosternRecordAddressedToThisKeySignedBy)
	ctx.Given(`^a postern reply for bead "([^"]*)" with answer "([^"]*)" and txid "([^"]*)" addressed to this key claiming to be from "([^"]*)"$`,
		c.aPosternReplyClaimingToBeFrom)
	ctx.Given(`^a postern reply for bead "([^"]*)" with answer "([^"]*)" and txid "([^"]*)" addressed to this key signed by "([^"]*)"$`,
		c.aPosternReplySignedBy)
	ctx.Given(`^mw postern inbox trusts "([^"]*)" as the Governor's key$`, c.thePosternGovernorKeyIs)
	ctx.Given(`^a postern message from "([^"]*)" threaded on bead "([^"]*)" with text "([^"]*)" and txid "([^"]*)"$`,
		c.aPosternMessageFromThreadedOnBead)
	ctx.Given(`^a postern message from "([^"]*)" threaded on bead "([^"]*)" with text "([^"]*)" and txid "([^"]*)" carrying a screenshot$`,
		c.aPosternMessageFromThreadedOnBeadCarryingAScreenshot)
	ctx.Given(`^a postern message from "([^"]*)" on topic "([^"]*)" with text "([^"]*)" and txid "([^"]*)"$`,
		c.aPosternMessageFromOnTopic)

	ctx.Given(`^a postern action "([^"]*)" on bead "([^"]*)" from "([^"]*)" with txid "([^"]*)"$`, c.aPosternActionOnBeadFrom)
	ctx.Given(`^a postern priority (\d+) action on bead "([^"]*)" from the Governor with txid "([^"]*)"$`, c.aPosternPriorityActionFromTheGovernor)
	ctx.Given(`^bead "([^"]*)" is claimed by "([^"]*)"$`, c.beadIsClaimedBy)
	ctx.When(`^mw postern inbox --apply is run$`, c.mwPosternInboxApplyIsRun)
	ctx.Given(`^the postern inbox hears voice notes as "([^"]*)"$`, c.thePosternInboxHearsVoiceNotesAs)
	ctx.Given(`^a postern voice note from "([^"]*)" threaded on bead "([^"]*)" with txid "([^"]*)"$`, c.aPosternVoiceNoteThreadedOnBead)
	ctx.Then(`^the transcript "([^"]*)" was sent back to the Governor in bead "([^"]*)"'s thread, re "([^"]*)"$`, c.theTranscriptWasSentBack)
	ctx.Then(`^bead "([^"]*)" now stands "([^"]*)"$`, c.beadNowStands)
	ctx.Then(`^bead "([^"]*)" now has priority (\d+)$`, c.beadNowHasPriority)
	ctx.Then(`^bead "([^"]*)"'s last comment reads "([^"]*)"$`, c.beadsLastCommentReads)
	ctx.Then(`^the txid "([^"]*)" is marked applied$`, c.theTxidIsMarkedApplied)
	ctx.Then(`^the postern inbox cursor has not moved$`, c.thePosternInboxCursorHasNotMoved)
	ctx.When(`^mw postern inbox is run$`, c.mwPosternInboxIsRun)
	ctx.When(`^mw postern inbox --unread-count is run$`, c.mwPosternInboxUnreadCountIsRun)

	ctx.Then(`^reading succeeds$`, c.itSucceeds)
	ctx.Then(`^(\d+) messages? (?:is|are) printed$`, c.nMessagesArePrinted)
	ctx.Then(`^the first message printed is classed "([^"]*)"$`, c.theFirstMessagePrintedIsClassed)
	ctx.Then(`^the second message printed is classed "([^"]*)"$`, c.theSecondMessagePrintedIsClassed)
	ctx.Then(`^the unread count is (\d+)$`, c.theUnreadCountIs)
	ctx.Then(`^the postern inbox cursor is saved as a note$`, c.thePosternInboxCursorIsSavedAsANote)
	ctx.Then(`^no story state was set$`, c.noStoryStateWasSet)
	ctx.Then(`^bead "([^"]*)" is commented an ANSWER with txid "([^"]*)" from "([^"]*)" saying "([^"]*)"$`, c.beadIsCommentedTheAnswer)
	ctx.Then(`^bead "([^"]*)"'s question note is cleared$`, c.beadsQuestionNoteIsCleared)
	ctx.Then(`^mail "([^"]*)" was sent to mayor$`, c.mailWasSentToMayor)
	ctx.Then(`^bead "([^"]*)" has no comment$`, c.beadHasNoComment)
	ctx.Then(`^bead "([^"]*)" is commented by the Governor saying "([^"]*)"$`, c.beadIsCommentedByTheGovernor)
	ctx.Then(`^bead "([^"]*)" has (\d+) comments?$`, c.beadHasNComments)
	ctx.Then(`^it did not print "([^"]*)"$`, c.itDidNotPrintText)
	ctx.Then(`^no mail was sent for the reply$`, c.noMailWasSent)
	ctx.Then(`^it printed "([^"]*)"$`, c.itPrintedText)
	ctx.Then(`^epic "([^"]*)"'s held stories are released$`, c.epicsHeldStoriesAreReleased)
	ctx.Then(`^bead "([^"]*)" is commented a RELEASED with txid "([^"]*)"$`, c.beadIsCommentedARELEASEDWithTxid)
	ctx.Then(`^the decrypted image is written under the attachment directory$`, c.theDecryptedImageIsWrittenUnderTheAttachmentDirectory)
	ctx.Then(`^bead "([^"]*)"'s last comment names the decrypted image's path$`, c.beadsLastCommentNamesTheDecryptedImagesPath)
}

func (c *posternInboxContext) aThrowawayPosternKey() error {
	home, err := os.MkdirTemp("", "mw-postern-inbox-")
	if err != nil {
		return err
	}
	c.home = home
	c.attachDir = filepath.Join(home, "postern-attachments")
	c.keys = postern.New(filepath.Join(home, "postern.key"))
	if err := c.keys.Generate(); err != nil {
		return err
	}
	pubKey, _, err := c.keys.PublicKey()
	if err != nil {
		return err
	}
	c.pubKey = pubKey
	return nil
}

func (c *posternInboxContext) addRecord(class, to, txid string) error {
	ciphertext, err := c.cipher.Encrypt(to, fmt.Sprintf("%s text", class))
	if err != nil {
		return err
	}
	c.backend.AddRecord(application.PosternRecord{
		Txid:       txid,
		Class:      class,
		From:       c.cipher.From,
		To:         to,
		Ciphertext: ciphertext,
	})
	return nil
}

func (c *posternInboxContext) aPosternRecordAddressedToThisKey(class string) error {
	return c.addRecord(class, c.pubKey, "")
}

func (c *posternInboxContext) aPosternRecordAddressedToThisKeyWithTxid(class, txid string) error {
	return c.addRecord(class, c.pubKey, txid)
}

func (c *posternInboxContext) aPosternRecordAddressedToAnotherKey(class string) error {
	return c.addRecord(class, "another-key-pubkey-hex", "")
}

// aPosternRecordAddressedToThisKeySignedBy adds a record whose transaction
// signing key the postern backend supplies (application.PosternRecord.Signer)
// as signer, alongside the genuine envelope sender every fake record is
// really encrypted under.
func (c *posternInboxContext) aPosternRecordAddressedToThisKeySignedBy(class, signer string) error {
	ciphertext, err := c.cipher.Encrypt(c.pubKey, fmt.Sprintf("%s text", class))
	if err != nil {
		return err
	}
	c.backend.AddRecord(application.PosternRecord{
		Class:      class,
		From:       c.cipher.From,
		To:         c.pubKey,
		Signer:     signer,
		Ciphertext: ciphertext,
	})
	return nil
}

func (c *posternInboxContext) aPosternRecordAddressedToThisKeyThreadedOnBead(class, bead string) error {
	wrapped, err := json.Marshal(application.PosternThreadedMessage{
		Thread: application.PosternThread{Bead: bead},
		Text:   fmt.Sprintf("%s text", class),
	})
	if err != nil {
		return err
	}
	ciphertext, err := c.cipher.Encrypt(c.pubKey, string(wrapped))
	if err != nil {
		return err
	}
	c.backend.AddRecord(application.PosternRecord{
		Class:      class,
		From:       c.cipher.From,
		To:         c.pubKey,
		Ciphertext: ciphertext,
	})
	return nil
}

func (c *posternInboxContext) aPosternRecordAddressedToThisKeyOnTopic(class, topic string) error {
	wrapped, err := json.Marshal(application.PosternThreadedMessage{
		Thread: application.PosternThread{Topic: topic},
		Text:   fmt.Sprintf("%s text", class),
	})
	if err != nil {
		return err
	}
	ciphertext, err := c.cipher.Encrypt(c.pubKey, string(wrapped))
	if err != nil {
		return err
	}
	c.backend.AddRecord(application.PosternRecord{
		Class:      class,
		From:       c.cipher.From,
		To:         c.pubKey,
		Ciphertext: ciphertext,
	})
	return nil
}

// aPosternMessageFromThreadedOnBead adds a record wrapped in postern's thread
// envelope naming bead, its envelope sender (and its payload's claimed From)
// both set to from, so a genuine, verified message is what these scenarios
// need to exercise a Governor's reply landing as a bead comment.
func (c *posternInboxContext) aPosternMessageFromThreadedOnBead(from, bead, text, txid string) error {
	wrapped, err := json.Marshal(application.PosternThreadedMessage{
		Thread: application.PosternThread{Bead: bead},
		Text:   text,
	})
	if err != nil {
		return err
	}
	c.cipher.From = from
	ciphertext, err := c.cipher.Encrypt(c.pubKey, string(wrapped))
	if err != nil {
		return err
	}
	c.backend.AddRecord(application.PosternRecord{
		Txid:       txid,
		Class:      "message",
		From:       from,
		To:         c.pubKey,
		Ts:         posternReplyStamp,
		Ciphertext: ciphertext,
	})
	return nil
}

// aPosternMessageFromThreadedOnBeadCarryingAScreenshot is
// aPosternMessageFromThreadedOnBead with an attachment: a stand-in image,
// encrypted to this key exactly as the real cipher would decrypt it, stored
// in the fake backend's blob store under its sha256 hash, and announced in
// the message's plaintext (postern's docs/protocol.md section 8). It
// remembers the image and the txid so a later Then step can compute the
// path mw postern inbox should have written it to.
func (c *posternInboxContext) aPosternMessageFromThreadedOnBeadCarryingAScreenshot(from, bead, text, txid string) error {
	image := bytes.Repeat([]byte{0x89, 0x50, 0x4e, 0x47, 0x00, 0x0d, 0x0a, 0x1a}, 400) // a stand-in PNG
	c.cipher.From = from
	imageCtB64, err := c.cipher.Encrypt(c.pubKey, string(image))
	if err != nil {
		return err
	}
	raw, err := base64.StdEncoding.DecodeString(imageCtB64)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(raw)
	hash := hex.EncodeToString(sum[:])
	c.backend.SetBlob(hash, raw)

	wrapped, err := json.Marshal(application.PosternThreadedMessage{
		Thread:     application.PosternThread{Bead: bead},
		Text:       text,
		Attachment: &application.PosternAttachment{Hash: hash, Size: int64(len(raw)), Mime: "image/png"},
	})
	if err != nil {
		return err
	}
	ciphertext, err := c.cipher.Encrypt(c.pubKey, string(wrapped))
	if err != nil {
		return err
	}
	c.backend.AddRecord(application.PosternRecord{
		Txid:       txid,
		Class:      "message",
		From:       from,
		To:         c.pubKey,
		Ts:         posternReplyStamp,
		Ciphertext: ciphertext,
	})
	c.attachImage = image
	c.attachTxid = txid
	return nil
}

// aPosternMessageFromOnTopic is aPosternMessageFromThreadedOnBead's twin for a
// named topic thread, rather than a bead.
func (c *posternInboxContext) aPosternMessageFromOnTopic(from, topic, text, txid string) error {
	wrapped, err := json.Marshal(application.PosternThreadedMessage{
		Thread: application.PosternThread{Topic: topic},
		Text:   text,
	})
	if err != nil {
		return err
	}
	c.cipher.From = from
	ciphertext, err := c.cipher.Encrypt(c.pubKey, string(wrapped))
	if err != nil {
		return err
	}
	c.backend.AddRecord(application.PosternRecord{
		Txid:       txid,
		Class:      "message",
		From:       from,
		To:         c.pubKey,
		Ts:         posternReplyStamp,
		Ciphertext: ciphertext,
	})
	return nil
}

func (c *posternInboxContext) aPosternQuestionForBeadAddressedToThisKey(bead string) error {
	question, err := json.Marshal(application.PosternQuestion{Bead: bead, Q: "Ship it?", Rec: "A", Options: []string{"A", "B"}})
	if err != nil {
		return err
	}
	ciphertext, err := c.cipher.Encrypt(c.pubKey, string(question))
	if err != nil {
		return err
	}
	c.backend.AddRecord(application.PosternRecord{
		Class:      "decision-needed",
		From:       c.cipher.From,
		To:         c.pubKey,
		Ciphertext: ciphertext,
	})
	return nil
}

func (c *posternInboxContext) thePosternGovernorKeyIs(key string) error {
	c.governorKey = key
	return nil
}

func (c *posternInboxContext) beadIsKnownToTheTracker(id string) error {
	c.memory.AddStory("epic", domain.Story{ID: id})
	return nil
}

func (c *posternInboxContext) beadHasAnOpenQuestion(id, txid string) error {
	return c.memory.SetNote(context.Background(), application.PosternQuestionKey(id), txid)
}

// epicHasNHeldStories files epic id with n held stories under it, named
// "<id>.1", "<id>.2" and so on, so a Release-tap scenario has something to
// release.
func (c *posternInboxContext) epicHasNHeldStories(id string, n int) error {
	c.memory.AddEpic(id, domain.Path{})
	for i := 1; i <= n; i++ {
		storyID := fmt.Sprintf("%s.%d", id, i)
		c.memory.AddStory(id, domain.Story{ID: storyID, Title: fmt.Sprintf("Story %d", i)})
		if err := c.memory.SetStatus(storyID, apptest.StatusDeferred); err != nil {
			return err
		}
	}
	return nil
}

// epicHasAnOpenQuestionOffering marks id's question open, offering the
// comma-separated options in optionsCSV — the shape
// PosternSend.recordQuestion's own note takes, so a Release tap can be
// checked against what was actually offered.
func (c *posternInboxContext) epicHasAnOpenQuestionOffering(id, optionsCSV, txid string) error {
	note, err := json.Marshal(struct {
		Txid    string   `json:"txid"`
		Options []string `json:"options,omitempty"`
	}{Txid: txid, Options: splitOptions(optionsCSV)})
	if err != nil {
		return err
	}
	return c.memory.SetNote(context.Background(), application.PosternQuestionKey(id), string(note))
}

func (c *posternInboxContext) aPosternReplyAddressedToThisKey(bead, answer, txid string) error {
	text, err := json.Marshal(application.PosternReply{Bead: bead, Answer: answer})
	if err != nil {
		return err
	}
	ciphertext, err := c.cipher.Encrypt(c.pubKey, string(text))
	if err != nil {
		return err
	}
	c.backend.AddRecord(application.PosternRecord{
		Txid:       txid,
		Class:      "message",
		From:       c.cipher.From,
		To:         c.pubKey,
		Ts:         posternReplyStamp,
		Ciphertext: ciphertext,
	})
	return nil
}

// aPosternReplyClaimingToBeFrom adds a reply whose payload's own from field
// (claimedFrom) is not the key the message is really encrypted under
// (c.cipher.From) — a forged claim the envelope does not back.
func (c *posternInboxContext) aPosternReplyClaimingToBeFrom(bead, answer, txid, claimedFrom string) error {
	text, err := json.Marshal(application.PosternReply{Bead: bead, Answer: answer})
	if err != nil {
		return err
	}
	ciphertext, err := c.cipher.Encrypt(c.pubKey, string(text))
	if err != nil {
		return err
	}
	c.backend.AddRecord(application.PosternRecord{
		Txid:       txid,
		Class:      "message",
		From:       claimedFrom,
		To:         c.pubKey,
		Ts:         posternReplyStamp,
		Ciphertext: ciphertext,
	})
	return nil
}

// aPosternReplySignedBy adds a reply whose payload's from field is genuine
// (c.cipher.From) but whose transaction signing key (signer) is not — as if
// the postern backend had supplied a signer that disagrees with the
// envelope.
func (c *posternInboxContext) aPosternReplySignedBy(bead, answer, txid, signer string) error {
	text, err := json.Marshal(application.PosternReply{Bead: bead, Answer: answer})
	if err != nil {
		return err
	}
	ciphertext, err := c.cipher.Encrypt(c.pubKey, string(text))
	if err != nil {
		return err
	}
	c.backend.AddRecord(application.PosternRecord{
		Txid:       txid,
		Class:      "message",
		From:       c.cipher.From,
		To:         c.pubKey,
		Signer:     signer,
		Ts:         posternReplyStamp,
		Ciphertext: ciphertext,
	})
	return nil
}

// aPosternReplyFromAddressedToThisKey is aPosternReplyAddressedToThisKey with
// an explicit, genuinely verified sender: the envelope and the payload's own
// claimed From both agree on from, so the reply is verified but is not
// necessarily the Governor's.
func (c *posternInboxContext) aPosternReplyFromAddressedToThisKey(from, bead, answer, txid string) error {
	text, err := json.Marshal(application.PosternReply{Bead: bead, Answer: answer})
	if err != nil {
		return err
	}
	c.cipher.From = from
	ciphertext, err := c.cipher.Encrypt(c.pubKey, string(text))
	if err != nil {
		return err
	}
	c.backend.AddRecord(application.PosternRecord{
		Txid:       txid,
		Class:      "message",
		From:       from,
		To:         c.pubKey,
		Ts:         posternReplyStamp,
		Ciphertext: ciphertext,
	})
	return nil
}

func (c *posternInboxContext) aPlainTextRecordAddressedToThisKey(text string) error {
	ciphertext, err := c.cipher.Encrypt(c.pubKey, text)
	if err != nil {
		return err
	}
	c.backend.AddRecord(application.PosternRecord{
		Class:      "message",
		From:       c.cipher.From,
		To:         c.pubKey,
		Ciphertext: ciphertext,
	})
	return nil
}

func (c *posternInboxContext) inbox() application.PosternInbox {
	inbox := application.PosternInbox{
		Postern:       c.backend,
		Cipher:        c.cipher,
		Keys:          c.keys,
		Memory:        c.memory,
		Tracker:       c.memory,
		Mailbox:       c.mailbox,
		GovernorKey:   c.governorKey,
		AttachmentDir: c.attachDir,
		Out:           c.out,
	}
	if c.transcriber != nil {
		inbox.Transcriber = c.transcriber
		inbox.Sender = &application.PosternSend{
			Postern: c.backend, Cipher: c.cipher, Keys: c.keys, GovernorKey: c.governorKey,
		}
	}
	return inbox
}

func (c *posternInboxContext) thePosternInboxHearsVoiceNotesAs(transcript string) error {
	c.transcriber = &apptest.FakeTranscriber{Transcript: transcript}
	return nil
}

// aPosternVoiceNoteThreadedOnBead adds a record whose plaintext carries an
// audio attachment in bead's thread, the audio encrypted to this key and
// stored in the fake backend's blob store under its sha256.
func (c *posternInboxContext) aPosternVoiceNoteThreadedOnBead(from, bead, txid string) error {
	c.cipher.From = from
	sealed, err := c.cipher.EncryptBytes(c.pubKey, []byte("OggS a voice"))
	if err != nil {
		return err
	}
	raw, err := base64.StdEncoding.DecodeString(sealed)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(raw)
	hash := hex.EncodeToString(sum[:])
	c.backend.SetBlob(hash, raw)
	body, err := json.Marshal(application.PosternThreadedMessage{
		Thread:     application.PosternThread{Bead: bead},
		Attachment: &application.PosternAttachment{Hash: hash, Size: int64(len(raw)), Mime: "audio/ogg"},
	})
	if err != nil {
		return err
	}
	ciphertext, err := c.cipher.Encrypt(c.pubKey, string(body))
	if err != nil {
		return err
	}
	c.backend.AddRecord(application.PosternRecord{
		Txid: txid, Class: "message", From: from, To: c.pubKey, Ts: posternReplyStamp, Ciphertext: ciphertext,
	})
	return nil
}

func (c *posternInboxContext) theTranscriptWasSentBack(transcript, bead, re string) error {
	delivered := c.backend.Delivered()
	if len(delivered) != 1 {
		return fmt.Errorf("expected one message sent back, got %d", len(delivered))
	}
	var payload application.PosternPayload
	if err := json.Unmarshal(delivered[0], &payload); err != nil {
		return err
	}
	text, _, err := c.cipher.Decrypt("governor", payload.Ct)
	if err != nil {
		return err
	}
	var body application.PosternThreadedMessage
	if err := json.Unmarshal([]byte(text), &body); err != nil {
		return err
	}
	if body.Text != transcript || body.Thread.Bead != bead || body.Re != re || body.Role != application.PosternRoleTranscript {
		return fmt.Errorf("expected the transcript %q in %s's thread re %s, got %s", transcript, bead, re, text)
	}
	return nil
}

func (c *posternInboxContext) mwPosternInboxIsRun() error {
	c.messages, c.err = c.inbox().Run(context.Background())
	return nil
}

func (c *posternInboxContext) mwPosternInboxUnreadCountIsRun() error {
	c.unreadCount, c.err = c.inbox().UnreadCount(context.Background())
	return nil
}

func (c *posternInboxContext) itSucceeds() error {
	if c.err != nil {
		return fmt.Errorf("expected it to succeed, got: %w", c.err)
	}
	return nil
}

func (c *posternInboxContext) nMessagesArePrinted(want int) error {
	if err := c.itSucceeds(); err != nil {
		return err
	}
	if len(c.messages) != want {
		return fmt.Errorf("expected %d messages, got %d: %+v", want, len(c.messages), c.messages)
	}
	return nil
}

func (c *posternInboxContext) theFirstMessagePrintedIsClassed(class string) error {
	if len(c.messages) < 1 {
		return fmt.Errorf("no first message: only %d printed", len(c.messages))
	}
	if c.messages[0].Class != class {
		return fmt.Errorf("expected the first message classed %q, got %q", class, c.messages[0].Class)
	}
	return nil
}

func (c *posternInboxContext) theSecondMessagePrintedIsClassed(class string) error {
	if len(c.messages) < 2 {
		return fmt.Errorf("no second message: only %d printed", len(c.messages))
	}
	if c.messages[1].Class != class {
		return fmt.Errorf("expected the second message classed %q, got %q", class, c.messages[1].Class)
	}
	return nil
}

func (c *posternInboxContext) theUnreadCountIs(want int) error {
	if c.err != nil {
		return fmt.Errorf("expected it to succeed, got: %w", c.err)
	}
	if c.unreadCount != want {
		return fmt.Errorf("expected an unread count of %d, got %d", want, c.unreadCount)
	}
	return nil
}

func (c *posternInboxContext) thePosternInboxCursorIsSavedAsANote() error {
	saved, err := c.memory.Note(context.Background(), application.PosternCursorKey)
	if err != nil {
		return err
	}
	if strings.TrimSpace(saved) == "" {
		return fmt.Errorf("no postern inbox cursor note was saved")
	}
	if _, err := strconv.ParseInt(saved, 10, 64); err != nil {
		return fmt.Errorf("the saved cursor %q is not a whole number: %w", saved, err)
	}
	return nil
}

func (c *posternInboxContext) noStoryStateWasSet() error {
	for _, asked := range c.memory.Asked() {
		if asked == "SetStoryState" {
			return fmt.Errorf("expected no SetStoryState call, but one was made: %v", c.memory.Asked())
		}
	}
	return nil
}

func (c *posternInboxContext) beadIsCommentedTheAnswer(bead, txid, from, answer string) error {
	if err := c.itSucceeds(); err != nil {
		return err
	}
	comments, err := c.memory.StoryComments(context.Background(), bead)
	if err != nil {
		return err
	}
	if len(comments) == 0 {
		return fmt.Errorf("expected a comment on %s, found none", bead)
	}
	want := fmt.Sprintf("ANSWER %s from %s, txid %s: %s", posternReplyStamp.UTC().Format(time.RFC3339), from, txid, answer)
	got := comments[len(comments)-1].Text
	if got != want {
		return fmt.Errorf("expected the comment\n%s\ngot\n%s", want, got)
	}
	return nil
}

func (c *posternInboxContext) beadsQuestionNoteIsCleared(bead string) error {
	saved, err := c.memory.Note(context.Background(), application.PosternQuestionKey(bead))
	if err != nil {
		return err
	}
	if saved != "" {
		return fmt.Errorf("expected %s's question note to be cleared, still holds %q", bead, saved)
	}
	return nil
}

func (c *posternInboxContext) mailWasSentToMayor(subject string) error {
	unread, err := c.mailbox.Inbox(context.Background(), "mayor")
	if err != nil {
		return err
	}
	for _, message := range unread {
		if message.Subject == subject {
			return nil
		}
	}
	return fmt.Errorf("expected mail %q to mayor, got: %+v", subject, unread)
}

// beadHasNoComment checks that bead carries no comment; a bead the tracker
// has never heard of — as an unknown bead in these scenarios is — counts as
// having none.
func (c *posternInboxContext) beadHasNoComment(bead string) error {
	comments, err := c.memory.StoryComments(context.Background(), bead)
	if err != nil {
		return nil
	}
	if len(comments) != 0 {
		return fmt.Errorf("expected no comment on %s, got: %+v", bead, comments)
	}
	return nil
}

// beadIsCommentedByTheGovernor checks that bead's last comment is the thread
// comment mw postern inbox writes for a Governor's message, stamped with
// posternReplyStamp exactly as beadIsCommentedTheAnswer checks the ANSWER
// comment's own timestamp.
func (c *posternInboxContext) beadIsCommentedByTheGovernor(bead, text string) error {
	if err := c.itSucceeds(); err != nil {
		return err
	}
	comments, err := c.memory.StoryComments(context.Background(), bead)
	if err != nil {
		return err
	}
	if len(comments) == 0 {
		return fmt.Errorf("expected a comment on %s, found none", bead)
	}
	want := fmt.Sprintf("The Governor by postern %s: %s", posternReplyStamp.UTC().Format(time.RFC3339), text)
	got := comments[len(comments)-1].Text
	if got != want {
		return fmt.Errorf("expected the comment\n%s\ngot\n%s", want, got)
	}
	return nil
}

func (c *posternInboxContext) beadHasNComments(bead string, want int) error {
	if err := c.itSucceeds(); err != nil {
		return err
	}
	comments, err := c.memory.StoryComments(context.Background(), bead)
	if err != nil {
		return err
	}
	if len(comments) != want {
		return fmt.Errorf("expected %d comment(s) on %s, got %d: %+v", want, bead, len(comments), comments)
	}
	return nil
}

// epicsHeldStoriesAreReleased checks that every story epic id holds is open,
// not held: what a Release tap's release, run through mw postern inbox,
// should have done.
func (c *posternInboxContext) epicsHeldStoriesAreReleased(id string) error {
	epic, err := c.memory.ShowEpic(context.Background(), id)
	if err != nil {
		return err
	}
	if len(epic.Stories) == 0 {
		return fmt.Errorf("expected %s to have stories, found none", id)
	}
	for _, story := range epic.Stories {
		if story.Status == apptest.StatusDeferred {
			return fmt.Errorf("expected %s to be released, still held", story.Story.ID)
		}
	}
	return nil
}

// beadIsCommentedARELEASEDWithTxid checks bead's last comment is the RELEASED
// comment a Release tap leaves, naming txid.
func (c *posternInboxContext) beadIsCommentedARELEASEDWithTxid(bead, txid string) error {
	comments, err := c.memory.StoryComments(context.Background(), bead)
	if err != nil {
		return err
	}
	if len(comments) == 0 {
		return fmt.Errorf("expected a comment on %s, found none", bead)
	}
	got := comments[len(comments)-1].Text
	want := fmt.Sprintf("RELEASED by mw on the Governor's Release tap (txid %s)", txid)
	if !strings.Contains(got, want) {
		return fmt.Errorf("expected the last comment on %s to contain %q, got %q", bead, want, got)
	}
	return nil
}

func (c *posternInboxContext) itDidNotPrintText(text string) error {
	if strings.Contains(c.out.String(), text) {
		return fmt.Errorf("expected the output not to contain %q, got:\n%s", text, c.out.String())
	}
	return nil
}

func (c *posternInboxContext) noMailWasSent() error {
	if c.mailbox.Writes() != 0 {
		return fmt.Errorf("expected no mail sent, but %d write(s) were made", c.mailbox.Writes())
	}
	return nil
}

func (c *posternInboxContext) itPrintedText(want string) error {
	if !strings.Contains(c.out.String(), want) {
		return fmt.Errorf("expected the output to contain %q, got:\n%s", want, c.out.String())
	}
	return nil
}

// attachmentPath is the path a "carrying a screenshot" step's image should
// have been written to: the attachment directory, its txid, and .png (the
// stand-in image's own mime).
func (c *posternInboxContext) attachmentPath() string {
	return filepath.Join(c.attachDir, c.attachTxid+".png")
}

func (c *posternInboxContext) theDecryptedImageIsWrittenUnderTheAttachmentDirectory() error {
	if err := c.itSucceeds(); err != nil {
		return err
	}
	path := c.attachmentPath()
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("expected %s to exist: %w", path, err)
	}
	if info.Mode().Perm() != 0o600 {
		return fmt.Errorf("expected %s to be mode 0600, got %o", path, info.Mode().Perm())
	}
	written, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if !bytes.Equal(written, c.attachImage) {
		return fmt.Errorf("expected %s to hold the decrypted image, got %d bytes", path, len(written))
	}
	if !strings.Contains(c.out.String(), path) {
		return fmt.Errorf("expected the output to print %s, got:\n%s", path, c.out.String())
	}
	return nil
}

func (c *posternInboxContext) beadsLastCommentNamesTheDecryptedImagesPath(bead string) error {
	comments, err := c.memory.StoryComments(context.Background(), bead)
	if err != nil {
		return err
	}
	if len(comments) == 0 {
		return fmt.Errorf("expected a comment on %s, found none", bead)
	}
	want := fmt.Sprintf(" [image: %s]", c.attachmentPath())
	got := comments[len(comments)-1].Text
	if !strings.HasSuffix(got, want) {
		return fmt.Errorf("expected the comment on %s to end with %q, got %q", bead, want, got)
	}
	return nil
}

// aPosternActionOnBeadFrom adds a record whose plaintext is postern's
// docs/protocol.md section 13 action, genuinely sent by from.
func (c *posternInboxContext) aPosternActionOnBeadFrom(action, bead, from, txid string) error {
	return c.addAction(from, txid, map[string]any{"action": action, "bead": bead})
}

func (c *posternInboxContext) aPosternPriorityActionFromTheGovernor(priority int, bead, txid string) error {
	return c.addAction(c.governorKey, txid, map[string]any{"action": "priority", "bead": bead, "priority": priority})
}

func (c *posternInboxContext) addAction(from, txid string, action map[string]any) error {
	text, err := json.Marshal(action)
	if err != nil {
		return err
	}
	c.cipher.From = from
	ciphertext, err := c.cipher.Encrypt(c.pubKey, string(text))
	if err != nil {
		return err
	}
	c.backend.AddRecord(application.PosternRecord{
		Txid: txid, Class: "message", From: from, To: c.pubKey, Ts: posternReplyStamp, Ciphertext: ciphertext,
	})
	return nil
}

func (c *posternInboxContext) beadIsClaimedBy(bead, holder string) error {
	return c.memory.ClaimAs(bead, holder, posternReplyStamp.Add(time.Hour))
}

func (c *posternInboxContext) mwPosternInboxApplyIsRun() error {
	_, c.err = c.inbox().Apply(context.Background())
	return nil
}

func (c *posternInboxContext) beadNowStands(bead, status string) error {
	if err := c.itSucceeds(); err != nil {
		return err
	}
	detail, err := c.memory.ShowStory(context.Background(), bead)
	if err != nil {
		return err
	}
	if detail.Status != status {
		return fmt.Errorf("expected %s to stand %s, got %s", bead, status, detail.Status)
	}
	return nil
}

func (c *posternInboxContext) beadNowHasPriority(bead string, priority int) error {
	detail, err := c.memory.ShowStory(context.Background(), bead)
	if err != nil {
		return err
	}
	if detail.Priority != priority {
		return fmt.Errorf("expected %s at priority %d, got %d", bead, priority, detail.Priority)
	}
	return nil
}

func (c *posternInboxContext) beadsLastCommentReads(bead, want string) error {
	comments, err := c.memory.StoryComments(context.Background(), bead)
	if err != nil {
		return err
	}
	if len(comments) == 0 || comments[len(comments)-1].Text != want {
		return fmt.Errorf("expected %s's last comment %q, got %+v", bead, want, comments)
	}
	return nil
}

func (c *posternInboxContext) theTxidIsMarkedApplied(txid string) error {
	note, err := c.memory.Note(context.Background(), application.PosternAppliedKey(txid))
	if err != nil {
		return err
	}
	if !strings.HasPrefix(note, "applied ") {
		return fmt.Errorf("expected %s marked applied, got %q", txid, note)
	}
	return nil
}

func (c *posternInboxContext) thePosternInboxCursorHasNotMoved() error {
	saved, err := c.memory.Note(context.Background(), application.PosternCursorKey)
	if err != nil {
		return err
	}
	if saved != "" {
		return fmt.Errorf("expected the cursor not to move, it reads %q", saved)
	}
	return nil
}
