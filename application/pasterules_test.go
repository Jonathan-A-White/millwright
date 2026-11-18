package application_test

import (
	"context"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
)

const newPowerShell = "Open a new Windows PowerShell and paste this:\n"

func fenced(lines ...string) string { return "```\n" + strings.Join(lines, "\n") + "\n```" }

// Each rule of the paste table: a message that breaks it is refused with the
// rule's name and reason in the error text; a clean one passes.
func TestPasteRules(t *testing.T) {
	cases := []struct {
		name    string
		surface application.PasteSurface
		text    string
		class   string
		long    bool
		rule    string // the rule that refuses; "" for none
		warn    string // substring of the one warning; "" for none
	}{
		{name: "a plain message", text: "Landed. Nothing to do."},

		{name: "no-base64: a pipe to base64 -d", text: "run " + fenced("echo W1Vu | base64 -d"), rule: "no-base64"},
		{name: "no-base64: in any case", text: "use BASE64 here", rule: "no-base64"},
		{name: "no-base64: -EncodedCommand", text: fenced("powershell -EncodedCommand abc"), rule: "no-base64"},
		{name: "no-base64: powershell -encodedcommand lower case", text: "x -encodedcommand y", rule: "no-base64"},
		{name: "no-base64: clean", text: "a plain message about a file"},

		{name: "windows-paste-one-line: three lines", text: newPowerShell + fenced("wsl.exe --shutdown", "Get-ScheduledTask foo", "Set-Foo bar"), rule: "windows-paste-one-line"},
		{name: "windows-paste-one-line: joined by ;", text: newPowerShell + fenced("wsl.exe --shutdown; Get-ScheduledTask foo; Set-Foo bar")},
		{name: "windows-paste-one-line: a PS C:\\ block of two lines", text: fenced(`PS C:\Users\j> Get-Date`, "next line"), rule: "windows-paste-one-line"},
		{name: "windows-paste-one-line: two lines of ordinary shell are not a Windows paste", text: fenced("cd /tmp", "ls")},
		{name: "windows-paste-one-line: a blank line at the end is not a second line", text: newPowerShell + "```\nwsl.exe --shutdown\n\n```"},

		{name: "code-in-one-fence: two fenced blocks", text: fenced("ls") + "\nand\n" + fenced("pwd"), rule: "code-in-one-fence"},
		{name: "code-in-one-fence: an inline mw command", text: "run `mw host trim-setup --check` now", rule: "code-in-one-fence"},
		{name: "code-in-one-fence: an inline git command", text: "try `git status` now", rule: "code-in-one-fence"},
		{name: "code-in-one-fence: one block", text: "run:\n" + fenced("mw host trim-setup --check")},
		{name: "code-in-one-fence: a backtick file name", text: "see `foo.go` for it"},
		{name: "code-in-one-fence: a command name alone", text: "the `go` tool and `mw` itself"},
		{name: "code-in-one-fence: a span inside a block is not inline", text: "run:\n" + fenced("echo `git status`")},

		{name: "windows-paste-names-powershell: no words", text: "paste this:\n" + fenced("wsl.exe --shutdown"), rule: "windows-paste-names-powershell"},
		{name: "windows-paste-names-powershell: the words", text: "In a NEW Windows PowerShell, paste:\n" + fenced("wsl.exe --shutdown")},
		{name: "windows-paste-names-powershell: a PS C:\\ prompt", text: "paste:\n" + fenced(`PS C:\> wsl.exe --shutdown`)},
		{name: "windows-paste-names-powershell: no paste, no words needed", text: "run:\n" + fenced("ls -l")},

		{name: "words: 80 sends clean", text: words(80)},
		{name: "words: 81 warns and sends", text: words(81), warn: "81 words"},
		{name: "words: 121 refuses", text: words(121), rule: "words"},
		{name: "words: 121 with --long sends", text: words(121), long: true},
		{name: "words: a card question of 61 warns", text: words(61), class: "decision-needed", warn: "61 words"},
		{name: "words: a card question of 121 refuses", text: words(121), class: "decision-needed", rule: "words"},
		{name: "words: a fence's words do not count", text: words(70) + "\n" + fenced(words(300))},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			surface := c.surface
			if surface == "" {
				surface = application.PasteSurfaceSend
			}
			verdict, err := application.PasteCheck{Surface: surface, Text: c.text, Class: c.class, Long: c.long}.Run()
			if c.rule != "" {
				if err == nil {
					t.Fatalf("expected the %s rule to refuse, got none", c.rule)
				}
				rule := findPasteRule(t, c.rule)
				for _, want := range []string{c.rule, rule.Reason} {
					if !strings.Contains(err.Error(), want) {
						t.Errorf("expected the refusal to carry %q, got: %v", want, err)
					}
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected refusal: %v", err)
			}
			switch {
			case c.warn == "" && len(verdict.Warnings) != 0:
				t.Fatalf("expected no warning, got %q", verdict.Warnings)
			case c.warn != "" && (len(verdict.Warnings) != 1 || !strings.Contains(verdict.Warnings[0], c.warn) || strings.Contains(verdict.Warnings[0], "\n")):
				t.Fatalf("expected one warning line naming %q, got %q", c.warn, verdict.Warnings)
			}
		})
	}
}

