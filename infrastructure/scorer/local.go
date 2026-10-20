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

// The local engine waits for a score as long as the clip allows: the scorer
// loads a model and listens to the whole clip in one pass, about 0.25 s of CPU
// for each second of audio (measured over 48 verse readings, mw-gq6.660), so
// the limit is LocalBaseTimeout plus LocalSecondsAllowance for each second of
// audio, never more than MaxLocalTimeout. A Lampas reading may be 600 s
// (recorder.ts): 120 + 0.5 x 600 = 420 s; one of 300 s gets 270 s.
const (
	// LocalBaseTimeout is the limit for no audio at all: the model's load.
	LocalBaseTimeout = 2 * time.Minute
	// LocalSecondsAllowance is the seconds allowed for each second of audio,
	// twice the 0.25 measured.
	LocalSecondsAllowance = 0.5
	// LocalTimeoutMargin is what the grind's own limit keeps for the model
	// step after the scoring.
	LocalTimeoutMargin = time.Minute
	// MaxLocalTimeout is the most the limit grows to: the grind's limit
	// (application.DefaultGristTimeout) less LocalTimeoutMargin.
	MaxLocalTimeout = application.DefaultGristTimeout - LocalTimeoutMargin
)

// LocalTimeoutFor is how long the local engine waits for the score of audio of
// the given seconds: LocalBaseTimeout plus LocalSecondsAllowance each second,
// at most MaxLocalTimeout.
func LocalTimeoutFor(audioSeconds float64) time.Duration {
	if audioSeconds < 0 {
		audioSeconds = 0
	}
	limit := LocalBaseTimeout + time.Duration(audioSeconds*LocalSecondsAllowance*float64(time.Second))
	return min(limit, MaxLocalTimeout)
}

// wav16kBytesPerSecond is what a second of 16 kHz mono 16-bit WAV takes.
const wav16kBytesPerSecond = 16000 * 2

// Local is the `local` engine: an HTTP client of the scorer in contrib/scorer,
// which runs on this host (or one the config names) and does the listening.
type Local struct {
	// URL is the scorer's base URL, "http://127.0.0.1:8765".
	URL string
	// Timeout bounds one request; zero is LocalTimeoutFor the clip's seconds.
	Timeout time.Duration
}

// Local satisfies the port.
var (
	_ application.Scorer        = (*Local)(nil)
	_ application.SegmentScorer = (*Local)(nil)
)

// NewLocal is the local engine talking to the scorer at url.
func NewLocal(url string) *Local {
	return &Local{URL: strings.TrimRight(url, "/")}
}

// localRequest is the body of POST <url>/score (docs/scorers.md): target_text
// and lang for a reading in one language, segments for a woven one (which the
// scorer scores by, each piece in its own language).
type localRequest struct {
	TargetText string                     `json:"target_text,omitempty"`
	Lang       string                     `json:"lang,omitempty"`
	Segments   []application.ScoreSegment `json:"segments,omitempty"`
	AudioWav   string                     `json:"audio_wav_base64"`
}

// Score converts the audio to 16 kHz mono WAV, posts it with the target text,
// and reads the ReadingResult the scorer answers with.
func (l *Local) Score(ctx context.Context, audio []byte, mime, targetText, lang string) (application.ReadingResult, error) {
	return l.score(ctx, audio, mime, localRequest{TargetText: targetText, Lang: lang})
}

// ScoreSegments posts the pieces of a woven reading in order, each with its
// language, for the scorer to score word by word in the language it is in.
func (l *Local) ScoreSegments(ctx context.Context, audio []byte, mime string, segments []application.ScoreSegment) (application.ReadingResult, error) {
	return l.score(ctx, audio, mime, localRequest{Segments: segments})
}

// score converts the audio, puts it in the request given and posts it.
func (l *Local) score(ctx context.Context, audio []byte, mime string, request localRequest) (application.ReadingResult, error) {
	wav, err := ToWav16k(audio, mime)
	if err != nil {
		return application.ReadingResult{}, err
	}
	request.AudioWav = base64.StdEncoding.EncodeToString(wav)
	body, err := json.Marshal(request)
	if err != nil {
		return application.ReadingResult{}, err
	}
	timeout := l.Timeout
	if timeout <= 0 {
		timeout = LocalTimeoutFor(float64(len(wav)) / wav16kBytesPerSecond)
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

// reach turns a transport failure into a line that says what to do; a timeout
// is an application.ScoreTimeout, so the grind can say the reading was not scored.
func (l *Local) reach(err error, timeout time.Duration) error {
	var timedOut net.Error
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &timedOut) && timedOut.Timeout()) {
		return &application.ScoreTimeout{Where: "the local scorer at " + l.URL, Limit: timeout}
	}
	return fmt.Errorf("the local scorer at %s cannot be reached (%v): install and start it as docs/scorers.md says, or set local_url in the [scorers] table", l.URL, err)
}
