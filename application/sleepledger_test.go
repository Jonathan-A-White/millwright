package application_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

// ledgerVault is the vault's improvements ledger and plan files in memory.
type ledgerVault struct {
	ledger  string
	plans   map[string]string // "dir/name" -> contents
	written map[string]string
	commits [][]string
}

func (v *ledgerVault) ReadImprovements(context.Context) (string, error) { return v.ledger, nil }

func (v *ledgerVault) ReadPlanFile(_ context.Context, dir, name string) (string, error) {
	text, ok := v.plans[dir+"/"+name]
	if !ok {
		return "", errors.New("no such plan file " + dir + "/" + name)
	}
	return text, nil
}

func (v *ledgerVault) PutPlanFile(_ context.Context, dir, name, contents string) (string, error) {
	if v.written == nil {
		v.written = map[string]string{}
	}
	v.written[dir+"/"+name] = contents
	return "plans/" + dir + "/" + name, nil
}

func (v *ledgerVault) Commit(_ context.Context, _ string, paths []string) ([]string, error) {
	v.commits = append(v.commits, paths)
	return paths, nil
}

const fixtureLedger = `{"id":"I1","filed":"2026-10-16","predicted":{"tokens":"-8%","speed":"stuck story noticed in 2h","quality":"technical: neutral"},"cost":{"size":"S","tokens":"150K"},"measure":{"metric":"p95 turns per landed story","baseline":"140","window":"14d"},"landed":"2026-10-17","measured_at":"2026-10-31","result":"p95 turns 118","verdict":"keep","lesson":"a cap shortens the tail without lowering the landed rate"}
{"id":"I2","filed":"2026-10-16","predicted":{"tokens":"-5%","speed":"-10 min a story","quality":"technical: same gate"},"cost":{"size":"M","tokens":"400K"},"measure":{"metric":"gate seconds","baseline":"1500","window":"14d"}}
`

func TestTheLedgerPrintsEachImprovementWithItsVerdict(t *testing.T) {
	var out bytes.Buffer
	err := application.SleepLedger{Vault: &ledgerVault{ledger: fixtureLedger}, Out: &out}.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("want one line an improvement, got %d:\n%s", len(lines), out.String())
	}
	for _, want := range []string{"I1", "filed 2026-10-16", "landed 2026-10-17", "KEEP", "p95 turns 118", "a cap shortens the tail"} {
		if !strings.Contains(lines[0], want) {
			t.Errorf("line 1 lacks %q:\n%s", want, lines[0])
		}
	}
	if !strings.Contains(lines[1], "I2") || !strings.Contains(lines[1], "PENDING") {
		t.Errorf("an improvement not yet measured is PENDING:\n%s", lines[1])
	}
}

func TestAnEmptyLedgerSaysSo(t *testing.T) {
	var out bytes.Buffer
	if err := (application.SleepLedger{Vault: &ledgerVault{}, Out: &out}).Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "no improvement") {
		t.Errorf("want a line saying the ledger is empty, got %q", out.String())
	}
}

func TestALedgerLineWithAVerdictOutsideKeepOrRollbackIsNamedByItsLine(t *testing.T) {
	bad := `{"id":"I1","verdict":"keep"}` + "\n" + `{"id":"I2","verdict":"maybe"}` + "\n"
	err := application.SleepLedger{Vault: &ledgerVault{ledger: bad}, Out: &bytes.Buffer{}}.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "line 2") || !strings.Contains(err.Error(), "maybe") {
		t.Fatalf("want an error naming line 2 and the verdict, got %v", err)
	}
}

// outcomeBeads are the live beads and their comments.
type outcomeBeads struct {
	beads    []application.TriageBead
	comments map[string][]application.Comment
}

func (o outcomeBeads) TriageBeads(context.Context) ([]application.TriageBead, error) {
	return o.beads, nil
}

func (o outcomeBeads) StoriesComments(_ context.Context, ids []string) (map[string][]application.Comment, error) {
	got := map[string][]application.Comment{}
	for _, id := range ids {
		got[id] = o.comments[id]
	}
	return got, nil
}

const fixtureCalls = `{"arc":"Postern","calls":[
 {"bead":"mw-a","from":"P2","to":"P1","why":"hot"},
 {"bead":"mw-b","from":"P3","to":"P4","why":"parked"},
 {"bead":"mw-c","from":"P2","to":"P3","why":"cool"},
 {"bead":"mw-d","from":"P3","to":"P1","why":"unblocks"},
 {"bead":"mw-e","from":"P2","to":"P4","why":"never opened"}
],"stuck":[],"investment_check":[]}`

