package application_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain"
)

// named is one attachment, announced under mime and the sender's name.
type named struct{ mime, name string }

// readNamed posts one message carrying a single attachment of each of files
// (several as `attachments`), runs the inbox, and reports what it printed and
// the file names left in the inbox directory.
func readNamed(t *testing.T, f *manyFiles, bead string, files ...named) (printed string, written []string) {
	t.Helper()
	var entries []any
	for _, file := range files {
		entry := f.add(t, file.mime, []byte("content of "+file.name))
		entry.Name = file.name
		entries = append(entries, entry)
	}
	body := map[string]any{"text": "a file"}
	if bead != "" {
		body["thread"] = map[string]any{"bead": bead}
	}
	if len(entries) == 1 {
		body["attachment"] = entries[0]
	} else {
		body["attachments"] = entries
	}
	f.post(t, "name-txid", body)
	out := &bytes.Buffer{}
	if _, err := f.inbox(out).Run(context.Background()); err != nil {
		t.Fatalf("running the inbox: %v", err)
	}
	dirEntries, err := os.ReadDir(f.dir)
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("reading %s: %v", f.dir, err)
	}
	for _, e := range dirEntries {
		written = append(written, e.Name())
	}
	return out.String(), written
}

// AC1: a name with a safe extension gives the file that extension, whatever
// its mime, and the printed line names the file.
func TestInboxKeepsAnAttachmentsOwnExtensionAndNamesIt(t *testing.T) {
	for _, tc := range []struct {
		mime, name, file string
	}{
		{"application/zip", "data.zip", "name-txid.zip"},
		{"text/markdown", "notes.md", "name-txid.md"},
		{"application/octet-stream", "Report.DOCX", "name-txid.docx"},
		{"image/png", "shot.final.png", "name-txid.png"},
	} {
		f := newManyFiles(t)
		printed, written := readNamed(t, f, "", named{tc.mime, tc.name})
		if len(written) != 1 || written[0] != tc.file {
			t.Errorf("%s: expected %s written, got %v", tc.name, tc.file, written)
			continue
		}
		path := filepath.Join(f.dir, tc.file)
		if !strings.Contains(printed, path) || !strings.Contains(printed, tc.name) || !strings.Contains(printed, tc.mime) {
			t.Errorf("%s: expected the line to name the path, name and mime, got:\n%s", tc.name, printed)
		}
	}
}

// AC1: the printed line also gives the size, and the bead comment names the
// file.
func TestInboxNamesANamedAttachmentInTheBeadComment(t *testing.T) {
	f := newManyFiles(t)
	f.tracker.AddStory("epic", domain.Story{ID: "mw-named.1"})
	printed, _ := readNamed(t, f, "mw-named.1", named{"application/zip", "data.zip"})

	if !strings.Contains(printed, "bytes") {
		t.Errorf("expected the size in the printed line, got:\n%s", printed)
	}
	comments, err := f.tracker.StoryComments(context.Background(), "mw-named.1")
	if err != nil || len(comments) == 0 {
		t.Fatalf("expected a comment on mw-named.1: %v", err)
	}
	last := comments[len(comments)-1].Text
	if !strings.Contains(last, "data.zip") || !strings.Contains(last, filepath.Join(f.dir, "name-txid.zip")) {
		t.Errorf("expected the comment to name data.zip and its path, got %q", last)
	}
}

// AC1: a name that is not a base name never leaves the inbox directory, and
// its extension is not trusted: the mime's extension, else .bin.
func TestInboxNeverLetsAnAttachmentsNameEscapeTheInboxDirectory(t *testing.T) {
	for _, tc := range []struct{ mime, name, file string }{
		{"application/pdf", "../../x.sh;rm", "name-txid.pdf"},
		{"application/zip", "../../x.sh;rm", "name-txid.bin"},
		{"application/pdf", "dir/notes.md", "name-txid.pdf"},
		{"application/pdf", `dir\notes.md`, "name-txid.pdf"},
		{"application/pdf", "evil.sh;rm", "name-txid.pdf"},
		{"application/pdf", "long.abcdefghijk", "name-txid.pdf"},
		{"application/pdf", "noext.", "name-txid.pdf"},
		{"application/pdf", "..", "name-txid.pdf"},
	} {
		f := newManyFiles(t)
		printed, written := readNamed(t, f, "", named{tc.mime, tc.name})
		if len(written) != 1 || written[0] != tc.file {
			t.Errorf("%q (%s): expected %s written inside the inbox, got %v", tc.name, tc.mime, tc.file, written)
		}
		if strings.ContainsAny(tc.name, `/\`) && strings.Contains(printed, "("+tc.name) {
			t.Errorf("%q: expected a name with a path part not printed, got:\n%s", tc.name, printed)
		}
		if _, err := os.Stat(filepath.Join(filepath.Dir(f.dir), "x.sh;rm")); err == nil {
			t.Errorf("%q: a file escaped the inbox directory", tc.name)
		}
	}
}

// AC1: each of several named files keeps its own extension.
func TestInboxKeepsEachOfSeveralNamedAttachmentsExtension(t *testing.T) {
	f := newManyFiles(t)
	_, written := readNamed(t, f, "", named{"application/zip", "a.zip"}, named{"text/markdown", "b.md"})
	if len(written) != 2 || written[0] != "name-txid-1.zip" || written[1] != "name-txid-2.md" {
		t.Errorf("expected name-txid-1.zip and name-txid-2.md, got %v", written)
	}
}

// AC2: a record with no name is written as before: the mime's extension, else
// .bin, and the line is the path alone.
func TestInboxWritesAnUnnamedAttachmentAsBefore(t *testing.T) {
	for mime, file := range map[string]string{"image/png": "name-txid.png", "application/zip": "name-txid.bin"} {
		f := newManyFiles(t)
		printed, written := readNamed(t, f, "", named{mime, ""})
		if len(written) != 1 || written[0] != file {
			t.Errorf("%s: expected %s, got %v", mime, file, written)
		}
		if !strings.Contains(printed, "\n"+filepath.Join(f.dir, file)+"\n") {
			t.Errorf("%s: expected the path alone on its line, got:\n%s", mime, printed)
		}
	}
}

// AC3: any file is attached, its mime from the extension map, else
// application/octet-stream, and its base name sent as the name.
func TestPosternSendAttachesAnyFileWithItsName(t *testing.T) {
	for _, tc := range []struct{ file, mime string }{
		{"notes.md", "text/plain"},
		{"data.zip", "application/octet-stream"},
		{"tool.exe", "application/octet-stream"},
		{"Makefile", "application/octet-stream"},
	} {
		f := newSendFixture()
		path := filepath.Join(t.TempDir(), tc.file)
		mustDo(t, os.WriteFile(path, []byte("bytes"), 0o600))
		if _, err := f.send("").Run(context.Background(), application.PosternSendRequest{Text: "x", Attachments: []string{path}}); err != nil {
			t.Errorf("%s: sending: %v", tc.file, err)
			continue
		}
		_, text := f.delivered(t, 0)
		if !strings.Contains(text, `"mime":"`+tc.mime+`"`) || !strings.Contains(text, `"name":"`+tc.file+`"`) {
			t.Errorf("%s: expected mime %s and the name announced, got %s", tc.file, tc.mime, text)
		}
	}
}
