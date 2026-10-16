package doctor

import (
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
)

func TestProxyAddressReadsTheHostAndPortAProxySettingPointsAt(t *testing.T) {
	cases := []struct {
		value string
		want  string
		ok    bool
	}{
		{"http://127.0.0.1:8080", "127.0.0.1:8080", true},
		{"127.0.0.1:8080", "127.0.0.1:8080", true},
		{"http://user:pw@proxy.example:3128/", "proxy.example:3128", true},
		{"https://proxy.example", "proxy.example:443", true},
		{"http://proxy.example", "proxy.example:80", true},
		{"socks5://localhost", "localhost:1080", true},
		{"http://[::1]:8080", "[::1]:8080", true},
		{"", "", false},
		{"http://", "", false},
		{"gopher://proxy.example", "", false},
		{"http://proxy.example:port", "", false},
	}
	for _, c := range cases {
		got, ok := proxyAddress(c.value)
		if got != c.want || ok != c.ok {
			t.Errorf("proxyAddress(%q) = %q, %v; want %q, %v", c.value, got, ok, c.want, c.ok)
		}
	}
}

func TestOnlyTheSixProxySettingsAreProxyNames(t *testing.T) {
	for _, name := range []string{"HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy", "ALL_PROXY", "all_proxy"} {
		if !isProxyName(name) {
			t.Errorf("%s should be a proxy name", name)
		}
	}
	for _, name := range []string{"NO_PROXY", "no_proxy", "PATH", ""} {
		if isProxyName(name) {
			t.Errorf("%s should not be a proxy name", name)
		}
	}
}

func TestTheLimitMenuIsReadWithItsNoticeAndChoices(t *testing.T) {
	pane := "● Reading the newest handoff first.\n\n" +
		"  ⎿  You've hit your weekly limit · resets Oct 16, 7am (UTC)\n\n" +
		" │ What do you want to do?\n" +
		" │ ❯ 1. Stop and wait for limit to reset\n" +
		" │   2. Switch to usage credits\n" +
		" │   3. Upgrade your plan\n"
	limit, ok := limitOf(pane)
	if !ok {
		t.Fatalf("expected the limit menu to be read, got nothing from:\n%s", pane)
	}
	want := "⎿  You've hit your weekly limit · resets Oct 16, 7am (UTC)\n│ ❯ 1. Stop and wait for limit to reset\n│   2. Switch to usage credits"
	if limit != want {
		t.Errorf("expected the notice and the two limit choices:\n%s\ngot:\n%s", want, limit)
	}
}

func TestTheNoticeAloneAtTheBottomOfThePaneIsTheLimit(t *testing.T) {
	pane := "● Reading the newest handoff first.\n\n  ⎿  You’ve hit your session limit · resets 3pm\n\n──────\n❯ \n──────\n  ? for shortcuts\n"
	if _, ok := limitOf(pane); !ok {
		t.Errorf("expected the harness's limit notice above the input box to be the limit:\n%s", pane)
	}
}

func TestTheLimitWordsQuotedInProseAreNotTheLimit(t *testing.T) {
	for name, pane := range map[string]string{
		"a story's title": "● mw-gq6.433 · millwright\n  [bug] The doctor's mayor-stuck misses a Mayor window parked on the weekly-limit menu ('Stop and wait / Switch to usage credits')\n❯ \n",
		"its criteria":    "  4. Sweep: the Builder panes too. 14 stories sat stopped at the limit with\n  result.json 'You've hit your weekly limit' and stayed in_progress.\n❯ \n",
		"a reply":         "● The Builders said they hit your weekly limit, so I held dispatch.\n❯ \n",
	} {
		if limit, ok := limitOf(pane); ok {
			t.Errorf("%s: words quoted in prose are not the limit, got %q", name, limit)
		}
	}
}

func TestALimitNoticeTheMayorWorkedPastIsNotTheLimit(t *testing.T) {
	pane := "  ⎿  You've hit your weekly limit · resets Oct 16, 7am (UTC)\n" + strings.Repeat("● Filed it.\n", application.PaneLimitTail) + "❯ \n"
	if limit, ok := limitOf(pane); ok {
		t.Errorf("a notice scrolled up the pane with no menu under it is not the limit, got %q", limit)
	}
}
