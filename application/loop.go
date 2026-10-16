package application

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
)

// ImprovementsFile is the sleep pass's ledger of improvement ideas in the vault.
const ImprovementsFile = "plans/improvements/ledger.jsonl"

// Words mw loop brief says where a source is not there yet.
const (
	LoopNoModels = "models: not yet (mw-2zh33g.16)"
	LoopNoIdeas  = "ideas: no ledger yet (mw-1ljpo9.11)"
)

// ImprovementIdea is one line of the improvements ledger as the brief reads it:
// what the idea is, the Expect line naming the number it should move, and the
// bead filed for it, empty while none is.
type ImprovementIdea struct {
	Title  string `json:"title"`
	Expect string `json:"expect,omitempty"`
	Bead   string `json:"bead,omitempty"`
}

// ImprovementBook is the improvements ledger: Improvements reads every idea in
// the order written, and says whether the ledger exists; none and no error when
// it does not. The real adapter is infrastructure/vault.
type ImprovementBook interface {
	Improvements(ctx context.Context) (ideas []ImprovementIdea, exists bool, err error)
}

// LoopInputs is everything the brief is read from: the one report result and
// the sources beside it. Nothing here is computed again.
type LoopInputs struct {
	Result      FactoryReportResult
	Classes     []ErrorClass
	Investments []Investment
	Ideas       []ImprovementIdea
	// HaveIdeas says the improvements ledger exists.
	HaveIdeas bool
	Now       time.Time
}

// LoopQualitySpeedFuel is the brief's first part: the summary lines of mw
// report for the whole factory, and MODELS when the result carries one.
type LoopQualitySpeedFuel struct {
	Window         ReportWindow  `json:"window"`
	PreviousWindow ReportWindow  `json:"previous_window"`
	Lines          []string      `json:"lines"`
	Models         *ModelCompare `json:"models"`
	modelsText     string
}

// LoopCauses is one rig's refusals by cause class.
type LoopCauses struct {
	Rig      string         `json:"rig"`
	Refused  int            `json:"refused"`
	Causes   map[string]int `json:"causes"`
	Uncaused []string       `json:"uncaused,omitempty"`
	line     string
}

// LoopError is an error class with an occurrence dated in the window.
type LoopError struct {
	Slug string   `json:"slug"`
	Rigs []string `json:"rigs"`
	// Shared is the class's shared-component verdict, empty while unweighed.
	Shared string `json:"shared"`
	Asks   bool   `json:"asks_shared_question"`
}

// LoopVerdicts are the factory changes whose 14th day falls in the window and
// the investments due for review.
type LoopVerdicts struct {
	Changes []FactoryChange `json:"changes"`
	Due     []InvestmentDue `json:"invest_due"`
	dueText string
}

// InvestmentDue is an investment past its review date with no outcome.
type InvestmentDue struct {
	Bead     string `json:"bead"`
	ReviewAt string `json:"review_at"`
	Aim      string `json:"aim"`
	Measure  string `json:"measure"`
}

// LoopIdeas are the improvements ledger's ideas with no bead filed for them.
type LoopIdeas struct {
	Ledger  bool              `json:"ledger"`
	Unfiled []ImprovementIdea `json:"unfiled"`
}

// LoopShare is the investment share as mw report tells it, and the bound the
// week calls for when the result carries one.
type LoopShare struct {
	Share *InvestShare `json:"share"`
	Lines []string     `json:"lines"`
	Bound string       `json:"bound,omitempty"`
}

// LoopBrief is the one read for the weekly loop sitting. Its six parts are the
// keys of its JSON.
type LoopBrief struct {
	QualitySpeedFuel LoopQualitySpeedFuel `json:"quality_speed_fuel"`
	Causes           []LoopCauses         `json:"causes"`
	Errors           []LoopError          `json:"errors"`
	Verdicts         LoopVerdicts         `json:"verdicts"`
	Ideas            LoopIdeas            `json:"ideas"`
	Share            LoopShare            `json:"share"`
}

