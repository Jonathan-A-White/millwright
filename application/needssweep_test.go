package application_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

var sweepNow = time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC)

const (
	sweepDir    = "/rigs/x"
	sweepMain   = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	sweepDeploy = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	internalTxt = "Built it.\n\nInternal: nothing for the Governor to look at\n\nFor the rig memory: nothing"
	hisLineTxt  = "Built it.\n\nHOW TO CHECK IT, for the Governor:\n1. Open the app.\n2. Tap \"Go\".\n\nFor the rig memory: nothing"
)

// sweepGit is a SweepGit over maps the test sets.
type sweepGit struct {
	in      map[string]bool // "outer inner" is contained
	missing map[string]bool // commits the checkout lacks
	revert  map[string]string
	refs    []application.SweepRef
}

func (g *sweepGit) Contains(_ context.Context, _, outer, inner string) (bool, error) {
	if g.missing[inner] || g.missing[outer] {
		return false, errors.New("unknown commit")
	}
	return outer == inner || g.in[outer+" "+inner], nil
}
func (g *sweepGit) MainRefs(context.Context, string) ([]application.SweepRef, error) {
	return g.refs, nil
}
func (g *sweepGit) RevertOf(_ context.Context, _, _, commit, _ string) (string, error) {
	return g.revert[commit], nil
}

type sweepMarks map[string]string

func (m sweepMarks) Built(_ context.Context, rig string) (string, error) { return m[rig], nil }
func (m sweepMarks) MarkBuilt(context.Context, string, string) error     { return nil }

type sweepWeb map[string]string

func (w sweepWeb) Get(_ context.Context, url string) (int, []byte, error) {
	if body, ok := w[url]; ok {
		return 200, []byte(body), nil
	}
	return 404, nil, nil
}

type sweepRules struct{ site string }

func (r sweepRules) EpicRequirements(context.Context, string) (domain.EpicRequirements, error) {
	return domain.EpicRequirements{SiteURL: r.site}, nil
}

type sweepRig struct {
	beads  *apptest.FakeNeedsSweep
	git    *sweepGit
	marks  sweepMarks
	mail   *apptest.FakeMailbox
	web    sweepWeb
	runDir string
	out    *bytes.Buffer
	sweep  application.NeedsSweep
}

// landed adds a closed, landed story of rig "x" closed age ago at the commit,
// with closing comment text, that main holds.
func (r *sweepRig) landed(id string, age time.Duration, commit, closing string) {
	closed := sweepNow.Add(-age)
	r.beads.Beads = append(r.beads.Beads, application.TriageBead{
		ID: id, Title: "Story " + id, Type: "task", Status: "closed", Labels: []string{"run:landed"}, Rig: "x",
		ClosedAt: closed, CloseReason: "landed on main at " + commit + " (tests: pass)",
	})
	r.beads.Comments[id] = []application.Comment{{Author: "builder", Created: closed.Add(-time.Minute), Text: closing}}
	r.git.in[sweepMain+" "+commit] = true
}

func newSweepRig(t *testing.T) *sweepRig {
	t.Helper()
	r := &sweepRig{
		beads:  &apptest.FakeNeedsSweep{Comments: map[string][]application.Comment{}, Now: sweepNow},
		git:    &sweepGit{in: map[string]bool{}, missing: map[string]bool{}, revert: map[string]string{}, refs: []application.SweepRef{{Name: "origin/main", Sha: sweepMain}}},
		marks:  sweepMarks{},
		mail:   apptest.NewFakeMailbox(),
		web:    sweepWeb{},
		runDir: filepath.Join(t.TempDir(), "needs"),
		out:    &bytes.Buffer{},
	}
	r.rebuild()
	return r
}

func (r *sweepRig) rebuild() {
	r.sweep = application.NeedsSweep{
		Beads: r.beads, Git: r.git, Marks: r.marks, Rules: sweepRules{site: "https://x.example/"}, Web: r.web, Mailbox: r.mail,
		Dirs: map[string]string{"x": sweepDir}, RunDir: r.runDir, Host: "laptop",
		Now: func() time.Time { return sweepNow }, Out: r.out,
	}
}

