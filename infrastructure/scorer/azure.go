package scorer

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

// DefaultAzureTimeout is how long the azure engine waits for an assessment.
const DefaultAzureTimeout = 2 * time.Minute

// azureMaxSeconds is the longest clip the engine sends: Azure says a
// pronunciation assessment should be no more than 30 seconds of audio (the
// short-audio REST API takes 60, but the miscue marks need the shorter one).
const azureMaxSeconds = 30

// Azure is the `azure` engine: Azure Speech's pronunciation assessment, asked
// through the speech-to-text REST API for short audio and mapped to a
// ReadingResult. The key is read from a file at each Score, so a clip scored
// with no key is that engine's refusal and nothing more.
type Azure struct {
	// KeyFile is the full path of the file holding the Speech resource's key
	// (`azure_key_file`); it must be mode 0600.
	KeyFile string
	// Region is the key's Azure region, "westus2" (`azure_region`).
	Region string
	// Endpoint replaces the region's short-audio URL when set; tests point it
	// at a stub (MW_AZURE_ENDPOINT).
	Endpoint string
	// Timeout bounds one request; zero is DefaultAzureTimeout.
	Timeout time.Duration
}

// Azure satisfies the port.
var _ application.Scorer = (*Azure)(nil)

// NewAzure is the azure engine for the key in keyFile and the region given.
func NewAzure(keyFile, region string) *Azure {
	return &Azure{KeyFile: keyFile, Region: strings.TrimSpace(region)}
}

// URL is the address the engine posts to, without the query: the endpoint when
// one is set, else the region's short-audio host.
func (a *Azure) URL() string {
	if a.Endpoint != "" {
		return a.Endpoint
	}
	return "https://" + a.Region + ".stt.speech.microsoft.com/speech/recognition/conversation/cognitiveservices/v1"
}