func findPasteRule(t *testing.T, name string) application.PasteRule {
	t.Helper()
	for _, r := range application.PasteRules {
		if r.Name == name {
			return r
		}
	}
	t.Fatalf("no paste rule named %q in the table", name)
	return application.PasteRule{}
}

// The table is the one place the rules are named: each row has a name and a reason.
func TestPasteRulesTableHoldsTheFiveRules(t *testing.T) {
	var got []string
	for _, r := range application.PasteRules {
		if r.Name == "" || r.Reason == "" {
			t.Errorf("a row of the table has no name or no reason: %+v", r)
		}
		got = append(got, r.Name)
	}
	want := "no-base64 windows-paste-one-line code-in-one-fence windows-paste-names-powershell words"
	if strings.Join(got, " ") != want {
		t.Fatalf("expected the rows %q, got %q", want, got)
	}
}

// mw hands add runs rules 1 and 2 only; a hands step is the commands as they
// run, not a message with fences.
func TestPasteRulesOfAHandsStep(t *testing.T) {
	hands := func(text string) error {
		_, err := application.PasteCheck{Surface: application.PasteSurfaceHands, Text: text}.Run()
		return err
	}
	if err := hands("echo W1Vu | base64 -d"); err == nil || !strings.Contains(err.Error(), "no-base64") {
		t.Errorf("expected base64 refused, got %v", err)
	}
	if err := hands("wsl.exe --shutdown\nGet-ScheduledTask x"); err == nil || !strings.Contains(err.Error(), "windows-paste-one-line") {
		t.Errorf("expected a multi-line Windows command refused, got %v", err)
	}
	for _, ok := range []string{
		"loginctl enable-linger jwhite",
		"wsl.exe --shutdown; Get-ScheduledTask x",
		"cd /tmp\nls",
		"run `git status` and " + words(200),
		"wsl.exe --shutdown",
	} {
		if err := hands(ok); err != nil {
			t.Errorf("expected %q to pass, got %v", ok, err)
		}
	}
}

// An override names the rule, says why, and names the bead the reason is
// written on; it is written only once the rule would have refused.
func TestPasteRulesAllow(t *testing.T) {
	base := application.PasteCheck{Surface: application.PasteSurfaceSend, Text: "echo W1Vu | base64 -d"}

	unknown := base
	unknown.Allow, unknown.Why, unknown.Bead = []string{"no-such-rule"}, "because", "mw-a.1"
	if _, err := unknown.Run(); err == nil || !strings.Contains(err.Error(), "no-such-rule") || !strings.Contains(err.Error(), "no-base64") {
		t.Errorf("expected an unknown rule refused with the table's names, got %v", err)
	}

	noWhy := base
	noWhy.Allow, noWhy.Why, noWhy.Bead = []string{"no-base64"}, "  ", "mw-a.1"
	if _, err := noWhy.Run(); err == nil || !strings.Contains(err.Error(), "--allow-why") {
		t.Errorf("expected an empty reason refused, got %v", err)
	}

	noBead := base
	noBead.Allow, noBead.Why = []string{"no-base64"}, "because"
	if _, err := noBead.Run(); err == nil || !strings.Contains(err.Error(), "name the bead the reason is written on") {
		t.Errorf("expected no bead refused, got %v", err)
	}

	ok := base
	ok.Allow, ok.Why, ok.Bead = []string{"no-base64"}, "a test fixture", "mw-a.1"
	verdict, err := ok.Run()
	if err != nil {
		t.Fatalf("expected the allowed rule to pass, got %v", err)
	}
	tracker := &commentRecorder{}
	if err := verdict.Record(context.Background(), tracker); err != nil {
		t.Fatal(err)
	}
	if len(tracker.comments) != 1 || tracker.comments[0] != "mw-a.1|ALLOWED no-base64: a test fixture" {
		t.Errorf("expected the reason written on the bead, got %q", tracker.comments)
	}

	// A rule allowed that did not fire writes nothing.
	clean := application.PasteCheck{Surface: application.PasteSurfaceSend, Text: "hello", Allow: []string{"no-base64"}, Why: "x", Bead: "mw-a.1"}
	verdict, err = clean.Run()
	if err != nil {
		t.Fatal(err)
	}
	tracker = &commentRecorder{}
	if err := verdict.Record(context.Background(), tracker); err != nil || len(tracker.comments) != 0 {
		t.Errorf("expected nothing written for a rule that did not fire, got %q, %v", tracker.comments, err)
	}

	// The rules not allowed still refuse.
	other := application.PasteCheck{Surface: application.PasteSurfaceSend, Text: "base64 " + words(121), Allow: []string{"no-base64"}, Why: "x", Bead: "mw-a.1"}
	if _, err := other.Run(); err == nil || !strings.Contains(err.Error(), "words") {
		t.Errorf("expected the words rule still to refuse, got %v", err)
	}
}

type commentRecorder struct{ comments []string }

func (c *commentRecorder) CommentOnStory(_ context.Context, id, text string) error {
	c.comments = append(c.comments, id+"|"+text)
	return nil
}

// The help is built from the table, so none can be left out.
func TestPasteRulesHelpNamesEveryRowOfTheTable(t *testing.T) {
	help := application.PasteRulesHelp(application.PasteSurfaceSend)
	for _, r := range application.PasteRules {
		if !strings.Contains(help, r.Name) || !strings.Contains(help, r.Reason) {
			t.Errorf("expected the help to carry %q and its reason, got:\n%s", r.Name, help)
		}
	}
}
