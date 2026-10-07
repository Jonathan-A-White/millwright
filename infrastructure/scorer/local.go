package scorer

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

// DefaultLocalTimeout is how long the local engine waits for a score: the
// scorer loads a model and listens to the whole clip.
const DefaultLocalTimeout = 2 * time.Minute

// Local is the `local` engine: an HTTP client of the scorer in contrib/scorer,
// which runs on this host (or one the config names) and does the listening.
type Local struct {
	// URL is the scorer's base URL, "http://127.0.0.1:8765".
	URL string
	// Timeout bounds one request; zero is DefaultLocalTimeout.
	Timeout time.Duration
}

// Local satisfies the port.
var _ application.Scorer = (*Local)(nil)

// NewLocal is the local engine talking to the scorer at url.
func NewLocal(url string) *Local {
	return &Local{URL: strings.TrimRight(url, "/")}
}

// localRequest is the body of POST <url>/score (docs/scorers.md).
type localRequest struct {
	TargetText string `json:"target_text"`
	Lang       string `json:"lang"`
	AudioWav   string `json:"audio_wav_base64"`
}

// Score converts the audio to 16 kHz mono WAV, posts it with the target text,
// and reads the ReadingResult the scorer answers with.
func (l *Local) Score(ctx context.Context, audio []byte, mime, targetText, lang string) (application.ReadingResult, error) {
	wav, err := ToWav16k(audio, mime)
	if err != nil {
		return application.ReadingResult{}, err
	}
	body, err := json.Marshal(localRequest{TargetText: targetText, Lang: lang, AudioWav: base64.StdEncoding.EncodeToString(wav)})
	if err != nil {
		return application.ReadingResult{}, err
	}
	timeout := l.Timeout
	if timeout <= 0 {
		timeout = DefaultLocalTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, l.URL+"/score", bytes.NewReader(body))
	if err != nil {
		return application.ReadingResult{}, fmt.Errorf("the local scorer's url %q is not usable: %w", l.URL, err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return application.ReadingResult{}, l.reach(err, timeout)
	}
	defer resp.Body.Close()
	answer, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return application.ReadingResult{}, l.reach(err, timeout)
	}
	if resp.StatusCode != http.StatusOK {
		return application.ReadingResult{}, fmt.Errorf("the local scorer at %s answered %s: %s", l.URL, resp.Status, strings.TrimSpace(string(answer)))
	}
	var result application.ReadingResult
	if err := json.Unmarshal(answer, &result); err != nil {
		return application.ReadingResult{}, fmt.Errorf("the local scorer at %s did not answer with a ReadingResult (docs/scorers.md): %w", l.URL, err)
	}
	if result.Engine == "" {
		result.Engine = "local"
	}
	for i := range result.Words {
		if result.Words[i].ExpectedPhonemes == nil {
			result.Words[i].ExpectedPhonemes = []string{}
		}
		if result.Words[i].ProducedPhonemes == nil {
			result.Words[i].ProducedPhonemes = []string{}
		}
	}
	if err := result.Validate(); err != nil {
		return application.ReadingResult{}, fmt.Errorf("the local scorer at %s broke the contract (docs/scorers.md): %w", l.URL, err)
	}
	return result, nil
}

// reach turns a transport failure into a line that says what to do.
func (l *Local) reach(err error, timeout time.Duration) error {
	var timedOut net.Error
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &timedOut) && timedOut.Timeout()) {
		return fmt.Errorf("the local scorer at %s timed out after %s", l.URL, timeout)
	}
	return fmt.Errorf("the local scorer at %s cannot be reached (%v): install and start it as docs/scorers.md says, or set local_url in the [scorers] table", l.URL, err)
}