// ReadLoopBrief lays the sources out in the brief's six parts. It reads the
// numbers off the result and the sources as they are; it computes none.
func ReadLoopBrief(in LoopInputs) LoopBrief {
	r := in.Result
	brief := LoopBrief{}

	var lines strings.Builder
	r.summaryQualitySpeedFuel(&lines)
	brief.QualitySpeedFuel = LoopQualitySpeedFuel{Window: r.Window, PreviousWindow: r.PreviousWindow,
		Lines: strings.Split(strings.TrimRight(lines.String(), "\n"), "\n")}
	if r.ModelCompare.hadSessions() {
		var models strings.Builder
		writeModels(&models, r.ModelCompare)
		brief.QualitySpeedFuel.Models = r.ModelCompare
		brief.QualitySpeedFuel.modelsText = strings.Trim(models.String(), "\n")
	}

	brief.Causes = []LoopCauses{}
	for _, rig := range r.Rigs {
		if rig.Quality.Refused == 0 {
			continue
		}
		brief.Causes = append(brief.Causes, LoopCauses{Rig: rig.Name, Refused: rig.Quality.Refused, Causes: rig.Quality.Causes,
			Uncaused: rig.Quality.Uncaused,
			line:     fmt.Sprintf("%s: refused %d\n    %s", fitLine(rig.Name, ReportWidth-20), rig.Quality.Refused, rig.Quality.causesText())})
	}

	brief.Errors = []LoopError{}
	for _, class := range in.Classes {
		rigs := map[string]bool{}
		for _, seen := range class.Occurrences() {
			if r.inWindowDay(seen.Date) {
				rigs[seen.Rig] = true
			}
		}
		if len(rigs) == 0 {
			continue
		}
		e := LoopError{Slug: class.Slug, Shared: class.Shared, Asks: class.AsksSharedQuestion()}
		for rig := range rigs {
			e.Rigs = append(e.Rigs, rig)
		}
		sort.Strings(e.Rigs)
		brief.Errors = append(brief.Errors, e)
	}

	brief.Verdicts = LoopVerdicts{Changes: []FactoryChange{}, Due: []InvestmentDue{}}
	for _, c := range r.Changes {
		switch c.Verdict {
		case ChangeKept, ChangeWorse, ChangeNone, ChangeNoData:
			if r.inWindowDay(c.AfterEnds) {
				brief.Verdicts.Changes = append(brief.Verdicts.Changes, c)
			}
		}
	}
	due := DueInvestments(in.Investments, in.Now)
	for _, inv := range due {
		brief.Verdicts.Due = append(brief.Verdicts.Due, InvestmentDue{Bead: inv.Made.Bead, ReviewAt: inv.ReviewAt().Format(LedgerDate),
			Aim: inv.Made.Aim, Measure: inv.Made.Measure})
	}
	var dueText strings.Builder
	writeDue(&dueText, due)
	brief.Verdicts.dueText = strings.TrimRight(dueText.String(), "\n")

	brief.Ideas = LoopIdeas{Ledger: in.HaveIdeas, Unfiled: []ImprovementIdea{}}
	for _, idea := range in.Ideas {
		if strings.TrimSpace(idea.Bead) == "" {
			brief.Ideas.Unfiled = append(brief.Ideas.Unfiled, idea)
		}
	}

	brief.Share = LoopShare{Share: r.InvestShare, Lines: r.InvestShare.lines()}
	if brief.Share.Lines == nil {
		brief.Share.Lines = []string{}
	}
	return brief
}

// inWindowDay says a YYYY-MM-DD day is one the window takes in, the last day
// whole.
func (r FactoryReportResult) inWindowDay(day string) bool {
	at, err := time.Parse(LedgerDate, day)
	if err != nil {
		return false
	}
	return !at.Before(r.Window.Since) && !at.After(reportDay(r.Window.Until))
}

