package scorer_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/scorer"
)

const azureSecret = "s3cret-azure-key-0123456789"

func azureAnswer(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/azure_answer.json")
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// azureKey writes a key file in dir with the mode given and returns its path.
func azureKey(t *testing.T, mode os.FileMode) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "azure.key")
	if err := os.WriteFile(path, []byte(azureSecret+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
	return path
}

func azureAgainst(t *testing.T, srv *httptest.Server) *scorer.Azure {
	t.Helper()
	a := scorer.NewAzure(azureKey(t, 0o600), "westus2")
	a.Endpoint = srv.URL + "/stt"
	return a
}

func TestAzureSendsTheRequestAzureDocumentsAndMapsTheAnswer(t *testing.T) {
	needFfmpeg(t)
	var gotHeader http.Header
	var gotQuery, gotPath, gotMethod string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader, gotQuery, gotPath, gotMethod = r.Header.Clone(), r.URL.RawQuery, r.URL.Path, r.Method
		gotBody, _ = io.ReadAll(r.Body)
		_, _ = w.Write(azureAnswer(t))
	}))
	defer srv.Close()

	result, err := azureAgainst(t, srv).Score(context.Background(), clip(t), "audio/webm", "the cat sat", "en")
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if gotMethod != http.MethodPost || gotPath != "/stt" {
		t.Errorf("expected POST /stt, got %s %s", gotMethod, gotPath)
	}
	for _, want := range []string{"language=en-US", "format=detailed"} {
		if !strings.Contains(gotQuery, want) {
			t.Errorf("query %q lacks %q", gotQuery, want)
		}
	}
	if gotHeader.Get("Ocp-Apim-Subscription-Key") != azureSecret {
		t.Errorf("the key was not sent in Ocp-Apim-Subscription-Key: %q", gotHeader.Get("Ocp-Apim-Subscription-Key"))
	}
	if ct := gotHeader.Get("Content-Type"); ct != "audio/wav; codecs=audio/pcm; samplerate=16000" {
		t.Errorf("Content-Type = %q", ct)
	}
	if !strings.HasPrefix(string(gotBody), "RIFF") {
		t.Errorf("expected the body to be a WAV file, got %.12q", gotBody)
	}
	raw, err := base64.StdEncoding.DecodeString(gotHeader.Get("Pronunciation-Assessment"))
	if err != nil {
		t.Fatalf("Pronunciation-Assessment is not base64: %v", err)
	}
	var assess map[string]any
	if err := json.Unmarshal(raw, &assess); err != nil {
		t.Fatalf("Pronunciation-Assessment does not hold JSON: %v", err)
	}
	for key, want := range map[string]any{"ReferenceText": "the cat sat", "GradingSystem": "HundredMark", "Granularity": "Phoneme",
		"Dimension": "Comprehensive", "EnableMiscue": true, "PhonemeAlphabet": "IPA"} {
		if assess[key] != want {
			t.Errorf("Pronunciation-Assessment %s = %v, want %v", key, assess[key], want)
		}
	}
	if assess["NBestPhonemeCount"] == nil {
		t.Errorf("Pronunciation-Assessment asks for no NBestPhonemeCount, so no phoneme is heard: %v", assess)
	}

	if result.Engine != "azure" || len(result.Words) != 4 {
		t.Fatalf("unexpected result %+v", result)
	}
	if result.Accuracy != 72 || result.Seconds != 1.5 {
		t.Errorf("accuracy %d, seconds %v: want 72 and 1.5", result.Accuracy, result.Seconds)
	}
}

