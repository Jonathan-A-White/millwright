package domain

import (
	"strings"
	"testing"
	"time"
)

func TestParseHomeReadsHostTimeAndActor(t *testing.T) {
	got, err := ParseHome("laptop 2026-09-29T00:10:00Z mw@laptop\n")
	if err != nil {
		t.Fatal(err)
	}
	want := HomeRecord{Host: "laptop", At: time.Date(2026, 9, 29, 0, 10, 0, 0, time.UTC), By: "mw@laptop"}
	if got != want {
		t.Errorf("got %+v, wanted %+v", got, want)
	}
	if got.String() != "laptop 2026-09-29T00:10:00Z mw@laptop\n" {
		t.Errorf("the record does not print as it was read: %q", got.String())
	}
}

func TestParseHomeRefusesWhatIsNotOneLineOfTheKind(t *testing.T) {
	for name, text := range map[string]string{
		"empty":        "",
		"vps":          "vps 2026-09-29T00:10:00Z mw@laptop\n",
		"no time":      "laptop\n",
		"no actor":     "laptop 2026-09-29T00:10:00Z\n",
		"bad time":     "laptop yesterday mw@laptop\n",
		"two lines":    "laptop 2026-09-29T00:10:00Z mw@laptop\ndesktop 2026-09-29T00:10:00Z mw@laptop\n",
		"extra field":  "laptop 2026-09-29T00:10:00Z mw@laptop extra\n",
		"non-UTC time": "laptop 2026-09-29T00:10:00+02:00 mw@laptop\n",
	} {
		if _, err := ParseHome(text); err == nil {
			t.Errorf("%s: %q was accepted", name, text)
		} else if !strings.Contains(err.Error(), "home") {
			t.Errorf("%s: the refusal does not say what it is about: %v", name, err)
		}
	}
}
