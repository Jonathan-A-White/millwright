package config_test

import (
	"testing"

	"github.com/Jonathan-A-White/millwright/infrastructure/config"
)

func TestGeminiModelDefaultsToNanoBananaAndIsReadFromTheFileAndTheEnvironment(t *testing.T) {
	t.Setenv(config.GeminiModelEnv, "")
	writeConfig(t, "")
	if got, err := config.GeminiModel(); err != nil || got != config.DefaultGeminiModel {
		t.Fatalf("got %q %v, want the default", got, err)
	}
	writeConfig(t, "gemini_model = \"gemini-3.1-flash-image\"\n")
	if got, _ := config.GeminiModel(); got != "gemini-3.1-flash-image" {
		t.Fatalf("got %q, want the file's", got)
	}
	t.Setenv(config.GeminiModelEnv, "gemini-3-pro-image")
	if got, _ := config.GeminiModel(); got != "gemini-3-pro-image" {
		t.Fatalf("got %q, want the environment's", got)
	}
}