// key reads the key file, refusing a missing setting, a missing file, a file
// others can read, and an empty one. The refusals name the file and never show
// the key.
func (a *Azure) key() (string, error) {
	if a.KeyFile == "" {
		return "", fmt.Errorf("the azure engine has no key: set azure_key_file in the [scorers] table of the config file (docs/scorers.md)")
	}
	file, err := os.Open(a.KeyFile)
	if err != nil {
		return "", fmt.Errorf("the azure key file %s (azure_key_file) cannot be read: %w", a.KeyFile, err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "", fmt.Errorf("the azure key file %s (azure_key_file) cannot be read: %w", a.KeyFile, err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		return "", fmt.Errorf("the azure key file %s (azure_key_file) is mode %o: others can read it; chmod it to 0600", a.KeyFile, info.Mode().Perm())
	}
	data, err := io.ReadAll(io.LimitReader(file, 4096))
	if err != nil {
		return "", fmt.Errorf("the azure key file %s (azure_key_file) cannot be read: %w", a.KeyFile, err)
	}
	key := strings.TrimSpace(string(data))
	if key == "" {
		return "", fmt.Errorf("the azure key file %s (azure_key_file) is empty", a.KeyFile)
	}
	return key, nil
}

// azureAssessment is the Pronunciation-Assessment header's JSON, base64-encoded.
type azureAssessment struct {
	ReferenceText     string `json:"ReferenceText"`
	GradingSystem     string `json:"GradingSystem"`
	Granularity       string `json:"Granularity"`
	Dimension         string `json:"Dimension"`
	EnableMiscue      bool   `json:"EnableMiscue"`
	PhonemeAlphabet   string `json:"PhonemeAlphabet"`
	NBestPhonemeCount int    `json:"NBestPhonemeCount"`
}

// Score converts the audio to 16 kHz mono WAV, posts it with the target as the
// reference text, and maps what Azure answers.
func (a *Azure) Score(ctx context.Context, audio []byte, mime, targetText, lang string) (application.ReadingResult, error) {
	key, err := a.key()
	if err != nil {
		return application.ReadingResult{}, err
	}
	if a.Endpoint == "" && a.Region == "" {
		return application.ReadingResult{}, fmt.Errorf("the azure engine has no region: set azure_region in the [scorers] table of the config file (docs/scorers.md)")
	}
	wav, err := ToWav16k(audio, mime)
	if err != nil {
		return application.ReadingResult{}, err
	}
	if seconds := float64(len(wav)-44) / 32000; seconds > azureMaxSeconds {
		return application.ReadingResult{}, fmt.Errorf("the recording is %.0f seconds: azure assesses at most %d seconds of reading at a time", seconds, azureMaxSeconds)
	}
	header, err := json.Marshal(azureAssessment{ReferenceText: targetText, GradingSystem: "HundredMark", Granularity: "Phoneme",
		Dimension: "Comprehensive", EnableMiscue: true, PhonemeAlphabet: "IPA", NBestPhonemeCount: 5})
	if err != nil {
		return application.ReadingResult{}, err
	}
	timeout := a.Timeout
	if timeout <= 0 {
		timeout = DefaultAzureTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	query := url.Values{"language": {azureLocale(lang)}, "format": {"detailed"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.URL()+"?"+query.Encode(), bytes.NewReader(wav))
	if err != nil {
		return application.ReadingResult{}, fmt.Errorf("the azure endpoint %q is not usable: %w", a.URL(), err)
	}
	req.Header.Set("Ocp-Apim-Subscription-Key", key)
	req.Header.Set("Content-Type", "audio/wav; codecs=audio/pcm; samplerate=16000")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Pronunciation-Assessment", base64.StdEncoding.EncodeToString(header))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return application.ReadingResult{}, a.reach(err, timeout)
	}
	defer resp.Body.Close()
	answer, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return application.ReadingResult{}, a.reach(err, timeout)
	}
	if resp.StatusCode != http.StatusOK {
		return application.ReadingResult{}, a.refusal(resp.StatusCode, resp.Status, answer)
	}
	result, err := ParseAzure(answer)
	if err != nil {
		return application.ReadingResult{}, fmt.Errorf("azure: %w", err)
	}
	return result, nil
}

// azureLocale is the locale Azure wants for a language code: "en" is en-US,
// a code that already has a region is left as it is.
func azureLocale(lang string) string {
	switch lang = strings.TrimSpace(lang); {
	case lang == "" || lang == "en":
		return "en-US"
	case lang == "es":
		return "es-ES"
	default:
		return lang
	}
}

// refusal turns an error status into a line that says what to do. The key is
// never in it; Azure's own words are, trimmed.
func (a *Azure) refusal(code int, status string, body []byte) error {
	said := strings.TrimSpace(string(body))
	if len(said) > 300 {
		said = said[:300] + "..."
	}
	switch code {
	case http.StatusUnauthorized, http.StatusForbidden:
		return fmt.Errorf("azure answered %s: the key was refused; check azure_key_file and that azure_region is the key's region: %s", status, said)
	case http.StatusTooManyRequests:
		return fmt.Errorf("azure answered %s: the rate limit or the free tier's monthly quota (5 audio hours) is used up; try again later: %s", status, said)
	default:
		return fmt.Errorf("azure answered %s: %s", status, said)
	}
}

// reach turns a transport failure into a line that says what to do.
func (a *Azure) reach(err error, timeout time.Duration) error {
	var timedOut net.Error
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &timedOut) && timedOut.Timeout()) {
		return fmt.Errorf("azure at %s timed out after %s", a.URL(), timeout)
	}
	return fmt.Errorf("azure at %s cannot be reached (%v): check the network and azure_region", a.URL(), err)
}

// azureNumber is a JSON number that Azure sometimes quotes ("Offset": "123").
type azureNumber float64

func (n *azureNumber) UnmarshalJSON(data []byte) error {
	text := strings.Trim(string(data), `"`)
	if text == "" || text == "null" {
		return nil
	}
	value, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return err
	}
	*n = azureNumber(value)
	return nil
}

// azureScores are the scores Azure puts under PronunciationAssessment, or, in
// the short-audio docs' older shape, straight on the word or the result.
type azureScores struct {
	AccuracyScore *float64 `json:"AccuracyScore"`
	ErrorType     string   `json:"ErrorType"`
	// NBestPhonemes are the likeliest phonemes actually spoken, with scores.
	NBestPhonemes []struct {
		Phoneme string  `json:"Phoneme"`
		Score   float64 `json:"Score"`
	} `json:"NBestPhonemes"`
}

type azureAnswer struct {
	RecognitionStatus json.RawMessage `json:"RecognitionStatus"`
	Duration          azureNumber     `json:"Duration"`
	NBest             []struct {
		azureScores
		PronunciationAssessment *azureScores `json:"PronunciationAssessment"`
		Words                   []struct {
			azureScores
			Word                    string       `json:"Word"`
			PronunciationAssessment *azureScores `json:"PronunciationAssessment"`
			Phonemes                []struct {
				azureScores
				Phoneme                 string       `json:"Phoneme"`
				PronunciationAssessment *azureScores `json:"PronunciationAssessment"`
			} `json:"Phonemes"`
		} `json:"Words"`
	} `json:"NBest"`
}

