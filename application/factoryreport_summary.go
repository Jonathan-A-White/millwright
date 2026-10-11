package application

import (
	"fmt"
	"strings"
)

// SummaryLines is the most lines the phone summary of mw report send has.
const SummaryLines = 14

// Summary is the report as a phone reads it: the window, then QUALITY, SPEED,
// FUEL and CHEAP WINS for the whole factory, each number with its arrow against
// the window before, in at most SummaryLines lines of ReportWidth columns.
func (r FactoryReportResult) Summary() string {
	days := int(reportDay(r.Window.Until).Sub(r.Window.Since).Hours() / 24)
	var b strings.Builder
	fmt.Fprintf(&b, "Factory, %d days to %s\n", days, r.Window.Until.UTC().Format(LedgerDate))
	whole := r.Whole
	if whole == nil {
		whole = &ReportBlock{}
	}
	q, s, f := whole.Quality, whole.Speed, whole.Fuel

	summaryLine(&b, "", "QUALITY ", fmt.Sprintf("landed %d%s, refused %d (%.0f%%%s)",
		q.Landed, whole.arrow("landed"), q.Refused, q.RefusalRate, whole.arrow("refusal_rate")))
	if q.Landed > 0 {
		summaryLine(&b, "  ", "", fmt.Sprintf("attempts %.2f/landing%s, bugs %.2f/landing%s",
			q.AttemptsPerLanding, whole.arrow("attempts_per_landing"), q.BugsPerLanding, whole.arrow("bugs_per_landing")))
	}

	if s.WithPar == 0 {
		summaryLine(&b, "", "SPEED ", "no par yet")
	} else {
		summaryLine(&b, "", "SPEED ", fmt.Sprintf("par x%.1f median%s, x%.1f p90%s",
			s.ParMedian, whole.arrow("par_median"), s.ParP90, whole.arrow("par_p90")))
	}
	if s.Measured > 0 {
		summaryLine(&b, "  ", "", fmt.Sprintf("dispatch to landing %s%s",
			gateClock(seconds(s.DispatchToLandingS)), whole.arrow("dispatch_to_landing")))
	}

	if f.WithTokens == 0 && f.WithCost == 0 {
		summaryLine(&b, "", "FUEL ", "no fuel figures yet")
	} else {
		label := "FUEL "
		if f.WithTokens > 0 {
			summaryLine(&b, "", label, fmt.Sprintf("tokens/landing %s%s", Thousands(int(f.TokensPerLanding)), whole.arrow("tokens_per_landing")))
			label = "  "
		}
		if f.WithCost > 0 {
			summaryLine(&b, "", label, fmt.Sprintf("$/landing %.2f%s", f.USDPerLanding, whole.arrow("usd_per_landing")))
		}
	}

	var wins strings.Builder
	writeCheapWins(&wins, r.CheapWins)
	b.WriteString(strings.TrimPrefix(wins.String(), "\n"))
	return b.String()
}

// summaryLine writes one line of the summary, cut to ReportWidth columns.
func summaryLine(b *strings.Builder, indent, label, text string) {
	b.WriteString(fitLine(indent+label+text, ReportWidth) + "\n")
}
