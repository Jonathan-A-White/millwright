package application

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// RigLegacyFiles is the port migrate moves a rig through: from the one memory
// file a seat keeps of it (and the archive it was pruned into) to an about text
// and a folder of facts. The vault adapter is the directory on disk.
type RigLegacyFiles interface {
	// ReadRigMemoryFiles reads the rig's one memory file and its archive, each
	// only when there is one, and says whether the rig already has a facts folder.
	ReadRigMemoryFiles(ctx context.Context, seat, rig string) (RigMemoryFiles, error)

	// WriteRigAbout writes the rig's about text, making the rig's folder if it is
	// not there, and reports where it landed.
	WriteRigAbout(ctx context.Context, seat, rig, text string) (string, error)

	// RemoveRigMemoryFiles removes the rig's one memory file and its archive, and
	// nothing else. A file that is not there is not an error.
	RemoveRigMemoryFiles(ctx context.Context, seat, rig string) error
}

// RigMemoryFiles is what a seat keeps of a rig as one memory file.
type RigMemoryFiles struct {
	Memory, Archive                 string
	HasMemory, HasArchive, HasFacts bool
}

// MemoryMigrate is the request mw memory migrate makes.
type MemoryMigrate struct {
	Rig    string
	DryRun bool
}

// AboutMaxBytes is how much of a memory file's head becomes the about text.
const AboutMaxBytes = 600

// SubjectMaxChars is the longest path or backticked token taken as a fact's subject.
const SubjectMaxChars = 40

// scannedLine is one '- ' line of a memory file, read.
type scannedLine struct {
	Sentence, Source, Subject, Since string
	Kind                             FactKind
	// Heading is the text of the heading the line sits under, and Dated the one
	// nearest above it that has a date in it, with that date.
	Heading, Dated, DatedOn string
}

// scannedMemory is a memory file, read: its head, its '- ' lines, and the
// lines it could not place.
type scannedMemory struct {
	Head      string
	Lines     []scannedLine
	NotPlaced []string
}

var (
	dateRe     = regexp.MustCompile(`\b\d{4}-\d{2}-\d{2}\b`)
	beadRe     = regexp.MustCompile(`\bmw-[a-z0-9]+(?:\.[0-9]+)*\b`)
	bracketRe  = regexp.MustCompile(`\s*[(\[][^()\[\]]*[)\]]`)
	urlRe      = regexp.MustCompile(`https?://\S+`)
	slashPath  = regexp.MustCompile(`[A-Za-z0-9_~.<>\-]+(?:/[A-Za-z0-9_<][A-Za-z0-9_.<>\-]*)+`)
	filePath   = regexp.MustCompile(`(?:\b|<)[A-Za-z0-9_<>\-]+\.(?:go|md|toml|json|sh|feature|ts|tsx|js|yml|yaml|txt)\b`)
	backticked = regexp.MustCompile("`([^`]+)`")
)

// firstDate is the first date in text that is a day on the calendar.
func firstDate(text string) string {
	for _, d := range dateRe.FindAllString(text, -1) {
		if _, err := time.Parse(FactDateLayout, d); err == nil {
			return d
		}
	}
	return ""
}

// sourceOf finds the last bracketed bead id in a line — '(mw-xxx.N)' or
// '[mw-xxx]' — and the text with that bracket taken out of it.
func sourceOf(text string) (source, rest string) {
	groups := bracketRe.FindAllStringIndex(text, -1)
	for i := len(groups) - 1; i >= 0; i-- {
		ids := beadRe.FindAllString(text[groups[i][0]:groups[i][1]], -1)
		if len(ids) == 0 {
			continue
		}
		return ids[len(ids)-1], text[:groups[i][0]] + text[groups[i][1]:]
	}
	return "", text
}

// pathIn is the first path-like token of text (a/b, a.go, a.md) no longer than
// SubjectMaxChars, a placeholder such as <epic> kept in it whole, else "".
func pathIn(text string) string {
	plain := urlRe.ReplaceAllString(text, " ")
	var found [][]int
	for _, re := range []*regexp.Regexp{slashPath, filePath} {
		found = append(found, re.FindAllStringIndex(plain, -1)...)
	}
	sort.Slice(found, func(i, j int) bool { return found[i][0] < found[j][0] })
	for _, loc := range found {
		if path := strings.TrimRight(plain[loc[0]:loc[1]], ".,"); path != "" && len(path) <= SubjectMaxChars {
			return path
		}
	}
	return ""
}

// subjectOf is the first path-like token of a sentence (a/b, a.go, a.md), else
// the first backticked token, else "general". A backticked token with a space in
// it, or longer than SubjectMaxChars, is a command and not a subject: the first
// path-like token inside it stands in, else "general".
func subjectOf(sentence string) string {
	if path := pathIn(sentence); path != "" {
		return path
	}
	if m := backticked.FindStringSubmatch(sentence); m != nil {
		token := strings.TrimSpace(m[1])
		switch {
		case token == "":
		case len(token) > SubjectMaxChars || strings.ContainsAny(token, " \t"):
			if path := pathIn(token); path != "" {
				return path
			}
		default:
			return token
		}
	}
	return "general"
}

