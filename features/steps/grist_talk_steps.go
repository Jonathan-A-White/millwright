package steps

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/cucumber/godog"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/infrastructure/grist"
)

// talkContext is what features/grist_talk.feature adds to a mill: a stand-in
// for the claude program that keeps its sessions as claude does, the real
// talk store under the scenario's home, and what `mw grist talks` printed.
type talkContext struct {
	c *gristContext

	dir   string // the stand-in's own directory: its logs, its answer
	store *grist.Talks
	next  string            // the talk the next grist names; empty is none
	sent  map[string]string // each grist's question to its txid
	out   bytes.Buffer
}

// talkStandIn is the claude program of this feature. Each run logs "start",
// then "end", with its pid, cold or warm, and the request's "q"; keeps its
// arguments, its directory and the CLAUDE_CODE_PROMPT_CACHE_TTL it saw ("-"
// for none). A session it starts (--session-id) or resumes (--resume) is kept
// as claude keeps it, a transcript under projects/<cwd's slug>/, and its
// cumulative cost grows by 0.0125 a turn. A resume fails, exit 1, when the
// transcript is gone or resume-fails exists. A run whose q has a hold-<q> file
// waits until the run named in it has started.
const talkStandIn = `#!/bin/sh
dir=$(dirname "$0")
mode=cold
case "$*" in *--input-format*) mode=warm;; esac
for a; do last=$a; done
q=$(printf '%s' "$last" | sed -n 's/.*"q":"\([^"]*\)".*/\1/p')
printf '%s\0' "$@" > "$dir/args.$$"
pwd > "$dir/cwd.$$"
printf '%s\n' "${CLAUDE_CODE_PROMPT_CACHE_TTL:--}" > "$dir/ttl.$$"
printf 'start %s %s %s\n' "$$" "$mode" "$q" >> "$dir/runs.log"
: > "$dir/started-$q"
if [ "$mode" = warm ]; then IFS= read -r line || exit 0; cat > /dev/null; fi
if [ -f "$dir/hold-$q" ]; then
  until=$(cat "$dir/hold-$q"); i=0
  while [ ! -f "$dir/started-$until" ] && [ $i -lt 600 ]; do sleep 0.05; i=$((i+1)); done
fi
sid=""; new=""; resume=""
prev=""
for a; do
  case "$prev" in --session-id) sid=$a; new=1;; --resume) sid=$a; resume=1;; esac
  prev=$a
done
slug=$(pwd | sed 's/[^A-Za-z0-9]/-/g')
transcript="$dir/projects/$slug/$sid.jsonl"
if [ -n "$resume" ] && { [ -f "$dir/resume-fails" ] || [ ! -f "$transcript" ]; }; then
  printf '{"type":"result","subtype":"error_during_execution","is_error":true,"result":"No conversation found with session ID: %s","num_turns":0,"duration_ms":5}\n' "$sid"
  printf 'end %s %s %s\n' "$$" "$mode" "$q" >> "$dir/runs.log"
  exit 1
fi
turns=1
if [ -n "$sid" ]; then
  mkdir -p "$dir/projects/$slug"
  printf '{"turn":"%s"}\n' "$q" >> "$transcript"
  turns=$(wc -l < "$transcript" | tr -d ' ')
fi
cost=$(printf '0.%04d' $((turns * 125)))
printf '{"type":"result","subtype":"success","is_error":false,"result":"","stop_reason":"end_turn","num_turns":1,"duration_ms":100,"session_id":"%s","total_cost_usd":%s,"usage":{"input_tokens":10,"output_tokens":20,"cache_read_input_tokens":%d,"cache_creation_input_tokens":500},"structured_output":' "$sid" "$cost" $((turns * 1000))
cat "$dir/answer.json"
printf '}\n'
printf 'end %s %s %s\n' "$$" "$mode" "$q" >> "$dir/runs.log"
`

// talkRun is one start or end the stand-in logged.
type talkRun struct {
	event, pid, mode, q string
}

