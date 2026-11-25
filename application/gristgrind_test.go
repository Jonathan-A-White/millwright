package application_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// tutorGrind is a grind file of lampas's tutor kind that runs a session on
// sonnet at low effort and takes version 1, with more written into it: each of
// more is a field as JSON (`"attachments":{"min":1,"max":2}`).
func tutorGrind(more ...string) string {
	fields := append([]string{`"grind":1,"app":"lampas","kind":"tutor","versions":["1"]`,
		`"model":"sonnet","effort":"low","instructions":"grinds/tutor.md","answerSchema":"schema.json"`}, more...)
	return "{" + strings.Join(fields, ",") + "}"
}

// refFile is one file a grist carries: its type and its bytes.
type refFile struct {
	mime string
	data []byte
}

// refGrist is one grist of lampas's tutor kind as `mw grist eval --ref` sends
// it: it brings the ref's instructions and schema and the grind file ref, to a
// mill whose main holds the grind file main. It comes from the factory's test
// key in an eval talk unless from ("phone") or talk say otherwise.
type refGrist struct {
	main, ref  string
	from, talk string
	v, input   string
	files      []refFile
}

// refRig is the mill a refGrist waits at, and what its grind reaches.
type refRig struct {
	mill    application.GristGrind
	backend *apptest.FakePostern
	grinder *apptest.FakeGrinder
	mailbox *apptest.FakeMailbox
	scorer  *apptest.FakeScorer
}

// run is one pass of the mill, and the answer it gave the grist.
func (r refRig) run(t *testing.T) application.GristAnswer {
	t.Helper()
	if _, err := r.mill.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	delivered := r.backend.Delivered()
	if len(delivered) != 1 {
		t.Fatalf("expected one answer, got %d", len(delivered))
	}
	return openFakeAnswer(t, delivered[0])
}

// mails is how many mails the Mayor has.
func (r refRig) mails(t *testing.T) int {
	t.Helper()
	mail, err := r.mailbox.Inbox(context.Background(), application.GristForwardMayor)
	if err != nil {
		t.Fatal(err)
	}
	return len(mail)
}

func refMill(t *testing.T, g refGrist) refRig {
	t.Helper()
	sender, talk, v, input := blobFactoryKey, "eval:greets:ab12cd34", "1", `{"question":"hi"}`
	if g.from == "phone" {
		sender = blobPhoneKey
	}
	if g.talk != "" {
		talk = g.talk
	}
	if g.v != "" {
		v = g.v
	}
	if g.input != "" {
		input = g.input
	}
	keys := &fakeMillKey{made: true}
	millKey, _, _ := keys.PublicKey()
	backend := apptest.NewFakePostern()
	sealer := &apptest.FakeCipher{From: sender}
	var attachments []application.PosternAttachment
	for _, f := range g.files {
		sealed, _ := sealer.EncryptBytes(millKey, f.data)
		hash, size, _ := backend.UploadBlob(context.Background(), mustBase64(t, sealed))
		attachments = append(attachments, application.PosternAttachment{Hash: hash, Size: size, Mime: f.mime})
	}
	override := &application.GristGrindOverride{Instructions: "SAY REF", Schema: `{"type": "object", "required": ["answer"]}`}
	if g.ref != "" {
		override.Grind = json.RawMessage(g.ref)
	}
	plain, _ := json.Marshal(application.GristPlaintext{
		Grist: application.GristName{App: "lampas", Kind: "tutor", V: v, Talk: talk}, Input: json.RawMessage(input),
		Attachments: attachments, GrindOverride: override,
	})
	ct, _ := sealer.Encrypt(millKey, string(plain))
	backend.AddRecord(application.PosternRecord{Txid: "direct:r1", Class: application.GristClass, From: sender, To: millKey,
		Signer: sender, SignerApps: []string{"lampas"}, Ciphertext: ct})

	grinds := apptest.NewFakeGrinds()
	grinds.SetFile("/rigs/lampas", "c0ffee", "grinds/tutor.json", []byte(g.main))
	grinds.SetFile("/rigs/lampas", "c0ffee", "grinds/tutor.md", []byte("SAY MAIN"))
	grinds.SetFile("/rigs/lampas", "c0ffee", "schema.json", []byte(`{"type":"object","required":["items"]}`))
	grinder := &apptest.FakeGrinder{Result: application.SessionResult{Subtype: "success", Answer: json.RawMessage(`{"answer":"ref"}`)}}
	mailbox := apptest.NewFakeMailbox()
	scorer := &apptest.FakeScorer{Result: application.ReadingResult{Engine: "local", Accuracy: 90}}
	return refRig{
		mill: application.GristGrind{
			Postern: backend, Cipher: &apptest.FakeCipher{From: millKey}, Keys: keys, TestKey: blobFactoryKey,
			State: apptest.NewFakeGristState(), Grinds: grinds, Grinder: grinder,
			Pass: &apptest.FakeGristLock{}, Grinding: &apptest.FakeGristLock{}, Host: "laptop",
			Scorers: application.NewScorerRegistry(map[string]application.Scorer{"local": scorer}),
			Apps:    map[string]string{"lampas": "/rigs/lampas"}, TempDir: t.TempDir(),
			Mailbox: mailbox, Forwards: keptForwards{dir: t.TempDir()}, Credits: apptest.UsableCredits(),
			Now: func() time.Time { return time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC) },
		},
		backend: backend, grinder: grinder, mailbox: mailbox, scorer: scorer,
	}
}

