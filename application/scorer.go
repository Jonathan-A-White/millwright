package application

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// The kinds of error a scorer reports for one word of a reading. docs/scorers.md
// is the contract; these are its five words.
const (
	ErrNone             = "none"
	ErrOmission         = "omission"
	ErrInsertion        = "insertion"
	ErrMispronunciation = "mispronunciation"
	ErrHesitation       = "hesitation"
)

// ReadingWord is one word of a reading, as a scorer heard it: the phonemes the
// target text asked for, the phonemes it heard, what went wrong, and how well
// the word was read, 0 to 100. An inserted word is one the reader added, so
// its expected phonemes are empty; an omitted one is one the reader left out,
// so its produced phonemes are empty.
type ReadingWord struct {
	Text             string   `json:"text"`
	ExpectedPhonemes []string `json:"expected_phonemes"`
	ProducedPhonemes []string `json:"produced_phonemes"`
	Error            string   `json:"error"`
	Accuracy         int      `json:"accuracy"`
	SelfCorrected    bool     `json:"self_corrected"`
}

// ReadingResult is what every Scorer returns: the engine that made it, a
// ReadingWord for each word of the reading, the reading's accuracy as a whole,
// 0 to 100, and the seconds of speech scored.
type ReadingResult struct {
	Engine   string        `json:"engine"`
	Words    []ReadingWord `json:"words"`
	Accuracy int           `json:"accuracy"`
	Seconds  float64       `json:"seconds"`
}

// Validate refuses a result that breaks the contract in docs/scorers.md, so a
// misbehaving engine fails where it is read rather than where it is used.
func (r ReadingResult) Validate() error {
	if len(r.Words) == 0 {
		return fmt.Errorf("the result has no words")
	}
	if r.Accuracy < 0 || r.Accuracy > 100 {
		return fmt.Errorf("the result's accuracy is %d: it must be 0 to 100", r.Accuracy)
	}
	if r.Seconds < 0 {
		return fmt.Errorf("the result's seconds is %v: it cannot be negative", r.Seconds)
	}
	for i, w := range r.Words {
		if strings.TrimSpace(w.Text) == "" {
			return fmt.Errorf("word %d of the result has no text", i+1)
		}
		switch w.Error {
		case ErrNone, ErrOmission, ErrInsertion, ErrMispronunciation, ErrHesitation:
		default:
			return fmt.Errorf("word %d (%q) of the result has the error %q: it must be none, omission, insertion, mispronunciation or hesitation", i+1, w.Text, w.Error)
		}
		if w.Accuracy < 0 || w.Accuracy > 100 {
			return fmt.Errorf("word %d (%q) of the result has the accuracy %d: it must be 0 to 100", i+1, w.Text, w.Accuracy)
		}
	}
	return nil
}

// Scorer is one engine that scores a reading: audio of someone reading
// targetText aloud in lang (a language code, "en"), of the given MIME type,
// which the engine turns into whatever it listens to. Engines are named in
// config ([scorers] engines) and found by ScorerRegistry.
type Scorer interface {
	Score(ctx context.Context, audio []byte, mime, targetText, lang string) (ReadingResult, error)
}

// LangScorer is an engine that can say which languages it cannot score. An
// engine that does not implement it is taken to score every language.
type LangScorer interface {
	ScoresLang(lang string) bool
}

// ScoresLang reports whether engine can score a reading in lang.
func ScoresLang(engine Scorer, lang string) bool {
	if l, ok := engine.(LangScorer); ok {
		return l.ScoresLang(lang)
	}
	return true
}

// ScorerRegistry is the engines this host is configured to run, by name.
type ScorerRegistry struct {
	engines map[string]Scorer
}

// NewScorerRegistry is a registry of the engines given, by name.
func NewScorerRegistry(engines map[string]Scorer) ScorerRegistry {
	return ScorerRegistry{engines: engines}
}

// Names are the configured engines' names, sorted.
func (r ScorerRegistry) Names() []string {
	names := make([]string, 0, len(r.engines))
	for name := range r.engines {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Get is the engine called name; when there is none, the error lists the ones
// that are configured.
func (r ScorerRegistry) Get(name string) (Scorer, error) {
	if engine, ok := r.engines[name]; ok {
		return engine, nil
	}
	if len(r.engines) == 0 {
		return nil, fmt.Errorf("no scorer engine named %q: none is configured (engines in the [scorers] table of the config file)", name)
	}
	return nil, fmt.Errorf("no scorer engine named %q: the configured engines are %s", name, strings.Join(r.Names(), ", "))
}
