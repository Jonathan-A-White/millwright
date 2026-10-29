package steps

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"

	"github.com/cucumber/godog"
)

// tocContext holds the fake markdown files and what the last run of mw toc
// printed and said.
type tocContext struct {
	files   *apptest.FakeTocFiles
	printed bytes.Buffer
	failed  bool
	err     error

	// before is the text of each file just before the last fix.
	before map[string]string
	// order is the files in the order the scenario gave them.
	order []string
}

// InitializeTocScenario registers the steps of features/toc.feature.
func InitializeTocScenario(ctx *godog.ScenarioContext) {
	c := &tocContext{}

	ctx.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
		*c = tocContext{files: apptest.NewFakeTocFiles()}
		return ctx, nil
	})

	ctx.Given(`^the toc doc "([^"]*)" has (\d+) "##" headings and (\d+) "###" headings and no markers$`, c.hasNoMarkers)
	ctx.Given(`^the toc doc "([^"]*)" has (\d+) "##" headings and (\d+) "###" headings and the line "([^"]*)"$`, c.hasLine)
	ctx.Given(`^the toc doc "([^"]*)" has markers around an old list and (\d+) "##" headings$`, c.hasOldList)
	ctx.Given(`^the toc doc "([^"]*)" has markers around a current list and (\d+) "##" headings$`, c.hasCurrentList)
	ctx.Given(`^the toc doc "([^"]*)" has the "##" headings "([^"]*)", "([^"]*)", "([^"]*)" and "([^"]*)" and no markers$`, c.hasTitled)
	ctx.When(`^mw toc fixes the docs$`, c.fixes)
	ctx.When(`^mw toc checks the docs$`, func() error { return c.check(0) })
	ctx.When(`^mw toc checks the docs with --min (\d+)$`, c.check)
	ctx.Then(`^the toc doc "([^"]*)" has its table before the first "##" heading$`, c.tableBeforeFirst)
	ctx.Then(`^the toc doc "([^"]*)" is byte-identical to what it was before that fix$`, c.unchanged)
	ctx.Then(`^the toc doc "([^"]*)" has the table line "([^"]*)"$`, c.hasTableLine)
	ctx.Then(`^toc printed "([^"]*)"$`, c.printedSays)
	ctx.Then(`^toc printed nothing$`, c.printedNothing)
	ctx.Then(`^toc passes, printing nothing$`, c.passesSilently)
	ctx.Then(`^toc fails$`, c.fails)
	ctx.Then(`^toc printed exactly these lines:$`, c.printedLines)
}

// tocDocument builds a document of the given numbers of sections, each "###" under the
// first "##"; the lines, if any, go before the first "##".
func tocDocument(twos, threes int, lines ...string) string {
	var b strings.Builder
	b.WriteString("# Title\n\nIntro.\n\n")
	for _, line := range lines {
		b.WriteString(line + "\n\n")
	}
	for i := 1; i <= twos; i++ {
		fmt.Fprintf(&b, "## Section %d\n\ntext\n\n", i)
		if i == 1 {
			for j := 1; j <= threes; j++ {
				fmt.Fprintf(&b, "### Part %d\n\ntext\n\n", j)
			}
		}
	}
	return b.String()
}

func (c *tocContext) add(name, text string) {
	c.files.Docs[name] = text
	c.order = append(c.order, name)
}

func (c *tocContext) hasNoMarkers(name string, twos, threes int) error {
	c.add(name, tocDocument(twos, threes))
	return nil
}

func (c *tocContext) hasTitled(name, a, b, d, e string) error {
	var doc strings.Builder
	doc.WriteString("# Title\n\nIntro.\n\n")
	for _, title := range []string{a, b, d, e} {
		doc.WriteString("## " + title + "\n\ntext\n\n")
	}
	c.add(name, doc.String())
	return nil
}

func (c *tocContext) hasTableLine(name, line string) error {
	for _, got := range strings.Split(c.files.Docs[name], "\n") {
		if got == line {
			return nil
		}
	}
	return fmt.Errorf("%s has no line %q:\n%s", name, line, c.files.Docs[name])
}

func (c *tocContext) hasLine(name string, twos, threes int, line string) error {
	c.add(name, tocDocument(twos, threes)+line+"\n")
	return nil
}

func (c *tocContext) hasOldList(name string, twos int) error {
	c.add(name, tocDocument(twos, 0, domain.TocOpen+"\n- [Old](#old)\n"+domain.TocClose))
	return nil
}

func (c *tocContext) hasCurrentList(name string, twos int) error {
	bare := tocDocument(twos, 0, domain.TocOpen+"\n"+domain.TocClose)
	c.add(name, domain.ApplyToc(bare, 0))
	return nil
}

func (c *tocContext) run(fix bool, min int) {
	c.printed.Reset()
	toc := application.Toc{Files: c.files, Min: min, Out: &c.printed}
	var report application.TocReport
	if fix {
		report, c.err = toc.Fix(context.Background(), c.order...)
	} else {
		report, c.err = toc.Check(context.Background(), c.order...)
	}
	c.failed = report.Failed()
}

func (c *tocContext) fixes() error {
	c.before = map[string]string{}
	for name, text := range c.files.Docs {
		c.before[name] = text
	}
	c.run(true, 0)
	return c.err
}

func (c *tocContext) check(min int) error {
	c.run(false, min)
	return c.err
}

func (c *tocContext) tableBeforeFirst(name string) error {
	text := c.files.Docs[name]
	open := strings.Index(text, domain.TocOpen)
	first := strings.Index(text, "\n## ")
	if open < 0 || first < 0 || open > first || !strings.Contains(text[open:first], domain.TocClose) {
		return fmt.Errorf("%s has no table before its first ## heading:\n%s", name, text)
	}
	if !strings.Contains(text[open:first], "  - [Part 1](#part-1)") {
		return fmt.Errorf("%s table does not nest the ### headings:\n%s", name, text[open:first])
	}
	return nil
}

func (c *tocContext) unchanged(name string) error {
	if c.files.Docs[name] != c.before[name] {
		return fmt.Errorf("%s changed in the second fix:\n%s", name, c.files.Docs[name])
	}
	return nil
}

func (c *tocContext) printedSays(want string) error {
	if !strings.Contains(c.printed.String(), want) {
		return fmt.Errorf("expected toc to print %q, it printed %q", want, c.printed.String())
	}
	return nil
}

func (c *tocContext) printedNothing() error {
	if c.printed.Len() != 0 {
		return fmt.Errorf("expected toc to print nothing, it printed %q", c.printed.String())
	}
	return nil
}

func (c *tocContext) passesSilently() error {
	if c.failed {
		return fmt.Errorf("expected toc to pass, it failed:\n%s", c.printed.String())
	}
	return c.printedNothing()
}

func (c *tocContext) fails() error {
	if !c.failed {
		return fmt.Errorf("expected toc to fail (exit 1), it passed; it printed %q", c.printed.String())
	}
	return nil
}

func (c *tocContext) printedLines(want *godog.DocString) error {
	if got := strings.TrimRight(c.printed.String(), "\n"); got != strings.TrimRight(want.Content, "\n") {
		return fmt.Errorf("expected toc to print:\n%s\ngot:\n%s", want.Content, got)
	}
	return nil
}