func (r *sweepRig) run(t *testing.T, opts application.NeedsSweepOptions) application.NeedsSweepResult {
	t.Helper()
	res, err := r.sweep.Run(context.Background(), opts)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	return res
}

func (r *sweepRig) sent() []application.Message {
	msgs, _ := r.mail.Inbox(context.Background(), application.MayorMailbox)
	return msgs
}

func (r *sweepRig) classOf(res application.NeedsSweepResult, id string) string {
	for _, l := range res.Lines {
		if l.Bead == id {
			return l.Class
		}
	}
	return ""
}

func (r *sweepRig) wrote(id string) bool {
	for _, w := range r.beads.Written {
		if w.ID == id {
			return true
		}
	}
	return false
}

func TestNeedsSweepDryRunPrintsAClassLinePerLandingAndWritesNothing(t *testing.T) {
	r := newSweepRig(t)
	r.landed("x-internal", 48*time.Hour, "1111111", internalTxt)
	r.landed("x-his", 48*time.Hour, "2222222", hisLineTxt)
	r.landed("x-fresh", time.Hour, "3333333", internalTxt)
	res := r.run(t, application.NeedsSweepOptions{})
	if len(r.beads.Written) != 0 || len(r.sent()) != 0 {
		t.Fatalf("a dry run wrote %v and mailed %d", r.beads.Written, len(r.sent()))
	}
	if _, err := os.Stat(r.runDir); err == nil {
		t.Errorf("a dry run kept a record in %s", r.runDir)
	}
	want := map[string]string{"x-internal": "internal", "x-his": "no-deploy", "x-fresh": "fresh"}
	for id, class := range want {
		if got := r.classOf(res, id); got != class {
			t.Errorf("%s is class %q, want %q", id, got, class)
		}
		line := ""
		for _, l := range strings.Split(r.out.String(), "\n") {
			if strings.HasPrefix(l, id) {
				line = l
			}
		}
		if !strings.Contains(line, class) {
			t.Errorf("the output has no line for %s naming %q: %q", id, class, r.out.String())
		}
	}
	if !strings.Contains(r.out.String(), "would") {
		t.Errorf("the dry run does not say what it would write:\n%s", r.out.String())
	}
}

func TestNeedsSweepWritesTheInternalClassOnlyAndInTheStandingForm(t *testing.T) {
	r := newSweepRig(t)
	r.landed("x-internal", 48*time.Hour, "1111111", internalTxt)
	r.landed("x-his", 48*time.Hour, "2222222", hisLineTxt)
	r.run(t, application.NeedsSweepOptions{Write: true})
	if len(r.beads.Written) != 1 || r.beads.Written[0].ID != "x-internal" {
		t.Fatalf("wrote %+v", r.beads.Written)
	}
	text := r.beads.Written[0].Text
	if !strings.HasPrefix(text, "VERIFIED") {
		t.Errorf("the comment must open VERIFIED: %q", text)
	}
	for _, want := range []string{"not by his use", "1111111", "x main", "2026-11-12 12:00Z", "Internal"} {
		if !strings.Contains(text, want) {
			t.Errorf("the comment %q lacks %q", text, want)
		}
	}
}

