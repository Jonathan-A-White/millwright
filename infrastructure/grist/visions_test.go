package grist

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestVisionsReadTheFileAtTheRootOfTheCheckout(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "VISION.md"), []byte("What it is for.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	text, found, err := NewVisions().Read(context.Background(), dir)
	if err != nil || !found || text != "What it is for.\n" {
		t.Fatalf("got %q, found %v, err %v", text, found, err)
	}
}

func TestVisionsReportACheckoutWithNoFileAsNotFound(t *testing.T) {
	text, found, err := NewVisions().Read(context.Background(), t.TempDir())
	if err != nil || found || text != "" {
		t.Fatalf("got %q, found %v, err %v", text, found, err)
	}
}
