package application

import (
	"fmt"
	"io"
	"strings"

	"github.com/Jonathan-A-White/millwright/domain"
)

// TestCallers is the count of a story's test callers against its size
// (mw-gq6.661). A story is sized by the screens it names, but the tests that
// call those screens are changed with them, so two Lampas stories ran out of
// turns at 70 and 135 changed files. The count is the rig's test files that name
// something a file of the story's files key declares (RigFiles.TestCallers),
// not the file's base name, which a common word like status would match in
// hundreds of tests that never call it (mw-gq6.856).
type TestCallers struct {
	// Label is how the story is named: its plan key, or its id.
	Label string
	Count int
	Size  domain.Size
	// Waived is the story's callers_ok, the reason more callers than its size
	// allows do not need a split (mw-gq6.672); empty waives nothing. The count
	// line is still said for a waived story, the warning is not.
	Waived string
}

// Allows is the most test callers the size allows.
func (t TestCallers) Allows() int { return t.Size.CallersAllowed() }

// Over says the count is more than the size allows.
func (t TestCallers) Over() bool { return t.Count > t.Allows() }

// Unwaived says the count is more than the size allows and the story does not
// say why that is fine.
func (t TestCallers) Unwaived() bool { return t.Over() && strings.TrimSpace(t.Waived) == "" }

// Line is the count as mw file and mw path set say it, whatever the count is.
func (t TestCallers) Line() string {
	return fmt.Sprintf("%s: test callers: %d for size %s (allows %d)", t.Label, t.Count, t.Size, t.Allows())
}

// Warning is what is said of a story over its size's allowance.
func (t TestCallers) Warning() string {
	return fmt.Sprintf("WARN: %d test callers for size %s; split (a first story moving the callers onto one helper) or size up, for %s",
		t.Count, t.Size, t.Label)
}

// testCallersOf counts the test callers of a story worked by this path, and says
// whether it could: a story with no size or no plain file in its files key, a rig
// not checked out here, and a checkout that cannot be searched are none to judge
// and warn of nothing.
func testCallersOf(rigs map[string]string, files RigFiles, label string, path domain.Path) (TestCallers, bool) {
	if files == nil || !domain.KnownSize(path.Size) {
		return TestCallers{}, false
	}
	dir, here := rigs[strings.TrimSpace(path.Rig)]
	named := domain.CallerFiles(path.Files)
	if !here || dir == "" || len(named) == 0 {
		return TestCallers{}, false
	}
	n, err := files.TestCallers(dir, named)
	if err != nil {
		return TestCallers{}, false
	}
	return TestCallers{Label: label, Count: n, Size: path.Size, Waived: path.CallersOK}, true
}

// callerNotes prints the count of test callers of a story to out, and, when it
// is over its size's allowance and does not carry callers_ok, the warning to
// warn. It reports whether the story is over without a callers_ok, so that mw
// file --check can fail for it.
func callerNotes(out, warn io.Writer, callers TestCallers) bool {
	if out != nil {
		fmt.Fprintln(out, callers.Line())
	}
	if callers.Unwaived() && warn != nil {
		fmt.Fprintln(warn, callers.Warning())
	}
	return callers.Unwaived()
}
