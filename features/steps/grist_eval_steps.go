package steps

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cucumber/godog"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/claude"
)

// gristEvalContext is one eval: a rig checkout and a photos directory on
// disk, and a shell script standing in for claude that answers from canned
// result files by model and photo, and logs every grind it is given. The
// grinder is the real claude.Grinder, so the flags and the result file are
// the ones a live grind has.
type gristEvalContext struct {
	root     string // everything a scenario writes lives here
	rig      string
	photos   string
	canned   string
	out      string
	program  string
	log      string
	models   []string // the [grist] models
	grindCmd string   // the grind file, inside the rig

	report application.GristEvalReport
	err    error
	ran    bool
}

// InitializeGristEvalScenario registers the steps of features/grist_eval.feature.
func InitializeGristEvalScenario(ctx *godog.ScenarioContext) {
	c := &gristEvalContext{}

	ctx.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
		*c = gristEvalContext{}
		return ctx, nil
	})
	ctx.After(func(ctx context.Context, sc *godog.Scenario, err error) (context.Context, error) {
		if c.root != "" {
			os.RemoveAll(c.root)
		}
		return ctx, nil
	})

	ctx.Given(`^a rig checkout holding the grind "([^"]*)" on the model "([^"]*)" at "([^"]*)" effort$`, c.aRigCheckout)
	ctx.Given(`^the factory allows the models "([^"]*)"$`, c.theFactoryAllows)
	ctx.Given(`^the photo "([^"]*)" expecting:$`, c.aPhotoExpecting)
	ctx.Given(`^a photo "([^"]*)" with no file listing what is expected$`, c.aPhotoWithoutAnswers)
	ctx.Given(`^the model "([^"]*)" answers for "([^"]*)" with the items "([^"]*)" at a cost of ([0-9.]+) USD$`, c.modelAnswersWithItems)
	ctx.Given(`^the model "([^"]*)" answers for "([^"]*)" with this answer:$`, c.modelAnswersWith)
	ctx.Given(`^the model "([^"]*)" gives no answer for "([^"]*)"$`, c.modelGivesNoAnswer)

	ctx.When(`^mw grist eval runs on the models "([^"]*)"$`, c.evalRuns)
	ctx.When(`^mw grist eval runs on the grind file's own model$`, c.evalRunsOnTheGrindsModel)

	ctx.Then(`^the eval has (\d+) rows?$`, c.theEvalHasRows)
	ctx.Then(`^the row for "([^"]*)" on "([^"]*)" has (\d+) hits of (\d+), misses "([^"]*)", extras "([^"]*)" and (\d+) unsure$`, c.theRowHas)
	ctx.Then(`^the row for "([^"]*)" on "([^"]*)" spent ([0-9.]+) USD and (\d+) tokens in and (\d+) out$`, c.theRowSpent)
	ctx.Then(`^the row for "([^"]*)" on "([^"]*)" failed with an error and counts of zero$`, c.theRowFailed)
	ctx.Then(`^the summary for "([^"]*)" has (\d+) photos, (\d+) hits of (\d+) and a mean cost of ([0-9.]+) USD$`, c.theSummaryHas)
	ctx.Then(`^the summary for "([^"]*)" has (\d+) failed$`, c.theSummaryFailed)
	ctx.Then(`^the printed table names "([^"]*)", "([^"]*)", "([^"]*)" and "([^"]*)"$`, c.thePrintedTableNames)
	ctx.Then(`^the eval's notes say "([^"]*)"$`, c.theNotesSay)
	ctx.Then(`^no grind was given "([^"]*)"$`, c.noGrindWasGiven)
	ctx.Then(`^the eval is refused, saying "([^"]*)"$`, c.theEvalIsRefused)
	ctx.Then(`^no eval grind was run$`, c.noGrindWasRun)
	ctx.Then(`^nothing was written under the out directory$`, c.nothingWritten)
	ctx.Then(`^every grind ran on "([^"]*)" at "([^"]*)" effort$`, c.everyGrindRanOn)
	ctx.Then(`^every grind ran alone in its own private directory, now gone$`, c.everyGrindWasPrivate)
	ctx.Then(`^each grind was given the preamble, the grind's instructions and the photo by its full path$`, c.eachGrindWasGiven)
	ctx.Then(`^the out directory holds an eval jsonl with (\d+) lines carrying the full answers$`, c.theJSONLHolds)
	ctx.Then(`^the out directory holds an eval md that names the photos by file name only$`, c.theMDNamesFileNames)
}