func headingText(line string) string {
	return strings.TrimSpace(strings.TrimLeft(line, "#"))
}

// kindUnder is decision for a line that sits under a heading about what was
// decided or what to know before starting, gotcha for the rest.
func kindUnder(heading string) FactKind {
	lower := strings.ToLower(heading)
	if strings.Contains(lower, "before you start") || strings.Contains(lower, "decided") {
		return FactDecision
	}
	return FactGotcha
}

// scanMemory reads a memory file or its archive. The head is what comes before
// the first heading (past a title on the first line) or '- ' line. A line that
// is not a '- ' line, a heading or blank, and is not the head, is not placed.
func scanMemory(text string) scannedMemory {
	var out scannedMemory
	var head []string
	var heading, dated, datedOn string
	inHead, titled := true, false
	for i, raw := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		line := strings.TrimRight(raw, " \t")
		isHeading := strings.HasPrefix(line, "#")
		isItem := strings.HasPrefix(line, "- ")
		if inHead {
			if !titled && strings.TrimSpace(line) == "" {
				continue
			}
			if !titled && strings.HasPrefix(line, "# ") {
				titled = true
				continue
			}
			titled = true
			if !isHeading && !isItem {
				head = append(head, line)
				continue
			}
			inHead = false
		}
		switch {
		case strings.TrimSpace(line) == "":
		case isHeading:
			heading = headingText(line)
			if d := firstDate(heading); d != "" {
				dated, datedOn = heading, d
			}
		case isItem:
			scanned, ok := scanItem(strings.TrimPrefix(line, "- "))
			if !ok {
				out.NotPlaced = append(out.NotPlaced, fmt.Sprintf("line %d: %s", i+1, line))
				continue
			}
			scanned.Kind, scanned.Heading, scanned.Dated, scanned.DatedOn = kindUnder(heading), heading, dated, datedOn
			out.Lines = append(out.Lines, scanned)
		default:
			out.NotPlaced = append(out.NotPlaced, fmt.Sprintf("line %d: %s", i+1, line))
		}
	}
	out.Head = strings.TrimSpace(strings.Join(head, "\n"))
	return out
}

// scanItem reads what follows a '- '. It is not a fact when nothing is left of
// it once its source is taken out.
func scanItem(text string) (scannedLine, bool) {
	source, rest := sourceOf(text)
	sentence := strings.TrimSpace(rest)
	if sentence == "" {
		return scannedLine{}, false
	}
	return scannedLine{
		Sentence: sentence, Source: source, Subject: subjectOf(sentence), Since: firstDate(sentence),
	}, true
}

// aboutFrom is the head cut to what an about text may be, and a warning when it
// had to be cut: the first 600 bytes, back to the last sentence end in them.
func aboutFrom(file, head string) (about, warning string) {
	if len(head) < AboutMaxBytes {
		return head, ""
	}
	cut := head[:AboutMaxBytes]
	for !utf8.ValidString(cut) {
		cut = cut[:len(cut)-1]
	}
	for i := len(cut) - 1; i >= 0; i-- {
		if strings.IndexByte(".!?", cut[i]) >= 0 && (i == len(cut)-1 || cut[i+1] == ' ' || cut[i+1] == '\n') {
			cut = cut[:i+1]
			break
		}
	}
	return cut, fmt.Sprintf("the head of %s is %d bytes; about.md keeps the first %d, cut at a sentence end", file, len(head), len(cut))
}

// migrated is one rig's memory, read as facts and not yet written.
type migrated struct {
	about     string
	warnings  []string
	facts     []RigFact
	notPlaced []string
}

func (mg migrated) retired() int {
	n := 0
	for _, fact := range mg.facts {
		if fact.Status == FactRetired {
			n++
		}
	}
	return n
}