var (
	aPNG       = refFile{"image/png", []byte("png bytes")}
	aBigPNG    = refFile{"image/png", []byte(strings.Repeat("png ", 16<<10))}
	aRecording = refFile{"audio/webm", []byte("webm bytes")}
)

// An --ref eval of a grind whose ref changes what it takes in attachments is
// judged by the ref's attachments, not main's: what the ref takes is ground,
// and what the ref refuses is refused (mw-gq6.1108).
func TestGristGrindAnEvalOfARefIsJudgedByTheRefsAttachments(t *testing.T) {
	for name, tc := range map[string]struct {
		main, ref string
		files     []refFile
		refused   string // the reason, or a part of it; "" when the ref takes the grist
	}{
		"a photo, where main's grind takes none": {
			`"attachments":{"min":0,"max":0}`, `"attachments":{"min":1,"max":2,"mime":["image/png"]}`, []refFile{aPNG}, ""},
		"no photo, where the ref needs one": {
			`"attachments":{"min":0,"max":2}`, `"attachments":{"min":1,"max":2}`, nil, "its grind needs at least 1"},
		"two photos, where the ref takes one": {
			`"attachments":{"min":0,"max":2}`, `"attachments":{"min":0,"max":1}`, []refFile{aPNG, aPNG}, "its grind takes at most 1"},
		"a type the ref does not list": {
			`"attachments":{"min":0,"max":2,"mime":["image/png","image/jpeg"]}`, `"attachments":{"min":0,"max":2,"mime":["image/jpeg"]}`,
			[]refFile{aPNG}, application.GristReasonMime},
		"a photo over the ref's maxBytes": {
			`"attachments":{"min":0,"max":2,"maxBytes":1048576}`, `"attachments":{"min":0,"max":2,"maxBytes":1024}`,
			[]refFile{aBigPNG}, application.GristReasonTooLarge},
	} {
		t.Run(name, func(t *testing.T) {
			rig := refMill(t, refGrist{main: tutorGrind(tc.main), ref: tutorGrind(tc.ref), files: tc.files})
			answer := rig.run(t)
			calls := rig.grinder.Calls()
			if tc.refused != "" {
				if answer.Status != application.GristRefused || !strings.Contains(answer.Reason, tc.refused) || len(calls) != 0 {
					t.Fatalf("expected the grist refused by the ref's attachments (%q) and nothing ground, got %+v and %d grinds", tc.refused, answer, len(calls))
				}
				return
			}
			if answer.Status != application.GristAnswered || len(calls) != 1 {
				t.Fatalf("expected the grist ground as the ref's attachments take it, got %+v and %d grinds", answer, len(calls))
			}
			if len(calls[0].Files) != len(tc.files) {
				t.Errorf("expected the session given %d photos, got %d", len(tc.files), len(calls[0].Files))
			}
		})
	}
}