const evalInstructions = "List the kinds of things you can see kept at the Place."

func (c *gristEvalContext) aRigCheckout(kind, model, effort string) error {
	var err error
	if c.root, err = os.MkdirTemp("", "mw-eval-feature-"); err != nil {
		return err
	}
	c.rig = filepath.Join(c.root, "Cairn")
	c.photos = filepath.Join(c.root, "photos")
	c.canned = filepath.Join(c.root, "canned")
	c.out = filepath.Join(c.root, "out")
	c.log = filepath.Join(c.root, "grinds.log")
	for _, dir := range []string{filepath.Join(c.rig, "grinds"), filepath.Join(c.rig, "contexts"), c.photos, c.canned} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	grind := fmt.Sprintf(`{"grind": 1, "app": "cairn", "kind": %q, "versions": ["1.1"], "model": %q, "effort": %q,
  "instructions": "grinds/%s.md", "answerSchema": "contexts/sweep-result.schema.json",
  "attachments": {"min": 1, "max": 4, "mime": ["image/jpeg", "image/png", "image/webp"], "maxBytes": 4194304}}`, kind, model, effort, kind)
	c.grindCmd = filepath.Join(c.rig, "grinds", kind+".json")
	for path, text := range map[string]string{
		c.grindCmd: grind,
		filepath.Join(c.rig, "grinds", kind+".md"):                   evalInstructions,
		filepath.Join(c.rig, "contexts", "sweep-result.schema.json"): gristSchema,
	} {
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			return err
		}
	}
	// The stand-in claude: which model, which photo (its content is its
	// name), and a log line with the directory and the modes it saw.
	c.program = filepath.Join(c.root, "claude")
	script := `#!/bin/sh
model=""
prompt=""
while [ $# -gt 0 ]; do
  [ "$1" = "--model" ] && model="$2"
  [ "$1" = "--effort" ] && effort="$2"
  [ "$1" = "--system-prompt" ] && system="$2"
  prompt="$1"
  shift
done
photo=$(ls "$(pwd)"/photo-* | head -1)
key=$(cat "$photo")
printf '%s\t%s\t%s\t%s\t%s\t%s\n' "$model" "$effort" "$(pwd)" "$(stat -c %a "$(pwd)")" "$(stat -c %a "$photo")" "$key" >> ` + c.log + `
printf '%s' "$system" > ` + c.log + `.system
printf '%s' "$prompt" > ` + c.log + `.prompt
canned=` + c.canned + `/"$model"."$key".json
[ -f "$canned" ] || { echo "no canned answer for $model $key" >&2; exit 1; }
cat "$canned"
`
	return os.WriteFile(c.program, []byte(script), 0o755)
}

func (c *gristEvalContext) theFactoryAllows(models string) error {
	c.models = strings.Split(models, ",")
	return nil
}

func (c *gristEvalContext) aPhotoExpecting(name string, doc *godog.DocString) error {
	if err := c.aPhotoWithoutAnswers(name); err != nil {
		return err
	}
	stem := strings.TrimSuffix(name, filepath.Ext(name))
	return os.WriteFile(filepath.Join(c.photos, stem+".txt"), []byte(doc.Content+"\n"), 0o644)
}

// aPhotoWithoutAnswers writes a photo whose bytes are its own stem, which is
// how the stand-in tells photos apart.
func (c *gristEvalContext) aPhotoWithoutAnswers(name string) error {
	stem := strings.TrimSuffix(name, filepath.Ext(name))
	return os.WriteFile(filepath.Join(c.photos, name), []byte(stem), 0o644)
}

