package application_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// weeklyDump is a dump the size of the Sunday of 2026-11-15 (mw-gq6.1344): 811
// live beads, each with a title and a description of 500 characters.
func weeklyDump(beads int) string {
	var dump strings.Builder
	for i := 0; i < beads; i++ {
		fmt.Fprintf(&dump, `{"id":"mw-w.%d","title":"Title of bead number %d, said in a line","type":"task","status":"open","priority":%d,"labels":["x"],"rig":"alpha","size":"S","parent":"mw-w","created":"2026-10-01T00:00:00Z","updated":"2026-10-15T00:00:00Z","days_since_update":%d,"ready":%t,"days_ready":1,"deps":[],"dependents":[],"description":"%s","his_priority":false}`+"\n",
			i, i, i%5, i%9, i%3 == 0, strings.Repeat("d", 500))
	}
	return dump.String()
}

// weeklyReport is a thirty-day report in mw report's text form: the sections in
// its order, QUALITY, SPEED and FACTORY CHANGES long, and the ones a weekly pass
// reads (FUEL by model and host, INVESTMENTS, CHEAP WINS) at the end or short.
func weeklyReport() string {
	var out strings.Builder
	out.WriteString("QUALITY 2026-10-17 to 2026-11-16 (30 days)\n\nby rig\n")
	for out.Len() < 20_000 {
		out.WriteString("lampas: landed 1168 up better, refused 171 (13% down better)\n  tests-fail 41\n")
	}
	out.WriteString("\nSPEED\n\nby rig\n")
	for out.Len() < 40_000 {
		out.WriteString("bsv-kit\n  par x0.9 median, x3.2 p90\n")
	}
	out.WriteString("\nFUEL\n\nby rig\nbsv-kit\n  tokens/landing 1,554,373\n\nby model\n  fable $13.33/landing, 19M tokens\n\nby host\n  laptop 41M tokens a day\n")
	out.WriteString("\nSEATS\n  mayor $3221.13 (1.27/landing)\n\nHIS TIME\n  review queue 19 (limit 8)\n\nMODELS                          opus sonnet  haiku  fable\nlanded 122 2534 8 24\n")
	out.WriteString("\nFACTORY CHANGES (millwright landings, 30 days)\n")
	for out.Len() < 65_000 {
		out.WriteString("[bug] a change of the factory, said in a line\n  landed 2026-10-17, Expect: bugs/landing down\n  escaped bugs 0 in the 14 days after\n")
	}
	out.WriteString("\nINVESTMENTS\n  WIN mw-5r3p30.452\n    gain: 0 audible-link hits\n\nCHEAP WINS\n1. 8 refusals have no cause: mw cause mw-0bc564\n")
	return out.String()
}

