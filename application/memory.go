package application

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
	"unicode"
)

// RigFactFiles is the port the Mayor places facts through: the folder of one
// rig in a seat's rigs directory, with an about text and a facts folder of one
// file each. The vault adapter is the directory on disk. Nothing is ever
// deleted through it.
type RigFactFiles interface {
	// ReadRigFacts reads the rig's about text and the text of every file in its
	// facts folder, by file name. known is false when the seat has no folder for
	// the rig, which is not an error; a folder with no facts folder yet is known
	// and has no files.
	ReadRigFacts(ctx context.Context, seat, rig string) (about string, files map[string]string, known bool, err error)

	// WriteRigFact writes the file of one fact, making the facts folder if it is
	// not there, replacing the file if there is one, and reports where it landed.
	WriteRigFact(ctx context.Context, seat, rig, slug, text string) (string, error)
}

// factSlugWords is how many words of a sentence a slug is made of by default.
const factSlugWords = 5

// Memory is mw memory: the Mayor places, replaces and retires the facts of a
// rig that is kept as facts. Each verb writes files and nothing else — no
// commit, no push — and no verb deletes a file, so a fact's reason outlives it.
type Memory struct {
	Files RigFactFiles
	Seat  string
	// Budget is what a Builder may read of a rig at boot, in bytes.
	Budget int
	Now    func() time.Time
	Out    io.Writer
}

// MemoryAdd is the request mw memory add makes.
type MemoryAdd struct {
	Rig, Slug, Subject, Source, Sentence string
	Kind                                 FactKind
}

// MemorySupersede is the request mw memory supersede makes. The new fact keeps
// the old one's subject and kind.
type MemorySupersede struct {
	Rig, Old, Slug, Source, Sentence string
}

// MemoryList is the request mw memory list makes. An empty Status lists every
// status.
type MemoryList struct {
	Rig    string
	Status FactStatus
	Oldest bool
}

func (m Memory) today() string {
	now := time.Now
	if m.Now != nil {
		now = m.Now
	}
	return now().UTC().Format(FactDateLayout)
}

// load reads a rig's facts, refusing a rig the seat has no folder for. The
// facts that are not whole are not in the map; a slug that names one is
// refused with why.
func (m Memory) load(ctx context.Context, rig string) (about string, files map[string]string, facts map[string]RigFact, err error) {
	about, files, known, err := m.Files.ReadRigFacts(ctx, m.Seat, rig)
	if err != nil {
		return "", nil, nil, err
	}
	if !known {
		return "", nil, nil, fmt.Errorf("no rig %q in the %s seat's memory: make seats/%s/rigs/%s/ first", rig, m.Seat, m.Seat, rig)
	}
	facts = map[string]RigFact{}
	list, _ := LoadRigFacts(files)
	for _, fact := range list {
		facts[fact.Slug] = fact
	}
	return about, files, facts, nil
}

// existing finds the fact a verb acts on, refusing a slug that is not there.
func existing(rig, slug string, files map[string]string, facts map[string]RigFact) (RigFact, error) {
	if fact, ok := facts[slug]; ok {
		return fact, nil
	}
	if _, ok := files[slug+MemoryExt]; ok {
		_, err := ParseRigFact(slug, files[slug+MemoryExt])
		return RigFact{}, fmt.Errorf("the fact %q of the rig %s is not a whole fact: %v", slug, rig, err)
	}
	return RigFact{}, fmt.Errorf("no fact %q for the rig %s", slug, rig)
}

// factLine refuses a value that has a newline in it or nothing at all.
func factLine(what, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s is empty", what)
	}
	if strings.ContainsAny(value, "\r\n") {
		return fmt.Errorf("%s must be one line, with no newline", what)
	}
	return nil
}

