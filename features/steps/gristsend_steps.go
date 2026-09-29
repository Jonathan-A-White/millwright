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
	"strings"
	"time"

	ec "github.com/bsv-blockchain/go-sdk/primitives/ec"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/infrastructure/postern"

	"github.com/cucumber/godog"
)

// gristSendContext is one terminal sending a grist: a throwaway key file
// (the vectors' cairnPhone), a fake backend that names the vectors' mill key,
// a reversible cipher, and a clock the wait between pages moves, so that no
// scenario waits in real time.
type gristSendContext struct {
	vectors gristVectors
	dir     string

	keys    *postern.KeyFile
	phone   string
	mill    string
	backend *apptest.FakePostern
	now     time.Time

	stdout, stderr bytes.Buffer
	exit           int
	report         application.GristSendReport
	requestFile    string
	photoFiles     []string
}

const gristSendTxid = "direct:the-grist"

// InitializeGristSendScenario registers the steps of features/grist_send.feature.
func InitializeGristSendScenario(ctx *godog.ScenarioContext) {
	c := &gristSendContext{}

	ctx.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
		*c = gristSendContext{now: gristNow}
		raw, err := os.ReadFile(gristVectorsPath)
		if err != nil {
			return ctx, err
		}
		if err := json.Unmarshal(raw, &c.vectors); err != nil {
			return ctx, fmt.Errorf("reading %s: %w", gristVectorsPath, err)
		}
		return ctx, nil
	})
	ctx.After(func(ctx context.Context, sc *godog.Scenario, err error) (context.Context, error) {
		if c.dir != "" {
			os.RemoveAll(c.dir)
		}
		return ctx, nil
	})

	ctx.Given(`^a backend that names the mill key$`, c.aBackendNamingTheMill)
	ctx.Given(`^the backend names no mill key$`, c.theBackendNamesNoMill)
	ctx.Given(`^a key file holding the phone's key$`, c.aKeyFileHoldingThePhonesKey)
	ctx.Given(`^the mill has answered the grist "([^"]*)" as "([^"]*)"$`, c.theMillHasAnswered)

	ctx.When(`^mw grist send sends a "([^"]*)" "([^"]*)" grist with (\d+) photos? and waits (\d+) minutes?$`, c.sendAndWait)
	ctx.When(`^mw grist send sends a "([^"]*)" "([^"]*)" grist with (\d+) photos? and does not wait$`, c.sendAndDoNotWait)

	ctx.Then(`^mw grist send left with (\d+)$`, c.leftWith)
	ctx.Then(`^mw grist send said "([^"]*)"$`, c.said)
	ctx.Then(`^the backend took (\d+) blobs? and (\d+) grist, all sealed to the mill key$`, c.theBackendTook)
	ctx.Then(`^the backend took nothing$`, c.theBackendTookNothing)
	ctx.Then(`^the grist is from the phone's key to the mill key and names "([^"]*)" "([^"]*)" "([^"]*)" with the photo and the request$`, c.theGristIs)
	ctx.Then(`^mw grist send printed the answer with the status "([^"]*)"$`, c.printedTheAnswer)
	ctx.Then(`^mw grist send printed only the id "([^"]*)"$`, c.printedOnlyTheID)
}

func (c *gristSendContext) aBackendNamingTheMill() error {
	c.backend = apptest.NewFakePostern()
	c.backend.NextTxid = gristSendTxid
	var err error
	if c.mill, _, err = keyFromVectors(c.vectors, "mill"); err != nil {
		return err
	}
	c.backend.Mine = application.PosternMe{Mill: c.mill, Network: "testnet"}
	return nil
}

func (c *gristSendContext) theBackendNamesNoMill() error {
	c.backend.Mine.Mill = ""
	return nil
}

// keyFromVectors is a key of the grist vectors by name: its public hex and
// its private key as the WIF a key file holds.
func keyFromVectors(v gristVectors, name string) (pub, wif string, err error) {
	key, ok := v.Keys[name]
	if !ok {
		return "", "", fmt.Errorf("the grist vectors have no key %q", name)
	}
	raw, err := hex.DecodeString(key.PrivateKeyHex)
	if err != nil {
		return "", "", err
	}
	priv, _ := ec.PrivateKeyFromBytes(raw)
	return key.PublicKeyHex, priv.WifPrefix(byte(ec.TestNet)), nil
}

