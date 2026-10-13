package application

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

// NoteStampToken is what a person types in a note where the time goes.
const NoteStampToken = "{now}"

// NoteTimeLayout is how mw note writes the time: UTC to the minute, like
// 2026-10-13 06:30Z.
const NoteTimeLayout = "2006-01-02 15:04Z"

// Note adds a comment to a bead with the text given, every {now} in it
// replaced by the clock's UTC time at the moment of writing, so that a seat
// never types a time. Text without {now} is written as it is. RunFile reads
// the text from a file first.
//
// Every refusal comes before the write: a bead the tracker does not know, a
// file that cannot be read and a note with no text change nothing.
type Note struct {
	Tracker WorkTracker

	// Now is the clock the time is read from. The zero value reads the real one.
	Now func() time.Time
	// ReadFile reads the text of RunFile. The zero value reads the disk.
	ReadFile func(path string) ([]byte, error)
	// Out is where the report is printed. A nil Out prints nothing.
	Out io.Writer
}

// NoteReport is what one run of mw note wrote.
type NoteReport struct {
	BeadID string
	// Text is the comment as written, {now} replaced.
	Text string
	// Time is the time every {now} was replaced with.
	Time string
}

// Run adds text to bead id as a comment.
func (n Note) Run(ctx context.Context, id, text string) (NoteReport, error) {
	report := NoteReport{BeadID: strings.TrimSpace(id)}
	if n.Tracker == nil {
		return report, fmt.Errorf("noting on a bead: there is no work tracker to note it in")
	}
	if report.BeadID == "" {
		return report, fmt.Errorf("noting on a bead: which bead?")
	}
	if strings.TrimSpace(text) == "" {
		return report, fmt.Errorf("noting on %s: say the text of the note", report.BeadID)
	}
	if _, err := n.Tracker.ShowStory(ctx, report.BeadID); err != nil {
		return report, fmt.Errorf("noting on %s: %w", report.BeadID, err)
	}

	now := time.Now
	if n.Now != nil {
		now = n.Now
	}
	report.Time = now().UTC().Format(NoteTimeLayout)
	report.Text = strings.ReplaceAll(text, NoteStampToken, report.Time)

	if err := n.Tracker.CommentOnStory(ctx, report.BeadID, report.Text); err != nil {
		return report, fmt.Errorf("noting on %s: %w", report.BeadID, err)
	}
	if n.Out != nil {
		fmt.Fprint(n.Out, report.String())
	}
	return report, nil
}

// RunFile adds the text of the file at path to bead id as a comment.
func (n Note) RunFile(ctx context.Context, id, path string) (NoteReport, error) {
	read := os.ReadFile
	if n.ReadFile != nil {
		read = n.ReadFile
	}
	body, err := read(path)
	if err != nil {
		return NoteReport{BeadID: strings.TrimSpace(id)}, fmt.Errorf("noting on %s: reading the text: %w", strings.TrimSpace(id), err)
	}
	return n.Run(ctx, id, strings.TrimRight(string(body), "\r\n"))
}

// String is the report as a person reads it: the time used.
func (r NoteReport) String() string {
	return fmt.Sprintf("noted on %s at %s\n", r.BeadID, r.Time)
}