// talks is the mill's talks, when the stand-in is in use.
func (t *talkContext) talks() *application.GristTalks {
	if t.store == nil {
		return nil
	}
	return application.NewGristTalks(t.store)
}

func registerGristTalk(ctx *godog.ScenarioContext, c *gristContext) {
	t := &c.talk
	ctx.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		t.c = c
		return ctx, nil
	})

	ctx.Given(`^the mill runs its talk grinds through a stand-in claude$`, t.standIn)
	ctx.Given(`^the phone sends a talk grist in "([^"]*)" asking "([^"]*)"$`, t.sendInTalk)
	ctx.Given(`^the phone sends a grist asking "([^"]*)" with no talk$`, t.sendNoTalk)
	ctx.Given(`^the stand-in holds "([^"]*)" until "([^"]*)" has started$`, t.holds)
	ctx.Given(`^the app's grind instructions change$`, t.instructionsChange)
	ctx.Given(`^the talk's transcript is deleted$`, t.transcriptDeleted)
	ctx.Given(`^every resume exits 1$`, t.resumesFail)
	ctx.Given(`^the talk's last turn was (\d+) minutes ago$`, t.lastTurnAgo)
	ctx.Given(`^the talk "([^"]*)" last turned (\d+) minutes ago$`, t.talkLastTurnedAgo)
	ctx.Given(`^the mill grinds again$`, c.theMillGrinds)
	ctx.When(`^mw grist talks is run$`, func() error { return t.list(false) })
	ctx.When(`^mw grist talks --json is run$`, func() error { return t.list(true) })

	ctx.Then(`^(session \d+|the last session) started a new session from cairn's own directory, with CLAUDE_CODE_PROMPT_CACHE_TTL=1h$`, t.startedNew)
	ctx.Then(`^(session \d+|the last session) resumed session (\d+)'s from cairn's own directory, with CLAUDE_CODE_PROMPT_CACHE_TTL=1h$`, t.resumed)
	ctx.Then(`^session (\d+) was given the same --model, --system-prompt and --json-schema as session (\d+)$`, t.sameFlags)
	ctx.Then(`^(session \d+|the last session)'s message holds the request (with|without) its history$`, t.messageHistory)
	ctx.Then(`^the answer to "([^"]*)" says the talk's session is "([^"]*)"(?: because "([^"]*)")?, at turn (\d+)$`, t.answerSays)
	ctx.Then(`^the answer to "([^"]*)" says nothing of a talk$`, t.answerSaysNothing)
	ctx.Then(`^the mill keeps no talk$`, t.keepsNoTalk)
	ctx.Then(`^session (\d+)'s arguments, its system prompt, schema and message set aside, are exactly:$`, t.argumentsAre)
	ctx.Then(`^session (\d+) had no CLAUDE_CODE_PROMPT_CACHE_TTL$`, t.noTTL)
	ctx.Then(`^no warm session was started$`, t.noWarm)
	ctx.Then(`^"([^"]*)" started before "([^"]*)" ended$`, t.startedBeforeEnded)
	ctx.Then(`^"([^"]*)" started after "([^"]*)" ended, resuming its session$`, t.startedAfterEnded)
	ctx.Then(`^the last session's id is not session 1's$`, t.newID)
	ctx.Then(`^the talk's record names session 1's as ended$`, t.recordNamesEnded)
	ctx.Then(`^(\d+) sessions ran$`, t.sessionsRan)
	ctx.Then(`^no session ran$`, func() error { return t.sessionsRan(0) })
	ctx.Then(`^no session ran with --resume$`, t.noResume)
	ctx.Then(`^the mill's last line names the talk "([^"]*)", session (\d+)'s id, resumed, and costs ([\d.]+) of a session total of ([\d.]+)$`, t.lastLine)
	ctx.Then(`^it prints one line for "([^"]*)": cairn, (\d+) turns, ([\d,]+) context tokens, ([\d,]+) cache read, (\S+) since last use, and session (\d+)'s id$`, t.printsLine)
	ctx.Then(`^it prints no line for "([^"]*)"$`, t.printsNoLine)
	ctx.Then(`^its JSON lists "([^"]*)" with (\d+) turns, (\d+) context tokens, (\d+) cache read and session (\d+)'s id$`, t.jsonLists)
}

