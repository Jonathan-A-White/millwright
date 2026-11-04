package application

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
)

// GeminiKeyName is the SecretStore name the Gemini API key is kept under: a
// Google AI Studio key.
const GeminiKeyName = "gemini_api_key"

// geminiKeyHint is what an error about the key points a person at.
const geminiKeyHint = "mw secrets put " + GeminiKeyName

// ErrImageQuota is what an ImageMaker wraps when the API refused the request
// over quota, rate or billing: a free key that may not make pictures, a quota
// spent, a project whose billing is off.
var ErrImageQuota = errors.New("quota or billing refusal")

// MadeImage is a picture an ImageMaker made.
type MadeImage struct {
	Data []byte
	// MimeType is what the API says the bytes are, like image/png.
	MimeType string
}

// ImageMaker makes one picture from a prompt, with one request: it never
// retries, and it has no way to touch billing. An error it gives never quotes
// the key.
type ImageMaker interface {
	Make(ctx context.Context, prompt string) (MadeImage, error)
}

// imageExtensions is the picture files mw image writes, and the kind each says.
var imageExtensions = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".webp": "image/webp",
}

// extensionOf is the extension a picture of kind mime should have, out's own
// basename with the right one, or ".<kind>" when the kind is not one of the
// files mw image writes.
func extensionOf(mime, out string) string {
	for ext, kind := range imageExtensions {
		if kind == mime && ext != ".jpeg" {
			return strings.TrimSuffix(filepath.Base(out), filepath.Ext(out)) + ext
		}
	}
	return "." + strings.TrimPrefix(mime, "image/")
}

// Image is mw image: it makes one picture from a prompt with the Gemini API
// and writes it to a file. One request a run, no retry, no batch.
type Image struct {
	Secrets SecretStore
	// Open is the maker for a key (the model is its own business); the error
	// it gives never quotes the key.
	Open func(key string) (ImageMaker, error)
	// Write puts the picture in the file.
	Write func(path string, data []byte) error
	Out   io.Writer
}

// Run implements mw image: the prompt is checked, and the file name, before a
// request is made, because a request may be paid for.
func (i Image) Run(ctx context.Context, prompt, out string) error {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return errors.New("the prompt is empty: say what the picture should show")
	}
	want, ok := imageExtensions[strings.ToLower(filepath.Ext(out))]
	if !ok {
		return fmt.Errorf("--out %q is not a picture file: name it with .png, .jpg, .jpeg or .webp", out)
	}
	raw, err := i.Secrets.Get(ctx, GeminiKeyName)
	if errors.Is(err, ErrNoSecret) {
		return fmt.Errorf("no Gemini key is kept: put a Google AI Studio API key with %s", geminiKeyHint)
	}
	if err != nil {
		return fmt.Errorf("reading %s from the vault (%s puts it): %w", GeminiKeyName, geminiKeyHint, err)
	}
	key := strings.TrimSpace(string(raw))
	maker, err := i.Open(key)
	if err != nil {
		return cutKey(err, key)
	}
	made, err := maker.Make(ctx, prompt)
	if errors.Is(err, ErrImageQuota) {
		return cutKey(fmt.Errorf("Gemini refused the request over quota or billing, so billing may be off for this key's Google project (image models can need it on): %v. mw image never touches billing: change it in Google AI Studio or the Google Cloud console yourself, then run this again", err), key)
	}
	if err != nil {
		return cutKey(err, key)
	}
	if err := i.Write(out, made.Data); err != nil {
		return fmt.Errorf("the picture was made but not written: %w", err)
	}
	if i.Out != nil {
		fmt.Fprintf(i.Out, "wrote %s (%d bytes, %s)\n", out, len(made.Data), made.MimeType)
		if made.MimeType != "" && made.MimeType != want {
			fmt.Fprintf(i.Out, "note: the file is named for %s but the picture is %s: name it %s to match\n", want, made.MimeType, extensionOf(made.MimeType, out))
		}
	}
	return nil
}

// cutKey is err with the key cut out of its text: a last net under the
// adapter's own.
func cutKey(err error, key string) error {
	if key == "" || !strings.Contains(err.Error(), key) {
		return err
	}
	return errors.New(strings.ReplaceAll(err.Error(), key, "<key>"))
}
