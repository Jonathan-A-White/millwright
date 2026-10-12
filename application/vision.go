package application

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// VisionFile is the file at the root of an app's checkout that says what the
// app is for; feedback about the app is weighed against it.
const VisionFile = "VISION.md"

// VisionCap is how many bytes of an app's VISION.md a forwarded mail carries.
const VisionCap = 3000

// VisionCut ends a vision that was longer than VisionCap.
const VisionCut = "(cut)"

// VisionMissing is what a forwarded mail says of an app with no VISION.md.
const VisionMissing = "This app has no VISION.md: weigh this with the Governor."

// VisionSource reads an app's VISION.md from its checkout. The real adapter is
// infrastructure/grist's Visions.
type VisionSource interface {
	// Read reports the text of the VISION.md at the root of the checkout, and
	// found false, with no error, when the checkout has none.
	Read(ctx context.Context, checkout string) (text string, found bool, err error)
}

// VisionPath is where an app's VISION.md is, in its checkout.
func VisionPath(checkout string) string { return filepath.Join(checkout, VisionFile) }

// VisionSection is the last section of a forwarded mail: the app's vision under
// a heading that names the file, cut to VisionCap bytes (on a character
// boundary) and marked when it was cut; or, when the app has none, the line
// that says so. A vision that could not be read says that instead.
func VisionSection(ctx context.Context, source VisionSource, checkout string) string {
	text, found, err := source.Read(ctx, checkout)
	switch {
	case err != nil:
		return fmt.Sprintf("The app's VISION.md (%s) could not be read: %v", VisionPath(checkout), err)
	case !found:
		return VisionMissing
	}
	text = strings.TrimSpace(text)
	cut := false
	if len(text) > VisionCap {
		end := VisionCap
		for end > 0 && !utf8.RuneStart(text[end]) {
			end--
		}
		text, cut = text[:end], true
	}
	section := fmt.Sprintf("THE APP'S VISION (%s)\n%s", VisionPath(checkout), text)
	if cut {
		section += "\n" + VisionCut
	}
	return section
}