func evalResult(cost float64, answer string) string {
	return fmt.Sprintf(`{"type":"result","subtype":"success","is_error":false,"duration_ms":21000,"duration_api_ms":19000,
"num_turns":4,"result":"","stop_reason":"end_turn","session_id":"abc","total_cost_usd":%g,
"usage":{"input_tokens":8,"output_tokens":512,"cache_read_input_tokens":14000,"cache_creation_input_tokens":2100},
"modelUsage":{"claude-x":{"inputTokens":8,"outputTokens":512,"cacheReadInputTokens":14000,"cacheCreationInputTokens":2100}},
"structured_output":%s}`, cost, answer)
}

func (c *gristEvalContext) canAnswer(model, photo, result string) error {
	stem := strings.TrimSuffix(photo, filepath.Ext(photo))
	return os.WriteFile(filepath.Join(c.canned, model+"."+stem+".json"), []byte(result), 0o644)
}

func (c *gristEvalContext) modelAnswersWithItems(model, photo, items string, cost float64) error {
	var list []map[string]any
	for _, name := range strings.Split(items, ",") {
		list = append(list, map[string]any{"name": name})
	}
	answer, err := json.Marshal(map[string]any{
		"schemaVersion": "1.1", "responseType": "sweep-result", "placeName": "Place", "items": list,
	})
	if err != nil {
		return err
	}
	return c.canAnswer(model, photo, evalResult(cost, string(answer)))
}

func (c *gristEvalContext) modelAnswersWith(model, photo string, doc *godog.DocString) error {
	return c.canAnswer(model, photo, evalResult(0.02, doc.Content))
}

// modelGivesNoAnswer leaves the stand-in with nothing canned, so it exits 1.
func (c *gristEvalContext) modelGivesNoAnswer(model, photo string) error { return nil }

func (c *gristEvalContext) eval() application.GristEval {
	return application.GristEval{
		Grinder:  claude.NewGrinder(claude.WithGrindProgram(c.program)),
		Ceilings: application.GristCeilings{Models: c.models},
		TempDir:  c.root,
	}
}

func (c *gristEvalContext) run(models []string) error {
	c.ran = true
	c.report, c.err = c.eval().Run(context.Background(), application.GristEvalRequest{
		Grind: c.grindCmd, Photos: c.photos, Models: models, Out: c.out,
	})
	return nil
}

func (c *gristEvalContext) evalRuns(models string) error { return c.run(strings.Split(models, ",")) }

func (c *gristEvalContext) evalRunsOnTheGrindsModel() error { return c.run(nil) }

func (c *gristEvalContext) ok() error {
	if c.err != nil {
		return fmt.Errorf("the eval failed: %v", c.err)
	}
	return nil
}

func (c *gristEvalContext) theEvalHasRows(n int) error {
	if err := c.ok(); err != nil {
		return err
	}
	if len(c.report.Rows) != n {
		return fmt.Errorf("expected %d rows, got %d", n, len(c.report.Rows))
	}
	return nil
}

func (c *gristEvalContext) row(photo, model string) (application.GristEvalRow, error) {
	if err := c.ok(); err != nil {
		return application.GristEvalRow{}, err
	}
	for _, r := range c.report.Rows {
		if r.Photo == photo && r.Model == model {
			return r, nil
		}
	}
	return application.GristEvalRow{}, fmt.Errorf("no row for %s on %s in %v", photo, model, c.report.Rows)
}

func (c *gristEvalContext) theRowHas(photo, model string, hits, expected int, misses, extras string, unsure int) error {
	r, err := c.row(photo, model)
	if err != nil {
		return err
	}
	if r.Hits != hits || r.Expected != expected || r.Unsure != unsure ||
		strings.Join(r.Missed, ", ") != misses || strings.Join(r.Extra, ", ") != extras {
		return fmt.Errorf("expected %d hits of %d, misses %q, extras %q, %d unsure; got %d of %d, misses %q, extras %q, %d unsure",
			hits, expected, misses, extras, unsure, r.Hits, r.Expected, strings.Join(r.Missed, ", "), strings.Join(r.Extra, ", "), r.Unsure)
	}
	return nil
}

func (c *gristEvalContext) theRowSpent(photo, model string, cost float64, in, out int) error {
	r, err := c.row(photo, model)
	if err != nil {
		return err
	}
	if r.CostUSD != cost || r.Fuel.Input != in || r.Fuel.Output != out {
		return fmt.Errorf("expected %g USD and %d in / %d out, got %g USD and %d in / %d out", cost, in, out, r.CostUSD, r.Fuel.Input, r.Fuel.Output)
	}
	return nil
}