func (c *gristSendContext) aKeyFileHoldingThePhonesKey() error {
	var err error
	if c.dir, err = os.MkdirTemp("", "mw-grist-send-"); err != nil {
		return err
	}
	pub, wif, err := keyFromVectors(c.vectors, "cairnPhone")
	if err != nil {
		return err
	}
	path := filepath.Join(c.dir, "phone.key")
	if err := os.WriteFile(path, []byte(wif+"\n"), 0o600); err != nil {
		return err
	}
	c.keys, c.phone = postern.New(path), pub
	return nil
}

// theMillHasAnswered puts the mill's answer to the grist txid, of the
// vectors' status, in the backend's index, sealed to the phone.
func (c *gristSendContext) theMillHasAnswered(txid, status string) error {
	answer, ok := c.vectors.Answers[status]
	if !ok {
		return fmt.Errorf("the grist vectors have no %q answer", status)
	}
	answer.Re = txid
	text, err := json.Marshal(answer)
	if err != nil {
		return err
	}
	sealed, err := (&apptest.FakeCipher{From: c.mill}).Encrypt(c.phone, string(text))
	if err != nil {
		return err
	}
	c.backend.AddRecord(application.PosternRecord{
		Txid: "direct:answer", Class: application.GristClass, From: c.mill, To: c.phone,
		Signer: c.mill, Ts: gristNow, Ciphertext: sealed,
	})
	return nil
}

func (c *gristSendContext) send(app, kind string, photos int, wait time.Duration) error {
	request := filepath.Join(c.dir, "request.json")
	if err := os.WriteFile(request, []byte(`{"schemaVersion": "1.1", "requestType": "sweep", "place": {"name": "Top drawer"}}`), 0o600); err != nil {
		return err
	}
	c.requestFile = request
	c.photoFiles = nil
	for i := 1; i <= photos; i++ {
		photo := filepath.Join(c.dir, fmt.Sprintf("photo-%d.jpg", i))
		if err := os.WriteFile(photo, []byte(fmt.Sprintf("jpeg bytes of photo %d", i)), 0o600); err != nil {
			return err
		}
		c.photoFiles = append(c.photoFiles, photo)
	}
	send := application.GristSend{
		Postern: c.backend, Cipher: &apptest.FakeCipher{From: c.phone}, Keys: c.keys,
		Now: func() time.Time { return c.now },
		Sleep: func(_ context.Context, d time.Duration) error {
			c.now = c.now.Add(d)
			return nil
		},
		Out: &c.stdout, Log: &c.stderr,
	}
	var err error
	c.report, err = send.Run(context.Background(), application.GristSendRequest{
		App: app, Kind: kind, RequestFile: request, Photos: c.photoFiles, Wait: wait,
	})
	if err != nil {
		fmt.Fprintln(&c.stderr, err)
	}
	c.exit = application.ExitStatus(err)
	return nil
}

func (c *gristSendContext) sendAndWait(app, kind string, photos, minutes int) error {
	return c.send(app, kind, photos, time.Duration(minutes)*time.Minute)
}

func (c *gristSendContext) sendAndDoNotWait(app, kind string, photos int) error {
	return c.send(app, kind, photos, 0)
}

func (c *gristSendContext) leftWith(status int) error {
	if c.exit != status {
		return fmt.Errorf("expected mw grist send to leave with %d, it left with %d\nstdout: %s\nstderr: %s", status, c.exit, c.stdout.String(), c.stderr.String())
	}
	return nil
}

func (c *gristSendContext) said(text string) error {
	if !strings.Contains(c.stderr.String(), text) {
		return fmt.Errorf("expected mw grist send to say %q, it said:\n%s", text, c.stderr.String())
	}
	return nil
}

// opened is a body the fake cipher sealed, opened: who it was sealed to, and
// by whom, and what it says.
func opened(sealed string) (to, from, text string, err error) {
	raw, err := base64.StdEncoding.DecodeString(sealed)
	if err != nil {
		return "", "", "", err
	}
	parts := strings.SplitN(string(raw), "\x00", 3)
	if len(parts) != 3 {
		return "", "", "", fmt.Errorf("not the fake cipher's")
	}
	return parts[0], parts[1], parts[2], nil
}