// The ref's scoring is what says whether a recording is taken, in which
// languages it is scored, and whether the scoring is one the mill knows.
func TestGristGrindAnEvalOfARefIsJudgedByTheRefsScoring(t *testing.T) {
	const takesAudio = `"attachments":{"min":0,"max":2,"mime":["audio/webm"]}`
	const reading = `{"schemaVersion":"1","target_text":"hello","lang":"he"}`
	for name, tc := range map[string]struct {
		main, ref, input string
		refused          string // the reason, or a part of it; "" when the ref scores the recording
	}{
		"a recording, where main's grind scores none": {
			"", `"scoring":{"audio":true,"target_field":"target_text","langs":["en","he"]}`, reading, ""},
		"a language the ref does not score in": {
			`"scoring":{"audio":true,"target_field":"target_text","langs":["en","he"]}`,
			`"scoring":{"audio":true,"target_field":"target_text","langs":["en"]}`, reading, "its grind scores in en"},
		"a language the mill does not know, in the ref's scoring": {
			`"scoring":{"audio":true,"target_field":"target_text","langs":["en","he"]}`,
			`"scoring":{"audio":true,"target_field":"target_text","langs":["fr"]}`, reading, application.GristReasonBrokenGrind},
		"a request the ref's scoring cannot add to": {
			"", `"scoring":{"audio":true,"target_field":"target_text"}`, `"hello"`, application.GristReasonMalformed},
	} {
		t.Run(name, func(t *testing.T) {
			main, ref := []string{takesAudio}, []string{takesAudio}
			if tc.main != "" {
				main = append(main, tc.main)
			}
			ref = append(ref, tc.ref)
			rig := refMill(t, refGrist{main: tutorGrind(main...), ref: tutorGrind(ref...), input: tc.input, files: []refFile{aRecording}})
			answer := rig.run(t)
			scored, calls := rig.scorer.Calls(), rig.grinder.Calls()
			if tc.refused != "" {
				if answer.Status != application.GristRefused || !strings.Contains(answer.Reason, tc.refused) || len(calls) != 0 || len(scored) != 0 {
					t.Fatalf("expected the grist refused by the ref's scoring (%q), nothing scored and nothing ground, got %+v, %d scorings and %d grinds",
						tc.refused, answer, len(scored), len(calls))
				}
				return
			}
			if answer.Status != application.GristAnswered || len(calls) != 1 {
				t.Fatalf("expected the recording scored and ground as the ref says, got %+v and %d grinds", answer, len(calls))
			}
			if len(scored) != 1 || scored[0].Lang != "he" {
				t.Errorf("expected the recording scored once in he, the ref's language, got %+v", scored)
			}
		})
	}
}

// The session of an --ref eval is cut at the ref's timeoutSeconds and maxTurns:
// the ones the ref sets, and none where the ref took main's away.
func TestGristGrindAnEvalOfARefIsCutAtTheRefsTimeoutAndMaxTurns(t *testing.T) {
	for name, tc := range map[string]struct {
		main, ref string
		timeout   time.Duration
		maxTurns  int
	}{
		"the ref sets them":           {"", `"timeoutSeconds":30,"maxTurns":3`, 30 * time.Second, 3},
		"the ref changes main's":      {`"timeoutSeconds":120,"maxTurns":9`, `"timeoutSeconds":30,"maxTurns":3`, 30 * time.Second, 3},
		"the ref takes main's away":   {`"timeoutSeconds":120,"maxTurns":9`, "", application.DefaultGristTimeout, 0},
		"the ref sets one, not both":  {`"timeoutSeconds":120`, `"maxTurns":3`, application.DefaultGristTimeout, 3},
		"the ref keeps what main has": {`"timeoutSeconds":120,"maxTurns":9`, `"timeoutSeconds":120,"maxTurns":9`, 120 * time.Second, 9},
	} {
		t.Run(name, func(t *testing.T) {
			grind := func(more string) string {
				if more == "" {
					return tutorGrind()
				}
				return tutorGrind(more)
			}
			rig := refMill(t, refGrist{main: grind(tc.main), ref: grind(tc.ref)})
			answer := rig.run(t)
			calls := rig.grinder.Calls()
			if answer.Status != application.GristAnswered || len(calls) != 1 {
				t.Fatalf("expected the grist ground, got %+v and %d grinds", answer, len(calls))
			}
			if got := calls[0].Call; got.Timeout != tc.timeout || got.MaxTurns != tc.maxTurns {
				t.Errorf("expected the session cut at the ref's %s and %d turns, got %s and %d turns", tc.timeout, tc.maxTurns, got.Timeout, got.MaxTurns)
			}
		})
	}
}

// forwardingGrind is a grind file of lampas's tutor kind that forwards to who
// and runs no session: it names no model, effort, instructions or schema.
func forwardingGrind(who string) string {
	return `{"grind":1,"app":"lampas","kind":"tutor","versions":["1"],"forward":"` + who + `"}`
}

