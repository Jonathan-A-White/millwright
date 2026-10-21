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

func TestDirectoriesReadsEachNameThatMayBeADirectory(t *testing.T) {
	got := domain.Directories(" src/, src/Talk.tsx, src/chat/*.tsx, src/ipa/**, ./tests/e2e/*, features/ask.feature, **, ")
	want := []domain.NamedDirectory{
		{Written: "src/", Dir: "src", Whole: true},
		{Written: "src/Talk.tsx", Dir: "src/Talk.tsx"},
		{Written: "src/ipa/**", Dir: "src/ipa", Whole: true},
		{Written: "./tests/e2e/*", Dir: "tests/e2e", Whole: true},
		{Written: "features/ask.feature", Dir: "features/ask.feature"},
		{Written: "**", Dir: "", Whole: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("expected\n%+v\ngot\n%+v", want, got)
	}
}

func TestGlobsReadsEachNameNarrowerThanAWholeDirectory(t *testing.T) {
	got := domain.Globs(" src/, src/Talk.tsx, src/chat/*.tsx, src/ipa/**, ./tests/e2e/*.ts, tests/**/spec.ts, **, ")
	want := []domain.NamedGlob{
		{Written: "src/chat/*.tsx", Pattern: "src/chat/*.tsx", BareWildcard: true},
		{Written: "./tests/e2e/*.ts", Pattern: "tests/e2e/*.ts", BareWildcard: true},
		{Written: "tests/**/spec.ts", Pattern: "tests/**/spec.ts"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("expected\n%+v\ngot\n%+v", want, got)
	}
}

func TestCallerStemsAreTheBaseNamesOfThePlainFilesOnce(t *testing.T) {
	got := domain.CallerStems("src/screens/Settings.tsx, ./src/Settings.test.tsx, src/a/, src/**/*.ts, Makefile, .gitignore, src/screens/Settings.css")
	want := []string{"Settings", "Settings.test", "Makefile"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("CallerStems = %q, want %q", got, want)
	}
}

func TestEachSizeAllowsItsTestCallers(t *testing.T) {
	for size, want := range map[domain.Size]int{domain.SizeXS: 5, domain.SizeS: 10, domain.SizeM: 20, domain.SizeL: 35, domain.SizeXL: 50, "": 0} {
		if got := size.CallersAllowed(); got != want {
			t.Errorf("%q allows %d, want %d", size, got, want)
		}
	}
}