func (t *talkContext) standIn() error {
	c := t.c
	t.dir = filepath.Join(c.home, "talk-standin")
	t.sent = map[string]string{}
	if err := os.MkdirAll(t.dir, 0o755); err != nil {
		return err
	}
	program := filepath.Join(t.dir, "claude")
	if err := os.WriteFile(program, []byte(talkStandIn), 0o755); err != nil {
		return err
	}
	var answer bytes.Buffer
	if err := json.Compact(&answer, c.vectors.Answers["answered"].Answer); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(t.dir, "answer.json"), answer.Bytes(), 0o644); err != nil {
		return err
	}
	t.store = &grist.Talks{Dir: filepath.Join(c.home, "state"), Projects: filepath.Join(t.dir, "projects")}
	// The real grinder runs the stand-in, billed as features/grist_credits.feature bills.
	cc := &c.credits
	cc.program, cc.dir = program, t.dir
	cc.state = filepath.Join(c.home, "credits-state")
	cc.keyFile = filepath.Join(c.home, "anthropic-api.key")
	cc.events = &apptest.FakeEventLog{}
	return nil
}

func (t *talkContext) sendInTalk(talk, q string) error {
	t.next = talk
	defer func() { t.next = "" }()
	return t.send(q, `,"history":[{"role":"user","text":"my son Luke is 14"}]`)
}

func (t *talkContext) sendNoTalk(q string) error { return t.send(q, "") }

func (t *talkContext) send(q, history string) error {
	c := t.c
	c.askInput = json.RawMessage(fmt.Sprintf(`{"q":%q%s}`, q, history))
	defer func() { c.askInput = nil }()
	if err := c.thePhoneSends("cairn", "sweep", "1.1", 1); err != nil {
		return err
	}
	t.sent[q] = c.grist.Txid
	return nil
}

func (t *talkContext) holds(q, until string) error {
	return os.WriteFile(filepath.Join(t.dir, "hold-"+q), []byte(until), 0o644)
}

func (t *talkContext) instructionsChange() error {
	c := t.c
	c.grinds.SetFile(c.checkout("cairn"), c.commit, "grinds/sweep.md", []byte(gristInstructions+" Count them too."))
	return nil
}

func (t *talkContext) transcriptDeleted() error {
	return os.RemoveAll(filepath.Join(t.dir, "projects"))
}

func (t *talkContext) resumesFail() error {
	return os.WriteFile(filepath.Join(t.dir, "resume-fails"), nil, 0o644)
}

func (t *talkContext) lastTurnAgo(minutes int) error { return t.talkLastTurnedAgo("", minutes) }

// talkLastTurnedAgo moves the last use of the talk named (the only one, when
// talk is empty) back by minutes.
func (t *talkContext) talkLastTurnedAgo(talk string, minutes int) error {
	record, err := t.record(talk)
	if err != nil {
		return err
	}
	record.LastUsed = record.LastUsed.Add(-time.Duration(minutes) * time.Minute)
	return t.store.Save(context.Background(), record)
}

// record is the record of the talk named, or the only one when talk is empty.
func (t *talkContext) record(talk string) (application.GristTalk, error) {
	talks, err := t.store.List(context.Background())
	if err != nil {
		return application.GristTalk{}, err
	}
	var found []application.GristTalk
	for _, r := range talks {
		if talk == "" || r.Talk == talk {
			found = append(found, r)
		}
	}
	if len(found) != 1 {
		return application.GristTalk{}, fmt.Errorf("the mill keeps %d talks named %q, not 1: %+v", len(found), talk, talks)
	}
	return found[0], nil
}

