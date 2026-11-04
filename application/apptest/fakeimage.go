package apptest

import (
	"context"

	"github.com/Jonathan-A-White/millwright/application"
)

var _ application.ImageMaker = (*FakeImageMaker)(nil)

// FakeImageMaker is a picture maker in memory: Prompts keeps what Make was
// asked, in order; Err, when set, is what it returns; Image is what it makes.
type FakeImageMaker struct {
	Prompts []string
	Image   application.MadeImage
	Err     error
}

// Make implements application.ImageMaker.
func (f *FakeImageMaker) Make(_ context.Context, prompt string) (application.MadeImage, error) {
	f.Prompts = append(f.Prompts, prompt)
	if f.Err != nil {
		return application.MadeImage{}, f.Err
	}
	return f.Image, nil
}
