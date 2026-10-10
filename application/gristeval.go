package application

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"text/tabwriter"
	"time"
)

// GristEval is `mw grist eval`: one grind tried on a directory of photos
// whose answers are known, on each model named, and scored. Each grind is the
// one the mill would run for that photo, through the same Grinder, one
// session at a time. It never touches the backend, the mill's state or the
// dispatch cap, and keeps the photos and answers out of beads and the vault:
// its report names photos by file name only.
type GristEval struct {
	Grinder Grinder
	// Ceilings are the factory's limits above every grind: each model
	// tried must be among Ceilings.Models, and a grind may run Ceilings.Timeout.
	Ceilings GristCeilings
	// TempDir is where each grind's private directory is made; empty is the
	// system's.
	TempDir string
	// Now is the clock; nil is time.Now.
	Now func() time.Time
	// Out is where the report is printed; nil prints nothing.
	Out io.Writer
}

// GristEvalRequest is one eval.
type GristEvalRequest struct {
	// Grind is a grinds/<kind>.json in a rig checkout, read from the working
	// tree; its instructions and schema are read from the same checkout.
	Grind string
	// Photos is the directory of photos, each with a <name>.txt beside it.
	Photos string
	// Models and Effort default to the grind file's own.
	Models []string
	Effort string
	// Out is the directory the eval's files are written to.
	Out string
}

// GristEvalNoNames is what a row says of an answer holding no names to score,
// in place of counting every expected name a miss.
const GristEvalNoNames = "no names to score"

// GristEvalRow is one photo on one model. A grind that failed has Error set
// and every count zero.
type GristEvalRow struct {
	Photo    string          `json:"photo"`
	Model    string          `json:"model"`
	Effort   string          `json:"effort"`
	Expected int             `json:"expected"`
	Hits     int             `json:"hits"`
	Missed   []string        `json:"missed"`
	Extra    []string        `json:"extra"`
	Unsure   int             `json:"unsure"`
	Seconds  float64         `json:"seconds"`
	CostUSD  float64         `json:"costUsd"`
	Fuel     Fuel            `json:"fuel"`
	Note     string          `json:"note,omitempty"`
	Error    string          `json:"error,omitempty"`
	Answer   json.RawMessage `json:"answer,omitempty"`
}

// GristEvalModel is what one model did over every photo.
type GristEvalModel struct {
	Model    string
	Photos   int
	Failed   int
	Expected int
	Hits     int
	Misses   int
	Extras   int
	Unsure   int
	Seconds  float64
	CostUSD  float64
	Fuel     Fuel
}

// MeanCost is the model's cost per photo, in USD.
func (m GristEvalModel) MeanCost() float64 {
	if m.Photos == 0 {
		return 0
	}
	return m.CostUSD / float64(m.Photos)
}

// GristEvalReport is what an eval found, and where it wrote it.
type GristEvalReport struct {
	Rows   []GristEvalRow
	Models []GristEvalModel
	Notes  []string
	// JSONL and Markdown are the files written, full paths.
	JSONL    string
	Markdown string
}

// String is the report as the tables: one row a photo and model, then a
// summary a model, then the names behind the counts.
func (r GristEvalReport) String() string {
	var b strings.Builder
	for _, note := range r.Notes {
		fmt.Fprintf(&b, "note: %s\n", note)
	}
	b.WriteString(r.tables("", true))
	if r.JSONL != "" {
		fmt.Fprintf(&b, "\nRows with the full answers: %s\nTables: %s\n", r.JSONL, r.Markdown)
	}
	return b.String()
}

// markdown is the report's tables, for the .md file.
func (r GristEvalReport) markdown() string {
	var b strings.Builder
	b.WriteString("# mw grist eval\n\n")
	for _, note := range r.Notes {
		fmt.Fprintf(&b, "- %s\n", note)
	}
	if len(r.Notes) > 0 {
		b.WriteString("\n")
	}
	b.WriteString(r.tables("|", false))
	return b.String()
}

