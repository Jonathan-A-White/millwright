package application_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// sizeRig is a lampas rig whose main holds a small tutor grind and whose branch
// holds one with oversized instructions; it has no examples, so a smoke of it
// sends nothing.
func sizeRig(sender application.GristSmokeSender) (application.GristSmoke, *bytes.Buffer) {
	const checkout = "/rigs/lampas"
	grinds := apptest.NewFakeGrinds()
	grind := `{"grind": 1, "app": "lampas", "kind": "tutor", "versions": ["1"], "model": "sonnet", "effort": "low", "instructions": "grinds/tutor.md", "answerSchema": "schemas/tutor.json"}`
	big := strings.Repeat("x", application.GristInstructionsWarnBytes+1)
	for _, f := range []struct{ path, main, ref string }{
		{"grinds/tutor.json", grind, grind},
		{"grinds/tutor.md", "SAY MAIN", big},
		{"schemas/tutor.json", `{"type": "object"}`, `{"type": "object"}`},
	} {
		grinds.SetFile(checkout, "c1", f.path, []byte(f.main))
		grinds.SetRefFile(checkout, "mw/story", "c2", f.path, []byte(f.ref))
	}
	var out bytes.Buffer
	return application.GristSmoke{Grinds: grinds, Send: sender, Apps: map[string]string{"lampas": checkout}, Out: &out}, &out
}

func TestSizeAtRefWarnsOnlyForTheRefsOversizedInstructions(t *testing.T) {
	sender := &textSender{}
	smoke, _ := sizeRig(sender)

	report, err := smoke.SizeAtRef(context.Background(), "lampas", "", "mw/story")
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Warnings) != 1 || !strings.Contains(report.Warnings[0], "lampas/tutor: its instructions are 100001 bytes, over 100000") {
		t.Errorf("expected the size warning for the ref, got %v", report.Warnings)
	}
	if report.Commit != "c2" || report.Failed() {
		t.Errorf("expected the report at the ref's commit with no failure, got %+v", report)
	}

	// Main's instructions are small, so the same size check at main is quiet,
	// and the smoke without a ref (which reads main) warns of nothing.
	report, err = smoke.Run(context.Background(), "lampas", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range report.Warnings {
		if strings.Contains(w, "its instructions are") {
			t.Errorf("expected no size warning at main, got %q", w)
		}
	}
	if len(sender.sent) != 0 {
		t.Errorf("expected nothing sent, got %d sends", len(sender.sent))
	}
}

func TestSmokeWithoutARefStillWarnsOfMainsOversizedInstructions(t *testing.T) {
	smoke, _ := sizeRig(&textSender{})
	grinds := smoke.Grinds.(*apptest.FakeGrinds)
	grinds.SetFile("/rigs/lampas", "c1", "grinds/tutor.md", []byte(strings.Repeat("y", application.GristInstructionsWarnBytes+5)))
	report, err := smoke.Run(context.Background(), "lampas", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Warnings) == 0 || !strings.Contains(report.Warnings[0], "its instructions are 100005 bytes") {
		t.Errorf("expected main's size warning, got %v", report.Warnings)
	}
}

func TestSizeAtRefReadsNoKeyNorSenderAndSaysWhereItRead(t *testing.T) {
	smoke, out := sizeRig(nil)
	if _, err := smoke.SizeAtRef(context.Background(), "lampas", "tutor", "mw/story"); err != nil {
		t.Fatalf("expected the size check to need no sender: %v", err)
	}
	if !strings.Contains(out.String(), "ref mw/story (c2)") {
		t.Errorf("expected it to say it read the ref, got %q", out.String())
	}
	if _, err := smoke.SizeAtRef(context.Background(), "lampas", "", "no/such"); err == nil || !strings.Contains(err.Error(), "no/such") {
		t.Errorf("expected a refusal naming the ref, got %v", err)
	}
	if _, err := smoke.SizeAtRef(context.Background(), "lampas", "nokind", "mw/story"); err == nil || !strings.Contains(err.Error(), "nokind") {
		t.Errorf("expected a refusal naming the kind, got %v", err)
	}
	if _, err := smoke.SizeAtRef(context.Background(), "nope", "", "mw/story"); err == nil {
		t.Error("expected a refusal of an app with no rig")
	}
}
