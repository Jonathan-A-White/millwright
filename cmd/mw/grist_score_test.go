package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
)

const scoreAnswer = `{"engine":"local","words":[
 {"text":"the","expected_phonemes":["DH","AH"],"produced_phonemes":["DH","AH"],"error":"none","accuracy":96,"self_corrected":false},
 {"text":"cat","expected_phonemes":["K","AE","T"],"produced_phonemes":["K","AH","T"],"error":"mispronunciation","accuracy":61,"self_corrected":false},
 {"text":"sat","expected_phonemes":["S","AE","T"],"produced_phonemes":[],"error":"omission","accuracy":0,"self_corrected":false}],
 "accuracy":52,"seconds":1.5}`

// scorersHome points HOME at a directory whose config names the engines and
// the local scorer's url, and returns it.
func scorersHome(t *testing.T, engines, url string) {
	t.Helper()
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".config", "mw"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := "[scorers]\nengines = " + engines + "\nlocal_url = \"" + url + "\"\n"
	if err := os.WriteFile(filepath.Join(home, ".config", "mw", "config.toml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	for _, env := range []string{"MW_VAULT", "MW_HOST"} {
		t.Setenv(env, "")
	}
}

func runGristScore(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	out, errOut := &bytes.Buffer{}, &bytes.Buffer{}
	root := newRootCmd()
	root.SetOut(out)
	root.SetErr(errOut)
	root.SetArgs(append([]string{"grist"}, args...))
	err = root.Execute()
	return out.String(), errOut.String(), err
}

func TestGristScorePrintsTheReadingResultAsJSON(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg is not installed: the local engine converts audio with it, so mw grist score is not tried here")
	}
	var seen struct {
		TargetText string `json:"target_text"`
		Lang       string `json:"lang"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&seen)
		_, _ = w.Write([]byte(scoreAnswer))
	}))
	defer srv.Close()
	scorersHome(t, `["local"]`, srv.URL)

	out, errOut, err := runGristScore(t, "score", "--engine", "local", "--target", "the cat sat", "--audio", "../../testdata/clip.webm")
	if err != nil {
		t.Fatalf("mw grist score failed: %v\n%s", err, errOut)
	}
	var result application.ReadingResult
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("expected JSON on standard output, got %q: %v", out, err)
	}
	if len(result.Words) != 3 || result.Words[1].Error != application.ErrMispronunciation {
		t.Errorf("unexpected result %+v", result)
	}
	if seen.TargetText != "the cat sat" || seen.Lang != "en" {
		t.Errorf("the scorer was sent %+v: expected the target and the default lang en", seen)
	}
}

func TestGristScoreNamesTheConfiguredEnginesWhenOneIsMissing(t *testing.T) {
	scorersHome(t, `["local"]`, "http://127.0.0.1:1")
	out, _, err := runGristScore(t, "score", "--engine", "nosuch", "--target", "the cat sat", "--audio", "../../testdata/clip.webm")
	if err == nil || !strings.Contains(err.Error(), `"nosuch"`) || !strings.Contains(err.Error(), "local") {
		t.Fatalf("expected the engine refused and the configured ones named, got %v", err)
	}
	if out != "" {
		t.Errorf("expected nothing printed, got %q", out)
	}
}

func TestGristScoreWantsAnEngineATargetAndAnAudioFile(t *testing.T) {
	scorersHome(t, `["local"]`, "http://127.0.0.1:1")
	for _, args := range [][]string{
		{"score", "--target", "x", "--audio", "a.webm"},
		{"score", "--engine", "local", "--audio", "a.webm"},
		{"score", "--engine", "local", "--target", "x"},
	} {
		if _, _, err := runGristScore(t, args...); err == nil {
			t.Errorf("expected %v refused", args)
		}
	}
}

// azureHome points HOME at a config that names the engines and the azure key
// file and region; a keyFile of "" leaves azure_key_file out.
func azureHome(t *testing.T, keyFile string) {
	t.Helper()
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".config", "mw"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := "[scorers]\nengines = [\"local\", \"azure\"]\nazure_region = \"westus2\"\n"
	if keyFile != "" {
		cfg += "azure_key_file = \"" + keyFile + "\"\n"
	}
	if err := os.WriteFile(filepath.Join(home, ".config", "mw", "config.toml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	for _, env := range []string{"MW_VAULT", "MW_HOST"} {
		t.Setenv(env, "")
	}
}

func TestGristScoreAzurePrintsAzuresErrorTypes(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg is not installed: the azure engine converts audio with it, so mw grist score is not tried here")
	}
	answer, err := os.ReadFile("../../infrastructure/scorer/testdata/azure_answer.json")
	if err != nil {
		t.Fatal(err)
	}
	var key, language string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key, language = r.Header.Get("Ocp-Apim-Subscription-Key"), r.URL.Query().Get("language")
		_, _ = w.Write(answer)
	}))
	defer srv.Close()
	keyFile := filepath.Join(t.TempDir(), "azure.key")
	if err := os.WriteFile(keyFile, []byte("a-test-key\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	azureHome(t, keyFile)
	t.Setenv("MW_AZURE_ENDPOINT", srv.URL)

	out, errOut, err := runGristScore(t, "score", "--engine", "azure", "--target", "the cat sat", "--audio", "../../testdata/clip.webm")
	if err != nil {
		t.Fatalf("mw grist score failed: %v\n%s", err, errOut)
	}
	var result application.ReadingResult
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("expected JSON on standard output, got %q: %v", out, err)
	}
	var errors []string
	for _, w := range result.Words {
		errors = append(errors, w.Error)
	}
	if result.Engine != "azure" || strings.Join(errors, ",") != "none,mispronunciation,omission,insertion" {
		t.Errorf("expected azure's error types on the words, got %s: %+v", strings.Join(errors, ","), result)
	}
	if key != "a-test-key" || language != "en-US" {
		t.Errorf("the stub was sent key %q and language %q", key, language)
	}
	if strings.Contains(out, "a-test-key") {
		t.Errorf("the key is in the output")
	}
}

func TestGristScoreAzureWithoutAKeyFileNamesAzureKeyFile(t *testing.T) {
	azureHome(t, "")
	t.Setenv("MW_AZURE_ENDPOINT", "http://127.0.0.1:1")
	out, _, err := runGristScore(t, "score", "--engine", "azure", "--target", "the cat sat", "--audio", "../../testdata/clip.webm")
	if err == nil || !strings.Contains(err.Error(), "azure_key_file") {
		t.Fatalf("expected a refusal naming azure_key_file, got %v", err)
	}
	if out != "" {
		t.Errorf("expected nothing printed, got %q", out)
	}

	azureHome(t, filepath.Join(t.TempDir(), "absent.key"))
	_, _, err = runGristScore(t, "score", "--engine", "azure", "--target", "the cat sat", "--audio", "../../testdata/clip.webm")
	if err == nil || !strings.Contains(err.Error(), "azure_key_file") || !strings.Contains(err.Error(), "absent.key") {
		t.Fatalf("expected a refusal naming azure_key_file and the file, got %v", err)
	}
}