func (c *gristEvalContext) theRowFailed(photo, model string) error {
	r, err := c.row(photo, model)
	if err != nil {
		return err
	}
	if r.Error == "" || r.Hits != 0 || len(r.Missed) != 0 || len(r.Extra) != 0 || r.Unsure != 0 || len(r.Answer) != 0 {
		return fmt.Errorf("expected an error and counts of zero, got %+v", r)
	}
	return nil
}

func (c *gristEvalContext) summary(model string) (application.GristEvalModel, error) {
	if err := c.ok(); err != nil {
		return application.GristEvalModel{}, err
	}
	for _, m := range c.report.Models {
		if m.Model == model {
			return m, nil
		}
	}
	return application.GristEvalModel{}, fmt.Errorf("no summary for %s", model)
}

func (c *gristEvalContext) theSummaryHas(model string, photos, hits, expected int, mean float64) error {
	m, err := c.summary(model)
	if err != nil {
		return err
	}
	if m.Photos != photos || m.Hits != hits || m.Expected != expected || fmt.Sprintf("%.3f", m.MeanCost()) != fmt.Sprintf("%.3f", mean) {
		return fmt.Errorf("expected %d photos, %d hits of %d, mean %.3f; got %+v (mean %.3f)", photos, hits, expected, mean, m, m.MeanCost())
	}
	return nil
}

func (c *gristEvalContext) theSummaryFailed(model string, failed int) error {
	m, err := c.summary(model)
	if err != nil {
		return err
	}
	if m.Failed != failed {
		return fmt.Errorf("expected %d failed, got %d", failed, m.Failed)
	}
	return nil
}

func (c *gristEvalContext) thePrintedTableNames(a, b, d, e string) error {
	if err := c.ok(); err != nil {
		return err
	}
	text := c.report.String()
	for _, want := range []string{a, b, d, e, "hits", "misses", "extras", "unsure", "cost"} {
		if !strings.Contains(text, want) {
			return fmt.Errorf("expected the table to name %q, got:\n%s", want, text)
		}
	}
	return nil
}

func (c *gristEvalContext) theNotesSay(text string) error {
	if err := c.ok(); err != nil {
		return err
	}
	if !strings.Contains(strings.Join(c.report.Notes, "\n"), text) {
		return fmt.Errorf("expected a note naming %q, got %q", text, c.report.Notes)
	}
	return nil
}

// grinds reads what the stand-in logged: one line a grind.
func (c *gristEvalContext) grinds() [][]string {
	raw, _ := os.ReadFile(c.log)
	var lines [][]string
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if line != "" {
			lines = append(lines, strings.Split(line, "\t"))
		}
	}
	return lines
}

func (c *gristEvalContext) noGrindWasGiven(photo string) error {
	stem := strings.TrimSuffix(photo, filepath.Ext(photo))
	for _, g := range c.grinds() {
		if g[5] == stem {
			return fmt.Errorf("a grind was given %s", photo)
		}
	}
	return nil
}

func (c *gristEvalContext) theEvalIsRefused(text string) error {
	if c.err == nil || !strings.Contains(c.err.Error(), text) {
		return fmt.Errorf("expected the eval refused, naming %q, got %v", text, c.err)
	}
	return nil
}

func (c *gristEvalContext) noGrindWasRun() error {
	if n := len(c.grinds()); n != 0 {
		return fmt.Errorf("expected no grind, %d ran", n)
	}
	return nil
}

func (c *gristEvalContext) nothingWritten() error {
	if entries, _ := os.ReadDir(c.out); len(entries) != 0 {
		return fmt.Errorf("expected nothing under %s, found %d entries", c.out, len(entries))
	}
	return nil
}

func (c *gristEvalContext) everyGrindRanOn(model, effort string) error {
	if err := c.ok(); err != nil {
		return err
	}
	grinds := c.grinds()
	if len(grinds) == 0 {
		return fmt.Errorf("no grind ran")
	}
	for _, g := range grinds {
		if g[0] != model || g[1] != effort {
			return fmt.Errorf("expected every grind on %s at %s, one ran on %s at %s", model, effort, g[0], g[1])
		}
	}
	return nil
}