func (t *talkContext) list(asJSON bool) error {
	t.out.Reset()
	_, err := application.GristTalksList{Store: t.store, Out: &t.out, Now: func() time.Time { return gristNow }}.Run(context.Background(), "", asJSON)
	return err
}

// runs is every start and end the stand-in logged, in order.
func (t *talkContext) runs() ([]talkRun, error) {
	raw, err := os.ReadFile(filepath.Join(t.dir, "runs.log"))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var runs []talkRun
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		f := strings.Fields(line)
		if len(f) < 3 {
			continue
		}
		run := talkRun{event: f[0], pid: f[1], mode: f[2]}
		if len(f) > 3 {
			run.q = f[3]
		}
		runs = append(runs, run)
	}
	return runs, nil
}

// started is the pid of every run in the order they started.
func (t *talkContext) started() ([]string, error) {
	runs, err := t.runs()
	if err != nil {
		return nil, err
	}
	var pids []string
	for _, r := range runs {
		if r.event == "start" {
			pids = append(pids, r.pid)
		}
	}
	return pids, nil
}

// session is the pid of the session named "session N" or "the last session".
func (t *talkContext) session(name string) (string, error) {
	pids, err := t.started()
	if err != nil {
		return "", err
	}
	if len(pids) == 0 {
		return "", fmt.Errorf("no session ran")
	}
	if name == "the last session" {
		return pids[len(pids)-1], nil
	}
	n, err := strconv.Atoi(strings.TrimPrefix(name, "session "))
	if err != nil {
		return "", err
	}
	if n < 1 || n > len(pids) {
		return "", fmt.Errorf("%d sessions ran, so there is no session %d", len(pids), n)
	}
	return pids[n-1], nil
}

func (t *talkContext) args(pid string) ([]string, error) {
	raw, err := os.ReadFile(filepath.Join(t.dir, "args."+pid))
	if err != nil {
		return nil, err
	}
	return strings.Split(strings.TrimSuffix(string(raw), "\x00"), "\x00"), nil
}

func (t *talkContext) file(kind, pid string) (string, error) {
	raw, err := os.ReadFile(filepath.Join(t.dir, kind+"."+pid))
	return strings.TrimSpace(string(raw)), err
}

// flag is the value after name in args, and whether it is there.
func flag(args []string, name string) (string, bool) {
	i := slices.Index(args, name)
	if i < 0 || i+1 >= len(args) {
		return "", false
	}
	return args[i+1], true
}

// talkSession checks the session of pid ran from cairn's talk directory with
// the one-hour cache, and reports its arguments.
func (t *talkContext) talkSession(pid string) ([]string, error) {
	args, err := t.args(pid)
	if err != nil {
		return nil, err
	}
	if slices.Contains(args, "--no-session-persistence") {
		return nil, fmt.Errorf("the session was given --no-session-persistence: %q", args)
	}
	cwd, err := t.file("cwd", pid)
	if err != nil {
		return nil, err
	}
	want, err := t.store.Cwd("cairn")
	if err != nil {
		return nil, err
	}
	if cwd != want {
		return nil, fmt.Errorf("the session ran in %s, not cairn's talk directory %s", cwd, want)
	}
	ttl, err := t.file("ttl", pid)
	if err != nil {
		return nil, err
	}
	if ttl != "1h" {
		return nil, fmt.Errorf("the session's CLAUDE_CODE_PROMPT_CACHE_TTL was %q, not 1h", ttl)
	}
	return args, nil
}

func (t *talkContext) startedNew(name string) error {
	pid, err := t.session(name)
	if err != nil {
		return err
	}
	args, err := t.talkSession(pid)
	if err != nil {
		return err
	}
	id, ok := flag(args, "--session-id")
	if !ok || len(id) != 36 {
		return fmt.Errorf("%s was not given --session-id <uuid>: %q", name, args)
	}
	if slices.Contains(args, "--resume") {
		return fmt.Errorf("%s was given --resume: %q", name, args)
	}
	return nil
}