func TestNeedsSweepWritesNothingOnWhatItCannotShow(t *testing.T) {
	cases := []struct {
		name  string
		class string
		setup func(r *sweepRig)
	}{
		{"his words after it closed", "his-words", func(r *sweepRig) {
			r.beads.Comments["b"] = append(r.beads.Comments["b"], application.Comment{Created: sweepNow.Add(-time.Hour), Text: "The Governor by postern 2026-11-11, txid direct:x, on b:\n\nthis is not right"})
		}},
		{"his ANSWER after it closed", "his-words", func(r *sweepRig) {
			r.beads.Comments["b"] = append(r.beads.Comments["b"], application.Comment{Created: sweepNow.Add(-time.Hour), Text: "ANSWER A on the card"})
		}},
		{"an open bug names it", "follow-up-open", func(r *sweepRig) {
			r.beads.Beads = append(r.beads.Beads, application.TriageBead{ID: "bug-1", Type: "bug", Status: "open", Description: "Found while checking b: the thing fails."})
		}},
		{"a held bug names it", "follow-up-open", func(r *sweepRig) {
			r.beads.Beads = append(r.beads.Beads, application.TriageBead{ID: "bug-1", Type: "bug", Status: "deferred", Title: "b is broken"})
		}},
		{"younger than min_age", "fresh", func(r *sweepRig) { r.beads.Beads[0].ClosedAt = sweepNow.Add(-2 * time.Hour) }},
		{"its commit is not in main", "unproven", func(r *sweepRig) { delete(r.git.in, sweepMain+" 1111111") }},
		{"the checkout lacks its commit", "unproven", func(r *sweepRig) { r.git.missing["1111111"] = true }},
		{"it names no commit", "unproven", func(r *sweepRig) { r.beads.Beads[0].CloseReason = "closed by hand" }},
		{"the rig has no checkout here", "unproven", func(r *sweepRig) { r.beads.Beads[0].Rig = "elsewhere" }},
		{"it is not in the deployed commit", "not-deployed", func(r *sweepRig) { r.marks["x.deployed"] = sweepDeploy }},
		{"its closing step gives no check", "no-check", func(r *sweepRig) { r.beads.Comments["b"][0].Text = "Built it." }},
		{"its merge was reverted", "reverted", func(r *sweepRig) { r.git.revert["1111111"] = "cccccccccccc" }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := newSweepRig(t)
			r.landed("b", 48*time.Hour, "1111111", internalTxt)
			c.setup(r)
			res := r.run(t, application.NeedsSweepOptions{Write: true})
			if got := r.classOf(res, "b"); got != c.class {
				t.Errorf("class %q, want %q", got, c.class)
			}
			if r.wrote("b") {
				t.Errorf("wrote %+v", r.beads.Written)
			}
		})
	}
}

func TestNeedsSweepInternalLandingIsWrittenWhenTheDeployedCommitHoldsIt(t *testing.T) {
	r := newSweepRig(t)
	r.landed("b", 48*time.Hour, "1111111", internalTxt)
	r.marks["x.deployed"] = sweepDeploy
	r.git.in[sweepDeploy+" 1111111"] = true
	r.run(t, application.NeedsSweepOptions{Write: true})
	if !r.wrote("b") {
		t.Fatal("not written")
	}
	if !strings.Contains(r.beads.Written[0].Text, "bbbbbbbbbbbb") {
		t.Errorf("the comment does not name the deployed commit: %q", r.beads.Written[0].Text)
	}
}

func TestNeedsSweepSkipsWhatIsAlreadyVerifiedOrNotMet(t *testing.T) {
	r := newSweepRig(t)
	r.landed("v", 48*time.Hour, "1111111", internalTxt)
	r.landed("n", 48*time.Hour, "2222222", internalTxt)
	r.beads.Comments["v"] = append(r.beads.Comments["v"], application.Comment{Created: sweepNow.Add(-time.Hour), Text: "VERIFIED by the Governor's use, inferred by the Mayor from direct:a: x (by hand)"})
	r.beads.Comments["n"] = append(r.beads.Comments["n"], application.Comment{Created: sweepNow.Add(-time.Hour), Text: "NOT MET by the Governor's report, txid direct:a (t), no fix named"})
	res := r.run(t, application.NeedsSweepOptions{Write: true})
	if len(res.Lines) != 0 || len(r.beads.Written) != 0 {
		t.Errorf("lines %+v, wrote %+v", res.Lines, r.beads.Written)
	}
}

func TestNeedsSweepIgnoresWhatIsNotAnUnverifiedLanding(t *testing.T) {
	r := newSweepRig(t)
	r.landed("old", 40*24*time.Hour, "1111111", internalTxt)
	r.landed("hitl", 48*time.Hour, "2222222", internalTxt)
	r.landed("dropped", 48*time.Hour, "3333333", internalTxt)
	r.beads.Beads[1].Labels = []string{"run:landed", "hitl"}
	r.beads.Beads[2].Labels = nil
	res := r.run(t, application.NeedsSweepOptions{Write: true})
	if len(res.Lines) != 0 {
		t.Errorf("lines %+v", res.Lines)
	}
}

