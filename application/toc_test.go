package application_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

func tocDoc(sections int, extra string) string {
	var b strings.Builder
	b.WriteString("# Title\n\n")
	for i := 1; i <= sections; i++ {
		b.WriteString("## Section " + strings.Repeat("x", i) + "\n\ntext\n\n")
	}
	return b.String() + extra
}

func TestTocCheckNamesTheStaleAndTheMissingAndNothingElse(t *testing.T) {
	files := apptest.NewFakeTocFiles()
	files.Docs["stale.md"] = "# T\n\n" + domain.TocOpen + "\n- [old](#old)\n" + domain.TocClose + "\n\n## A\n"
	files.Docs["missing.md"] = tocDoc(4, "")
	files.Docs["below.md"] = tocDoc(3, "")
	files.Docs["optout.md"] = tocDoc(6, domain.TocNone+"\n")
	files.Docs["current.md"] = domain.ApplyToc("# T\n\n"+domain.TocOpen+"\n"+domain.TocClose+"\n\n## A\n\n## B\n", 4)
	files.Docs["CHANGELOG.md"] = tocDoc(10, "")

	var out bytes.Buffer
	report, err := application.Toc{Files: files, Out: &out}.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := "stale.md: TOC stale\nmissing.md: TOC missing (4 ## headings)\nmw toc fix stale.md missing.md\n"
	if out.String() != want || !report.Failed() {
		t.Fatalf("printed %q, want %q", out.String(), want)
	}
	if files.Writes != 0 {
		t.Fatalf("check wrote %d files", files.Writes)
	}
}

func TestTocFixThenCheckIsSilentAndAFixAgainWritesNothing(t *testing.T) {
	files := apptest.NewFakeTocFiles()
	files.Docs["doc.md"] = "# T\n\nintro\n\n## A\n### A1\n## B\n### B1\n## C\n## D\n## E\n"
	toc := application.Toc{Files: files}
	if _, err := toc.Fix(context.Background()); err != nil {
		t.Fatal(err)
	}
	once := files.Docs["doc.md"]
	if !strings.Contains(once, "intro\n\n"+domain.TocOpen+"\n\n- [A](#a)\n  - [A1](#a1)\n") {
		t.Fatalf("no table before the first ##:\n%s", once)
	}
	var out bytes.Buffer
	toc.Out = &out
	if _, err := toc.Fix(context.Background()); err != nil {
		t.Fatal(err)
	}
	if files.Docs["doc.md"] != once || files.Writes != 1 || out.Len() != 0 {
		t.Fatalf("a second fix wrote (writes %d, printed %q)", files.Writes, out.String())
	}
	if report, err := toc.Check(context.Background()); err != nil || report.Failed() || out.Len() != 0 {
		t.Fatalf("check after fix: %v %v %q", report, err, out.String())
	}
}

func TestTocFixPrintsAFileEachAndNamedPathsSkipChangelogs(t *testing.T) {
	files := apptest.NewFakeTocFiles()
	files.Docs["a.md"] = tocDoc(4, "")
	files.Docs["CHANGELOG-old.md"] = tocDoc(9, "")
	var out bytes.Buffer
	if _, err := (application.Toc{Files: files, Out: &out}).Fix(context.Background(), "a.md", "CHANGELOG-old.md"); err != nil {
		t.Fatal(err)
	}
	if out.String() != "a.md: TOC written\n" || files.Docs["CHANGELOG-old.md"] != tocDoc(9, "") {
		t.Fatalf("printed %q", out.String())
	}
}

func TestTocWithNoPathsTakesTrackedFilesOnly(t *testing.T) {
	files := apptest.NewFakeTocFiles()
	files.Docs["tracked.md"] = tocDoc(5, "")
	files.Docs["untracked.md"] = tocDoc(5, "")
	files.Untracked["untracked.md"] = true
	var out bytes.Buffer
	if _, err := (application.Toc{Files: files, Out: &out}).Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "tracked.md") || strings.Contains(out.String(), "untracked.md") {
		t.Fatalf("printed %q", out.String())
	}
}

func TestTocMinIsTheThreshold(t *testing.T) {
	files := apptest.NewFakeTocFiles()
	files.Docs["a.md"] = tocDoc(3, "")
	var out bytes.Buffer
	toc := application.Toc{Files: files, Out: &out}
	if report, _ := toc.Check(context.Background()); report.Failed() {
		t.Fatalf("3 ## failed at the default threshold: %q", out.String())
	}
	toc.Min = 3
	if report, _ := toc.Check(context.Background()); !report.Failed() {
		t.Fatal("3 ## passed at --min 3")
	}
}

func TestTocAMissingNamedFileIsAnError(t *testing.T) {
	if _, err := (application.Toc{Files: apptest.NewFakeTocFiles()}).Check(context.Background(), "nope.md"); err == nil || !strings.Contains(err.Error(), "nope.md") {
		t.Fatalf("err = %v", err)
	}
}