// FactSlug is the file name a sentence gets by default: its first five words,
// lower-case, joined by hyphens, with what is not a letter or a digit left out.
func FactSlug(sentence string) string {
	var words []string
	for _, word := range strings.Fields(sentence) {
		kept := strings.Map(func(r rune) rune {
			if r < unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r)) {
				return unicode.ToLower(r)
			}
			return -1
		}, word)
		if kept == "" {
			continue
		}
		words = append(words, kept)
		if len(words) == factSlugWords {
			break
		}
	}
	return strings.Join(words, "-")
}

// checkSlug refuses a slug that is not lower-case letters, digits and hyphens.
func checkSlug(slug string) error {
	if slug == "" {
		return fmt.Errorf("the slug is empty: give one with --slug")
	}
	for _, r := range slug {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
			return fmt.Errorf("the slug %q may hold only a-z, 0-9 and hyphens", slug)
		}
	}
	return nil
}

func (m Memory) put(ctx context.Context, rig string, fact RigFact) error {
	if err := fact.validate(); err != nil {
		return err
	}
	path, err := m.Files.WriteRigFact(ctx, m.Seat, rig, fact.Slug, fact.String())
	if err != nil {
		return err
	}
	fmt.Fprintln(m.Out, path)
	return nil
}

// Add writes a new current fact.
func (m Memory) Add(ctx context.Context, req MemoryAdd) error {
	switch req.Kind {
	case FactGotcha, FactDecision:
	default:
		return fmt.Errorf("unknown kind %q (gotcha or decision)", req.Kind)
	}
	if err := factLine("the sentence", req.Sentence); err != nil {
		return err
	}
	if err := factLine("the subject", req.Subject); err != nil {
		return err
	}
	if err := factLine("the source", req.Source); err != nil {
		return err
	}
	slug := req.Slug
	if slug == "" {
		slug = FactSlug(req.Sentence)
	}
	if err := checkSlug(slug); err != nil {
		return err
	}
	_, files, facts, err := m.load(ctx, req.Rig)
	if err != nil {
		return err
	}
	if err := m.free(req.Rig, slug, files, facts); err != nil {
		return err
	}
	return m.put(ctx, req.Rig, RigFact{
		Slug: slug, Subject: req.Subject, Kind: req.Kind, Status: FactCurrent,
		Source: req.Source, Since: m.today(), Sentence: strings.TrimSpace(req.Sentence),
	})
}

// free refuses a slug that already has a file, saying so plainly for a fact
// that is flagged to recheck.
func (m Memory) free(rig, slug string, files map[string]string, facts map[string]RigFact) error {
	if _, taken := files[slug+MemoryExt]; !taken {
		return nil
	}
	if facts[slug].Status == FactRecheck {
		return fmt.Errorf("the fact %q of the rig %s is flagged to recheck: supersede or retire it", slug, rig)
	}
	return fmt.Errorf("the fact %q of the rig %s already exists", slug, rig)
}

// Supersede writes a new current fact in place of an old one and links the two.
func (m Memory) Supersede(ctx context.Context, req MemorySupersede) error {
	if err := factLine("the sentence", req.Sentence); err != nil {
		return err
	}
	if err := factLine("the source", req.Source); err != nil {
		return err
	}
	slug := req.Slug
	if slug == "" {
		slug = FactSlug(req.Sentence)
	}
	if err := checkSlug(slug); err != nil {
		return err
	}
	_, files, facts, err := m.load(ctx, req.Rig)
	if err != nil {
		return err
	}
	old, err := existing(req.Rig, req.Old, files, facts)
	if err != nil {
		return err
	}
	switch old.Status {
	case FactSuperseded, FactRetired:
		return fmt.Errorf("the fact %q of the rig %s is already %s", old.Slug, req.Rig, old.Status)
	}
	if slug == old.Slug {
		return fmt.Errorf("the new fact needs a slug other than %q: give one with --slug", slug)
	}
	if err := m.free(req.Rig, slug, files, facts); err != nil {
		return err
	}
	successor := RigFact{
		Slug: slug, Subject: old.Subject, Kind: old.Kind, Status: FactCurrent,
		Source: req.Source, Since: m.today(), Sentence: strings.TrimSpace(req.Sentence),
		Supersedes: old.Slug,
	}
	old.Status, old.SupersededBy = FactSuperseded, slug
	if err := successor.validate(); err != nil {
		return err
	}
	if err := m.put(ctx, req.Rig, successor); err != nil {
		return err
	}
	return m.put(ctx, req.Rig, old)
}

