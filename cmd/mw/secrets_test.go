package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestToATerminalIsFalseForAPipeAFileAndABuffer(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	if toATerminal(w) {
		t.Error("a pipe was taken for a terminal")
	}

	file, err := os.Create(filepath.Join(t.TempDir(), "out"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if toATerminal(file) {
		t.Error("a plain file was taken for a terminal")
	}

	if toATerminal(&bytes.Buffer{}) {
		t.Error("a buffer was taken for a terminal")
	}
}

func TestToATerminalErrsTowardRefusingACharacterDevice(t *testing.T) {
	null, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Skipf("no %s here: %v", os.DevNull, err)
	}
	defer null.Close()
	if !toATerminal(null) {
		t.Error("a character device was not taken for a terminal")
	}
}
