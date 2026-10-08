package steps

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"

	ec "github.com/bsv-blockchain/go-sdk/primitives/ec"

	"github.com/Jonathan-A-White/millwright/infrastructure/postern"

	"github.com/cucumber/godog"
)

// posternSignatureContext is a postern backend that checks, on its own,
// every signed request against the v2 message, and the client talking to it.
type posternSignatureContext struct {
	mu        sync.Mutex
	server    *httptest.Server
	home      string
	client    *postern.HTTP
	refuse    int
	challenge int
	signed    []string // "METHOD target" of each signed request
	verified  int
	problems  []string
	err       error
}

// InitializePosternRequestSignatureScenario registers the steps of
// features/postern_request_signature.feature.
func InitializePosternRequestSignatureScenario(ctx *godog.ScenarioContext) {
	c := &posternSignatureContext{}

	ctx.After(func(sc context.Context, _ *godog.Scenario, _ error) (context.Context, error) {
		if c.server != nil {
			c.server.Close()
		}
		os.RemoveAll(c.home)
		*c = posternSignatureContext{}
		return sc, nil
	})

	ctx.Given(`^a postern backend that checks every signed request against the v2 message$`, c.aCheckingBackend)
	ctx.Given(`^the postern backend refuses its first (\d+) signed requests? with the reason "([^"]*)"$`, c.theBackendRefuses)
	ctx.When(`^mw delivers a message record to the postern backend$`, c.deliver)
	ctx.When(`^mw reads the postern backend's messages since (\d+)$`, c.readSince)
	ctx.Then(`^the delivery succeeds$`, c.noError)
	ctx.Then(`^the read succeeds$`, c.noError)
	ctx.Then(`^the delivery fails naming the 401$`, c.failedWith401)
	ctx.Then(`^the postern backend saw (\d+) signed requests?, verified as v2$`, c.sawVerified)
	ctx.Then(`^the postern backend issued (\d+) challenges$`, c.issued)
	ctx.Then(`^the signed request was "([^"]*)"$`, c.theSignedRequestWas)
}

func (c *posternSignatureContext) aCheckingBackend() error {
	home, err := os.MkdirTemp("", "mw-postern-signature-")
	if err != nil {
		return err
	}
	c.home = home
	keys := postern.New(filepath.Join(home, "postern.key"))
	if err := keys.Generate(); err != nil {
		return err
	}
	c.server = httptest.NewServer(http.HandlerFunc(c.handle))
	c.client = postern.NewHTTP(c.server.URL, keys)
	return nil
}

func (c *posternSignatureContext) theBackendRefuses(n int, reason string) error {
	if reason != "nonce" {
		return fmt.Errorf("only the reason \"nonce\" is modelled, not %q", reason)
	}
	c.refuse = n
	return nil
}

// handle answers a challenge with a fresh nonce and checks any other
// request's header with the message built here, apart from the client's.
func (c *posternSignatureContext) handle(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	c.mu.Lock()
	defer c.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	if r.Method == http.MethodGet && r.URL.Path == "/api/challenge" {
		c.challenge++
		fmt.Fprintf(w, `{"nonce":"nonce-%d"}`, c.challenge)
		return
	}
	c.signed = append(c.signed, r.Method+" "+r.RequestURI)
	if problem := verifyV2(r, body); problem != "" {
		c.problems = append(c.problems, problem)
	} else {
		c.verified++
	}
	if len(c.signed) <= c.refuse {
		w.WriteHeader(http.StatusUnauthorized)
		io.WriteString(w, `{"error":"nonce unknown, expired or used","reason":"nonce"}`)
		return
	}
	io.WriteString(w, `{"txid":"direct:ab12","seq":1,"records":[],"next":3}`)
}

// verifyV2 is "" when r carries a Postern2 header whose signature verifies
// over the v2 message of r as received, else what was wrong.
func verifyV2(r *http.Request, body []byte) string {
	header, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Postern2 ")
	if !ok {
		return fmt.Sprintf("not a Postern2 header: %q", r.Header.Get("Authorization"))
	}
	parts := strings.Split(header, ":")
	if len(parts) != 3 {
		return fmt.Sprintf("not pubkey:nonce:sig: %q", header)
	}
	sigBytes, err := hex.DecodeString(parts[2])
	if err != nil {
		return "the signature is not hex"
	}
	sig, err := ec.ParseDERSignature(sigBytes)
	if err != nil {
		return "the signature is not DER"
	}
	pub, err := ec.PublicKeyFromString(parts[0])
	if err != nil {
		return "the public key does not parse"
	}
	bodyHash := sha256.Sum256(body)
	message := strings.Join([]string{"postern-v2", r.Method, r.RequestURI, hex.EncodeToString(bodyHash[:]), parts[1]}, "\n")
	hash := sha256.Sum256([]byte(message))
	if !sig.Verify(hash[:], pub) {
		return fmt.Sprintf("the signature does not verify over %q", message)
	}
	return ""
}

func (c *posternSignatureContext) deliver() error {
	_, c.err = c.client.Deliver(context.Background(), []byte(`{"v":1}`))
	return nil
}

func (c *posternSignatureContext) readSince(since int) error {
	_, c.err = c.client.Messages(context.Background(), int64(since))
	return nil
}

func (c *posternSignatureContext) noError() error {
	if c.err != nil {
		return fmt.Errorf("expected the call to succeed, got: %v", c.err)
	}
	return nil
}

func (c *posternSignatureContext) failedWith401() error {
	if c.err == nil || !strings.Contains(c.err.Error(), "401") {
		return fmt.Errorf("expected an error naming the 401, got: %v", c.err)
	}
	return nil
}

func (c *posternSignatureContext) sawVerified(n int) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.signed) != n || c.verified != n {
		return fmt.Errorf("expected %d signed requests all verified, saw %d, %d verified; problems: %v", n, len(c.signed), c.verified, c.problems)
	}
	return nil
}

func (c *posternSignatureContext) issued(n int) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.challenge != n {
		return fmt.Errorf("expected %d challenges issued, got %d", n, c.challenge)
	}
	return nil
}

func (c *posternSignatureContext) theSignedRequestWas(want string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.signed) != 1 || c.signed[0] != want {
		return fmt.Errorf("expected the one signed request %q, got %v", want, c.signed)
	}
	return nil
}
