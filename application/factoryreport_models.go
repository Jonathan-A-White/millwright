package application

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/Jonathan-A-White/millwright/domain"
)

// ModelColumns are the models MODELS sets side by side, in this order. The
// first ModelsAlwaysShown of them are columns whatever the window held; the
// others only once a story was landed or refused on them in it.
var ModelColumns = []domain.Model{domain.ModelOpus, domain.ModelSonnet, domain.ModelHaiku, domain.ModelFable}

// ModelsAlwaysShown is how many of ModelColumns are always columns.
const ModelsAlwaysShown = 2

// ModelUse is what one model did in the window, over every kind or over one:
// the stories it landed, each once, and the refusals the ledger gave it. A
// story counts under the model its ledger row names, the one that ran it.
type ModelUse struct {
	Model string `json:"model"`
	// Kind is the rig and bug or feature (rig/bug), empty for the model's whole.
	Kind string `json:"kind,omitempty"`
	// Sessions are its landings and refusals; none is a model with nothing to
	// measure, every number of it printed "-".
	Sessions int `json:"sessions"`
	Landed   int `json:"landed"`
	// FirstTry are the landed stories that took one attempt, FirstTryShare
	// their share of the landed in percent; Retried are the ones that took more.
	FirstTry      int     `json:"first_try"`
	FirstTryShare float64 `json:"first_try_share"`
	Retried       int     `json:"retried"`
	Refused       int     `json:"refused"`
	// Fuel is the tokens and dollars per landed story, every attempt summed.
	Fuel FuelUse `json:"fuel"`
	// EscapedBugs are the bugs traced to its landings (escapedBugs) so far, each
	// against the newest landing it names, as ESCAPED BUGS counts them.
	EscapedBugs       int     `json:"escaped_bugs"`
	EscapedPerLanding float64 `json:"escaped_per_landing"`
}

// ModelCompare is MODELS: each model shown, and each kind of each model that
// had a session.
type ModelCompare struct {
	Models []ModelUse `json:"models"`
	Kinds  []ModelUse `json:"kinds"`
	// Testers, Repeats and Others say how the landed over Models differs from
	// QUALITY's: Testers are Tester sessions (a "tested" row counts as a landing
	// here, QUALITY leaves it out), Repeats the landing rows of a story that
	// MODELS counts once but QUALITY counts each, Others the landings of a model
	// that has no column.
	Testers int `json:"testers,omitempty"`
	Repeats int `json:"repeats,omitempty"`
	Others  int `json:"others,omitempty"`
}

// modelCompare sums the window's landings and refusals by the model that ran
// them, narrowed like the blocks; escapedOf is the bugs traced to each landed
// story. A Tester's session is one of them: its "tested" row a landing, its
// "not landed" row a refusal, under the kind rig/test.
func (r FactoryReport) modelCompare(entries []reportEntry, escapedOf map[string]int) *ModelCompare {
	byModel := map[string][]reportEntry{}
	byKind := map[string]map[string][]reportEntry{}
	landed := map[string]bool{}
	c := &ModelCompare{Models: []ModelUse{}, Kinds: []ModelUse{}}
	shown := map[string]bool{}
	for _, model := range ModelColumns {
		shown[string(model)] = true
	}
	for _, e := range entries {
		if e.previous || e.bug || e.escaped || (!e.landed && !e.refused) || (r.Rig != "" && e.rig != r.Rig) || (r.Host != "" && e.host != r.Host) {
			continue
		}
		if e.landed {
			// A story counts once, on its first landing row, the one with its fuel.
			if landed[e.id] {
				c.Repeats++
				continue
			}
			landed[e.id] = true
			switch {
			case e.tester:
				c.Testers++
			case !shown[e.ran]:
				c.Others++
			}
		}
		byModel[e.ran] = append(byModel[e.ran], e)
		kind := e.rig + "/feature"
		switch {
		case e.tester:
			kind = e.rig + "/test"
		case e.bugStory:
			kind = e.rig + "/bug"
		}
		if byKind[kind] == nil {
			byKind[kind] = map[string][]reportEntry{}
		}
		byKind[kind][e.ran] = append(byKind[kind][e.ran], e)
	}
	for i, model := range ModelColumns {
		if mine := byModel[string(model)]; len(mine) > 0 || i < ModelsAlwaysShown {
			c.Models = append(c.Models, sumModelUse(string(model), "", mine, escapedOf))
		}
	}
	kinds := make([]string, 0, len(byKind))
	for kind := range byKind {
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)
	for _, kind := range kinds {
		for _, m := range c.Models {
			if mine := byKind[kind][m.Model]; len(mine) > 0 {
				c.Kinds = append(c.Kinds, sumModelUse(m.Model, kind, mine, escapedOf))
			}
		}
	}
	return c
}

