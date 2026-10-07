package scorer_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/scorer"
)

const goodAnswer = `{"engine":"local","words":[
 {"text":"the","expected_phonemes":["DH","AH"],"produced_phonemes":["DH","AH"],"error":"none","accuracy":96,"self_corrected":false},
 {"text":"cat","expected_phonemes":["K","AE","T"],"produced_phonemes":["K","AH","T"],"error":"mispronunciation","accuracy":61,"self_corrected":false},
 {"text":"sat","expected_phonemes":["S","AE","T"],"produced_phonemes":[],"error":"omission","accuracy":0,"self_corrected":false}],
 "accuracy":52,"seconds":1.5}`

func needFfmpeg(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg is not installed: the local client converts audio with it, so it is not tried here")
	}
}

func clip(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("../../testdata/clip.webm")
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestLocalPostsTheWavAndTargetAndReadsTheResult(t *testing.T) {
	needFfmpeg(t)
	var got struct {
		TargetText string `json:"target_text"`
		Lang       string `json:"lang"`
		Audio      string `json:"audio_wav_base64"`
	}
	var path, method string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path, method = r.URL.Path, r.Method
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(goodAnswer))
	}))
	defer srv.Close()

	result, err := scorer.NewLocal(srv.URL).Score(context.Background(), clip(t), "audio/webm", "the cat sat", "en")
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if method != http.MethodPost || path != "/score" {
		t.Errorf("expected POST /score, got %s %s", method, path)
	}
	if got.TargetText != "the cat sat" || got.Lang != "en" {
		t.Errorf("request carried %+v", got)
	}
	wav, err := base64.StdEncoding.DecodeString(got.Audio)
	if err != nil || !strings.HasPrefix(string(wav), "RIFF") {
		t.Errorf("expected audio_wav_base64 to hold a WAV file, got %v %.12q", err, wav)
	}
	if len(result.Words) != 3 || result.Engine != "local" || result.Accuracy != 52 || result.Seconds != 1.5 {
		t.Fatalf("unexpected result %+v", result)
	}
	if w := result.Words[1]; w.Text != "cat" || w.Error != application.ErrMispronunciation || w.ProducedPhonemes[1] != "AH" {
		t.Errorf("unexpected second word %+v", w)
	}
}

func TestLocalNamesTheEngineWhenTheAnswerLeavesItOut(t *testing.T) {
	needFfmpeg(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"words":[{"text":"cat","error":"none","accuracy":90}],"accuracy":90,"seconds":1}`))
	}))
	defer srv.Close()
	result, err := scorer.NewLocal(srv.URL).Score(context.Background(), clip(t), "audio/webm", "cat", "en")
	if err != nil {
		t.Fatal(err)
	}
	if result.Engine != "local" {
		t.Errorf("expected the engine named local, got %q", result.Engine)
	}
	out, _ := json.Marshal(result)
	if strings.Contains(string(out), "null") {
		t.Errorf("expected no null in %s: a word with no phonemes has empty lists", out)
	}
}

func TestLocalRefusesAnAnswerWithAnUnknownErrorKind(t *testing.T) {
	needFfmpeg(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"words":[{"text":"cat","error":"stutter","accuracy":90}],"accuracy":90,"seconds":1}`))
	}))
	defer srv.Close()
	_, err := scorer.NewLocal(srv.URL).Score(context.Background(), clip(t), "audio/webm", "cat", "en")
	if err == nil || !strings.Contains(err.Error(), "stutter") {
		t.Fatalf("expected the unknown error kind named, got %v", err)
	}
}

func TestLocalReportsAnHTTPRefusalWithItsStatusAndBody(t *testing.T) {
	needFfmpeg(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "model not loaded", http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	_, err := scorer.NewLocal(srv.URL).Score(context.Background(), clip(t), "audio/webm", "cat", "en")
	if err == nil || !strings.Contains(err.Error(), "503") || !strings.Contains(err.Error(), "model not loaded") {
		t.Fatalf("expected the status and the body, got %v", err)
	}
}

func TestLocalConnectionRefusedNamesTheURLAndTheInstallDoc(t *testing.T) {
	needFfmpeg(t)
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	_, err := scorer.NewLocal(url).Score(context.Background(), clip(t), "audio/webm", "cat", "en")
	if err == nil || !strings.Contains(err.Error(), url) || !strings.Contains(err.Error(), "docs/scorers.md") {
		t.Fatalf("expected the url and docs/scorers.md named, got %v", err)
	}
}

func TestLocalTimesOut(t *testing.T) {
	needFfmpeg(t)
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(release)
	local := scorer.NewLocal(srv.URL)
	local.Timeout = 100 * time.Millisecond
	_, err := local.Score(context.Background(), clip(t), "audio/webm", "cat", "en")
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("expected a timeout, got %v", err)
	}
}
