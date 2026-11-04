package application_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

func imageKeyKept() *apptest.FakeSecretStore {
	return &apptest.FakeSecretStore{Values: map[string][]byte{application.GeminiKeyName: []byte(" sentinel-gemini-key\n")}}
}

type written struct {
	path string
	data []byte
}

func imageRun(store application.SecretStore, maker *apptest.FakeImageMaker, prompt, out string) (string, []written, string, error) {
	var said bytes.Buffer
	var files []written
	var keyOpened string
	err := application.Image{
		Secrets: store,
		Open: func(key string) (application.ImageMaker, error) {
			keyOpened = key
			return maker, nil
		},
		Write: func(path string, data []byte) error {
			files = append(files, written{path, data})
			return nil
		},
		Out: &said,
	}.Run(context.Background(), prompt, out)
	return said.String(), files, keyOpened, err
}

func TestImageMakesOnePictureAndWritesItWhereAsked(t *testing.T) {
	maker := &apptest.FakeImageMaker{Image: application.MadeImage{Data: []byte("PNGDATA"), MimeType: "image/png"}}
	out, files, key, err := imageRun(imageKeyKept(), maker, "a lighthouse", "/tmp/x/lighthouse.png")
	if err != nil {
		t.Fatal(err)
	}
	if key != "sentinel-gemini-key" {
		t.Errorf("the key given to Open = %q, want the stored one trimmed", key)
	}
	if len(maker.Prompts) != 1 || maker.Prompts[0] != "a lighthouse" {
		t.Errorf("prompts = %v, want the one", maker.Prompts)
	}
	if len(files) != 1 || files[0].path != "/tmp/x/lighthouse.png" || string(files[0].data) != "PNGDATA" {
		t.Errorf("files = %v", files)
	}
	if !strings.Contains(out, "/tmp/x/lighthouse.png") {
		t.Errorf("out = %q, want it to say where the picture is", out)
	}
}

func TestImageRefusesAnEmptyPromptAndAnOutNotNamingAPictureFile(t *testing.T) {
	for _, c := range [][2]string{{"", "a.png"}, {"  ", "a.png"}, {"p", ""}, {"p", "a.txt"}, {"p", "dir/"}} {
		maker := &apptest.FakeImageMaker{}
		_, files, _, err := imageRun(imageKeyKept(), maker, c[0], c[1])
		if err == nil {
			t.Errorf("%q %q: no refusal", c[0], c[1])
		}
		if len(maker.Prompts) != 0 || len(files) != 0 {
			t.Errorf("%q %q: a request was made or a file written before the refusal", c[0], c[1])
		}
	}
}

func TestImageWithNoKeyNamesTheCommandThatPutsIt(t *testing.T) {
	maker := &apptest.FakeImageMaker{}
	_, _, _, err := imageRun(&apptest.FakeSecretStore{}, maker, "p", "a.png")
	if err == nil || !strings.Contains(err.Error(), "mw secrets put gemini_api_key") {
		t.Fatalf("err = %v, want one naming mw secrets put gemini_api_key", err)
	}
	if len(maker.Prompts) != 0 {
		t.Error("a request was made without a key")
	}
}

func TestImageSaysPlainlyThatBillingMayBeOffOnAQuotaError(t *testing.T) {
	maker := &apptest.FakeImageMaker{Err: fmt.Errorf("HTTP 429: quota exceeded: %w", application.ErrImageQuota)}
	_, files, _, err := imageRun(imageKeyKept(), maker, "p", "a.png")
	if err == nil {
		t.Fatal("no error")
	}
	for _, want := range []string{"billing may be off", "never touches billing", "429"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("err = %q, want it to say %q", err, want)
		}
	}
	if len(maker.Prompts) != 1 {
		t.Errorf("%d requests, want one: no retry", len(maker.Prompts))
	}
	if len(files) != 0 {
		t.Error("a file was written for a failed request")
	}
}

func TestImageCutsTheKeyOutOfAnyError(t *testing.T) {
	maker := &apptest.FakeImageMaker{Err: errors.New("the API said no to sentinel-gemini-key")}
	_, _, _, err := imageRun(imageKeyKept(), maker, "p", "a.png")
	if err == nil || strings.Contains(err.Error(), "sentinel-gemini-key") {
		t.Fatalf("err = %v, want one without the key", err)
	}
}

func TestImageSaysWhenThePictureIsNotTheKindTheFileNameSays(t *testing.T) {
	maker := &apptest.FakeImageMaker{Image: application.MadeImage{Data: []byte("J"), MimeType: "image/jpeg"}}
	out, files, _, err := imageRun(imageKeyKept(), maker, "p", "a.png")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || !strings.Contains(out, "image/jpeg") {
		t.Errorf("files = %v, out = %q: it should still keep the paid picture and say what kind it is", files, out)
	}
}

func TestImageNamesTheRightExtensionWhenThePictureIsNotTheKindTheFileNameSays(t *testing.T) {
	maker := &apptest.FakeImageMaker{Image: application.MadeImage{Data: []byte("J"), MimeType: "image/jpeg"}}
	out, files, _, err := imageRun(imageKeyKept(), maker, "p", "a.png")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].path != "a.png" {
		t.Errorf("files = %v, want the file written as asked", files)
	}
	if !strings.Contains(out, ".jpg") {
		t.Errorf("out = %q, want it to say the right extension, .jpg", out)
	}
}