func fixtureOutcome() (*ledgerVault, outcomeBeads) {
	vault := &ledgerVault{plans: map[string]string{"sleep-2026-10-01/recommendations.json": fixtureCalls}}
	beads := outcomeBeads{
		beads: []application.TriageBead{
			{ID: "mw-a", Status: "closed", Priority: 1},   // landed
			{ID: "mw-b", Status: "deferred", Priority: 4}, // still iced
			{ID: "mw-c", Status: "open", Priority: 3},     // still where it was put
			{ID: "mw-d", Status: "open", Priority: 1},     // still where it was put
			{ID: "mw-e", Status: "open", Priority: 2},     // iced, then thawed
		},
		comments: map[string][]application.Comment{
			"mw-a": {{Text: "VERIFIED by the Governor via postern (txid)"}},
		},
	}
	return vault, beads
}

func TestOutcomeScoresAPassesCallsAtFourteenDays(t *testing.T) {
	vault, beads := fixtureOutcome()
	var out bytes.Buffer
	outcome := application.SleepOutcome{
		Vault: vault, Beads: beads, Out: &out,
		Now: func() time.Time { return time.Date(2026, 10, 15, 8, 0, 0, 0, time.UTC) },
	}
	score, err := outcome.Run(context.Background(), "2026-10-01")
	if err != nil {
		t.Fatal(err)
	}
	if score.Held != 4 || score.Calls != 5 {
		t.Fatalf("want 4 of 5 calls held, got %d of %d:\n%s", score.Held, score.Calls, out.String())
	}
	if score.Landed != 1 || score.Verified != 1 || score.Thawed != 1 || score.Overridden != 0 {
		t.Errorf("want landed 1, verified 1, thawed 1, overridden 0, got %+v", score)
	}
	for _, want := range []string{"4 of 5 calls", "landed 1", "verified 1", "thawed 1", "overridden 0"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("the report lacks %q:\n%s", want, out.String())
		}
	}
	if _, ok := vault.written["sleep-2026-10-01/outcome.json"]; !ok {
		t.Errorf("the score is kept in the pass's plans directory, wrote %v", vault.written)
	}
	if len(vault.commits) != 1 {
		t.Errorf("want one vault commit, got %v", vault.commits)
	}
}

func TestACallTheGovernorMovedIsOverriddenNotHeld(t *testing.T) {
	vault, beads := fixtureOutcome()
	beads.beads[3].Priority = 2 // mw-d: called P1, he made it P2
	score, err := application.SleepOutcome{Vault: vault, Beads: beads, Out: &bytes.Buffer{}}.Run(context.Background(), "2026-10-01")
	if err != nil {
		t.Fatal(err)
	}
	if score.Overridden != 1 || score.Held != 3 {
		t.Errorf("want overridden 1 and held 3, got %+v", score)
	}
}

func TestOutcomeBeforeFourteenDaysSaysHowFarTheCallsAreAlong(t *testing.T) {
	vault, beads := fixtureOutcome()
	var out bytes.Buffer
	_, err := application.SleepOutcome{
		Vault: vault, Beads: beads, Out: &out,
		Now: func() time.Time { return time.Date(2026, 10, 8, 8, 0, 0, 0, time.UTC) },
	}.Run(context.Background(), "2026-10-01")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "day 7 of 14") {
		t.Errorf("want a note that the calls are at day 7 of 14, got:\n%s", out.String())
	}
}

func TestOutcomeReadsAFlatListOfCallsAndTheMayorsNumbers(t *testing.T) {
	vault := &ledgerVault{plans: map[string]string{"sleep-2026-10-01/recommendations.json": `[{"id":"mw-b","from":3,"to":4,"ice":true}]`}}
	_, beads := fixtureOutcome()
	score, err := application.SleepOutcome{Vault: vault, Beads: beads, Out: &bytes.Buffer{}}.Run(context.Background(), "2026-10-01")
	if err != nil {
		t.Fatal(err)
	}
	if score.Held != 1 || score.Calls != 1 {
		t.Errorf("want 1 of 1, got %d of %d", score.Held, score.Calls)
	}
}

func TestOutcomeForADateWithNoPassNamesIt(t *testing.T) {
	_, beads := fixtureOutcome()
	_, err := application.SleepOutcome{Vault: &ledgerVault{}, Beads: beads, Out: &bytes.Buffer{}}.Run(context.Background(), "2026-09-01")
	if err == nil || !strings.Contains(err.Error(), "2026-09-01") {
		t.Fatalf("want an error naming the date, got %v", err)
	}
}

func TestOutcomeRefusesADateThatIsNotADate(t *testing.T) {
	_, beads := fixtureOutcome()
	_, err := application.SleepOutcome{Vault: &ledgerVault{}, Beads: beads, Out: &bytes.Buffer{}}.Run(context.Background(), "../etc")
	if err == nil {
		t.Fatal("want a refusal of a date that is not YYYY-MM-DD")
	}
}
