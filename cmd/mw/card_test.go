package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain"
	"github.com/Jonathan-A-White/millwright/infrastructure/postern"
)

// recordingCipher keeps every plaintext it is asked to seal, so a test reads
// back what a card record carries.
type recordingCipher struct{ sealed *[]string }

func (c recordingCipher) Encrypt(_ string, text string) (string, error) {
	*c.sealed = append(*c.sealed, text)
	return "c2VhbGVk", nil
}
func (c recordingCipher) EncryptBytes(to string, plain []byte) (string, error) {
	return c.Encrypt(to, string(plain))
}
func (c recordingCipher) Decrypt(string, string) (string, string, error) {
	return "", "", fmt.Errorf("recordingCipher does not decrypt")
}

// cardBackend stands in for the postern backend's POST /api/messages and
// GET /api/prompts/top5, answering the challenge every call is proven with.
func cardBackend(t *testing.T) (url string, delivered *int) {
	t.Helper()
	var count, challenges int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/challenge":
			challenges++
			fmt.Fprintf(w, `{"nonce":"nonce-%d"}`, challenges)
		case r.Method == http.MethodPost && r.URL.Path == "/api/messages":
			io.Copy(io.Discard, r.Body)
			count++
			w.WriteHeader(http.StatusCreated)
			fmt.Fprintf(w, `{"txid":"direct:card-%d","seq":%d}`, count, count)
		case r.Method == http.MethodGet && r.URL.Path == "/api/prompts/top5":
			io.WriteString(w, `{"name":"top5","summary":"The five next","signature":[],"body":"Name the top five."}`)
		default:
			w.WriteHeader(http.StatusNotFound)
			io.WriteString(w, `{"error":"no such route"}`)
		}
	}))
	t.Cleanup(srv.Close)
	return srv.URL, &count
}

// cardHome is a home configured against cardBackend, with the cipher swapped
// for one that keeps what it seals.
func cardHome(t *testing.T) (sealed *[]string, delivered *int) {
	t.Helper()
	f := loadPosternRecordFixture(t)
	url, delivered := cardBackend(t)
	posternHome(t, url, f.SenderWIF, f.RecipientPubKey)
	t.Setenv("MW_POSTERN_CHANNEL", "direct") // the backend here takes direct records; the default is chain
	sealed = &[]string{}
	realCipher := posternCipher
	t.Cleanup(func() { posternCipher = realCipher })
	posternCipher = func(*postern.KeyFile) application.Cipher { return recordingCipher{sealed: sealed} }
	return sealed, delivered
}

func TestCardSendPostsTheItemsGivenAsFlags(t *testing.T) {
	sealed, _ := cardHome(t)
	out, err := runMw(t, "card", "send", "--title", "Top 5", "--item", "Approve mw-a|mw-a|", "--item", "Check the map|mw-b|mw-b:landed", "--bead-channel", "mw-x")
	if err != nil {
		t.Fatalf("mw card send failed: %v\n%s", err, out)
	}
	if first, _, _ := strings.Cut(out, "\n"); first != "direct:card-1" {
		t.Fatalf("expected the txid printed first, got:\n%s", out)
	}
	if len(*sealed) != 1 {
		t.Fatalf("expected one record sealed, got %d", len(*sealed))
	}
	var card domain.Card
	if err := json.Unmarshal([]byte((*sealed)[0]), &card); err != nil {
		t.Fatal(err)
	}
	if card.Title != "Top 5" || card.Thread == nil || card.Thread.Bead != "mw-x" || len(card.Items) != 2 ||
		card.Items[1].Expect == nil || card.Items[1].Expect.State != domain.ExpectLanded {
		t.Fatalf("expected the card Top 5 in mw-x's channel, item 2 expecting landed, got %s", (*sealed)[0])
	}
}

func TestCardUpdateTakesLinksAsTwoWordsOrOne(t *testing.T) {
	sealed, _ := cardHome(t)
	out, err := runMw(t, "card", "update", "direct:abc", "--item", "3. Release mw-c", "--link", "2", "mw-b.1", "--link", "4:mw-d", "--tick", "1")
	if err != nil {
		t.Fatalf("mw card update failed: %v\n%s", err, out)
	}
	var update domain.CardUpdate
	if err := json.Unmarshal([]byte((*sealed)[0]), &update); err != nil {
		t.Fatal(err)
	}
	if update.Re != "direct:abc" || len(update.Items) != 1 || update.Items[0].N != 3 ||
		strings.Join(update.Links[2], ",") != "mw-b.1" || strings.Join(update.Links[4], ",") != "mw-d" ||
		len(update.Tick) != 1 || update.Tick[0] != 1 {
		t.Fatalf("expected re direct:abc, item 3, links 2 and 4 and item 1 ticked, got %s", (*sealed)[0])
	}
}

func TestCardUpdateRefusesALinkWithNoBead(t *testing.T) {
	_, delivered := cardHome(t)
	out, err := runMw(t, "card", "update", "direct:abc", "--link", "2")
	if err == nil || !strings.Contains(err.Error(), "--link 2") {
		t.Fatalf("expected --link 2 with no bead refused, got %v\n%s", err, out)
	}
	if *delivered != 0 {
		t.Fatalf("expected nothing delivered, got %d", *delivered)
	}
}

func TestPromptRunCardSendsTheCardWithThePromptsName(t *testing.T) {
	sealed, _ := cardHome(t)
	out, err := runMw(t, "prompt", "run", "top5", "--card", "--title", "Top 5", "--item", "Approve mw-a")
	if err != nil {
		t.Fatalf("mw prompt run --card failed: %v\n%s", err, out)
	}
	if strings.Contains(out, "FACTS") || !strings.HasPrefix(out, "direct:card-1\n") {
		t.Fatalf("expected the card's txid printed and no facts, got:\n%s", out)
	}
	var card domain.Card
	if err := json.Unmarshal([]byte((*sealed)[0]), &card); err != nil {
		t.Fatal(err)
	}
	if card.Prompt != "top5" || card.Title != "Top 5" || len(card.Items) != 1 ||
		card.Items[0].Expect == nil || *card.Items[0].Expect != (domain.Expectation{Bead: "mw-a", State: domain.ExpectAnswered}) {
		t.Fatalf("expected the card to record top5 and item 1 to expect mw-a answered, got %s", (*sealed)[0])
	}
}
