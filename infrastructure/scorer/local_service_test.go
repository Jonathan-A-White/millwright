package scorer_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/scorer"
)

// TestLocalScoresAFixtureWithTheRealScorer talks to the scorer of
// contrib/scorer itself, at MW_SCORER_URL ("http://127.0.0.1:8765"), and skips
// when that is not set: the gate never needs the model.
func TestLocalScoresAFixtureWithTheRealScorer(t *testing.T) {
	url := os.Getenv("MW_SCORER_URL")
	if url == "" {
		t.Skip("MW_SCORER_URL is not set: no real scorer to talk to")
	}
	needFfmpeg(t)
	audio, err := os.ReadFile("../../contrib/scorer/fixtures/cap.wav")
	if err != nil {
		t.Fatal(err)
	}
	result, err := scorer.NewLocal(url).Score(context.Background(), audio, "audio/wav", "the cat sat on the mat", "en")
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	var got []string
	for _, w := range result.Words {
		got = append(got, w.Text+":"+w.Error)
	}
	want := "the:none cat:mispronunciation sat:none on:none the:none mat:none"
	if strings.Join(got, " ") != want {
		t.Fatalf("expected %s, got %s", want, strings.Join(got, " "))
	}
	if result.Engine != "local" || result.Words[1].Error != application.ErrMispronunciation {
		t.Errorf("unexpected result %+v", result)
	}
}