// tables writes both tables, as a tab-aligned text or, with a bar, as
// Markdown.
func (r GristEvalReport) tables(bar string, aligned bool) string {
	var b strings.Builder
	rows := [][]string{{"photo", "model", "hits/expected", "misses", "extras", "unsure", "seconds", "cost USD", "tokens in", "tokens out"}}
	for _, row := range r.Rows {
		score := fmt.Sprintf("%d/%d", row.Hits, row.Expected)
		if row.Note != "" {
			score = row.Note
		}
		rows = append(rows, []string{
			row.Photo, row.Model, score, fmt.Sprint(len(row.Missed)),
			fmt.Sprint(len(row.Extra)), fmt.Sprint(row.Unsure), fmt.Sprintf("%.1f", row.Seconds),
			fmt.Sprintf("%.4f", row.CostUSD), Thousands(row.Fuel.Input), Thousands(row.Fuel.Output),
		})
	}
	writeGrid(&b, rows, bar, aligned)
	b.WriteString("\n")
	sums := [][]string{{"model", "photos", "failed", "hits/expected", "misses", "extras", "unsure", "seconds", "cost USD", "mean cost USD a photo", "tokens in", "tokens out"}}
	for _, m := range r.Models {
		sums = append(sums, []string{
			m.Model, fmt.Sprint(m.Photos), fmt.Sprint(m.Failed), fmt.Sprintf("%d/%d", m.Hits, m.Expected),
			fmt.Sprint(m.Misses), fmt.Sprint(m.Extras), fmt.Sprint(m.Unsure), fmt.Sprintf("%.1f", m.Seconds),
			fmt.Sprintf("%.4f", m.CostUSD), fmt.Sprintf("%.4f", m.MeanCost()), Thousands(m.Fuel.Input), Thousands(m.Fuel.Output),
		})
	}
	writeGrid(&b, sums, bar, aligned)
	var names strings.Builder
	for _, row := range r.Rows {
		switch {
		case row.Error != "":
			fmt.Fprintf(&names, "- %s on %s failed: %s\n", row.Photo, row.Model, row.Error)
		case row.Note != "":
			fmt.Fprintf(&names, "- %s on %s: %s\n", row.Photo, row.Model, row.Note)
		case len(row.Missed) > 0 || len(row.Extra) > 0:
			fmt.Fprintf(&names, "- %s on %s: missed [%s]; extra [%s]\n", row.Photo, row.Model, strings.Join(row.Missed, ", "), strings.Join(row.Extra, ", "))
		}
	}
	if names.Len() > 0 {
		b.WriteString("\n" + names.String())
	}
	return b.String()
}

// writeGrid writes rows as a Markdown table (bar set) or an aligned one.
func writeGrid(b *strings.Builder, rows [][]string, bar string, aligned bool) {
	if aligned {
		w := tabwriter.NewWriter(b, 0, 0, 2, ' ', 0)
		for _, row := range rows {
			fmt.Fprintln(w, strings.Join(row, "\t"))
		}
		w.Flush()
		return
	}
	for i, row := range rows {
		fmt.Fprintf(b, "%s %s %s\n", bar, strings.Join(row, " "+bar+" "), bar)
		if i == 0 {
			fmt.Fprintf(b, "%s%s\n", bar, strings.Repeat(" --- "+bar, len(row)))
		}
	}
}

// evalPhoto is a photo to try, and what it should be found to hold.
type evalPhoto struct {
	name     string
	path     string
	expected []string
}

// Run tries the grind on every photo that has answers, on every model,
// scores each answer, and writes the rows and the tables under req.Out. It
// refuses, before any grind runs, a grind file it cannot read, a model the
// factory does not allow, or a directory with nothing to try.
func (e GristEval) Run(ctx context.Context, req GristEvalRequest) (GristEvalReport, error) {
	var report GristEvalReport
	if e.Grinder == nil {
		return report, fmt.Errorf("mw grist eval: nothing to run a grind with")
	}
	grind, instructions, schema, err := readGrindTree(req.Grind)
	if err != nil {
		return report, err
	}
	ceilings := e.Ceilings.filled()
	models := req.Models
	if len(models) == 0 {
		models = []string{grind.Model}
	}
	for _, model := range models {
		if !slices.Contains(ceilings.Models, model) {
			return report, fmt.Errorf("the model %q is not one the factory allows (config [grist] models: %s)", model, strings.Join(ceilings.Models, ", "))
		}
	}
	effort := req.Effort
	if effort == "" {
		effort = grind.Effort
	}
	if !slices.Contains(GrindEfforts, effort) {
		return report, fmt.Errorf("the effort %q is not one of %s", effort, strings.Join(GrindEfforts, ", "))
	}
	photos, notes, err := evalPhotos(req.Photos)
	if err != nil {
		return report, err
	}
	report.Notes = notes
	if len(photos) == 0 {
		return report, fmt.Errorf("nothing to evaluate in %s: no photo (.jpg, .jpeg, .png or .webp) has a .txt beside it", req.Photos)
	}
	if req.Out == "" {
		return report, fmt.Errorf("mw grist eval: no directory to write the results to")
	}

	plain := GristPlaintext{Grist: GristName{App: grind.App, Kind: grind.Kind}}
	if len(grind.Versions) > 0 {
		plain.Grist.V = grind.Versions[0]
		plain.Input, _ = json.Marshal(map[string]string{"schemaVersion": grind.Versions[0]})
	}
	byModel := map[string]*GristEvalModel{}
	for _, model := range models {
		byModel[model] = &GristEvalModel{Model: model}
	}
	for _, photo := range photos {
		for _, model := range models {
			if err := ctx.Err(); err != nil {
				return report, err
			}
			row := e.grindPhoto(ctx, plain, photo, model, effort, instructions, schema, ceilings.Timeout)
			report.Rows = append(report.Rows, row)
			sum := byModel[model]
			sum.Photos++
			sum.Expected += row.Expected
			sum.Hits += row.Hits
			sum.Misses += len(row.Missed)
			sum.Extras += len(row.Extra)
			sum.Unsure += row.Unsure
			sum.Seconds += row.Seconds
			sum.CostUSD += row.CostUSD
			sum.Fuel.Input += row.Fuel.Input
			sum.Fuel.Output += row.Fuel.Output
			sum.Fuel.CacheRead += row.Fuel.CacheRead
			sum.Fuel.CacheWrite += row.Fuel.CacheWrite
			if row.Error != "" {
				sum.Failed++
			}
		}
	}
	for _, model := range models {
		report.Models = append(report.Models, *byModel[model])
	}
	if err := e.write(req.Out, &report); err != nil {
		return report, err
	}
	if e.Out != nil {
		fmt.Fprint(e.Out, report.String())
	}
	return report, nil
}

