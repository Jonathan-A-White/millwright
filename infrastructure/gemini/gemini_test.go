package gemini_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/gemini"
)

const sentinelKey = "AIzaSy-SENTINEL-gemini-key-0123456789"

var pngBytes = []byte("\x89PNG\r\n\x1a\nnot really a picture")

// fakeAPI is the Gemini API in memory: it records each request, and answers
// each with status and body. An unkind one: its errors quote the key back.
type fakeAPI struct {
	mu      sync.Mutex
	status  int
	body    string
	reqs    []*http.Request
	bodies  []string
	quoteIt bool
}

func (f *fakeAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	raw, _ := io.ReadAll(r.Body)
	f.reqs = append(f.reqs, r)
	f.bodies = append(f.bodies, string(raw))
	w.WriteHeader(f.status)
	body := f.body
	if f.quoteIt {
		body = strings.ReplaceAll(body, "KEY", r.Header.Get("x-goog-api-key"))
	}
	io.WriteString(w, body)
}

func serve(t *testing.T, status int, body string) (*fakeAPI, *gemini.Maker) {
	t.Helper()
	api := &fakeAPI{status: status, body: body}
	srv := httptest.NewServer(api)
	t.Cleanup(srv.Close)
	maker, err := gemini.New(srv.URL, "test-image-model", sentinelKey)
	if err != nil {
		t.Fatal(err)
	}
	return api, maker
}

func okBody() string {
	return `{"candidates":[{"content":{"parts":[{"text":"here"},{"inlineData":{"mimeType":"image/png","data":"` +
		base64.StdEncoding.EncodeToString(pngBytes) + `"}}]},"finishReason":"STOP"}]}`
}

func TestMakeSendsOneRequestWithTheKeyInAHeaderAndNeverInTheURL(t *testing.T) {
	api, maker := serve(t, 200, okBody())
	got, err := maker.Make(context.Background(), "a lighthouse at dusk")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.Data, pngBytes) || got.MimeType != "image/png" {
		t.Errorf("got %q %q, want the picture the API sent", got.Data, got.MimeType)
	}
	if len(api.reqs) != 1 {
		t.Fatalf("%d requests, want one", len(api.reqs))
	}
	r := api.reqs[0]
	if r.Method != http.MethodPost || r.URL.Path != "/v1beta/models/test-image-model:generateContent" {
		t.Errorf("request = %s %s", r.Method, r.URL.Path)
	}
	if r.Header.Get("x-goog-api-key") != sentinelKey {
		t.Errorf("the key is not in the x-goog-api-key header: %v", r.Header)
	}
	if r.URL.RawQuery != "" || strings.Contains(r.URL.String(), sentinelKey) {
		t.Errorf("the URL carries a query or the key: %s", r.URL)
	}
	if !strings.Contains(api.bodies[0], "a lighthouse at dusk") || strings.Contains(api.bodies[0], sentinelKey) {
		t.Errorf("the body should hold the prompt and not the key: %s", api.bodies[0])
	}
}

func TestMakeIsOneRequestEvenWhenTheAPIFailsOrRefuses(t *testing.T) {
	for _, status := range []int{429, 500, 503, 403, 400} {
		api, maker := serve(t, status, `{"error":{"code":1,"message":"nope","status":"X"}}`)
		if _, err := maker.Make(context.Background(), "p"); err == nil {
			t.Errorf("%d: no error", status)
		}
		if len(api.reqs) != 1 {
			t.Errorf("%d: %d requests, want exactly one: no retry", status, len(api.reqs))
		}
	}
}