// scoresOf is the scores under PronunciationAssessment when there are any,
// else the flat ones.
func scoresOf(nested *azureScores, flat azureScores) azureScores {
	if nested != nil {
		return *nested
	}
	return flat
}

// azureAccuracy rounds a 0 to 100 score to a whole number inside that range.
func azureAccuracy(score *float64) int {
	if score == nil {
		return 0
	}
	return int(math.Min(100, math.Max(0, math.Round(*score))))
}

// azureHeardPhonemeMinimum is the phoneme accuracy at which, when Azure names
// no likelier phoneme, the expected one is taken to have been heard (Azure
// itself calls a word below 60 a mispronunciation).
const azureHeardPhonemeMinimum = 60

// ParseAzure maps an Azure `format=detailed` answer with pronunciation
// assessment to a ReadingResult (docs/scorers.md says how). The error types map
// None, Omission, Insertion and Mispronunciation as they are named;
// UnexpectedBreak is a hesitation; MissingBreak and Monotone are prosody notes,
// not misreadings, so the word is none. Another error type is refused.
func ParseAzure(answer []byte) (application.ReadingResult, error) {
	var got azureAnswer
	if err := json.Unmarshal(answer, &got); err != nil {
		return application.ReadingResult{}, fmt.Errorf("did not answer with JSON: %w", err)
	}
	if status := strings.Trim(string(got.RecognitionStatus), `"`); status != "" && status != "Success" && status != "0" {
		return application.ReadingResult{}, fmt.Errorf("heard no reading: the recognition status was %s", status)
	}
	if len(got.NBest) == 0 || len(got.NBest[0].Words) == 0 {
		return application.ReadingResult{}, fmt.Errorf("answered with no words (the answer had no NBest[0].Words)")
	}
	best := got.NBest[0]
	result := application.ReadingResult{Engine: "azure", Words: []application.ReadingWord{}, Seconds: float64(got.Duration) / 1e7}
	result.Accuracy = azureAccuracy(scoresOf(best.PronunciationAssessment, best.azureScores).AccuracyScore)
	for _, word := range best.Words {
		scores := scoresOf(word.PronunciationAssessment, word.azureScores)
		out := application.ReadingWord{Text: word.Word, ExpectedPhonemes: []string{}, ProducedPhonemes: []string{}, Accuracy: azureAccuracy(scores.AccuracyScore)}
		switch scores.ErrorType {
		case "None", "":
			out.Error = application.ErrNone
		case "Omission":
			out.Error = application.ErrOmission
		case "Insertion":
			out.Error = application.ErrInsertion
		case "Mispronunciation":
			out.Error = application.ErrMispronunciation
		case "UnexpectedBreak":
			out.Error = application.ErrHesitation
		case "MissingBreak", "Monotone":
			out.Error = application.ErrNone
		default:
			return application.ReadingResult{}, fmt.Errorf("gave the word %q the error type %q, which this mw does not know (docs/scorers.md)", word.Word, scores.ErrorType)
		}
		for _, phoneme := range word.Phonemes {
			ps := scoresOf(phoneme.PronunciationAssessment, phoneme.azureScores)
			switch out.Error {
			case application.ErrInsertion:
				// An added word has no expected phonemes; those listed are what was said.
				out.ProducedPhonemes = append(out.ProducedPhonemes, phoneme.Phoneme)
				continue
			case application.ErrOmission:
				out.ExpectedPhonemes = append(out.ExpectedPhonemes, phoneme.Phoneme)
				continue
			}
			out.ExpectedPhonemes = append(out.ExpectedPhonemes, phoneme.Phoneme)
			if len(ps.NBestPhonemes) > 0 {
				top := ps.NBestPhonemes[0]
				for _, candidate := range ps.NBestPhonemes[1:] {
					if candidate.Score > top.Score {
						top = candidate
					}
				}
				out.ProducedPhonemes = append(out.ProducedPhonemes, top.Phoneme)
			} else if ps.AccuracyScore != nil && *ps.AccuracyScore >= azureHeardPhonemeMinimum {
				out.ProducedPhonemes = append(out.ProducedPhonemes, phoneme.Phoneme)
			}
		}
		result.Words = append(result.Words, out)
	}
	if err := result.Validate(); err != nil {
		return application.ReadingResult{}, fmt.Errorf("broke the contract (docs/scorers.md): %w", err)
	}
	return result, nil
}
