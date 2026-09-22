// Package millwright holds what is built into the mw binary: the template a
// fresh vault is born from, so that an installed mw needs no checkout beside it.
package millwright

import (
	"embed"
	"io/fs"
)

//go:embed all:template
var template embed.FS

// Template is the files of template/ as a fresh vault has them, rooted at the
// vault's own top directory.
func Template() fs.FS {
	files, err := fs.Sub(template, "template")
	if err != nil {
		panic(err) // "template" is embedded above, so it cannot be missing
	}
	return files
}
