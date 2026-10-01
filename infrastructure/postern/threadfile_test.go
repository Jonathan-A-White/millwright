package postern

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
)

func TestThreadFileRemembersChannelsBareOrPrefixed(t *testing.T) {
	f := NewThreadFile(filepath.Join(t.TempDir(), "state", "threads.jsonl"))
	if _, found, err := f.Lookup("direct:aaa"); err != nil || found {
		t.Fatalf("a missing file knows no post: found=%v err=%v", found, err)
	}
	for txid, thread := range map[string]application.PosternThread{
		"direct:aaa": {Bead: "mw-x"},
		"bbb":        {Topic: "roadmap"},
		"direct:ccc": {},
	} {
		if err := f.Remember(txid, thread); err != nil {
			t.Fatal(err)
		}
	}
	for txid, want := range map[string]application.PosternThread{
		"aaa": {Bead: "mw-x"}, "direct:aaa": {Bead: "mw-x"},
		"direct:bbb": {Topic: "roadmap"}, "ccc": {},
	} {
		got, found, err := f.Lookup(txid)
		if err != nil || !found || got != want {
			t.Errorf("Lookup(%q) = %+v, %v, %v; want %+v", txid, got, found, err, want)
		}
	}
	if _, found, _ := f.Lookup("direct:zzz"); found {
		t.Error("an unseen post was found")
	}
}

func TestThreadFileLastLineWinsAndSkipsGarbage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "threads.jsonl")
	f := NewThreadFile(path)
	if err := f.Remember("aaa", application.PosternThread{Bead: "mw-x"}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(mustRead(t, path), []byte("not json\n")...), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := f.Remember("aaa", application.PosternThread{Topic: "roadmap"}); err != nil {
		t.Fatal(err)
	}
	got, found, err := f.Lookup("aaa")
	if err != nil || !found || got != (application.PosternThread{Topic: "roadmap"}) {
		t.Errorf("Lookup = %+v, %v, %v; want the later line", got, found, err)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("the file should be 0600: %v %v", info, err)
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