// sessionID is the id session n started its session with.
func (t *talkContext) sessionID(n int) (string, error) {
	pid, err := t.session(fmt.Sprintf("session %d", n))
	if err != nil {
		return "", err
	}
	args, err := t.args(pid)
	if err != nil {
		return "", err
	}
	id, ok := flag(args, "--session-id")
	if !ok {
		return "", fmt.Errorf("session %d started no session of its own: %q", n, args)
	}
	return id, nil
}

func (t *talkContext) resumed(name string, of int) error {
	pid, err := t.session(name)
	if err != nil {
		return err
	}
	args, err := t.talkSession(pid)
	if err != nil {
		return err
	}
	want, err := t.sessionID(of)
	if err != nil {
		return err
	}
	if got, _ := flag(args, "--resume"); got != want {
		return fmt.Errorf("%s resumed %q, not session %d's %q: %q", name, got, of, want, args)
	}
	if slices.Contains(args, "--session-id") {
		return fmt.Errorf("%s was given --session-id as well: %q", name, args)
	}
	return nil
}

// withoutSession is args without the session flag and its value, and
// without the message.
func withoutSession(args []string) []string {
	var kept []string
	for i := 0; i < len(args)-1; i++ {
		if args[i] == "--session-id" || args[i] == "--resume" {
			i++
			continue
		}
		kept = append(kept, args[i])
	}
	return kept
}

func (t *talkContext) sameFlags(n, m int) error {
	var both [2][]string
	for i, k := range []int{n, m} {
		pid, err := t.session(fmt.Sprintf("session %d", k))
		if err != nil {
			return err
		}
		if both[i], err = t.args(pid); err != nil {
			return err
		}
	}
	a, b := withoutSession(both[0]), withoutSession(both[1])
	for _, name := range []string{"--model", "--system-prompt", "--json-schema"} {
		if _, ok := flag(a, name); !ok {
			return fmt.Errorf("session %d was not given %s", n, name)
		}
	}
	if !slices.Equal(a, b) {
		return fmt.Errorf("session %d's flags differ from session %d's:\n%q\n%q", n, m, a, b)
	}
	return nil
}

func (t *talkContext) messageHistory(name, with string) error {
	pid, err := t.session(name)
	if err != nil {
		return err
	}
	args, err := t.args(pid)
	if err != nil {
		return err
	}
	message := args[len(args)-1]
	if !strings.Contains(message, `{"q":"`) {
		return fmt.Errorf("%s's message holds no request: %q", name, message)
	}
	if has := strings.Contains(message, `"history"`); has != (with == "with") {
		return fmt.Errorf("%s's message holds the history: %v, want %s: %q", name, has, with, message)
	}
	return nil
}

// answerTo is the answer delivered to the grist that asked q.
func (t *talkContext) answerTo(q string) (application.GristAnswer, error) {
	txid, ok := t.sent[q]
	if !ok {
		return application.GristAnswer{}, fmt.Errorf("no grist asked %q", q)
	}
	_, _, answers, err := t.c.delivered()
	if err != nil {
		return application.GristAnswer{}, err
	}
	for _, a := range answers {
		if a.Re == txid {
			return a, nil
		}
	}
	return application.GristAnswer{}, fmt.Errorf("no answer to %q was delivered", q)
}

func (t *talkContext) answerSays(q, session, reason string, turns int) error {
	a, err := t.answerTo(q)
	if err != nil {
		return err
	}
	if a.Status != application.GristAnswered {
		return fmt.Errorf("the answer to %q is %s (%s)", q, a.Status, a.Reason)
	}
	want := application.GristAnswerTalk{Session: session, Reason: reason, Turns: turns, ContextTokens: 10 + 500 + 1000*turns}
	if a.Talk == nil || *a.Talk != want {
		return fmt.Errorf("the answer to %q says of its talk %+v, want %+v", q, a.Talk, want)
	}
	return nil
}

func (t *talkContext) answerSaysNothing(q string) error {
	a, err := t.answerTo(q)
	if err != nil {
		return err
	}
	if a.Talk != nil {
		return fmt.Errorf("the answer to %q says of a talk %+v", q, *a.Talk)
	}
	return nil
}