// grindPhoto runs the grind once on one photo copied, private, into a
// directory of its own that is removed afterwards.
func (e GristEval) grindPhoto(ctx context.Context, plain GristPlaintext, photo evalPhoto, model, effort, instructions, schema string, timeout time.Duration) GristEvalRow {
	row := GristEvalRow{Photo: photo.name, Model: model, Effort: effort, Missed: []string{}, Extra: []string{}}
	fail := func(why string) GristEvalRow {
		row.Error = why
		return row
	}
	// An error's own words carry paths; the row says only what failed.
	dir, err := os.MkdirTemp(e.TempDir, "mw-eval-")
	if err != nil {
		return fail("could not make a private directory for the grind")
	}
	defer os.RemoveAll(dir)
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	data, err := os.ReadFile(photo.path)
	if err != nil {
		return fail("could not read the photo")
	}
	copied := filepath.Join(dir, "photo-1"+evalExtension(photo.name))
	if err := os.WriteFile(copied, data, 0o600); err != nil {
		return fail("could not copy the photo into the grind's directory")
	}
	started := e.now()
	result, err := e.Grinder.Grind(ctx, gristGrindCall(dir, plain, []string{copied}, model, effort, instructions, schema, timeout))
	row.Seconds = e.now().Sub(started).Seconds()
	row.CostUSD, row.Fuel = result.CostUSD, result.Fuel
	if status, reason := gristOutcome(result, err, schema); status != GristAnswered {
		why := reason
		switch {
		case err != nil:
			why += " " + err.Error()
		case strings.TrimSpace(result.Said) != "":
			why += " " + strings.TrimSpace(result.Said)
		}
		return fail(why)
	}
	answered, unsure, ok := evalAnswered(result.Answer)
	if !ok {
		return fail(GristReasonNoAnswer)
	}
	row.Answer = result.Answer
	if len(answered) == 0 {
		row.Note = GristEvalNoNames
		return row
	}
	row.Unsure = unsure
	row.Expected = len(photo.expected)
	for _, want := range photo.expected {
		if slices.Contains(answered, want) {
			row.Hits++
		} else {
			row.Missed = append(row.Missed, want)
		}
	}
	for _, got := range answered {
		if !slices.Contains(photo.expected, got) {
			row.Extra = append(row.Extra, got)
		}
	}
	return row
}

// write puts the rows, one a line with the full answer, and the tables in
// files of a UTC timestamp's name under dir, private.
func (e GristEval) write(dir string, report *GristEvalReport) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	stamp := e.now().UTC().Format("20060102T150405Z")
	var lines bytes.Buffer
	for _, row := range report.Rows {
		line, err := json.Marshal(row)
		if err != nil {
			return err
		}
		lines.Write(line)
		lines.WriteByte('\n')
	}
	report.JSONL = filepath.Join(dir, "eval-"+stamp+".jsonl")
	report.Markdown = filepath.Join(dir, "eval-"+stamp+".md")
	for path, data := range map[string][]byte{report.JSONL: lines.Bytes(), report.Markdown: []byte(report.markdown())} {
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return fmt.Errorf("writing %s: %w", path, err)
		}
		_, err = f.Write(data)
		if closeErr := f.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			return fmt.Errorf("writing %s: %w", path, err)
		}
	}
	return nil
}