// String is the brief as mw loop brief prints it: six headings in order.
func (b LoopBrief) String() string {
	var out strings.Builder
	days := int(reportDay(b.QualitySpeedFuel.Window.Until).Sub(b.QualitySpeedFuel.Window.Since).Hours() / 24)
	fmt.Fprintf(&out, "QUALITY, SPEED, FUEL (%s to %s, %d days, against the days before)\n",
		b.QualitySpeedFuel.Window.Since.UTC().Format(LedgerDate), b.QualitySpeedFuel.Window.Until.UTC().Format(LedgerDate), days)
	for _, line := range b.QualitySpeedFuel.Lines {
		out.WriteString(line + "\n")
	}
	if b.QualitySpeedFuel.Models == nil {
		out.WriteString(LoopNoModels + "\n")
	} else {
		out.WriteString("\n" + b.QualitySpeedFuel.modelsText + "\n")
	}

	out.WriteString("\nCAUSES (refusals by cause class, per rig)\n")
	if len(b.Causes) == 0 {
		out.WriteString("  no refusals in the window\n")
	}
	for _, c := range b.Causes {
		fmt.Fprintf(&out, "  %s\n", c.line)
	}

	out.WriteString("\nERRORS (classes seen in the window)\n")
	if len(b.Errors) == 0 {
		out.WriteString("  none seen in the window\n")
	}
	for _, e := range b.Errors {
		shared := "shared: " + e.Shared
		if e.Shared == "" {
			shared = "shared: not weighed"
			if e.Asks {
				shared += " (" + SharedComponentAsk + ")"
			}
		}
		fmt.Fprintf(&out, "  %s: seen in %s, %s\n", e.Slug, strings.Join(e.Rigs, ", "), shared)
	}

	out.WriteString("\nVERDICTS (factory changes at 14 days, investments due)\n")
	if len(b.Verdicts.Changes) == 0 {
		out.WriteString("  no factory change reached 14 days in the window\n")
	}
	for _, c := range b.Verdicts.Changes {
		fmt.Fprintf(&out, "  %s\n", fitLine(fmt.Sprintf("%s %s (%s)", c.Verdict, c.Title, c.Story), ReportWidth-2))
	}
	out.WriteString(b.Verdicts.dueText + "\n")

	out.WriteString("\nIDEAS (the sleep pass's, no bead filed)\n")
	switch {
	case !b.Ideas.Ledger:
		out.WriteString("  " + LoopNoIdeas + "\n")
	case len(b.Ideas.Unfiled) == 0:
		out.WriteString("  none unfiled\n")
	}
	for _, idea := range b.Ideas.Unfiled {
		line := idea.Title
		if strings.TrimSpace(idea.Expect) != "" {
			line += " — " + idea.Expect
		}
		fmt.Fprintf(&out, "  %s\n", strings.Join(strings.Fields(line), " "))
	}

	out.WriteString("\nSHARE\n")
	if len(b.Share.Lines) == 0 {
		out.WriteString("  no share read\n")
	}
	for _, line := range b.Share.Lines {
		out.WriteString(line + "\n")
	}
	if b.Share.Bound != "" {
		out.WriteString("  " + b.Share.Bound + "\n")
	}
	return out.String()
}

// LoopBriefRun is mw loop brief: it builds ONE report result the way mw report
// does, reads the register, the investments and the improvements beside it, and
// prints the brief. It spends no fuel and writes nothing.
type LoopBriefRun struct {
	// Report is the report to run; its Out, JSON, Notes, Mailbox and Snapshots
	// are cleared, so that it only reads.
	Report       FactoryReport
	Errors       ErrorFiles
	Seat         string
	Investments  InvestmentBook
	Improvements ImprovementBook
	JSON         bool
	Out          io.Writer
}

// Run reads and prints the brief.
func (l LoopBriefRun) Run(ctx context.Context) (LoopBrief, error) {
	report := l.Report
	report.Out, report.JSON = nil, false
	report.Notes, report.Mailbox, report.Snapshots = nil, nil, nil
	result, err := report.Run(ctx)
	if err != nil {
		return LoopBrief{}, err
	}
	now := time.Now()
	if report.Now != nil {
		now = report.Now()
	}
	in := LoopInputs{Result: result, Now: now}
	if l.Errors != nil {
		files, _, err := l.Errors.ReadErrorRegister(ctx, l.Seat)
		if err != nil {
			return LoopBrief{}, fmt.Errorf("reading the error register: %w", err)
		}
		in.Classes, _ = LoadErrorClasses(files)
	}
	if l.Investments != nil {
		records, err := l.Investments.Investments(ctx)
		if err != nil {
			return LoopBrief{}, fmt.Errorf("reading the investments: %w", err)
		}
		in.Investments = GroupInvestments(records)
	}
	if l.Improvements != nil {
		in.Ideas, in.HaveIdeas, err = l.Improvements.Improvements(ctx)
		if err != nil {
			return LoopBrief{}, fmt.Errorf("reading the improvements ledger: %w", err)
		}
	}
	brief := ReadLoopBrief(in)
	if l.Out != nil {
		if l.JSON {
			enc := json.NewEncoder(l.Out)
			enc.SetIndent("", "  ")
			return brief, enc.Encode(brief)
		}
		fmt.Fprint(l.Out, brief.String())
	}
	return brief, nil
}