func (t *talkContext) keepsNoTalk() error {
	talks, err := t.store.List(context.Background())
	if err != nil {
		return err
	}
	if len(talks) != 0 {
		return fmt.Errorf("the mill keeps %d talks: %+v", len(talks), talks)
	}
	return nil
}

func (t *talkContext) argumentsAre(n int, want *godog.DocString) error {
	pid, err := t.session(fmt.Sprintf("session %d", n))
	if err != nil {
		return err
	}
	args, err := t.args(pid)
	if err != nil {
		return err
	}
	shown := slices.Clone(args)
	for name, mark := range map[string]string{"--system-prompt": "<system prompt>", "--json-schema": "<schema>"} {
		if i := slices.Index(shown, name); i >= 0 && i+1 < len(shown) {
			shown[i+1] = mark
		}
	}
	shown[len(shown)-1] = "<message>"
	if got := strings.Join(shown, "\n"); got != strings.TrimSpace(want.Content) {
		return fmt.Errorf("session %d's arguments are:\n%s", n, got)
	}
	return nil
}

func (t *talkContext) noTTL(n int) error {
	pid, err := t.session(fmt.Sprintf("session %d", n))
	if err != nil {
		return err
	}
	ttl, err := t.file("ttl", pid)
	if err != nil {
		return err
	}
	if ttl != "-" {
		return fmt.Errorf("session %d had CLAUDE_CODE_PROMPT_CACHE_TTL=%s", n, ttl)
	}
	return nil
}

func (t *talkContext) noWarm() error {
	runs, err := t.runs()
	if err != nil {
		return err
	}
	for _, r := range runs {
		if r.mode == "warm" {
			return fmt.Errorf("a warm session was started: %+v", r)
		}
	}
	return nil
}

// at is where the event of the run that asked q falls in the log.
func (t *talkContext) at(event, q string) (int, talkRun, error) {
	runs, err := t.runs()
	if err != nil {
		return 0, talkRun{}, err
	}
	for i, r := range runs {
		if r.event == event && r.q == q {
			return i, r, nil
		}
	}
	return 0, talkRun{}, fmt.Errorf("no run asking %q logged %s: %+v", q, event, runs)
}

func (t *talkContext) startedBeforeEnded(q, other string) error {
	start, _, err := t.at("start", q)
	if err != nil {
		return err
	}
	end, _, err := t.at("end", other)
	if err != nil {
		return err
	}
	if start > end {
		return fmt.Errorf("%q started after %q ended", q, other)
	}
	return nil
}

func (t *talkContext) startedAfterEnded(q, other string) error {
	start, run, err := t.at("start", q)
	if err != nil {
		return err
	}
	end, first, err := t.at("end", other)
	if err != nil {
		return err
	}
	if start < end {
		return fmt.Errorf("%q started before %q ended", q, other)
	}
	args, err := t.args(run.pid)
	if err != nil {
		return err
	}
	firstArgs, err := t.args(first.pid)
	if err != nil {
		return err
	}
	resumed, _ := flag(args, "--resume")
	started, _ := flag(firstArgs, "--session-id")
	if resumed == "" || resumed != started {
		return fmt.Errorf("%q resumed %q, not %q's session %q", q, resumed, other, started)
	}
	return nil
}

func (t *talkContext) newID() error {
	first, err := t.sessionID(1)
	if err != nil {
		return err
	}
	pid, err := t.session("the last session")
	if err != nil {
		return err
	}
	args, err := t.args(pid)
	if err != nil {
		return err
	}
	if id, _ := flag(args, "--session-id"); id == first {
		return fmt.Errorf("the last session's id is session 1's, %s", id)
	}
	return nil
}

func (t *talkContext) recordNamesEnded() error {
	first, err := t.sessionID(1)
	if err != nil {
		return err
	}
	record, err := t.record("")
	if err != nil {
		return err
	}
	if !slices.Contains(record.Ended, first) || record.Session == first {
		return fmt.Errorf("the talk's record is on %s with ended %q, not session 1's %s ended", record.Session, record.Ended, first)
	}
	return nil
}

