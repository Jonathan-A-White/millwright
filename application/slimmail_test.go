package application

import (
	"strings"
	"testing"
)

const slimLedger = "| 2026-11-10 | The story (mw-1.1) | landed 0123456789ab on main | sonnet/high | 311,200 tokens (in 1,200, out 18,000), $4.21 | session s-1 |"

func slimReport() *NextReport {
	return &NextReport{
		Host: "vps", StoryID: "mw-1.1", Target: "main", Landed: true, Commits: 1, Pushes: 1,
		How:       Landed{Commit: "0123456789abcdef"},
		Ledger:    slimLedger,
		Committed: []string{"seats/builder/ledger.md"},
		Closed:    true,
		Bench:     []string{"gate 1m02s at load 0.3 of 8 cores", "FLAG tdd-feature: try a wider window (par off 80% over 5 landed)"},
	}
}

func slimMail(report *NextReport, verdict string) string {
	c := &closeOut{id: "mw-1.1", target: "main"}
	c.detail.Story.Title = "The story"
	return Next{Host: "vps"}.mayorMail(c, report, verdict, "").Body
}

func TestALandedMailLeavesOutWhatTheLedgerAndTheRunKeep(t *testing.T) {
	body := slimMail(slimReport(), MailLanded)
	for _, want := range []string{"0123456789ab", "sonnet/high", "311,200 tokens", "$4.21", "FLAG tdd-feature", "runs/mw-1.1/"} {
		if !strings.Contains(body, want) {
			t.Errorf("expected the mail to hold %q, got:\n%s", want, body)
		}
	}
	for _, not := range []string{"The story", "2026-11-10", "committed in the vault", "the story is closed", "gate 1m02s", "session s-1", "landed 0123456789ab on main |"} {
		if strings.Contains(body, not) {
			t.Errorf("expected the mail not to hold %q, got:\n%s", not, body)
		}
	}
	lines := strings.Split(strings.TrimRight(body, "\n"), "\n")
	if last := lines[len(lines)-1]; !strings.Contains(last, "runs/mw-1.1/") {
		t.Errorf("expected the last line to name the run, got %q", last)
	}
}

func TestAPlainLandingMailsUnderFiveHundredBytes(t *testing.T) {
	report := slimReport()
	report.Bench = []string{"gate 1m02s at load 0.3 of 8 cores", "tdd-feature took 20m against a par of 25m by size: 0.8x par"}
	if body := slimMail(report, MailLanded); len(body) >= 500 {
		t.Errorf("expected a plain landing to mail under 500 bytes, got %d:\n%s", len(body), body)
	}
}

func TestALandedMailKeepsEveryNoteAndTheBatchsFolder(t *testing.T) {
	report := slimReport()
	report.Notes = []string{"failed after-landing: make deploy: exit status 2"}
	report.RunFolder = "runs/batch/millwright/3/"
	body := slimMail(report, MailLanded)
	if !strings.Contains(body, "note    failed after-landing: make deploy: exit status 2") {
		t.Errorf("expected the note kept, got:\n%s", body)
	}
	if !strings.Contains(body, "runs/batch/millwright/3/") || strings.Contains(body, "runs/mw-1.1/") {
		t.Errorf("expected the batch's folder named, got:\n%s", body)
	}
}

func TestATestedMailIsCutTheSameWay(t *testing.T) {
	report := slimReport()
	report.Landed, report.Closed = false, true
	report.Tested = "mw-1.0"
	report.Findings = TesterFindings{Text: "FINDINGS", Held: []string{"mw-1.9"}}
	body := slimMail(report, MailTested)
	if !strings.Contains(body, "held    mw-1.9 filed by the Tester for its findings: release or drop them") {
		t.Errorf("expected the held line kept, got:\n%s", body)
	}
	for _, not := range []string{"committed in the vault", "the story is closed", "2026-11-10"} {
		if strings.Contains(body, not) {
			t.Errorf("expected the mail not to hold %q, got:\n%s", not, body)
		}
	}
}

func TestARefusedAndABlockedMailAreTheWholeReportAsBefore(t *testing.T) {
	for _, verdict := range []string{MailRefused, MailBlocked} {
		report := slimReport()
		report.Landed, report.Closed = false, false
		want := report.String()
		want = strings.TrimRight(want, "\n") + "\n\n" + CauseLine("mw-1.1") + "\n"
		if got := slimMail(report, verdict); got != want {
			t.Errorf("expected a %s mail to be the whole report and its cause line, got:\n%s\nwant:\n%s", verdict, got, want)
		}
	}
}