func TestParseAzureMapsWordsAndPhonemes(t *testing.T) {
	result, err := scorer.ParseAzure(azureAnswer(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := result.Validate(); err != nil {
		t.Fatalf("the mapped result breaks the contract: %v", err)
	}
	the, cat, sat, loudly := result.Words[0], result.Words[1], result.Words[2], result.Words[3]
	if the.Text != "the" || the.Error != application.ErrNone || the.Accuracy != 96 {
		t.Errorf("the: %+v", the)
	}
	if strings.Join(cat.ExpectedPhonemes, " ") != "k æ t" || strings.Join(cat.ProducedPhonemes, " ") != "k ʌ p" ||
		cat.Error != application.ErrMispronunciation || cat.Accuracy != 48 {
		t.Errorf("cat: %+v", cat)
	}
	if sat.Error != application.ErrOmission || len(sat.ProducedPhonemes) != 0 || strings.Join(sat.ExpectedPhonemes, " ") != "s æ t" {
		t.Errorf("sat: %+v", sat)
	}
	if loudly.Error != application.ErrInsertion || len(loudly.ExpectedPhonemes) != 0 || strings.Join(loudly.ProducedPhonemes, " ") != "l aʊ" {
		t.Errorf("loudly: %+v", loudly)
	}
	if the.SelfCorrected || cat.SelfCorrected {
		t.Errorf("azure does not report a self-correction")
	}
}

func TestParseAzureMapsEveryErrorType(t *testing.T) {
	for azure, want := range map[string]string{
		"None":             application.ErrNone,
		"Omission":         application.ErrOmission,
		"Insertion":        application.ErrInsertion,
		"Mispronunciation": application.ErrMispronunciation,
		"UnexpectedBreak":  application.ErrHesitation,
		"MissingBreak":     application.ErrNone,
		"Monotone":         application.ErrNone,
	} {
		answer := `{"RecognitionStatus":"Success","Duration":10000000,"NBest":[{"PronunciationAssessment":{"AccuracyScore":80},
		  "Words":[{"Word":"cat","PronunciationAssessment":{"AccuracyScore":80,"ErrorType":"` + azure + `"}}]}]}`
		result, err := scorer.ParseAzure([]byte(answer))
		if err != nil {
			t.Errorf("%s: %v", azure, err)
			continue
		}
		if got := result.Words[0].Error; got != want {
			t.Errorf("%s mapped to %q, want %q", azure, got, want)
		}
		if len(result.Words[0].ExpectedPhonemes) != 0 || result.Words[0].ProducedPhonemes == nil || result.Words[0].ExpectedPhonemes == nil {
			t.Errorf("%s: phoneme lists must be empty and not null: %+v", azure, result.Words[0])
		}
	}
	_, err := scorer.ParseAzure([]byte(`{"RecognitionStatus":"Success","NBest":[{"Words":[{"Word":"x","PronunciationAssessment":{"AccuracyScore":1,"ErrorType":"Bogus"}}]}]}`))
	if err == nil || !strings.Contains(err.Error(), "Bogus") {
		t.Errorf("expected an unknown ErrorType refused and named, got %v", err)
	}
}

func TestParseAzureReadsTheFlatShapeOfTheShortAudioDocs(t *testing.T) {
	answer := `{"RecognitionStatus":"Success","Duration":8400000,"NBest":[{"AccuracyScore":100.0,"Words":[
	  {"Word":"good","AccuracyScore":100.0,"ErrorType":"None"},{"Word":"morning","AccuracyScore":59.6,"ErrorType":"Mispronunciation"}]}]}`
	result, err := scorer.ParseAzure([]byte(answer))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Words) != 2 || result.Words[1].Error != application.ErrMispronunciation || result.Words[1].Accuracy != 60 || result.Accuracy != 100 {
		t.Errorf("unexpected result %+v", result)
	}
}

func TestParseAzureWithoutNBestPhonemesKeepsTheSoundPhonemes(t *testing.T) {
	answer := `{"RecognitionStatus":"Success","NBest":[{"Words":[{"Word":"cat","PronunciationAssessment":{"AccuracyScore":60,"ErrorType":"None"},
	  "Phonemes":[{"Phoneme":"k","PronunciationAssessment":{"AccuracyScore":90}},{"Phoneme":"æ","PronunciationAssessment":{"AccuracyScore":20}}]}]}]}`
	result, err := scorer.ParseAzure([]byte(answer))
	if err != nil {
		t.Fatal(err)
	}
	if w := result.Words[0]; strings.Join(w.ExpectedPhonemes, " ") != "k æ" || strings.Join(w.ProducedPhonemes, " ") != "k" {
		t.Errorf("unexpected phonemes %+v", w)
	}
}

func TestParseAzureRefusesAnAnswerWithoutWordsOrWithoutSuccess(t *testing.T) {
	for answer, want := range map[string]string{
		`{"RecognitionStatus":"NoMatch"}`:                        "NoMatch",
		`{"RecognitionStatus":"InitialSilenceTimeout"}`:          "InitialSilenceTimeout",
		`{"RecognitionStatus":"Success","NBest":[]}`:             "no words",
		`{"RecognitionStatus":"Success","NBest":[{"Words":[]}]}`: "no words",
		`not json`: "not",
	} {
		_, err := scorer.ParseAzure([]byte(answer))
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: expected an error saying %q, got %v", answer, want, err)
		}
	}
}