func TestNeedsSweepRevertedLandingGetsNoCommentAndOneMailLine(t *testing.T) {
	r := newSweepRig(t)
	r.landed("rev", 48*time.Hour, "1111111", internalTxt)
	r.landed("rev2", 48*time.Hour, "2222222", hisLineTxt)
	r.landed("ok", 48*time.Hour, "3333333", internalTxt)
	r.git.revert["1111111"] = "deadbeef0001"
	r.git.revert["2222222"] = "deadbeef0002"
	res := r.run(t, application.NeedsSweepOptions{Write: true})
	if r.classOf(res, "rev") != "reverted" || r.wrote("rev") || r.wrote("rev2") {
		t.Fatalf("class %q, wrote %+v", r.classOf(res, "rev"), r.beads.Written)
	}
	sent := r.sent()
	if len(sent) != 1 || sent[0].To != application.MayorMailbox {
		t.Fatalf("mail: %+v", sent)
	}
	body := sent[0].Body
	for _, want := range []string{"rev", "1111111", "deadbeef0001", "rev2", "deadbeef0002"} {
		if !strings.Contains(body, want) {
			t.Errorf("the mail lacks %q: %s", want, body)
		}
	}
	if strings.Contains(body, "ok ") {
		t.Errorf("the mail names a landing that was not reverted: %s", body)
	}
	// A second run does not mail the same landing again.
	r.run(t, application.NeedsSweepOptions{Write: true})
	if len(r.sent()) != 1 {
		t.Errorf("the second run mailed again: %d", len(r.sent()))
	}
}

func TestNeedsSweepDryRunMailsNothingAboutARevert(t *testing.T) {
	r := newSweepRig(t)
	r.landed("rev", 48*time.Hour, "1111111", internalTxt)
	r.git.revert["1111111"] = "deadbeef0001"
	res := r.run(t, application.NeedsSweepOptions{})
	if len(r.sent()) != 0 || !strings.Contains(r.out.String(), "deadbeef0001") || r.classOf(res, "rev") != "reverted" {
		t.Errorf("mail %d, out %s", len(r.sent()), r.out.String())
	}
}

func TestNeedsSweepSecondWriteRunWritesNothing(t *testing.T) {
	r := newSweepRig(t)
	r.landed("a", 48*time.Hour, "1111111", internalTxt)
	r.landed("b", 48*time.Hour, "2222222", internalTxt)
	r.run(t, application.NeedsSweepOptions{Write: true})
	if len(r.beads.Written) != 2 {
		t.Fatalf("first run wrote %d", len(r.beads.Written))
	}
	res := r.run(t, application.NeedsSweepOptions{Write: true})
	if len(r.beads.Written) != 2 || len(res.Lines) != 0 {
		t.Errorf("second run wrote %d, lines %+v", len(r.beads.Written), res.Lines)
	}
}

func TestNeedsSweepFailedCallStopsTheRunNamingTheBead(t *testing.T) {
	r := newSweepRig(t)
	r.landed("a", 72*time.Hour, "1111111", internalTxt)
	r.landed("b", 60*time.Hour, "2222222", internalTxt)
	r.landed("c", 48*time.Hour, "3333333", internalTxt)
	r.beads.FailOn = "b"
	_, err := r.sweep.Run(context.Background(), application.NeedsSweepOptions{Write: true})
	if err == nil || !strings.Contains(err.Error(), "b") || !strings.Contains(err.Error(), "stopped") {
		t.Fatalf("error %v", err)
	}
	if !r.wrote("a") || r.wrote("b") || r.wrote("c") {
		t.Errorf("wrote %+v", r.beads.Written)
	}
	entries, _ := os.ReadDir(r.runDir)
	if len(entries) != 1 {
		t.Fatalf("record: %v", entries)
	}
	raw, _ := os.ReadFile(filepath.Join(r.runDir, entries[0].Name()))
	if !strings.Contains(string(raw), "stopped") {
		t.Errorf("the record does not say it stopped: %s", raw)
	}
}