// weeklyGather is the gather fixture made the size of the Sunday of 2026-11-15:
// the dump, a diff against a dump of other beads, the 30-day report, a queue
// longer than the nightly budget, and a week of his messages and 25 pictures.
func weeklyGather(t *testing.T) (application.SleepGather, []string) {
	t.Helper()
	g := gatherFixture()
	dump := weeklyDump(811)
	var earlier strings.Builder
	for i := 700; i < 1500; i++ {
		fmt.Fprintf(&earlier, `{"id":"mw-w.%d","type":"task","status":"open","priority":2}`+"\n", i)
	}
	g.Dump = func(_ context.Context, w io.Writer) error { _, err := io.WriteString(w, dump); return err }
	g.Earlier = earlierDump{name: "plans/sleep-2026-11-14/triage.jsonl", text: earlier.String(), ok: true}
	report := weeklyReport()
	g.Mw = func(_ context.Context, args ...string) (string, error) {
		switch args[0] {
		case "report":
			return report, nil
		case "queue":
			return strings.Repeat(`  {"story": "mw-w.1", "why": "ready"},`+"\n", 1046), nil
		}
		return "mw " + strings.Join(args, " ") + "\n", nil
	}
	g.Now = func() time.Time { return time.Date(2026, 11, 15, 7, 30, 0, 0, time.UTC) }
	inbox := t.TempDir()
	var messages []string
	for n := 0; n < 20; n++ {
		stamp := g.Now().Add(-time.Duration(n+1) * 5 * time.Hour)
		for _, name := range []string{fmt.Sprintf("direct-m%02d.txt", n), fmt.Sprintf("direct-p%02d.jpg", n)} {
			path := filepath.Join(inbox, name)
			if err := os.WriteFile(path, []byte(strings.Repeat("I tapped Save and saw "+name+". ", 40)), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Chtimes(path, stamp, stamp); err != nil {
				t.Fatal(err)
			}
		}
		messages = append(messages, fmt.Sprintf("direct:m%02d", n))
	}
	for n := 20; n < 25; n++ {
		path := filepath.Join(inbox, fmt.Sprintf("direct-p%02d.jpg", n))
		stamp := g.Now().Add(-time.Duration(n+1) * 5 * time.Hour)
		if err := os.WriteFile(path, []byte("jpeg"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}
	g.Beads = &apptest.FakeVerifyBeads{Work: application.WorkInHand{Ready: []application.StoryDetail{verifyDemo("d-1", "e-1", "Open the app and look for the green tick.")}}}
	g.InboxDir = inbox
	return g, messages
}

func TestTheSundayBundleKeepsWhatTheWeeklyPassIsAskedFor(t *testing.T) {
	g, messages := weeklyGather(t)
	bundle, err := g.Gather(context.Background(), application.SleepWindow{Date: "2026-11-15", Deep: true})
	if err != nil {
		t.Fatal(err)
	}
	parts := partsByFile(bundle)
	t.Logf("the Sunday bundle:\n%s%s", bundle.Sizes(), bundle.Turns(true))

	// every bead of the dump keeps its id and its title
	triage := parts[application.SleepTriageFile]
	seen := 0
	for _, line := range strings.Split(strings.TrimSpace(triage.Text), "\n") {
		var bead struct{ ID, Title string }
		if err := json.Unmarshal([]byte(line), &bead); err != nil {
			continue
		}
		if bead.ID != "" && strings.HasPrefix(bead.Title, "Title of bead number") {
			seen++
		}
	}
	if seen != 811 {
		t.Errorf("the dump keeps %d of 811 beads with an id and a title (%s)", seen, triage.Cut)
	}

	// the diff is readable: it says what it holds, its last line is whole, and
	// it is not a raw cut that ends in the middle of the lists
	diff := parts[application.SleepDiffFile]
	if !strings.Contains(diff.Text, "New (") || !strings.Contains(diff.Text, "Closed (") {
		t.Errorf("the diff does not count what is new and closed:\n%.400s", diff.Text)
	}
	if strings.Contains(diff.Text, "mw cut this part") || len(diff.Text) > diff.Budget {
		t.Errorf("the diff is a raw cut (%d bytes of %d): %s", len(diff.Text), diff.Budget, diff.Cut)
	}

	// the report keeps fuel by model and host, the investments and the cheap wins
	report := parts[application.SleepReportFile].Text
	for _, want := range []string{"by model", "by host", "WIN mw-5r3p30.452", "CHEAP WINS", "8 refusals have no cause"} {
		if !strings.Contains(report, want) {
			t.Errorf("the report lost %q (%d bytes, cut: %s)", want, len(report), parts[application.SleepReportFile].Cut)
		}
	}
	if len(report) > parts[application.SleepReportFile].Budget {
		t.Errorf("the report is %d bytes, over its budget of %d", len(report), parts[application.SleepReportFile].Budget)
	}

	// the queue holds every line
	if got := strings.Count(parts[application.SleepQueueFile].Text, "\n"); got < 1046 {
		t.Errorf("the queue holds %d of 1,046 lines (%s)", got, parts[application.SleepQueueFile].Cut)
	}

	// the week's messages are whole and all 25 pictures are given
	evidence := parts[application.SleepEvidenceFile]
	for _, txid := range messages {
		if !strings.Contains(evidence.Text, txid) {
			t.Errorf("the evidence lacks the message %s (cut: %s)", txid, evidence.Cut)
		}
	}
	if len(evidence.Attach) != 25 {
		t.Errorf("%d of 25 pictures are given", len(evidence.Attach))
	}
}

func TestTheNightlyBundleOfTheSameWeekIsStillCutToTodaysBudgets(t *testing.T) {
	g, _ := weeklyGather(t)
	// 15 pictures sent in the last hour: the nightly pass is given the newest 12
	for n := 0; n < 15; n++ {
		path := filepath.Join(g.InboxDir, fmt.Sprintf("direct-n%02d.jpg", n))
		stamp := g.Now().Add(-time.Duration(n+1) * time.Minute)
		if err := os.WriteFile(path, []byte("jpeg"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}
	bundle, err := g.Gather(context.Background(), application.SleepWindow{Date: "2026-11-14"})
	if err != nil {
		t.Fatal(err)
	}
	today := map[string]int{
		application.SleepTriageFile: 50_000, application.SleepDiffFile: 10_000, application.SleepQueueFile: 24_000,
		application.SleepGitFile: 20_000, application.SleepRunsFile: 4_000, application.SleepReportFile: 24_000,
		application.SleepFoldsFile: 12_000, application.SleepStuckFile: 6_000, application.SleepNeedsYouFile: 30_000,
		application.SleepEvidenceFile: 16_000, application.SleepCandidatesFile: 40_000,
	}
	for _, part := range bundle {
		if part.Budget != today[part.File] || application.SleepBudgetOf(part.File) != today[part.File] {
			t.Errorf("%s: budget %d, named %d, want today's %d", part.File, part.Budget, application.SleepBudgetOf(part.File), today[part.File])
		}
		if len(part.Text) > part.Budget {
			t.Errorf("%s is %d bytes, over its nightly budget of %d", part.File, len(part.Text), part.Budget)
		}
	}
	if application.SleepTotalBudget != 236_000 {
		t.Errorf("the nightly total budget is %d, want 236,000", application.SleepTotalBudget)
	}
	if got := len(partsByFile(bundle)[application.SleepEvidenceFile].Attach); got != 12 {
		t.Errorf("the nightly pass is given %d pictures, want today's 12", got)
	}
}

func TestAWeeklyDryRunPrintsEachPartAgainstTheWeeklyBudgetAndTheBundleAgainstTheTurnCap(t *testing.T) {
	g, _ := weeklyGather(t)
	f := newSleepFixture()
	f.pass.Gatherer = g
	f.pass.Now = g.Now
	if _, err := f.pass.Run(context.Background(), application.SleepOptions{DryRun: true}); err != nil {
		t.Fatal(err)
	}
	said := f.out.String()
	sizes := said[strings.Index(said, "--- bundle ---"):]
	for _, want := range []string{
		"triage.jsonl", "bytes of 200,000", "triage-diff.txt", "of 30,000", "queue.json", "of 48,000",
		"report.txt", "evidence.txt", "of 64,000", "total", "of 532,000", "turns", "of 60 turns",
	} {
		if !strings.Contains(sizes, want) {
			t.Errorf("the dry run's sizes lack %q:\n%s", want, sizes)
		}
	}
	if !strings.Contains(said, "Sunday pass") {
		t.Errorf("the dry run does not say it is the Sunday pass:\n%.300s", said)
	}
}

func TestTheWeeklyBudgetsNameTheSamePartsAndComeToTheirTotal(t *testing.T) {
	weekly, nightly := 0, 0
	for _, file := range []string{
		application.SleepTriageFile, application.SleepDiffFile, application.SleepQueueFile, application.SleepGitFile,
		application.SleepRunsFile, application.SleepReportFile, application.SleepFoldsFile, application.SleepStuckFile,
		application.SleepNeedsYouFile, application.SleepEvidenceFile, application.SleepCandidatesFile,
	} {
		w, n := application.SleepBudgetFor(true, file), application.SleepBudgetFor(false, file)
		if w == 0 || n == 0 || w < n {
			t.Errorf("%s: weekly budget %d, nightly %d: want both named and the weekly not smaller", file, w, n)
		}
		weekly, nightly = weekly+w, nightly+n
	}
	if weekly != application.SleepWeeklyTotalBudget || nightly != application.SleepTotalBudget {
		t.Errorf("the budgets come to %d weekly and %d nightly, the totals are %d and %d", weekly, nightly, application.SleepWeeklyTotalBudget, application.SleepTotalBudget)
	}
	if application.SleepPartCount() != 11 {
		t.Errorf("the bundle names %d parts, this test names 11", application.SleepPartCount())
	}
}

func TestTheTurnCapHoldsTheWorstBundleOfEachKindAndTheSundayAskedPictures(t *testing.T) {
	for _, deep := range []bool{false, true} {
		if need, cap := application.SleepWorstTurns(deep), application.SleepTurnsFor(deep); need > cap {
			t.Errorf("deep=%v: the worst bundle takes %d turns, the cap is %d", deep, need, cap)
		}
	}
	if got := application.SleepTurnsFor(false); got != application.SleepMaxTurns {
		t.Errorf("the nightly turn cap is %d, want %d as it was", got, application.SleepMaxTurns)
	}
	if application.SleepMaxPicturesFor(true) < 25 {
		t.Errorf("the Sunday pass is given at most %d pictures, the week of 2026-11-15 held 25", application.SleepMaxPicturesFor(true))
	}
	f := newSleepFixture()
	f.onSunday()
	if _, err := f.pass.Run(context.Background(), application.SleepOptions{}); err != nil {
		t.Fatal(err)
	}
	if got := f.grinder.Calls()[0].Call.MaxTurns; got != application.SleepWeeklyMaxTurns {
		t.Errorf("the Sunday agent is given %d turns, want %d", got, application.SleepWeeklyMaxTurns)
	}
}

func TestADiffTooLongToListIsSummarisedWithItsCountsAndSaysWhatItLeftOut(t *testing.T) {
	g, _ := weeklyGather(t)
	// today's 811 beads against 4,000 others: the nightly budget cannot list them
	bundle, err := g.Gather(context.Background(), application.SleepWindow{Date: "2026-11-14"})
	if err != nil {
		t.Fatal(err)
	}
	diff := partsByFile(bundle)[application.SleepDiffFile]
	if len(diff.Text) > diff.Budget || diff.Cut == "" || !strings.Contains(diff.Text, "Counts: New 700, Closed 689") {
		t.Errorf("the nightly diff (%d of %d bytes, cut %q) is not a summary with counts:\n%.500s", len(diff.Text), diff.Budget, diff.Cut, diff.Text)
	}
	if !strings.Contains(diff.Text, "more left out to fit") || !strings.HasSuffix(diff.Text, "\n") {
		t.Errorf("the summary does not say what it left out, or ends mid-line:\n%s", diff.Text[max(0, len(diff.Text)-300):])
	}
	for _, line := range strings.Split(diff.Text, "\n") {
		if strings.HasPrefix(line, "  mw-w.") && len(strings.Fields(line)) == 0 {
			t.Errorf("an empty id line: %q", line)
		}
	}
}

func TestAReportWithNoSectionsIsCutByLinesAsBefore(t *testing.T) {
	g := gatherFixture()
	g.Mw = func(_ context.Context, args ...string) (string, error) {
		if args[0] == "report" {
			return strings.Repeat("report line\n", 3000), nil
		}
		return "", nil
	}
	bundle, err := g.Gather(context.Background(), application.SleepWindow{Date: "2026-10-16"})
	if err != nil {
		t.Fatal(err)
	}
	part := partsByFile(bundle)[application.SleepReportFile]
	if len(part.Text) > part.Budget || !strings.Contains(part.Text, "mw cut this part") || !strings.HasPrefix(part.Cut, "was ") {
		t.Errorf("the report is %d bytes of %d, cut %q", len(part.Text), part.Budget, part.Cut)
	}
}

func TestANightlyReportOverItsBudgetKeepsFuelInvestmentsAndCheapWinsToo(t *testing.T) {
	g, _ := weeklyGather(t)
	bundle, err := g.Gather(context.Background(), application.SleepWindow{Date: "2026-11-14"})
	if err != nil {
		t.Fatal(err)
	}
	part := partsByFile(bundle)[application.SleepReportFile]
	for _, want := range []string{"by model", "by host", "WIN mw-5r3p30.452", "CHEAP WINS"} {
		if !strings.Contains(part.Text, want) {
			t.Errorf("the nightly report lost %q (cut: %s)", want, part.Cut)
		}
	}
	if len(part.Text) > part.Budget || part.Cut == "" {
		t.Errorf("the nightly report is %d bytes of %d, cut %q", len(part.Text), part.Budget, part.Cut)
	}
}

func TestEvidenceOverItsBudgetLeavesOutTheOldestAndKeepsTheNewest(t *testing.T) {
	g, _ := weeklyGather(t)
	// the nightly window holds only the last day, so a short budget is made by
	// many long messages sent in the last hour
	for n := 0; n < 40; n++ {
		path := filepath.Join(g.InboxDir, fmt.Sprintf("direct-z%02d.txt", n))
		stamp := g.Now().Add(-time.Duration(n+1) * time.Minute)
		if err := os.WriteFile(path, []byte(strings.Repeat("a long word of his ", 200)), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}
	bundle, err := g.Gather(context.Background(), application.SleepWindow{Date: "2026-11-14"})
	if err != nil {
		t.Fatal(err)
	}
	part := partsByFile(bundle)[application.SleepEvidenceFile]
	if len(part.Text) > part.Budget || !strings.Contains(part.Cut, "oldest") || !strings.Contains(part.Text, "direct:z00") || strings.Contains(part.Text, "direct:z39") {
		t.Errorf("evidence is %d bytes of %d, cut %q: want the newest kept and the oldest left out:\n%.300s", len(part.Text), part.Budget, part.Cut, part.Text)
	}
	if part.Verify == nil {
		t.Fatal("no evidence carried for the proposals")
	}
	for _, e := range part.Verify.Evidence {
		if e.Txid == "direct:z39" {
			t.Errorf("the evidence left out of the text is kept to check proposals against")
		}
	}
}

func TestOnlyTheSundayDumpCarriesTitles(t *testing.T) {
	g, _ := weeklyGather(t)
	nightly, err := g.Gather(context.Background(), application.SleepWindow{Date: "2026-11-14"})
	if err != nil {
		t.Fatal(err)
	}
	if text := partsByFile(nightly)[application.SleepTriageFile].Text; strings.Contains(text, `"title"`) {
		t.Errorf("the nightly dump carries titles, which it has never had:\n%.300s", text)
	}
}