func TestAzureRefusesWithoutAKeyFileNamingTheSetting(t *testing.T) {
	for name, a := range map[string]*scorer.Azure{
		"unset":   scorer.NewAzure("", "westus2"),
		"missing": scorer.NewAzure(filepath.Join(t.TempDir(), "nokey"), "westus2"),
	} {
		_, err := a.Score(context.Background(), []byte("audio"), "audio/webm", "the cat sat", "en")
		if err == nil || !strings.Contains(err.Error(), "azure_key_file") {
			t.Errorf("%s: expected a refusal naming azure_key_file, got %v", name, err)
		}
	}
	missing := filepath.Join(t.TempDir(), "nokey")
	_, err := scorer.NewAzure(missing, "westus2").Score(context.Background(), []byte("audio"), "audio/webm", "x", "en")
	if err == nil || !strings.Contains(err.Error(), missing) {
		t.Errorf("expected the missing file named, got %v", err)
	}
}

func TestAzureRefusesAKeyFileOthersCanRead(t *testing.T) {
	path := azureKey(t, 0o644)
	_, err := scorer.NewAzure(path, "westus2").Score(context.Background(), []byte("audio"), "audio/webm", "x", "en")
	if err == nil || !strings.Contains(err.Error(), "0600") || !strings.Contains(err.Error(), path) {
		t.Fatalf("expected the key file refused for its mode, naming the file and 0600, got %v", err)
	}
	if strings.Contains(err.Error(), azureSecret) {
		t.Errorf("the refusal shows the key")
	}
}

func TestAzureRefusesAnEmptyKeyFileAndAMissingRegion(t *testing.T) {
	empty := filepath.Join(t.TempDir(), "azure.key")
	if err := os.WriteFile(empty, []byte("\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := scorer.NewAzure(empty, "westus2").Score(context.Background(), []byte("a"), "audio/webm", "x", "en"); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Errorf("expected an empty key refused, got %v", err)
	}
	_, err := scorer.NewAzure(azureKey(t, 0o600), "").Score(context.Background(), []byte("a"), "audio/webm", "x", "en")
	if err == nil || !strings.Contains(err.Error(), "azure_region") {
		t.Errorf("expected a missing region refused naming azure_region, got %v", err)
	}
}

func TestAzureReportsAnErrorStatusPlainly(t *testing.T) {
	needFfmpeg(t)
	for status, want := range map[int]string{
		http.StatusUnauthorized:    "azure_key_file",
		http.StatusForbidden:       "azure_key_file",
		http.StatusTooManyRequests: "quota",
		http.StatusBadRequest:      "400",
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "azure says no", status)
		}))
		_, err := azureAgainst(t, srv).Score(context.Background(), clip(t), "audio/webm", "the cat sat", "en")
		srv.Close()
		if err == nil || !strings.Contains(err.Error(), want) || !strings.Contains(err.Error(), http.StatusText(status)) {
			t.Errorf("status %d: expected an error saying %q and %q, got %v", status, want, http.StatusText(status), err)
		}
		if err != nil && strings.Contains(err.Error(), azureSecret) {
			t.Errorf("status %d: the error shows the key", status)
		}
	}
}

func TestAzureTimesOut(t *testing.T) {
	needFfmpeg(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	}))
	defer srv.Close()
	a := azureAgainst(t, srv)
	a.Timeout = 100 * time.Millisecond
	_, err := a.Score(context.Background(), clip(t), "audio/webm", "the cat sat", "en")
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Errorf("expected a timeout, got %v", err)
	}
}

func TestAzureDefaultEndpointIsTheRegionsShortAudioHost(t *testing.T) {
	a := scorer.NewAzure("/k", "westus2")
	want := "https://westus2.stt.speech.microsoft.com/speech/recognition/conversation/cognitiveservices/v1"
	if got := a.URL(); got != want {
		t.Errorf("URL() = %q, want %q", got, want)
	}
}

func TestAzureHasNoGreek(t *testing.T) {
	a := scorer.NewAzure(azureKey(t, 0o600), "westus2")
	for lang, want := range map[string]bool{"en": true, "es": true, "fr": true, "el": false, "el-GR": false, " EL ": false} {
		if got := a.ScoresLang(lang); got != want {
			t.Errorf("ScoresLang(%q) = %v, want %v", lang, got, want)
		}
	}
	if _, err := a.Score(context.Background(), []byte("x"), "audio/wav", "γειά", "el"); err == nil || !strings.Contains(err.Error(), "no pronunciation assessment for el") {
		t.Errorf("scoring Greek should be refused plainly, got %v", err)
	}
}
