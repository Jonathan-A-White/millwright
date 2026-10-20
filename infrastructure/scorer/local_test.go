package scorer_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
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

func TestLocalKeepsTheStartAndEndOfEachWordAndNullForTheWordsNotHeard(t *testing.T) {
	needFfmpeg(t)
	answer := `{"engine":"local","words":[
 {"text":"the","expected_phonemes":["DH","AH"],"produced_phonemes":["DH","AH"],"error":"none","accuracy":96,"self_corrected":false,"start":0.12,"end":0.3},
 {"text":"cat","expected_phonemes":["K","AE","T"],"produced_phonemes":[],"error":"omission","accuracy":0,"self_corrected":false,"start":null,"end":null},
 {"text":"sat","expected_phonemes":["S","AE","T"],"produced_phonemes":[],"error":"not_reached","accuracy":0,"self_corrected":false}],
 "accuracy":48,"seconds":1.5}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(answer))
	}))
	defer srv.Close()

	result, err := scorer.NewLocal(srv.URL).Score(context.Background(), clip(t), "audio/webm", "the cat sat", "en")
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	the := result.Words[0]
	if the.Start == nil || the.End == nil || *the.Start != 0.12 || *the.End != 0.3 {
		t.Errorf("expected the first word to keep 0.12 to 0.3, got %+v", the)
	}
	for _, w := range result.Words[1:] {
		if w.Start != nil || w.End != nil {
			t.Errorf("expected no times for %q, got %v to %v", w.Text, w.Start, w.End)
		}
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
	if strings.Contains(string(out), `phonemes":null`) {
		t.Errorf("expected no null list in %s: a word with no phonemes has empty lists", out)
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

func TestLocalPostsTheSegmentsOfAWovenReadingInOrder(t *testing.T) {
	needFfmpeg(t)
	var raw map[string]json.RawMessage
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&raw)
		_, _ = w.Write([]byte(goodAnswer))
	}))
	defer srv.Close()

	segments := []application.ScoreSegment{{Text: "In the beginning was", Lang: "en"}, {Text: "ὁ λόγος", Lang: "el"}}
	result, err := scorer.NewLocal(srv.URL).ScoreSegments(context.Background(), clip(t), "audio/webm", segments)
	if err != nil {
		t.Fatalf("ScoreSegments: %v", err)
	}
	if len(result.Words) != 3 {
		t.Errorf("unexpected result %+v", result)
	}
	var got []application.ScoreSegment
	if err := json.Unmarshal(raw["segments"], &got); err != nil || len(got) != 2 || got[0] != segments[0] || got[1] != segments[1] {
		t.Errorf("segments were posted as %s (%v), expected %+v", raw["segments"], err, segments)
	}
	if _, has := raw["audio_wav_base64"]; !has {
		t.Errorf("the request carried no audio_wav_base64: %v", raw)
	}
	if _, has := raw["lang"]; has {
		t.Errorf("a woven request names no lang of its own, got %s", raw["lang"])
	}
}

func TestLocalPostsNoSegmentsForAReadingInOneLanguage(t *testing.T) {
	needFfmpeg(t)
	var raw map[string]json.RawMessage
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&raw)
		_, _ = w.Write([]byte(goodAnswer))
	}))
	defer srv.Close()

	if _, err := scorer.NewLocal(srv.URL).Score(context.Background(), clip(t), "audio/webm", "the cat sat", "en"); err != nil {
		t.Fatalf("Score: %v", err)
	}
	if _, has := raw["segments"]; has {
		t.Errorf("a one-language request carried segments: %s", raw["segments"])
	}
}

// TestLocalTimeoutGrowsWithTheClip: the local scorer's limit is the old two
// minutes plus half a second for each second of audio, never past the grind's
// limit less the model step's margin (mw-gq6.660).
func TestLocalTimeoutGrowsWithTheClip(t *testing.T) {
	for _, c := range []struct {
		audio float64
		want  time.Duration
	}{
		{0, 120 * time.Second},
		{20, 130 * time.Second},
		{300, 270 * time.Second},
		{600, 420 * time.Second},
		{3600, scorer.MaxLocalTimeout},
	} {
		if got := scorer.LocalTimeoutFor(c.audio); got != c.want {
			t.Errorf("%v s of audio: limit %s, want %s", c.audio, got, c.want)
		}
	}
	if scorer.MaxLocalTimeout != application.DefaultGristTimeout-scorer.LocalTimeoutMargin {
		t.Errorf("the cap %s is not the grind's limit less the margin %s", scorer.MaxLocalTimeout, scorer.LocalTimeoutMargin)
	}
}

// TestLocalTimeoutIsATypedError: a request that runs out of time returns the
// application.ScoreTimeout the grind reports, with the limit it waited (mw-gq6.660).
func TestLocalTimeoutIsATypedError(t *testing.T) {
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
	var out *application.ScoreTimeout
	if !errors.As(err, &out) || out.Limit != 100*time.Millisecond {
		t.Fatalf("expected a *application.ScoreTimeout of 100ms, got %v", err)
	}
}
