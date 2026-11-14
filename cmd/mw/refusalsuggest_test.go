package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// A refusal that suggests a command line names flags and subcommands that exist
// (mw-gq6.1247). Three refusals in a row (mw-gq6.1161, .1167, .1197) named a flag
// that did not exist or could not express the refused case, and each time a
// person found it by running the suggestion. TestEverySuggestedCommandLineParsesAgainstTheCommandTree
// reads every string literal in application/ and cmd/mw/, takes each `mw ...`
// command line a message puts between backticks, and parses it against the
// cobra tree: a subcommand or flag the tree lacks fails the build.

// suggestedLine is one `mw ...` command line found in a string literal.
type suggestedLine struct {
	where string // file:line of the literal
	line  string // the command line, without the backticks
}

var backtickedMw = regexp.MustCompile("`(mw [^`]*)`")

// suggestedLines finds the `mw ...` command lines in the string literals of
// every non-test Go file in dirs, folding "a" + "b" literals into one string
// first: a refusal is often built that way.
func suggestedLines(t *testing.T, dirs ...string) []suggestedLine {
	t.Helper()
	var found []suggestedLine
	fset := token.NewFileSet()
	for _, dir := range dirs {
		files, err := filepath.Glob(filepath.Join(dir, "*.go"))
		if err != nil {
			t.Fatal(err)
		}
		for _, file := range files {
			if strings.HasSuffix(file, "_test.go") {
				continue
			}
			src, err := parser.ParseFile(fset, file, nil, 0)
			if err != nil {
				t.Fatalf("parsing %s: %v", file, err)
			}
			ast.Inspect(src, func(n ast.Node) bool {
				text, ok := literalText(n)
				if !ok {
					return true
				}
				where := filepath.Base(file) + ":" + strconv.Itoa(fset.Position(n.Pos()).Line)
				for _, m := range backtickedMw.FindAllStringSubmatch(text, -1) {
					found = append(found, suggestedLine{where: where, line: m[1]})
				}
				for _, line := range bareLines(backtickedMw.ReplaceAllString(text, "")) {
					found = append(found, suggestedLine{where: where, line: line})
				}
				return false
			})
		}
	}
	return found
}

var bareMwStart = regexp.MustCompile(`(?:^|[\s:(])mw `)

// bareLines are the command lines a message gives without backticks that name a
// flag: "mw", the words that follow while they could be subcommands (up to two),
// then the words that are flags or placeholders, up to the first punctuation. A
// line with no flag is left out, since "mw next" is as often a noun.
func bareLines(text string) []string {
	var lines []string
	for _, at := range bareMwStart.FindAllStringIndex(text, -1) {
		words := strings.Fields(text[at[1]:])
		line := []string{"mw"}
		sawFlag := false
		for i, word := range words {
			bare := strings.TrimRight(word, ".,;:)\"'")
			ends := bare != word
			flag := strings.HasPrefix(bare, "-")
			placeholder := strings.ContainsAny(bare, "%<>")
			subcommand := i < 2 && regexp.MustCompile(`^[a-z][a-z-]*$`).MatchString(bare)
			if !flag && !placeholder && !subcommand {
				break
			}
			sawFlag = sawFlag || flag
			line = append(line, bare)
			if ends {
				break
			}
		}
		if sawFlag {
			lines = append(lines, strings.Join(line, " "))
		}
	}
	return lines
}

// literalText is the text of a string literal, or of a chain of them joined by +.
func literalText(n ast.Node) (string, bool) {
	switch n := n.(type) {
	case *ast.BasicLit:
		if n.Kind != token.STRING {
			return "", false
		}
		text, err := strconv.Unquote(n.Value)
		return text, err == nil
	case *ast.BinaryExpr:
		if n.Op != token.ADD {
			return "", false
		}
		left, ok := literalText(n.X)
		if !ok {
			return "", false
		}
		right, ok := literalText(n.Y)
		if !ok {
			return "", false
		}
		return left + right, true
	}
	return "", false
}

// lineProblem parses one suggested command line against the tree and says what
// is wrong with it, or "" when every subcommand and flag in it exists.
func lineProblem(root *cobra.Command, line string) string {
	words := strings.Fields(line)
	if len(words) < 2 || words[0] != "mw" {
		return ""
	}
	cmd := root
	for _, word := range words[1:] {
		if stopsALine(word) {
			break
		}
		if strings.HasPrefix(word, "-") {
			if cmd.DisableFlagParsing {
				break
			}
			if problem := flagProblem(cmd, word); problem != "" {
				return problem
			}
			continue
		}
		if i := strings.Index(word, "%"); i > 0 && !strings.HasPrefix(word, "-") {
			word = word[:i] // "send%s": the word, then a verb that fills in a flag
		}
		if word == "..." || strings.ContainsAny(word, "%<>[]{}$|") {
			continue // a placeholder for the story, the part number, or an alternative
		}
		if child := childNamed(cmd, word); child != nil {
			cmd = child
			continue
		}
		if cmd.HasSubCommands() && !cmd.Runnable() {
			return "`" + cmd.CommandPath() + "` has no subcommand " + strconv.Quote(word)
		}
		// otherwise it is an argument of cmd, or the value of a flag
	}
	return ""
}