// sumModelUse sums one model's entries, of one kind or of all.
func sumModelUse(model, kind string, entries []reportEntry, escapedOf map[string]int) ModelUse {
	u := ModelUse{Model: model, Kind: kind, Sessions: len(entries)}
	var landings []reportEntry
	for _, e := range entries {
		if !e.landed {
			u.Refused++
			continue
		}
		landings = append(landings, e)
		u.Landed++
		if e.attempts > 1 {
			u.Retried++
		} else {
			u.FirstTry++
		}
		u.EscapedBugs += escapedOf[e.id]
	}
	u.FirstTryShare = percentOf(u.FirstTry, u.Landed)
	u.Fuel = sumFuel(landings)
	u.EscapedPerLanding = twoPlaces(fraction(float64(u.EscapedBugs), float64(u.Landed)))
	return u
}

// escapedOfLanding is the bugs traced to each landed story, each bug against
// the newest landing it names, the one ESCAPED BUGS counts it under.
func escapedOfLanding(escapes []escape) map[string]int {
	of := map[string]int{}
	for _, x := range escapes {
		of[x.landings[len(x.landings)-1].id]++
	}
	return of
}

// gapLine says on one line how MODELS' landed total differs from QUALITY's, ""
// when it does not: the signed counts behind it, the long words when they fit.
func (c *ModelCompare) gapLine() string {
	if c == nil || c.Testers+c.Repeats+c.Others == 0 {
		return ""
	}
	build := func(tester, repeat, other string) string {
		var parts []string
		for _, p := range []struct {
			sign string
			n    int
			name string
		}{{"+", c.Testers, tester}, {"-", c.Repeats, repeat}, {"-", c.Others, other}} {
			if p.n > 0 {
				parts = append(parts, fmt.Sprintf("%s%d %s", p.sign, p.n, p.name))
			}
		}
		return "landed vs QUALITY's: " + strings.Join(parts, ", ")
	}
	if line := build("Tester sessions", "repeat landings", "on other models"); utf8.RuneCountInString(line) <= ReportWidth {
		return line
	}
	return build("Tester", "repeat", "other")
}

// hadSessions says some model shown landed or was refused a story.
func (c *ModelCompare) hadSessions() bool {
	if c == nil {
		return false
	}
	for _, m := range c.Models {
		if m.Sessions > 0 {
			return true
		}
	}
	return false
}

// modelsLabel and modelsCell are the widths of a MODELS line's label and of
// each model's cell: four models fit in ReportWidth.
const (
	modelsLabel = 22
	modelsCell  = 9
)

// modelMeasures are the lines of MODELS, a cell each model: "-" where the
// model has nothing to measure the number by.
var modelMeasures = []struct {
	name string
	cell func(ModelUse) string
}{
	{"landed", func(u ModelUse) string { return orDash(u.Sessions > 0, fmt.Sprint(u.Landed)) }},
	{"first try", func(u ModelUse) string { return orDash(u.Landed > 0, fmt.Sprintf("%.0f%%", u.FirstTryShare)) }},
	{"refused", func(u ModelUse) string { return orDash(u.Sessions > 0, fmt.Sprint(u.Refused)) }},
	{"retried", func(u ModelUse) string { return orDash(u.Sessions > 0, fmt.Sprint(u.Retried)) }},
	{"tokens/landing", func(u ModelUse) string {
		return orDash(u.Fuel.WithTokens > 0, shortCount(u.Fuel.TokensPerLanding))
	}},
	{"$/landing", func(u ModelUse) string { return orDash(u.Fuel.WithCost > 0, fmt.Sprintf("%.2f", u.Fuel.USDPerLanding)) }},
	{"escaped bugs/landing", func(u ModelUse) string {
		return orDash(u.Landed > 0, fmt.Sprintf("%.2f", u.EscapedPerLanding))
	}},
}