func (c *gristSendContext) theBackendTook(blobs, grist int) error {
	if got := len(c.backend.Uploaded()); got != blobs {
		return fmt.Errorf("expected %d blobs uploaded, got %d", blobs, got)
	}
	for _, body := range c.backend.Uploaded() {
		to, from, _, err := opened(base64.StdEncoding.EncodeToString(body))
		if err != nil || to != c.mill || from != c.phone {
			return fmt.Errorf("expected a blob sealed by the phone to the mill key, got to %q from %q: %v", to, from, err)
		}
	}
	if got := len(c.backend.Delivered()); got != grist {
		return fmt.Errorf("expected %d grist posted, got %d", grist, got)
	}
	for _, raw := range c.backend.Delivered() {
		var p application.PosternPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		if to, _, _, err := opened(p.Ct); err != nil || to != c.mill {
			return fmt.Errorf("expected a grist sealed to the mill key, got %q: %v", to, err)
		}
	}
	return nil
}

func (c *gristSendContext) theBackendTookNothing() error {
	return c.theBackendTook(0, 0)
}

func (c *gristSendContext) theGristIs(app, kind, v string) error {
	delivered := c.backend.Delivered()
	if len(delivered) != 1 {
		return fmt.Errorf("expected one grist posted, got %d", len(delivered))
	}
	var p application.PosternPayload
	if err := json.Unmarshal(delivered[0], &p); err != nil {
		return err
	}
	if p.V != 1 || p.Kind != application.PosternMessageKind || p.Class != application.GristClass || p.To != c.mill || p.From != c.phone {
		return fmt.Errorf("expected a version 1 grist record from %s to %s, got %+v", c.phone, c.mill, p)
	}
	_, _, text, err := opened(p.Ct)
	if err != nil {
		return err
	}
	var plain application.GristPlaintext
	if err := json.Unmarshal([]byte(text), &plain); err != nil {
		return fmt.Errorf("the grist's plaintext is not one: %w", err)
	}
	if plain.Grist != (application.GristName{App: app, Kind: kind, V: v}) {
		return fmt.Errorf("expected the grist to name %s/%s %s, it names %+v", app, kind, v, plain.Grist)
	}
	var input struct {
		Place struct {
			Name string `json:"name"`
		} `json:"place"`
	}
	if err := json.Unmarshal(plain.Input, &input); err != nil || input.Place.Name != "Top drawer" {
		return fmt.Errorf("expected the request carried as it is, got %s", plain.Input)
	}
	uploaded := c.backend.Uploaded()
	if len(plain.Attachments) != len(uploaded) || len(uploaded) == 0 {
		return fmt.Errorf("expected the grist to name the %d photos uploaded, it names %d", len(uploaded), len(plain.Attachments))
	}
	for i, a := range plain.Attachments {
		sum := sha256.Sum256(uploaded[i])
		if a.Hash != hex.EncodeToString(sum[:]) || a.Size != int64(len(uploaded[i])) || a.Mime != "image/jpeg" {
			return fmt.Errorf("attachment %d does not name the photo uploaded: %+v", i, a)
		}
	}
	return nil
}

func (c *gristSendContext) printedTheAnswer(status string) error {
	var answer application.GristAnswer
	if err := json.Unmarshal(c.stdout.Bytes(), &answer); err != nil {
		return fmt.Errorf("expected the answer printed as JSON, got %q: %w", c.stdout.String(), err)
	}
	if answer.Status != status || answer.Re != gristSendTxid {
		return fmt.Errorf("expected the answer %q to %s, printed %+v", status, gristSendTxid, answer)
	}
	if status == application.GristAnswered && len(answer.Answer) == 0 {
		return fmt.Errorf("expected the answer's own answer printed, got %s", c.stdout.String())
	}
	return nil
}

func (c *gristSendContext) printedOnlyTheID(txid string) error {
	if got := strings.TrimSpace(c.stdout.String()); got != txid {
		return fmt.Errorf("expected only %q printed, got %q", txid, got)
	}
	return nil
}
