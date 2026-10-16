// Package millwright holds what is built into the mw binary: the template a
// fresh vault is born from, so that an installed mw needs no checkout beside it,
// and the instructions of the model passes mw runs itself.
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

// TodoRankInstructions is grinds/todo-rank.md: the rule mw todo rank's model
// pass is given, after its own preamble, as its system prompt.
//
//go:embed grinds/todo-rank.md
var TodoRankInstructions string

// SleepInstructions is grinds/sleep.md: the rule the sleep pass's agent is given,
// after its own preamble and before the principles, as its system prompt.
//
//go:embed grinds/sleep.md
var SleepInstructions string

// TrimMergeScript is scripts/trim-merge.sh: the trim-safe merge driver mw
// passes on every merge it makes (infrastructure/gitcmd.WriteMergeDriver), so
// that any rig and the vault have it with no checkout of millwright beside them.
//
//go:embed scripts/trim-merge.sh
var TrimMergeScript string
