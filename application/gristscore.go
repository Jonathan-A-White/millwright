package application

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// GristScore is `mw grist score`: one reading, an audio file, scored against
// its target text by one engine, and the ReadingResult printed as JSON. It is
// the seam the engines plug into and sends nothing anywhere itself.
type GristScore struct {
	Scorers ScorerRegistry
	// Out is where the result is printed; nil prints nothing.
	Out io.Writer
}

// GristScoreRequest is one reading to score.
type GristScoreRequest struct {
	// Engine is the engine's name, one of the configured.
	Engine string
	// Target is the text the reader was asked to read aloud.
	Target string
	// AudioFile is the recording, in any format the engine's conversion reads.
	AudioFile string
	// Lang is the language code of the target, "en" when empty.
	Lang string
}

// audioTypes are the MIME types of the recordings an app's phone sends, by
// extension; any other file is application/octet-stream and the engine's
// conversion decides whether it can read it.
var audioTypes = map[string]string{
	".webm": "audio/webm",
	".ogg":  "audio/ogg",
	".opus": "audio/ogg",
	".wav":  "audio/wav",
	".mp3":  "audio/mpeg",
	".m4a":  "audio/mp4",
	".mp4":  "audio/mp4",
	".flac": "audio/flac",
}

// Run scores the reading and prints the result. The engine is looked up and
// the file read before anything is asked of the engine, so a typo costs
// nothing.
func (g GristScore) Run(ctx context.Context, req GristScoreRequest) (ReadingResult, error) {
	engine, err := g.Scorers.Get(req.Engine)
	if err != nil {
		return ReadingResult{}, err
	}
	if strings.TrimSpace(req.Target) == "" {
		return ReadingResult{}, fmt.Errorf("the target text is empty: say what was read with --target")
	}
	audio, err := os.ReadFile(req.AudioFile)
	if err != nil {
		return ReadingResult{}, fmt.Errorf("reading the audio: %w", err)
	}
	mime := audioTypes[strings.ToLower(filepath.Ext(req.AudioFile))]
	if mime == "" {
		mime = "application/octet-stream"
	}
	lang := req.Lang
	if lang == "" {
		lang = "en"
	}
	result, err := engine.Score(ctx, audio, mime, req.Target, lang)
	if err != nil {
		return ReadingResult{}, err
	}
	if g.Out != nil {
		out, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return result, err
		}
		fmt.Fprintf(g.Out, "%s\n", out)
	}
	return result, nil
}