func (c *gristEvalContext) everyGrindWasPrivate() error {
	if err := c.ok(); err != nil {
		return err
	}
	grinds := c.grinds()
	if len(grinds) != len(c.report.Rows) || len(grinds) == 0 {
		return fmt.Errorf("expected one grind a row, %d grinds for %d rows", len(grinds), len(c.report.Rows))
	}
	seen := map[string]bool{}
	for _, g := range grinds {
		if seen[g[2]] {
			return fmt.Errorf("two grinds shared the directory %s", g[2])
		}
		seen[g[2]] = true
		if g[3] != "700" || g[4] != "600" {
			return fmt.Errorf("expected the directory 0700 and the photo 0600, got %s and %s", g[3], g[4])
		}
		if _, err := os.Stat(g[2]); err == nil {
			return fmt.Errorf("the directory %s is still there", g[2])
		}
	}
	return nil
}

// eachGrindWasGiven reads the last grind's system prompt and prompt, which
// the stand-in keeps: the same words the mill gives one photo.
func (c *gristEvalContext) eachGrindWasGiven() error {
	system, _ := os.ReadFile(c.log + ".system")
	prompt, _ := os.ReadFile(c.log + ".prompt")
	photo := ""
	for _, line := range strings.Split(string(prompt), "\n") {
		if strings.Contains(line, "/photo-1.") {
			photo = line
		}
	}
	switch {
	case !strings.Contains(string(system), "You are the factory's mill") || !strings.HasSuffix(string(system), evalInstructions):
		return fmt.Errorf("expected the mill's preamble then the grind's instructions, got %q", system)
	case !filepath.IsAbs(photo):
		return fmt.Errorf("expected the photo named by its full path in the prompt, got %q", prompt)
	case !strings.Contains(string(prompt), "GRIST INPUT "):
		return fmt.Errorf("expected the request between markers, got %q", prompt)
	}
	return nil
}

func (c *gristEvalContext) theJSONLHolds(n int) error {
	if err := c.ok(); err != nil {
		return err
	}
	matches, _ := filepath.Glob(filepath.Join(c.out, "eval-*.jsonl"))
	if len(matches) != 1 || matches[0] != c.report.JSONL {
		return fmt.Errorf("expected one eval jsonl at %q, found %v", c.report.JSONL, matches)
	}
	raw, err := os.ReadFile(matches[0])
	if err != nil {
		return err
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != n {
		return fmt.Errorf("expected %d lines, got %d", n, len(lines))
	}
	for _, line := range lines {
		var row struct {
			Photo  string          `json:"photo"`
			Model  string          `json:"model"`
			Answer json.RawMessage `json:"answer"`
		}
		if err := json.Unmarshal([]byte(line), &row); err != nil || row.Photo == "" || row.Model == "" || !strings.Contains(string(row.Answer), "sweep-result") {
			return fmt.Errorf("expected a row with its photo, model and full answer, got %s", line)
		}
		if strings.Contains(row.Photo, string(filepath.Separator)) {
			return fmt.Errorf("expected a photo named by file name only, got %s", row.Photo)
		}
	}
	return nil
}

func (c *gristEvalContext) theMDNamesFileNames() error {
	if err := c.ok(); err != nil {
		return err
	}
	matches, _ := filepath.Glob(filepath.Join(c.out, "eval-*.md"))
	if len(matches) != 1 || matches[0] != c.report.Markdown {
		return fmt.Errorf("expected one eval md at %q, found %v", c.report.Markdown, matches)
	}
	raw, err := os.ReadFile(matches[0])
	if err != nil {
		return err
	}
	text := string(raw)
	if !strings.Contains(text, "drawer.jpg") || !strings.Contains(text, "shelf.png") {
		return fmt.Errorf("expected the photos named, got:\n%s", text)
	}
	if strings.Contains(text, c.photos) || strings.Contains(text, c.root) {
		return fmt.Errorf("expected file names only, but the md holds a path:\n%s", text)
	}
	return nil
}
