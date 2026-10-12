package grist

import (
	"context"
	"errors"
	"os"

	"github.com/Jonathan-A-White/millwright/application"
)

// Visions reads the VISION.md at the root of an app's checkout (application's
// VisionSource).
type Visions struct{}

// Visions satisfies the port.
var _ application.VisionSource = Visions{}

// NewVisions is the reader of VISION.md files.
func NewVisions() Visions { return Visions{} }

// Read implements application.VisionSource: found is false, with no error, when
// the checkout has no such file.
func (Visions) Read(_ context.Context, checkout string) (string, bool, error) {
	raw, err := os.ReadFile(application.VisionPath(checkout))
	if errors.Is(err, os.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return string(raw), true, nil
}