func TestAQuotaOrBillingRefusalIsAnErrQuotaNamingBilling(t *testing.T) {
	cases := map[string]struct {
		status int
		body   string
	}{
		"429":            {429, `{"error":{"code":429,"message":"You exceeded your current quota, please check your plan and billing details.","status":"RESOURCE_EXHAUSTED"}}`},
		"403 billing":    {403, `{"error":{"code":403,"message":"Billing is not enabled for this project","status":"PERMISSION_DENIED"}}`},
		"400 free tier":  {400, `{"error":{"code":400,"message":"Image generation is not available on the free tier","status":"FAILED_PRECONDITION"}}`},
		"resource empty": {500, `{"error":{"code":500,"message":"x","status":"RESOURCE_EXHAUSTED"}}`},
	}
	for name, c := range cases {
		_, maker := serve(t, c.status, c.body)
		_, err := maker.Make(context.Background(), "p")
		if !errors.Is(err, application.ErrImageQuota) {
			t.Errorf("%s: err = %v, want one wrapping ErrImageQuota", name, err)
		}
	}
	_, maker := serve(t, 500, `{"error":{"code":500,"message":"internal","status":"INTERNAL"}}`)
	if _, err := maker.Make(context.Background(), "p"); errors.Is(err, application.ErrImageQuota) {
		t.Errorf("a plain server error is not a quota error: %v", err)
	}
}

func TestTheKeyIsNowhereInAnErrorTheAPIOrTheNetworkGives(t *testing.T) {
	api, maker := serve(t, 401, `{"error":{"code":401,"message":"bad key KEY","status":"UNAUTHENTICATED"}}`)
	api.quoteIt = true
	_, err := maker.Make(context.Background(), "p")
	if err == nil || strings.Contains(err.Error(), sentinelKey) || !strings.Contains(err.Error(), "HTTP 401") {
		t.Errorf("err = %v, want one with the status and not the key", err)
	}
	// A server that is not there: the transport's error names the URL.
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	dead, _ := gemini.New(url, "m", sentinelKey)
	if _, err := dead.Make(context.Background(), "p"); err == nil || strings.Contains(err.Error(), sentinelKey) {
		t.Errorf("err = %v, want one without the key", err)
	}
}

func TestAReplyWithNoPictureSaysWhyAndKeepsTheKeyOut(t *testing.T) {
	for name, body := range map[string]string{
		"blocked":  `{"promptFeedback":{"blockReason":"SAFETY"}}`,
		"text":     `{"candidates":[{"content":{"parts":[{"text":"I cannot draw that"}]},"finishReason":"STOP"}]}`,
		"no reply": `{}`,
		"not json": `<html>`,
	} {
		_, maker := serve(t, 200, body)
		_, err := maker.Make(context.Background(), "p")
		if err == nil || !strings.Contains(err.Error(), "no picture") {
			t.Errorf("%s: err = %v, want one saying there was no picture", name, err)
		}
	}
	_, maker := serve(t, 200, `{"promptFeedback":{"blockReason":"SAFETY"}}`)
	if _, err := maker.Make(context.Background(), "p"); err == nil || !strings.Contains(err.Error(), "SAFETY") {
		t.Errorf("err = %v, want the block reason", err)
	}
}

func TestARedirectIsNotFollowedSoTheKeyGoesNowhereElse(t *testing.T) {
	other := &fakeAPI{status: 200, body: okBody()}
	elsewhere := httptest.NewServer(other)
	defer elsewhere.Close()
	srv := httptest.NewServer(http.RedirectHandler(elsewhere.URL, http.StatusTemporaryRedirect))
	defer srv.Close()
	maker, _ := gemini.New(srv.URL, "m", sentinelKey)
	if _, err := maker.Make(context.Background(), "p"); err == nil {
		t.Error("a redirect was followed")
	}
	if len(other.reqs) != 0 {
		t.Error("the key was sent to the place the redirect named")
	}
}

func TestNewRefusesAModelThatCouldChangeThePathAndAnEmptyKey(t *testing.T) {
	for _, model := range []string{"", "a/b", "m?x=1", "m:generateContent", "../x", "m m"} {
		if _, err := gemini.New("https://x.test", model, sentinelKey); err == nil {
			t.Errorf("model %q: no error", model)
		}
	}
	if _, err := gemini.New("https://x.test", "gemini-2.5-flash-image", "  "); err == nil {
		t.Error("an empty key: no error")
	}
}

func TestNoSourceHereTouchesBillingOrRetries(t *testing.T) {
	src, err := os.ReadFile("gemini.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, banned := range []string{"cloudbilling", "billingAccounts", "serviceusage", "time.Sleep", "for attempt", "retry("} {
		if strings.Contains(string(src), banned) {
			t.Errorf("gemini.go names %q: mw image makes one request and never touches billing", banned)
		}
	}
}
