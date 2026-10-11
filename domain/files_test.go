package domain_test

import (
	"reflect"
	"testing"

	"github.com/Jonathan-A-White/millwright/domain"
)

func TestFileListSplitsTheFilesKeyAtCommas(t *testing.T) {
	got := domain.FileList(" src/Reader.tsx, ./src/Preface.tsx,,/docs/ , ")
	if want := []string{"src/Reader.tsx", "src/Preface.tsx", "docs"}; !reflect.DeepEqual(got, want) {
		t.Errorf("expected %q, got %q", want, got)
	}
	if got := domain.FileList(""); len(got) != 0 {
		t.Errorf("expected no files from an empty key, got %q", got)
	}
}

func TestSharedFile(t *testing.T) {
	for _, c := range []struct {
		name         string
		ours, theirs string
		want         string
		shared       bool
	}{
		{"the same file", "src/Reader.tsx", "src/Reader.tsx", "src/Reader.tsx", true},
		{"different files", "src/Reader.tsx", "src/Preface.tsx", "", false},
		{"the first of ours that meets", "src/Preface.tsx,src/Reader.tsx", "src/Reader.tsx,src/Preface.tsx", "src/Preface.tsx", true},
		{"their glob covers our file", "src/Reader.tsx", "src/*.tsx", "src/Reader.tsx", true},
		{"our glob covers their file", "src/*.tsx", "src/Reader.tsx", "src/Reader.tsx", true},
		{"a glob stays in its directory", "src/*.tsx", "src/data/settings.ts", "", false},
		{"a double star crosses directories", "src/**/*.ts", "src/data/repositories/settings.ts", "src/data/repositories/settings.ts", true},
		{"a double star matches no directory too", "src/**/*.ts", "src/settings.ts", "src/settings.ts", true},
		{"a directory covers what is under it", "src/data", "src/data/repositories/settings.ts", "src/data/repositories/settings.ts", true},
		{"a directory with a slash covers what is under it", "src/data/", "src/data/settings.ts", "src/data/settings.ts", true},
		{"a directory is not a file beside it", "src/data", "src/database.ts", "", false},
		{"nothing named", "", "src/Reader.tsx", "", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			got, shared := domain.SharedFile(domain.FileList(c.ours), domain.FileList(c.theirs))
			if got != c.want || shared != c.shared {
				t.Errorf("expected %q, %v, got %q, %v", c.want, c.shared, got, shared)
			}
		})
	}
}