// Retire keeps a fact's sentence and gives the reason it is no longer read.
func (m Memory) Retire(ctx context.Context, rig, slug, reason string) error {
	if err := factLine("the reason", reason); err != nil {
		return err
	}
	_, files, facts, err := m.load(ctx, rig)
	if err != nil {
		return err
	}
	fact, err := existing(rig, slug, files, facts)
	if err != nil {
		return err
	}
	if fact.Status == FactRetired {
		return fmt.Errorf("the fact %q of the rig %s is already retired", slug, rig)
	}
	fact.Status, fact.Retired, fact.Reason = FactRetired, m.today(), strings.TrimSpace(reason)
	return m.put(ctx, rig, fact)
}

// Recheck flags a fact as in doubt, with why appended to its reason. A flagged
// fact is not read at boot until it is superseded or retired.
func (m Memory) Recheck(ctx context.Context, rig, slug, why string) error {
	if strings.ContainsAny(why, "\r\n") {
		return fmt.Errorf("the reason must be one line, with no newline")
	}
	_, files, facts, err := m.load(ctx, rig)
	if err != nil {
		return err
	}
	fact, err := existing(rig, slug, files, facts)
	if err != nil {
		return err
	}
	switch fact.Status {
	case FactSuperseded, FactRetired:
		return fmt.Errorf("the fact %q of the rig %s is already %s", slug, rig, fact.Status)
	}
	fact.Status = FactRecheck
	if why = strings.TrimSpace(why); why != "" {
		if fact.Reason != "" {
			why = fact.Reason + "; " + why
		}
		fact.Reason = why
	}
	return m.put(ctx, rig, fact)
}

// statusRank puts the facts a Builder reads first.
func statusRank(s FactStatus) int {
	switch s {
	case FactCurrent:
		return 0
	case FactRecheck:
		return 1
	case FactSuperseded:
		return 2
	}
	return 3
}

// List prints one line a fact — slug, status, kind, [subject], since, source —
// the files that are not whole, and the size of what a Builder reads against
// the budget.
func (m Memory) List(ctx context.Context, req MemoryList) error {
	switch req.Status {
	case "", FactCurrent, FactRecheck, FactSuperseded, FactRetired:
	default:
		return fmt.Errorf("unknown status %q (current, recheck, superseded or retired)", req.Status)
	}
	about, files, _, err := m.load(ctx, req.Rig)
	if err != nil {
		return err
	}
	all, skipped := LoadRigFacts(files)
	shown := make([]RigFact, 0, len(all))
	for _, fact := range all {
		if req.Status == "" || fact.Status == req.Status {
			shown = append(shown, fact)
		}
	}
	sort.SliceStable(shown, func(i, j int) bool {
		a, b := shown[i], shown[j]
		if req.Oldest {
			if a.Since != b.Since {
				return a.Since < b.Since
			}
			return a.Slug < b.Slug
		}
		if statusRank(a.Status) != statusRank(b.Status) {
			return statusRank(a.Status) < statusRank(b.Status)
		}
		if a.Subject != b.Subject {
			return a.Subject < b.Subject
		}
		return a.Slug < b.Slug
	})
	for _, fact := range shown {
		fmt.Fprintf(m.Out, "%s  %s  %s  [%s]  %s  %s\n", fact.Slug, fact.Status, fact.Kind, fact.Subject, fact.Since, fact.Source)
	}
	for _, why := range skipped {
		fmt.Fprintf(m.Out, "skipped: %s\n", why)
	}
	fmt.Fprintf(m.Out, "render %d/%d bytes\n", len(RenderRigMemory(about, all)), m.Budget)
	return nil
}
