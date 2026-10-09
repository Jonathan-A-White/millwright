package domain

import (
	"strings"
	"testing"
)

func TestTheNextPatchRaisesOnlyTheLastNumber(t *testing.T) {
	for from, want := range map[string]string{"1.2.3": "1.2.4", "0.1.0": "0.1.1", "0.5.9": "0.5.10", "10.0.99": "10.0.100"} {
		got, err := NextPatch(from)
		if err != nil || got != want {
			t.Errorf("NextPatch(%q) = %q, %v; want %q", from, got, err, want)
		}
	}
	for _, bad := range []string{"", "1.2", "1.2.3-beta", "v1.2.3", "1.2.x", "1.2.3.4"} {
		if got, err := NextPatch(bad); err == nil {
			t.Errorf("NextPatch(%q) = %q; want an error", bad, got)
		}
	}
}

const aPackageLock = `{
  "name": "lampas",
  "version": "0.1.0",
  "lockfileVersion": 3,
  "requires": true,
  "packages": {
    "": {
      "name": "lampas",
      "version": "0.1.0",
      "dependencies": {
        "left-pad": "^1.3.0"
      }
    },
    "node_modules/left-pad": {
      "version": "1.3.0",
      "resolved": "https://registry.npmjs.org/left-pad/-/left-pad-1.3.0.tgz"
    }
  }
}
`

func TestAPackageLockChangesOnlyItsTwoVersionFieldsByteForByte(t *testing.T) {
	got, err := SetVersion([]byte(aPackageLock), "0.1.1")
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(aPackageLock, `"version": "0.1.0"`, `"version": "0.1.1"`, 2)
	if string(got) != want {
		t.Errorf("the lock file changed beyond its two version fields:\n%s\nwant:\n%s", got, want)
	}
	if strings.Count(string(got), `"0.1.1"`) != 2 || !strings.Contains(string(got), `"version": "1.3.0"`) {
		t.Errorf("a dependency's version was touched, or a field was missed:\n%s", got)
	}
}

func TestAPackageJSONKeepsItsFormattingAndNestedVersionsAlone(t *testing.T) {
	written := "{\n  \"name\": \"x\",\n  \"dependencies\": {\"version\": \"9.9.9\"},\n  \"version\": \"1.2.3\",\n  \"scripts\": {}\n}"
	got, err := SetVersion([]byte(written), "1.2.4")
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(written, `"version": "1.2.3"`, `"version": "1.2.4"`, 1)
	if string(got) != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
	if v, err := VersionOf([]byte(written)); err != nil || v != "1.2.3" {
		t.Errorf("VersionOf = %q, %v; want 1.2.3", v, err)
	}
}

func TestAFileWithNoVersionOrNoJSONIsAnError(t *testing.T) {
	for _, bad := range []string{`{"name": "x"}`, `not json`, `[1,2]`, `{"version": 3}`} {
		if _, err := VersionOf([]byte(bad)); err == nil {
			t.Errorf("VersionOf(%q) succeeded", bad)
		}
		if _, err := SetVersion([]byte(bad), "1.0.1"); err == nil {
			t.Errorf("SetVersion(%q) succeeded", bad)
		}
	}
}