func TestNeedsSweepRecordsEachWriteRun(t *testing.T) {
	r := newSweepRig(t)
	r.landed("a", 48*time.Hour, "1111111", internalTxt)
	res := r.run(t, application.NeedsSweepOptions{Write: true})
	raw, err := os.ReadFile(filepath.Join(r.runDir, res.Run+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var rec struct {
		Run   string `json:"run"`
		Write bool   `json:"write"`
		Lines []struct {
			Bead, Class string
			Wrote       bool
		} `json:"lines"`
	}
	if err := json.Unmarshal(raw, &rec); err != nil {
		t.Fatal(err)
	}
	if rec.Run != res.Run || !rec.Write || len(rec.Lines) != 1 || rec.Lines[0].Bead != "a" || !rec.Lines[0].Wrote {
		t.Errorf("record %+v", rec)
	}
}

const staleLiveRig = "Built it.\n\nHOW TO CHECK IT, for the Governor:\n1. Open the app.\n"

func staleLive(t *testing.T) *sweepRig {
	r := newSweepRig(t)
	r.landed("old", 15*24*time.Hour, "1111111", staleLiveRig)
	r.landed("young", 10*24*time.Hour, "2222222", staleLiveRig)
	r.marks["x.deployed"] = sweepDeploy
	r.git.in[sweepDeploy+" 1111111"] = true
	r.git.in[sweepDeploy+" 2222222"] = true
	r.web["https://x.example/"] = `<html>build 0.7.1 · bbbbbbbbb</html>`
	return r
}

func TestNeedsSweepStaleLiveWritesOnlyWithTheFlagAndOnlyTheOldSeenLiveClass(t *testing.T) {
	r := staleLive(t)
	res := r.run(t, application.NeedsSweepOptions{Write: true})
	if len(r.beads.Written) != 0 {
		t.Fatalf("without the flag wrote %+v", r.beads.Written)
	}
	if r.classOf(res, "old") != "his-line" {
		t.Errorf("class without the flag %q", r.classOf(res, "old"))
	}
	res = r.run(t, application.NeedsSweepOptions{Write: true, StaleLive: true})
	if r.classOf(res, "old") != "stale-live" || r.classOf(res, "young") != "his-line" {
		t.Errorf("classes %q %q", r.classOf(res, "old"), r.classOf(res, "young"))
	}
	if len(r.beads.Written) != 1 || r.beads.Written[0].ID != "old" {
		t.Fatalf("wrote %+v", r.beads.Written)
	}
	text := r.beads.Written[0].Text
	if !strings.HasPrefix(text, "VERIFIED") || !strings.Contains(text, "His own walk stays open to him") || !strings.Contains(text, "bbbbbbb") {
		t.Errorf("text %q", text)
	}
}

func TestNeedsSweepStaleLiveNeedsThePageToStateTheDeployedBuild(t *testing.T) {
	r := staleLive(t)
	r.web["https://x.example/"] = `<html>build 0.7.1 · 1234567</html>`
	res := r.run(t, application.NeedsSweepOptions{Write: true, StaleLive: true})
	if len(r.beads.Written) != 0 || r.classOf(res, "old") != "his-line" {
		t.Errorf("wrote %+v, class %q", r.beads.Written, r.classOf(res, "old"))
	}
}

func TestNeedsSweepStaleLiveWritesNothingWithAHisWordsMessageOrWithoutADeployMark(t *testing.T) {
	r := staleLive(t)
	r.beads.Comments["old"] = append(r.beads.Comments["old"], application.Comment{Created: sweepNow.Add(-time.Hour), Text: "The Governor by postern x: it is not right"})
	delete(r.marks, "x.deployed")
	res := r.run(t, application.NeedsSweepOptions{Write: true, StaleLive: true})
	if len(r.beads.Written) != 0 || r.classOf(res, "old") != "his-words" || r.classOf(res, "young") != "no-deploy" {
		t.Errorf("wrote %+v, classes %q %q", r.beads.Written, r.classOf(res, "old"), r.classOf(res, "young"))
	}
}
