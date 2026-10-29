package application

import (
	"context"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"

	"github.com/Jonathan-A-White/millwright/domain"
)

// TocFiles is the markdown mw toc reads and writes. The real adapter is cmd/mw's
// diskTocFiles, the fake apptest.FakeTocFiles.
type TocFiles interface {
	// Tracked are the tracked markdown files of the repository at the working
	// directory, by path from it.
	Tracked() ([]string, error)
	// Read is the text of the file at path.
	Read(path string) (string, error)
	// Write replaces the text of the file at path.
	Write(path, text string) error
}

// Toc is mw toc check and mw toc fix: a table of contents between <!-- toc -->
// and <!-- /toc --> built from a document's "## " and "### " headings, wanted
// at Min or more "## " headings. A document holding the line <!-- toc: none -->,
// every CHANGELOG*.md and anything under node_modules are left alone.
type Toc struct {
	Files TocFiles
	// Min is how many "## " headings make a document want a table. Zero reads
	// domain.TocMinHeadings.
	Min int
	// Out is where the report is printed. A nil Out prints nothing.
	Out io.Writer
}

// TocFinding is a document whose table is not as its headings give it.
type TocFinding struct {
	Path string
	// Headings is how many "## " headings the document has.
	Headings int
	// Missing is a document that wants a table and has no markers; otherwise its
	// table is stale.
	Missing bool
}

// TocReport is what one run of mw toc found or wrote.
type TocReport struct {
	// Findings are the documents with a stale table, then those with none.
	Findings []TocFinding
	// Written are the documents mw toc fix wrote.
	Written []string
}

// Failed is whether a document needs its table written.
func (r TocReport) Failed() bool { return len(r.Findings) > 0 }

// String is the report as a person reads it: a line per document, then for a
// check the command that cures them all.
func (r TocReport) String() string {
	var b strings.Builder
	for _, path := range r.Written {
		fmt.Fprintf(&b, "%s: TOC written\n", path)
	}
	var paths []string
	for _, f := range r.Findings {
		if f.Missing {
			fmt.Fprintf(&b, "%s: TOC missing (%d ## headings)\n", f.Path, f.Headings)
		} else {
			fmt.Fprintf(&b, "%s: TOC stale\n", f.Path)
		}
		paths = append(paths, f.Path)
	}
	if len(paths) > 0 {
		fmt.Fprintf(&b, "mw toc fix %s\n", strings.Join(paths, " "))
	}
	return b.String()
}

// tocLeftAlone is whether a document is never touched by mw toc: not markdown,
// a changelog, or under node_modules.
func tocLeftAlone(file string) bool {
	if !strings.HasSuffix(file, ".md") {
		return true
	}
	if strings.HasPrefix(strings.ToUpper(path.Base(file)), "CHANGELOG") {
		return true
	}
	for _, part := range strings.Split(path.Clean(file), "/") {
		if part == "node_modules" {
			return true
		}
	}
	return false
}

// files are the documents to look at: those named, or the tracked ones.
func (t Toc) files(named []string) ([]string, error) {
	if t.Files == nil {
		return nil, fmt.Errorf("toc: there are no files to read")
	}
	list := named
	if len(list) == 0 {
		tracked, err := t.Files.Tracked()
		if err != nil {
			return nil, fmt.Errorf("toc: listing the tracked markdown: %w", err)
		}
		list = append([]string{}, tracked...)
		sort.Strings(list)
	}
	var out []string
	for _, file := range list {
		if !tocLeftAlone(file) {
			out = append(out, file)
		}
	}
	return out, nil
}

func (t Toc) min() int {
	if t.Min > 0 {
		return t.Min
	}
	return domain.TocMinHeadings
}

// Check finds the documents, of those named or the tracked ones, whose table is
// stale or missing. It writes nothing.
func (t Toc) Check(ctx context.Context, named ...string) (TocReport, error) {
	return t.run(named, false)
}

// Fix writes the table of each document that needs one.
func (t Toc) Fix(ctx context.Context, named ...string) (TocReport, error) {
	return t.run(named, true)
}

func (t Toc) run(named []string, write bool) (TocReport, error) {
	var report TocReport
	files, err := t.files(named)
	if err != nil {
		return report, err
	}
	var stale, missing []TocFinding
	for _, file := range files {
		text, err := t.Files.Read(file)
		if err != nil {
			return report, fmt.Errorf("toc: reading %s: %w", file, err)
		}
		state := domain.TocStateOf(text, t.min())
		if state == domain.TocFine {
			continue
		}
		if write {
			if err := t.Files.Write(file, domain.ApplyToc(text, t.min())); err != nil {
				return report, fmt.Errorf("toc: writing %s: %w", file, err)
			}
			report.Written = append(report.Written, file)
			continue
		}
		finding := TocFinding{Path: file, Missing: state == domain.TocMissing}
		for _, h := range domain.Headings(text) {
			if h.Level == 2 {
				finding.Headings++
			}
		}
		if finding.Missing {
			missing = append(missing, finding)
		} else {
			stale = append(stale, finding)
		}
	}
	report.Findings = append(stale, missing...)
	if t.Out != nil {
		fmt.Fprint(t.Out, report.String())
	}
	return report, nil
}