// readGrindTree reads a grind file from a rig's working tree, and its
// instructions and compacted schema from the same rig, whose root is the
// parent of the grind file's directory.
func readGrindTree(path string) (grind GrindFile, instructions, schema string, err error) {
	if strings.TrimSpace(path) == "" {
		return grind, "", "", fmt.Errorf("mw grist eval: no grind file named")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return grind, "", "", fmt.Errorf("reading the grind file: %w", err)
	}
	if err := json.Unmarshal(raw, &grind); err != nil || grind.Grind != GrindFileFormat ||
		!gristKind.MatchString(grind.App) || !gristKind.MatchString(grind.Kind) || !slices.Contains(GrindEfforts, grind.Effort) {
		return grind, "", "", fmt.Errorf("%s is not a grind file the mill reads", path)
	}
	root := filepath.Dir(filepath.Dir(path))
	read := func(name, rel string) (string, error) {
		if !rigPath(rel) {
			return "", fmt.Errorf("the grind's %s, %q, is not a path inside its rig", name, rel)
		}
		text, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			return "", fmt.Errorf("reading the grind's %s: %w", name, err)
		}
		return string(text), nil
	}
	if instructions, err = read("instructions", grind.Instructions); err != nil {
		return grind, "", "", err
	}
	rawSchema, err := read("answer schema", grind.AnswerSchema)
	if err != nil {
		return grind, "", "", err
	}
	var compact bytes.Buffer
	if strings.TrimSpace(instructions) == "" || json.Compact(&compact, []byte(rawSchema)) != nil {
		return grind, "", "", fmt.Errorf("%s has empty instructions or an answer schema that is not JSON", path)
	}
	return grind, instructions, compact.String(), nil
}

// evalExtension is the extension a photo's copy carries: the mill's own
// for its type.
func evalExtension(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".png":
		return ".png"
	case ".webp":
		return ".webp"
	}
	return ".jpg"
}

// evalPhotos lists the photos in dir that have a <name>.txt beside them, by
// name, each with the names it is expected to hold; the others are noted.
func evalPhotos(dir string) (photos []evalPhoto, notes []string, err error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil, fmt.Errorf("reading the photos directory: %w", err)
	}
	for _, entry := range entries {
		ext := filepath.Ext(entry.Name())
		if entry.IsDir() || !slices.Contains([]string{".jpg", ".jpeg", ".png", ".webp"}, strings.ToLower(ext)) {
			continue
		}
		stem := strings.TrimSuffix(entry.Name(), ext)
		raw, err := os.ReadFile(filepath.Join(dir, stem+".txt"))
		if os.IsNotExist(err) {
			notes = append(notes, fmt.Sprintf("skipped %s: there is no %s.txt beside it", entry.Name(), stem))
			continue
		}
		if err != nil {
			return nil, nil, fmt.Errorf("reading %s.txt: %w", stem, err)
		}
		photos = append(photos, evalPhoto{name: entry.Name(), path: filepath.Join(dir, entry.Name()), expected: expectedNames(string(raw))})
	}
	sort.Slice(photos, func(i, j int) bool { return photos[i].name < photos[j].name })
	return photos, notes, nil
}

// normalName is a name as scoring compares it: trimmed, lower-case, single
// spaces.
func normalName(name string) string {
	return strings.ToLower(strings.Join(strings.Fields(name), " "))
}

// expectedNames is the names a photo's .txt lists: one a line, a
// "Container: X" line naming the container X, and the indented lines under
// it its items, which are names like any other.
func expectedNames(text string) []string {
	var names []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if head, rest, ok := strings.Cut(line, ":"); ok && strings.EqualFold(strings.TrimSpace(head), "container") {
			line = rest
		}
		if name := normalName(line); name != "" && !slices.Contains(names, name) {
			names = append(names, name)
		}
	}
	return names
}

// evalAnswered is every name an answer holds, normalised and once each in
// the order seen: each item, each container, each container's items and each
// line's text (a receipt-reconcile answer lists lines, not items). It
// counts the items marked unsure, which are answered all the same.
func evalAnswered(answer json.RawMessage) (names []string, unsure int, ok bool) {
	type item struct {
		Name   string `json:"name"`
		Unsure bool   `json:"unsure"`
	}
	var got struct {
		Items      []item `json:"items"`
		Containers []struct {
			Name  string `json:"name"`
			Items []item `json:"items"`
		} `json:"containers"`
		Lines []struct {
			Text string `json:"text"`
		} `json:"lines"`
	}
	if json.Unmarshal(answer, &got) != nil {
		return nil, 0, false
	}
	add := func(name string) {
		if name = normalName(name); name != "" && !slices.Contains(names, name) {
			names = append(names, name)
		}
	}
	for _, it := range got.Items {
		add(it.Name)
		if it.Unsure {
			unsure++
		}
	}
	for _, c := range got.Containers {
		add(c.Name)
		for _, it := range c.Items {
			add(it.Name)
			if it.Unsure {
				unsure++
			}
		}
	}
	for _, l := range got.Lines {
		add(l.Text)
	}
	return names, unsure, true
}

func (e GristEval) now() time.Time {
	if e.Now == nil {
		return time.Now()
	}
	return e.Now()
}
