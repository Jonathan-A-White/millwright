package apptest

import (
	"context"

	"github.com/Jonathan-A-White/millwright/application"
)

var _ application.HomeFile = (*FakeHomeFile)(nil)

// FakeHomeFile is a vault's home file held in memory: Text is what it reads, or
// Missing says there is none.
type FakeHomeFile struct {
	Text    string
	Missing bool
}

// ReadHome implements application.HomeFile.
func (f *FakeHomeFile) ReadHome(context.Context) (string, error) {
	if f.Missing {
		return "", application.ErrNoHomeFile
	}
	return f.Text, nil
}