// Whether an --ref eval's grist is forwarded or ground is the ref's say. A kind
// main forwards has no model or effort at main, so a ref that grinds it runs on
// the ref's.
func TestGristGrindAnEvalOfARefIsForwardedOrGroundAsTheRefSays(t *testing.T) {
	t.Run("the ref forwards a kind main grinds", func(t *testing.T) {
		rig := refMill(t, refGrist{main: tutorGrind(), ref: forwardingGrind(application.GristForwardMayor)})
		answer := rig.run(t)
		if answer.Status != application.GristAnswered || string(answer.Answer) != `{"status":"sent"}` {
			t.Fatalf("expected the grist forwarded and answered sent, got %+v", answer)
		}
		if mails, grinds := rig.mails(t), len(rig.grinder.Calls()); mails != 1 || grinds != 0 {
			t.Errorf("expected one mail to the Mayor and no session, got %d mails and %d grinds", mails, grinds)
		}
	})
	t.Run("the ref grinds a kind main forwards", func(t *testing.T) {
		rig := refMill(t, refGrist{main: forwardingGrind(application.GristForwardMayor), ref: tutorGrind()})
		answer := rig.run(t)
		calls := rig.grinder.Calls()
		if answer.Status != application.GristAnswered || !answer.Grind.Override || len(calls) != 1 {
			t.Fatalf("expected the grist ground with the ref's text, got %+v and %d grinds", answer, len(calls))
		}
		if got := calls[0].Call; got.Model != "sonnet" || got.Effort != "low" || !strings.HasSuffix(got.System, "SAY REF") {
			t.Errorf("expected the session on the ref's model and effort with its text, got %s, %s and %q", got.Model, got.Effort, got.System)
		}
		if mails := rig.mails(t); mails != 0 {
			t.Errorf("expected nothing mailed to the Mayor, got %d mails", mails)
		}
	})
	t.Run("the ref forwards to someone the mill does not forward to", func(t *testing.T) {
		rig := refMill(t, refGrist{main: tutorGrind(), ref: forwardingGrind("governor")})
		answer := rig.run(t)
		if answer.Status != application.GristRefused || answer.Reason != application.GristReasonForward {
			t.Fatalf("expected the grist refused for the ref's forward, got %+v", answer)
		}
		if mails, grinds := rig.mails(t), len(rig.grinder.Calls()); mails != 0 || grinds != 0 {
			t.Errorf("expected nothing mailed and nothing ground, got %d mails and %d grinds", mails, grinds)
		}
	})
}

// The versions an --ref eval takes are the ref's: one the ref no longer lists
// is refused, though main still takes it.
func TestGristGrindAnEvalOfARefRefusesAVersionTheRefDropped(t *testing.T) {
	const main = `{"grind":1,"app":"lampas","kind":"tutor","versions":["1","2"],"model":"sonnet","effort":"low","instructions":"grinds/tutor.md","answerSchema":"schema.json"}`
	const ref = `{"grind":1,"app":"lampas","kind":"tutor","versions":["2"],"model":"sonnet","effort":"low","instructions":"grinds/tutor.md","answerSchema":"schema.json"}`
	rig := refMill(t, refGrist{main: main, ref: ref, v: "1"})
	answer := rig.run(t)
	if answer.Status != application.GristRefused || answer.Reason != application.GristReasonVersion || len(rig.grinder.Calls()) != 0 {
		t.Fatalf("expected version 1 refused as the ref no longer takes it, got %+v and %d grinds", answer, len(rig.grinder.Calls()))
	}
}

// A grind file an eval brings that is not the grind of the grist's app and kind
// proves nothing of the ref: the grist is refused, not ground with main's.
func TestGristGrindAnEvalBringingAGrindFileThatIsNotItsKindsIsRefused(t *testing.T) {
	for name, ref := range map[string]string{
		"not a grind file":     `["grinds/tutor.json"]`,
		"another kind's grind": strings.Replace(tutorGrind(), `"kind":"tutor"`, `"kind":"sorter"`, 1),
		"another app's grind":  strings.Replace(tutorGrind(), `"app":"lampas"`, `"app":"cairn"`, 1),
		"another file format":  strings.Replace(tutorGrind(), `"grind":1`, `"grind":2`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			rig := refMill(t, refGrist{main: tutorGrind(), ref: ref})
			answer := rig.run(t)
			if answer.Status != application.GristRefused || answer.Reason != application.GristReasonBrokenGrind || len(rig.grinder.Calls()) != 0 {
				t.Fatalf("expected the grist refused for the grind file it brings, got %+v and %d grinds", answer, len(rig.grinder.Calls()))
			}
		})
	}
}