// stopsALine is a word that ends the command: a shell operator.
func stopsALine(word string) bool {
	switch word {
	case "|", "||", "&&", ";", ">", ">>", "<", "#":
		return true
	}
	return false
}

func childNamed(cmd *cobra.Command, name string) *cobra.Command {
	for _, child := range cmd.Commands() {
		if child.Name() == name || child.HasAlias(name) {
			return child
		}
	}
	return nil
}

// flagProblem is "" when word (--name, --name=value or -x) is a flag of cmd, its
// parents' persistent flags, or help.
func flagProblem(cmd *cobra.Command, word string) string {
	if word == "--" {
		return ""
	}
	var found *pflag.Flag
	name := strings.TrimLeft(word, "-")
	if i := strings.Index(name, "="); i >= 0 {
		name = name[:i]
	}
	long := strings.HasPrefix(word, "--")
	lookup := func(fs *pflag.FlagSet) {
		if found != nil {
			return
		}
		if long {
			found = fs.Lookup(name)
		} else if len(name) == 1 {
			found = fs.ShorthandLookup(name)
		}
	}
	lookup(cmd.Flags())
	lookup(cmd.InheritedFlags())
	lookup(cmd.PersistentFlags())
	if found == nil && (name == "help" || name == "h") {
		return ""
	}
	if found == nil {
		return "`" + cmd.CommandPath() + "` has no flag " + word
	}
	return ""
}

func TestEverySuggestedCommandLineParsesAgainstTheCommandTree(t *testing.T) {
	root := newRootCmd()
	lines := suggestedLines(t, "../../application", ".")
	if len(lines) < 50 {
		t.Fatalf("found only %d suggested command lines in application/ and cmd/mw/: the reader of the string literals is not finding them", len(lines))
	}
	var bad []string
	for _, l := range lines {
		if problem := lineProblem(root, l.line); problem != "" {
			bad = append(bad, l.where+": `"+l.line+"`: "+problem)
		}
	}
	sort.Strings(bad)
	if len(bad) > 0 {
		t.Errorf("%d refusals suggest a command line the command tree does not have:\n%s", len(bad), strings.Join(bad, "\n"))
	}
}

// A made-up flag, or a made-up subcommand, in a suggested line is found: this is
// what makes the test above red (mw-gq6.1247).
func TestALineThatNamesAMadeUpFlagOrSubcommandIsFound(t *testing.T) {
	root := newRootCmd()
	for line, want := range map[string]string{
		"mw check mw-x.1 --part 2":               "",
		"mw check mw-x.1 --background":           "",
		"mw check mw-x.1 --part %d --no-such":    "has no flag --no-such",
		"mw check mw-x.1 --wait=5m --made-up=1":  "has no flag --made-up",
		"mw errors nonesuch":                     "has no subcommand \"nonesuch\"",
		"mw errors shared some-class candidate":  "",
		"mw memory add <rig> --kind gotcha --nx": "has no flag --nx",
	} {
		got := lineProblem(root, line)
		if want == "" && got != "" {
			t.Errorf("%q: found %q, want none", line, got)
		}
		if want != "" && !strings.Contains(got, want) {
			t.Errorf("%q: found %q, want it to say %q", line, got, want)
		}
	}
}

// The suggested lines are found from the code: a string literal built by + is
// read whole, and a test file is not read.
func TestSuggestedLinesAreReadFromTheCodeJoinedLiteralsIncluded(t *testing.T) {
	dir := t.TempDir()
	src := "package x\n\nvar a = \"run \" + \"`mw check %s \" + \"--part %d` again\"\n"
	if err := os.WriteFile(filepath.Join(dir, "x.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "x_test.go"), []byte("package x\n\nvar b = \"`mw nonesuch`\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := suggestedLines(t, dir)
	if len(got) != 1 || got[0].line != "mw check %s --part %d" {
		t.Fatalf("found %+v, want the one joined line", got)
	}
}

// mw errors shared says what it accepts: none and candidate take nothing after
// them, exists takes a place, and the help does not run the three together as if
// each took a <where> (mw-gq6.1247).
func TestErrorsSharedHelpSaysWhatEachVerdictTakes(t *testing.T) {
	var shared *cobra.Command
	for _, c := range newErrorsCmd().Commands() {
		if c.Name() == "shared" {
			shared = c
		}
	}
	if shared == nil {
		t.Fatal("mw errors has no shared subcommand")
	}
	if strings.Contains(shared.Use, "candidate|") || strings.Contains(shared.Use, "|candidate") {
		t.Errorf("the usage %q still runs the verdicts together", shared.Use)
	}
	for _, want := range []string{"shared <slug> none", "shared <slug> candidate", "(takes nothing after it)", "shared <slug> exists <where>"} {
		if !strings.Contains(shared.Long, want) {
			t.Errorf("the help does not say %q: %s", want, shared.Long)
		}
	}
	if strings.Contains(shared.Long, "candidate <where>") {
		t.Errorf("the help gives candidate a <where>: %s", shared.Long)
	}
}