// migration reads the memory file and its archive as facts.
func (m Memory) migration(ctx context.Context, rig string, files RigMemoryFiles) (migrated, error) {
	var out migrated
	today := m.today()
	var sha string
	source := func(line scannedLine, file string) (string, error) {
		if line.Source != "" {
			return line.Source, nil
		}
		if sha == "" {
			if m.Head == nil {
				return "", fmt.Errorf("no way to read the vault's HEAD")
			}
			head, err := m.Head(ctx)
			if err != nil {
				return "", fmt.Errorf("reading the vault's HEAD for the source of a fact: %w", err)
			}
			if sha = head; len(sha) > 7 {
				sha = sha[:7]
			}
		}
		return fmt.Sprintf("mayor:%s@%s", file, sha), nil
	}

	taken := map[string]bool{}
	slugFor := func(sentence string) string {
		base := FactSlug(sentence)
		if base == "" {
			base = "fact"
		}
		slug := base
		for n := 2; taken[slug]; n++ {
			slug = fmt.Sprintf("%s-%d", base, n)
		}
		taken[slug] = true
		return slug
	}

	for _, part := range []struct {
		name, text   string
		has, archive bool
	}{
		{rig + MemoryExt, files.Memory, files.HasMemory, false},
		{rig + ArchiveSuffix + MemoryExt, files.Archive, files.HasArchive, true},
	} {
		if !part.has {
			continue
		}
		scanned := scanMemory(part.text)
		if !part.archive && scanned.Head != "" {
			var warning string
			out.about, warning = aboutFrom(part.name, scanned.Head)
			if warning != "" {
				out.warnings = append(out.warnings, warning)
			}
		}
		for _, note := range scanned.NotPlaced {
			out.notPlaced = append(out.notPlaced, part.name+" "+note)
		}
		for _, line := range scanned.Lines {
			src, err := source(line, part.name)
			if err != nil {
				return migrated{}, err
			}
			fact := RigFact{
				Slug: slugFor(line.Sentence), Subject: line.Subject, Kind: line.Kind, Status: FactCurrent,
				Source: src, Since: line.Since, Sentence: line.Sentence,
			}
			if fact.Since == "" {
				fact.Since = today
			}
			if part.archive {
				reason := line.Dated
				if reason == "" {
					reason = line.Heading
				}
				if reason == "" {
					reason = "the archive"
				}
				fact.Status, fact.Retired, fact.Reason = FactRetired, line.DatedOn, "pruned: "+reason
				if fact.Retired == "" {
					fact.Retired = today
				}
			}
			if err := fact.validate(); err != nil {
				return migrated{}, fmt.Errorf("%s: the line %q does not make a fact: %w", part.name, line.Sentence, err)
			}
			out.facts = append(out.facts, fact)
		}
	}
	return out, nil
}

// Migrate turns the one memory file a seat keeps of a rig, and its archive,
// into an about text and one fact file each, the archive's lines retired with
// the date and name of the heading they sat under. With DryRun it prints what
// it would write and writes nothing. It runs no git command: the Mayor commits.
func (m Memory) Migrate(ctx context.Context, req MemoryMigrate) error {
	if m.Legacy == nil {
		return fmt.Errorf("mw memory migrate has no way to read the seat's memory files")
	}
	files, err := m.Legacy.ReadRigMemoryFiles(ctx, m.Seat, req.Rig)
	if err != nil {
		return err
	}
	if files.HasFacts {
		return fmt.Errorf("the rig %s already has a facts folder: migrate moves a rig once", req.Rig)
	}
	if !files.HasMemory {
		return fmt.Errorf("no memory file for the rig %s in the %s seat: seats/%s/rigs/%s%s is missing", req.Rig, m.Seat, m.Seat, req.Rig, MemoryExt)
	}
	mg, err := m.migration(ctx, req.Rig, files)
	if err != nil {
		return err
	}

	for _, warning := range mg.warnings {
		fmt.Fprintf(m.Out, "warning: %s\n", warning)
	}
	counts := fmt.Sprintf("%d facts (%d current, %d retired); %s not placed",
		len(mg.facts), len(mg.facts)-mg.retired(), mg.retired(), lines(len(mg.notPlaced)))
	if mg.about != "" {
		counts = "about.md and " + counts
	}

	if req.DryRun {
		fmt.Fprintln(m.Out, "dry run: nothing is written")
		if mg.about != "" {
			fmt.Fprintf(m.Out, "%s  %d bytes\n", AboutFile, len(mg.about))
		}
		for _, fact := range mg.facts {
			fmt.Fprintf(m.Out, "%s  %s  %s  [%s]  %s  %s  %s", fact.Slug, fact.Status, fact.Kind, fact.Subject, fact.Since, fact.Source, fact.Sentence)
			if fact.Status == FactRetired {
				fmt.Fprintf(m.Out, "  retired %s (%s)", fact.Retired, fact.Reason)
			}
			fmt.Fprintln(m.Out)
		}
		for _, line := range mg.notPlaced {
			fmt.Fprintf(m.Out, "not placed: %s\n", line)
		}
		fmt.Fprintf(m.Out, "would migrate the rig %s: %s\n", req.Rig, counts)
		return nil
	}

	if mg.about != "" {
		path, err := m.Legacy.WriteRigAbout(ctx, m.Seat, req.Rig, mg.about+"\n")
		if err != nil {
			return err
		}
		fmt.Fprintln(m.Out, path)
	}
	for _, fact := range mg.facts {
		if err := m.put(ctx, req.Rig, fact); err != nil {
			return err
		}
	}
	if err := m.Legacy.RemoveRigMemoryFiles(ctx, m.Seat, req.Rig); err != nil {
		return err
	}
	for _, line := range mg.notPlaced {
		fmt.Fprintf(m.Out, "not placed: %s\n", line)
	}
	fmt.Fprintf(m.Out, "migrated the rig %s: %s\n", req.Rig, counts)
	return nil
}

func lines(n int) string {
	if n == 1 {
		return "1 line"
	}
	return fmt.Sprintf("%d lines", n)
}