// A live grist is never judged by a grind file it brings: from an app's key, or
// from the test key outside an eval or smoke talk, a file that would take its
// photo or forward it to the Mayor is refused with the grist, as before.
func TestGristGrindALiveGristIsNeverJudgedByAGrindFileItBrings(t *testing.T) {
	takesAPhoto := tutorGrind(`"attachments":{"min":1,"max":2,"mime":["image/png"]}`)
	for name, tc := range map[string]struct {
		from, talk, ref string
		files           []refFile
	}{
		"an app's key bringing attachments":         {"phone", "", takesAPhoto, []refFile{aPNG}},
		"an app's key in an eval talk":              {"phone", "eval:greets:ab12cd34", takesAPhoto, []refFile{aPNG}},
		"an app's key bringing a forward":           {"phone", "eval:greets:ab12cd34", forwardingGrind(application.GristForwardMayor), nil},
		"the test key in an ordinary talk":          {"factory", "chat:1", takesAPhoto, []refFile{aPNG}},
		"the test key forwarding, an ordinary talk": {"factory", "chat:1", forwardingGrind(application.GristForwardMayor), nil},
	} {
		t.Run(name, func(t *testing.T) {
			rig := refMill(t, refGrist{main: tutorGrind(`"attachments":{"min":0,"max":0}`), ref: tc.ref, from: tc.from, talk: tc.talk, files: tc.files})
			answer := rig.run(t)
			if answer.Status != application.GristRefused || answer.Reason != application.GristReasonOverride {
				t.Fatalf("expected the grist refused for bringing its own grind, got %+v", answer)
			}
			if mails, grinds := rig.mails(t), len(rig.grinder.Calls()); mails != 0 || grinds != 0 {
				t.Errorf("expected nothing mailed and nothing ground, got %d mails and %d grinds", mails, grinds)
			}
		})
	}
}

// A grind's line and the timing of its kept run name the Claude Code version
// the grinder said ran it, so a grind that answers differently on the mill
// than in a shell can be laid beside the shell's `claude --version` after the
// fact (mw-gq6.1712). A grind that failed keeps it too, and a result that
// names none writes no harness_version at all.
func TestGristLineKeepsTheHarnessVersion(t *testing.T) {
	const version = "2.1.7 (Claude Code)"
	for name, tc := range map[string]struct {
		result application.SessionResult
		err    error
		status string
	}{
		"answered": {application.SessionResult{Subtype: "success", HarnessVersion: version,
			Answer: json.RawMessage(`{"items":[],"placeName":"x"}`)}, nil, application.GristAnswered},
		"ran out of time": {application.SessionResult{HarnessVersion: version}, application.ErrGrindTimedOut, application.GristFailed},
	} {
		t.Run(name, func(t *testing.T) {
			mill, _, grinder := aSmallMill(t)
			runs := &apptest.FakeGristRuns{}
			mill.Runs = runs
			grinder.Result, grinder.Err = tc.result, tc.err
			if _, err := mill.Run(context.Background()); err != nil {
				t.Fatal(err)
			}
			lines, _ := mill.State.Lines(context.Background())
			if len(lines) != 1 || lines[0].Status != tc.status {
				t.Fatalf("expected one %s line, got %+v", tc.status, lines)
			}
			line, _ := json.Marshal(lines[0])
			if want := `"harness_version":"` + version + `"`; !strings.Contains(string(line), want) {
				t.Errorf("expected %s in the grinds.jsonl line, got %s", want, line)
			}
			if len(runs.Kept) != 1 {
				t.Fatalf("expected one run kept, got %+v", runs.Kept)
			}
			timing, _ := json.Marshal(runs.Kept[0])
			if want := `"harness_version":"` + version + `"`; !strings.Contains(string(timing), want) {
				t.Errorf("expected %s in the kept run's timing, got %s", want, timing)
			}
		})
	}

	bare, _, bareGrinder := aSmallMill(t)
	bareGrinder.Result = application.SessionResult{Subtype: "success", Answer: json.RawMessage(`{"items":[],"placeName":"x"}`)}
	if _, err := bare.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	lines, _ := bare.State.Lines(context.Background())
	if line, _ := json.Marshal(lines); strings.Contains(string(line), "harness_version") {
		t.Errorf("expected no harness_version from a result that names none, got %s", line)
	}
}