func (t *talkContext) sessionsRan(n int) error {
	pids, err := t.started()
	if err != nil {
		return err
	}
	if len(pids) != n {
		return fmt.Errorf("%d sessions ran, not %d", len(pids), n)
	}
	return nil
}

func (t *talkContext) noResume() error {
	pids, err := t.started()
	if err != nil {
		return err
	}
	for _, pid := range pids {
		args, err := t.args(pid)
		if err != nil {
			return err
		}
		if slices.Contains(args, "--resume") {
			return fmt.Errorf("a session ran with --resume: %q", args)
		}
	}
	return nil
}

func (t *talkContext) lastLine(talk string, n int, cost, total float64) error {
	lines, err := t.c.state.Lines(context.Background())
	if err != nil {
		return err
	}
	if len(lines) == 0 {
		return fmt.Errorf("the mill's record holds no line")
	}
	line := lines[len(lines)-1]
	id, err := t.sessionID(n)
	if err != nil {
		return err
	}
	if line.Talk != talk || line.SessionID != id || !line.Resumed || fmt.Sprintf("%.4f", line.CostUSD) != fmt.Sprintf("%.4f", cost) ||
		fmt.Sprintf("%.4f", line.SessionCostUSD) != fmt.Sprintf("%.4f", total) {
		return fmt.Errorf("the last line is talk %q, session %q, resumed %v, cost %v of %v", line.Talk, line.SessionID, line.Resumed, line.CostUSD, line.SessionCostUSD)
	}
	raw, err := json.Marshal(line)
	if err != nil {
		return err
	}
	for _, key := range []string{`"talk":`, `"session_id":`, `"resumed":true`, `"total_cost_usd":`, `"session_cost_usd":`} {
		if !bytes.Contains(raw, []byte(key)) {
			return fmt.Errorf("the last line has no %s: %s", key, raw)
		}
	}
	return nil
}

func (t *talkContext) printsLine(talk string, turns int, context, cacheRead, age string, n int) error {
	id, err := t.sessionID(n)
	if err != nil {
		return err
	}
	var found []string
	for _, line := range strings.Split(t.out.String(), "\n") {
		if strings.Contains(line, "  "+talk+"  ") {
			found = append(found, line)
		}
	}
	if len(found) != 1 {
		return fmt.Errorf("mw grist talks printed %d lines for %q:\n%s", len(found), talk, t.out.String())
	}
	for _, want := range []string{"cairn  ", fmt.Sprintf("%d turns", turns), context + " context tokens", cacheRead + " cache read", age + " since last use", "session " + id} {
		if !strings.Contains(found[0], want) {
			return fmt.Errorf("mw grist talks's line for %q has no %q: %s", talk, want, found[0])
		}
	}
	return nil
}

func (t *talkContext) printsNoLine(talk string) error {
	if strings.Contains(t.out.String(), "  "+talk+"  ") {
		return fmt.Errorf("mw grist talks printed a line for %q:\n%s", talk, t.out.String())
	}
	return nil
}

func (t *talkContext) jsonLists(talk string, turns, context, cacheRead, n int) error {
	id, err := t.sessionID(n)
	if err != nil {
		return err
	}
	var shown []application.GristTalkShown
	if err := json.Unmarshal(t.out.Bytes(), &shown); err != nil {
		return fmt.Errorf("mw grist talks --json printed no JSON list: %w\n%s", err, t.out.String())
	}
	for _, s := range shown {
		if s.Talk == talk {
			if s.App != "cairn" || s.Turns != turns || s.ContextTokens != context || s.CacheRead != cacheRead || s.Session != id {
				return fmt.Errorf("mw grist talks --json shows %+v", s)
			}
			return nil
		}
	}
	return fmt.Errorf("mw grist talks --json lists no %q:\n%s", talk, t.out.String())
}