// writeModels prints MODELS: a line per measure with a column per model, then
// by kind what each model landed and its first try share. A window in which no
// model had a session prints nothing.
func writeModels(b *strings.Builder, c *ModelCompare) {
	if !c.hadSessions() {
		return
	}
	line := func(label string, cells []string) {
		row := fmt.Sprintf("%-*s", modelsLabel, fitLine(label, modelsLabel-1))
		for _, cell := range cells {
			row += fmt.Sprintf("%*s", modelsCell, cell)
		}
		b.WriteString(strings.TrimRight(row, " ") + "\n")
	}
	var names []string
	for _, m := range c.Models {
		names = append(names, m.Model)
	}
	b.WriteString("\n")
	line("MODELS", names)
	for _, measure := range modelMeasures {
		var cells []string
		for _, m := range c.Models {
			cells = append(cells, measure.cell(m))
		}
		line(measure.name, cells)
	}
	if gap := c.gapLine(); gap != "" {
		b.WriteString(gap + "\n")
	}
	if len(c.Kinds) == 0 {
		return
	}
	b.WriteString("\nby kind: landed, first try\n")
	kinds := map[string]map[string]ModelUse{}
	var order []string
	for _, k := range c.Kinds {
		if kinds[k.Kind] == nil {
			kinds[k.Kind] = map[string]ModelUse{}
			order = append(order, k.Kind)
		}
		kinds[k.Kind][k.Model] = k
	}
	for _, kind := range order {
		var cells []string
		for _, m := range c.Models {
			k, ok := kinds[kind][m.Model]
			switch {
			case !ok:
				cells = append(cells, "-")
			case k.Landed == 0:
				cells = append(cells, "0 -")
			default:
				cells = append(cells, fmt.Sprintf("%d %.0f%%", k.Landed, k.FirstTryShare))
			}
		}
		line(kind, cells)
	}
}

// summaryModels prints MODELS for the phone summary: a heading, then one line
// a model, its cells those of writeModels but tokens.
func summaryModels(b *strings.Builder, c *ModelCompare) {
	if c == nil || len(c.Models) == 0 {
		return
	}
	widths := []int{8, 7, 8, 8, 8, 7, 9}
	row := func(cells []string) {
		var line strings.Builder
		fmt.Fprintf(&line, "%-*s", widths[0], cells[0])
		for i, cell := range cells[1:] {
			fmt.Fprintf(&line, "%*s", widths[i+1], cell)
		}
		summaryLine(b, "", "", strings.TrimRight(line.String(), " "))
	}
	row([]string{"MODELS", "landed", "1st try", "refused", "retried", "$/land", "esc/land"})
	for _, m := range c.Models {
		cells := []string{"  " + m.Model}
		for _, measure := range modelMeasures {
			if measure.name != "tokens/landing" {
				cells = append(cells, measure.cell(m))
			}
		}
		row(cells)
	}
}

// orDash is text, or "-" when there is nothing to measure it by.
func orDash(measured bool, text string) string {
	if !measured {
		return "-"
	}
	return text
}

// shortCount is a count in at most six columns: 950, 1.5k, 820k, 4.1M, 32M.
func shortCount(n float64) string {
	switch {
	case n < 1000:
		return fmt.Sprintf("%.0f", n)
	case n < 10_000:
		return fmt.Sprintf("%.1fk", n/1000)
	case n < 999_500:
		return fmt.Sprintf("%.0fk", n/1000)
	case n < 9_950_000:
		return fmt.Sprintf("%.1fM", n/1_000_000)
	}
	return fmt.Sprintf("%.0fM", n/1_000_000)
}
