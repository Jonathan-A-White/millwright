package application

import (
	"context"
	"errors"
	"fmt"
	"io"
	pathpkg "path"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
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

	// ReadRigEval reads the rig's eval.md, found false when it has none, which
	// is not an error.
	ReadRigEval(ctx context.Context, seat, rig string) (text string, found bool, err error)

	// WriteRigEval writes the rig's eval.md, replacing it if there is one, and
	// reports where it landed.
	WriteRigEval(ctx context.Context, seat, rig, text string) (string, error)
}

// MemoryQueryRecord is one `mw memory query` run, as the host's log keeps it:
// when, of which rig, with which terms, how many facts it hit, the subjects of
// those facts, and the story whose session ran it (empty for the Mayor's).
type MemoryQueryRecord struct {
	At       time.Time `json:"at"`
	Rig      string    `json:"rig"`
	Terms    []string  `json:"terms"`
	Hits     int       `json:"hits"`
	Subjects []string  `json:"subjects,omitempty"`
	Story    string    `json:"story,omitempty"`
}

// MemoryQueryLog is the port behind the count of what Builders query: the
// host's own log, one record a query. It is host state, never the vault.
type MemoryQueryLog interface {
	AppendMemoryQuery(ctx context.Context, record MemoryQueryRecord) error
	// ReadMemoryQueries reads every record, oldest first; no log is no records.
	ReadMemoryQueries(ctx context.Context) ([]MemoryQueryRecord, error)
}

// factSlugWords is how many words of a sentence a slug is made of by default.
const factSlugWords = 5

// Memory is mw memory: the Mayor places, replaces and retires the facts of a
// rig that is kept as facts. A verb that changes a fact commits the files it
// wrote, those alone, and pushes them when it has a Vault; no verb deletes a
// fact, so a fact's reason outlives it. (migrate removes the one memory file of
// a rig it has turned into facts, and nothing else.)
type Memory struct {
	Files RigFactFiles
	// Vault commits and pushes what a verb wrote; nil leaves that to a person.
	// A push that did not go is said on Warn.
	Vault RecordVault
	// Legacy and Head are what migrate needs: the rig's one memory file and the
	// vault's HEAD, which names the source of a line that cites no bead.
	Legacy RigLegacyFiles
	Head   func(context.Context) (string, error)
	Seat   string
	// Budget is what a Builder may read of a rig at boot, in bytes.
	Budget int
	Now    func() time.Time
	Out    io.Writer

	// Queries logs each query and reads them back for stats; nil logs nothing.
	// Story is the story whose session runs the query, Warn where a log that
	// cannot be written is said (a query still answers).
	Queries MemoryQueryLog
	Story   string
	Warn    io.Writer
}

// MemoryAdd is the request mw memory add makes.
type MemoryAdd struct {
	Rig, Slug, Subject, Source, Sentence string
	Kind                                 FactKind
	// MakeRoom demotes old gotchas first (MakeRoom, below) when the new line
	// would take the boot render over its budget.
	MakeRoom bool
}

// MemorySupersede is the request mw memory supersede makes. The new fact keeps
// the old one's subject and kind.
type MemorySupersede struct {
	Rig, Old, Slug, Source, Sentence string
}

// MemoryList is the request mw memory list makes. An empty Status lists every
// status. Current lists only the current facts and cannot be combined with a
// Status.
type MemoryList struct {
	Rig     string
	Status  FactStatus
	Oldest  bool
	Current bool
}

// MemoryQuery is the request mw memory query makes: every term must match.
type MemoryQuery struct {
	Rig   string
	Terms []string
}

func (m Memory) clock() time.Time {
	if m.Now != nil {
		return m.Now()
	}
	return time.Now()
}

func (m Memory) today() string {
	return m.clock().UTC().Format(FactDateLayout)
}

// NoFactsFolder is the refusal for a rig the seat keeps no facts folder for:
// a flat-file rig (seats/<seat>/rigs/<rig>.md) or none at all.
type NoFactsFolder struct{ Seat, Rig string }

func (e NoFactsFolder) Error() string {
	return fmt.Sprintf("no rig %q in the %s seat's memory: make seats/%s/rigs/%s/ first", e.Rig, e.Seat, e.Seat, e.Rig)
}

// readFactsFolder is the one test of whether a rig has a facts folder: it reads
// the rig's facts, and returns NoFactsFolder when the seat has no folder for
// it. Memory's verbs refuse through it and HasFactsFolder answers through it.
func readFactsFolder(ctx context.Context, files RigFactFiles, seat, rig string) (about string, text map[string]string, err error) {
	about, text, known, err := files.ReadRigFacts(ctx, seat, rig)
	if err != nil {
		return "", nil, err
	}
	if !known {
		return "", nil, NoFactsFolder{Seat: seat, Rig: rig}
	}
	return about, text, nil
}

// HasFactsFolder reports whether the seat keeps a facts folder for the rig,
// the same test mw memory's refusal makes. A flat-file rig has none.
func HasFactsFolder(ctx context.Context, files RigFactFiles, seat, rig string) (bool, error) {
	_, _, err := readFactsFolder(ctx, files, seat, rig)
	var none NoFactsFolder
	if errors.As(err, &none) {
		return false, nil
	}
	return err == nil, err
}

// FlatRigFile is where a flat-file rig's memory lives in the vault.
func FlatRigFile(seat, rig string) string {
	return fmt.Sprintf("seats/%s/rigs/%s.md", seat, rig)
}

// load reads a rig's facts, refusing a rig the seat has no folder for. The
// facts that are not whole are not in the map; a slug that names one is
// refused with why.
func (m Memory) load(ctx context.Context, rig string) (about string, files map[string]string, facts map[string]RigFact, err error) {
	about, files, err = readFactsFolder(ctx, m.Files, m.Seat, rig)
	if err != nil {
		return "", nil, nil, err
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

// put writes a fact and reports its path from the vault's root.
func (m Memory) put(ctx context.Context, rig string, fact RigFact) (string, error) {
	if err := fact.validate(); err != nil {
		return "", err
	}
	path, err := m.Files.WriteRigFact(ctx, m.Seat, rig, fact.Slug, fact.String())
	if err != nil {
		return "", err
	}
	fmt.Fprintln(m.Out, path)
	return factPath(m.Seat, rig, fact.Slug), nil
}

// factPath is where a fact's file is, from the vault's root.
func factPath(seat, rig, slug string) string {
	return pathpkg.Join(SeatsDir, seat, RigsDir, rig, FactsDir, strings.TrimSuffix(slug, MemoryExt)+MemoryExt)
}

// record commits what the verb wrote, under "mw memory <subject>".
func (m Memory) record(ctx context.Context, subject string, paths ...string) error {
	return recordInVault(ctx, m.Vault, m.Warn, "mw memory "+subject, paths)
}

// putAndRecord is put then record, for the verbs that write one fact.
func (m Memory) putAndRecord(ctx context.Context, rig string, fact RigFact, subject string) error {
	path, err := m.put(ctx, rig, fact)
	if err != nil {
		return err
	}
	return m.record(ctx, subject, path)
}

// Add writes a new current fact. It ends by saying plainly what happened:
// "added <slug>" (with the near facts named), or "not added: <why>" with the
// error returned, so the exit code matches the sentence.
func (m Memory) Add(ctx context.Context, req MemoryAdd) error {
	fact, near, err := m.newFact(ctx, req)
	if err != nil {
		return m.notAdded(err)
	}
	if req.MakeRoom {
		cost, free, err := m.renderCost(ctx, req.Rig, fact)
		if err != nil {
			return m.notAdded(err)
		}
		if cost > free {
			if err := m.makeRoom(ctx, req.Rig, cost, &fact, false); err != nil {
				return m.notAdded(err)
			}
		}
	}
	path, err := m.put(ctx, req.Rig, fact)
	if err != nil {
		return m.notAdded(err)
	}
	// A fact written but not committed is not "not added": record's error says so.
	if err := m.record(ctx, "add "+req.Rig+" "+fact.Slug, path); err != nil {
		return err
	}
	said := "added " + fact.Slug
	for i, other := range near {
		fmt.Fprintf(m.Out, "near: %s %s\n", other.Slug, firstWords(other.Sentence, nearWords))
		if i == 0 {
			said += " (near: " + other.Slug
		} else {
			said += ", " + other.Slug
		}
	}
	if len(near) > 0 {
		said += ")"
	}
	fmt.Fprintln(m.Out, said)
	return m.warnOverBudget(ctx, req.Rig)
}

// warnOverBudget says, after an add, that the render is over its budget and
// which command makes the room: the oldest eligible gotchas, chosen by mw.
func (m Memory) warnOverBudget(ctx context.Context, rig string) error {
	about, files, _, err := m.load(ctx, rig)
	if err != nil {
		return err
	}
	all, _ := LoadRigFacts(files)
	if size := len(RenderRigMemory(about, all)); m.Budget > 0 && size > m.Budget {
		fmt.Fprintf(m.Out, "WARN: the render is %d/%d bytes, over the budget by %d bytes: run `mw memory make-room %s --bytes 0` (or add with --make-room)\n",
			size, m.Budget, size-m.Budget, rig)
	}
	return nil
}

// renderCost is how many bytes a new fact adds to the rig's boot render, and
// how many the render has free under the budget now.
func (m Memory) renderCost(ctx context.Context, rig string, fact RigFact) (cost, free int, err error) {
	about, files, _, err := m.load(ctx, rig)
	if err != nil {
		return 0, 0, err
	}
	all, _ := LoadRigFacts(files)
	before := len(RenderRigMemory(about, all))
	after := len(RenderRigMemory(about, append(all[:len(all):len(all)], fact)))
	return after - before, m.Budget - before, nil
}

// notAdded says why add wrote nothing and returns the same error.
func (m Memory) notAdded(err error) error {
	fmt.Fprintf(m.Out, "not added: %v\n", err)
	return err
}

// newFact checks an add and builds the fact to write, with the current facts near it.
func (m Memory) newFact(ctx context.Context, req MemoryAdd) (RigFact, []RigFact, error) {
	switch req.Kind {
	case FactGotcha, FactDecision:
	default:
		return RigFact{}, nil, fmt.Errorf("unknown kind %q (gotcha or decision)", req.Kind)
	}
	if err := factLine("the sentence", req.Sentence); err != nil {
		return RigFact{}, nil, err
	}
	if err := factLine("the subject", req.Subject); err != nil {
		return RigFact{}, nil, err
	}
	if err := factLine("the source", req.Source); err != nil {
		return RigFact{}, nil, err
	}
	slug, derived := req.Slug, req.Slug == ""
	if derived {
		slug = FactSlug(req.Sentence)
	}
	if err := checkSlug(slug); err != nil {
		return RigFact{}, nil, err
	}
	_, files, facts, err := m.load(ctx, req.Rig)
	if err != nil {
		return RigFact{}, nil, err
	}
	if err := m.free(req.Rig, slug, derived, files, facts); err != nil {
		return RigFact{}, nil, err
	}
	fact := RigFact{
		Slug: slug, Subject: req.Subject, Kind: req.Kind, Status: FactCurrent,
		Source: req.Source, Since: m.today(), Sentence: strings.TrimSpace(req.Sentence),
	}
	return fact, nearFacts(fact, facts), nil
}

// nearWords is how many words of a near fact's sentence add prints after its slug.
const nearWords = 8

// nearSharedWords is how many long words a current fact of another subject must
// share with a new sentence to be called near it.
const nearSharedWords = 4

// nearMinWord is the shortest word that counts toward nearSharedWords.
const nearMinWord = 5

// nearFacts are the current facts a new fact repeats or comes close to: those of
// its subject, case aside, and those of another subject that share at least
// nearSharedWords words of nearMinWord letters or more with its sentence. They
// are in the order query reads them.
func nearFacts(fact RigFact, facts map[string]RigFact) []RigFact {
	words := longWords(fact.Slug + " " + fact.Sentence)
	var near []RigFact
	for _, other := range orderedFacts(facts) {
		if other.Status != FactCurrent || other.Slug == fact.Slug {
			continue
		}
		if strings.EqualFold(other.Subject, fact.Subject) {
			near = append(near, other)
			continue
		}
		shared := 0
		for word := range longWords(other.Slug + " " + other.Sentence) {
			if words[word] {
				shared++
			}
		}
		if shared >= nearSharedWords {
			near = append(near, other)
		}
	}
	return near
}

// longWords are the distinct lower-case words of text of nearMinWord letters or
// digits or more.
func longWords(text string) map[string]bool {
	words := map[string]bool{}
	for _, word := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		if utf8.RuneCountInString(word) >= nearMinWord {
			words[word] = true
		}
	}
	return words
}

// firstWords is the first n words of text, joined by single spaces.
func firstWords(text string, n int) string {
	words := strings.Fields(text)
	if len(words) > n {
		words = words[:n]
	}
	return strings.Join(words, " ")
}

// free refuses a slug that already has a file, saying so plainly for a fact
// that is flagged to recheck. A slug made from the sentence (derived) says so
// and says to pass --slug, because the person never chose it.
func (m Memory) free(rig, slug string, derived bool, files map[string]string, facts map[string]RigFact) error {
	if _, taken := files[slug+MemoryExt]; !taken {
		return nil
	}
	if facts[slug].Status == FactRecheck {
		return fmt.Errorf("the fact %q of the rig %s is flagged to recheck: supersede or retire it", slug, rig)
	}
	if derived {
		return fmt.Errorf("the fact %q of the rig %s already exists: that slug was made from the first %d words of the sentence; pass --slug <new> to name the new fact", slug, rig, factSlugWords)
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
	slug, derived := req.Slug, req.Slug == ""
	if derived {
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
	if err := m.free(req.Rig, slug, derived, files, facts); err != nil {
		return err
	}
	// The new fact is read where the old one was: a demoted fact stays out of
	// the boot render, so a supersede never grows it.
	successor := RigFact{
		Slug: slug, Subject: old.Subject, Kind: old.Kind, Status: FactCurrent,
		Source: req.Source, Since: m.today(), Sentence: strings.TrimSpace(req.Sentence),
		Supersedes: old.Slug, Unbooted: old.Unbooted,
	}
	old.Status, old.SupersededBy = FactSuperseded, slug
	if err := successor.validate(); err != nil {
		return err
	}
	written, err := m.put(ctx, req.Rig, successor)
	if err != nil {
		return err
	}
	retired, err := m.put(ctx, req.Rig, old)
	if err != nil {
		return err
	}
	paths := []string{written, retired}
	if successor.Unbooted {
		evalPath, err := m.carryEvalQuestions(ctx, req.Rig, old, successor, facts)
		if err != nil {
			return err
		}
		if evalPath != "" {
			paths = append(paths, evalPath)
		}
	}
	if err := m.record(ctx, "supersede "+req.Rig+" "+old.Slug+" with "+slug, paths...); err != nil {
		return err
	}
	if err := m.sayRender(ctx, req.Rig); err != nil {
		return err
	}
	fmt.Fprintf(m.Out, "superseded %s with %s\n", old.Slug, slug)
	return nil
}

// carryEvalQuestions makes the questions of eval.md that expect a demoted old
// fact expect its successor too, when they find it, so a Builder who asked for
// the old fact still finds the new one. It writes eval.md only if a question
// changed, and returns its path from the vault's root (empty when untouched). A
// successor that no question finds is warned of: it is out of the boot render
// and nothing reaches it.
func (m Memory) carryEvalQuestions(ctx context.Context, rig string, old, successor RigFact, facts map[string]RigFact) (string, error) {
	text, found, err := m.Files.ReadRigEval(ctx, m.Seat, rig)
	if err != nil {
		return "", err
	}
	all := orderedFacts(withFact(facts, successor))
	carried := false
	if found {
		questions, err := ParseEval(text)
		if err != nil {
			return "", err
		}
		for _, q := range questions {
			if !q.expects(old.Slug) || q.expects(successor.Slug) || !q.finds(all, successor.Slug) {
				continue
			}
			text, carried = addExpect(text, q, successor.Slug), true
		}
	}
	if carried {
		if _, err := m.Files.WriteRigEval(ctx, m.Seat, rig, text); err != nil {
			return "", err
		}
		return pathpkg.Join(SeatsDir, m.Seat, RigsDir, rig, EvalFile), nil
	}
	if found {
		if questions, err := ParseEval(text); err == nil {
			for _, q := range questions {
				if q.finds(all, successor.Slug) {
					return "", nil
				}
			}
		}
	}
	fmt.Fprintf(m.Out, "WARN: no eval question finds %s, which is out of the boot render (boot: no): add one to eval.md\n", successor.Slug)
	return "", nil
}

// withFact is facts with one more, by its slug, without changing the map given.
func withFact(facts map[string]RigFact, fact RigFact) map[string]RigFact {
	out := make(map[string]RigFact, len(facts)+1)
	for slug, f := range facts {
		out[slug] = f
	}
	out[fact.Slug] = fact
	return out
}

// sayRender prints the size of the rig's boot render against its budget, and
// WARNs when it is over: it is what every Builder pays on every story.
func (m Memory) sayRender(ctx context.Context, rig string) error {
	about, files, _, err := m.load(ctx, rig)
	if err != nil {
		return err
	}
	all, _ := LoadRigFacts(files)
	size := len(RenderRigMemory(about, all))
	fmt.Fprintf(m.Out, "render %d/%d bytes\n", size, m.Budget)
	if m.Budget > 0 && size > m.Budget {
		fmt.Fprintf(m.Out, "WARN: the render is over the budget by %d bytes: run `mw memory make-room %s --bytes 0` to demote the oldest eligible gotchas, or retire a fact by hand (seats/mayor/pruning.md)\n", size-m.Budget, rig)
	}
	return nil
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
	if err := m.putAndRecord(ctx, rig, fact, "retire "+rig+" "+slug); err != nil {
		return err
	}
	fmt.Fprintf(m.Out, "retired %s\n", slug)
	return nil
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
	if err := m.putAndRecord(ctx, rig, fact, "recheck "+rig+" "+slug); err != nil {
		return err
	}
	fmt.Fprintf(m.Out, "flagged %s to recheck\n", slug)
	return nil
}

// Demote keeps a current fact out of the boot render (boot: no). The fact stays
// current, and query and list still show it. It is refused unless some question
// of the rig's eval.md expects the fact and passes, so that a Builder who needs
// the fact can still find it. A question given appends that question, expecting
// the fact, to eval.md first; that question itself must put the fact in the top
// results (no other question stands in for it), it is checked before anything
// is written, and eval.md and the fact are committed together.
func (m Memory) Demote(ctx context.Context, rig, slug, question string) error {
	_, files, facts, err := m.load(ctx, rig)
	if err != nil {
		return err
	}
	fact, err := existing(rig, slug, files, facts)
	if err != nil {
		return err
	}
	if fact.Status != FactCurrent {
		return fmt.Errorf("the fact %q of the rig %s is %s, not current", slug, rig, fact.Status)
	}
	if fact.Unbooted {
		return fmt.Errorf("the fact %q of the rig %s is already demoted", slug, rig)
	}
	text, _, err := m.Files.ReadRigEval(ctx, m.Seat, rig)
	if err != nil {
		return err
	}
	asked := strings.TrimSpace(question) != ""
	if asked {
		if err := factLine("the question", question); err != nil {
			return err
		}
		text = appendEvalQuestion(text, strings.TrimSpace(question), slug)
	}
	report, err := evalText(text, facts)
	if err != nil {
		return err
	}
	// A question given is the one that must find the fact: it is the last
	// appended, so no earlier question may stand in for it.
	checked := report.Questions
	if asked && len(checked) > 0 {
		checked = checked[len(checked)-1:]
	}
	found := false
	for _, q := range checked {
		for _, expected := range q.Expect {
			if expected == slug && !q.Failed {
				found = true
			}
		}
	}
	if !found {
		if asked {
			return fmt.Errorf("no eval question finds %s: the question %q does not put it in the top %d; nothing was changed", slug, strings.TrimSpace(question), EvalTop)
		}
		return fmt.Errorf("no eval question finds %s; add one to eval.md first (or give --question)", slug)
	}
	fact.Unbooted = true
	var written []string
	if asked {
		if _, err := m.Files.WriteRigEval(ctx, m.Seat, rig, text); err != nil {
			return err
		}
		written = append(written, pathpkg.Join(SeatsDir, m.Seat, RigsDir, rig, EvalFile))
	}
	path, err := m.put(ctx, rig, fact)
	if err != nil {
		return err
	}
	if err := m.record(ctx, "demote "+rig+" "+slug, append(written, path)...); err != nil {
		return err
	}
	fmt.Fprintf(m.Out, "demoted %s out of the boot render\n", slug)
	return nil
}

// appendEvalQuestion adds a Q and its expect to an eval.md text, a blank line
// after what is there.
func appendEvalQuestion(text, question, slug string) string {
	if text != "" && !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	if text != "" && !strings.HasSuffix(text, "\n\n") {
		text += "\n"
	}
	return text + "Q: " + question + "\nexpect: " + slug + "\n"
}

// Promote puts a demoted fact back into the boot render.
func (m Memory) Promote(ctx context.Context, rig, slug string) error {
	_, files, facts, err := m.load(ctx, rig)
	if err != nil {
		return err
	}
	fact, err := existing(rig, slug, files, facts)
	if err != nil {
		return err
	}
	if !fact.Unbooted {
		return fmt.Errorf("the fact %q of the rig %s is not demoted", slug, rig)
	}
	fact.Unbooted = false
	if err := m.putAndRecord(ctx, rig, fact, "promote "+rig+" "+slug); err != nil {
		return err
	}
	fmt.Fprintf(m.Out, "promoted %s into the boot render\n", slug)
	return nil
}

// NearFullPercent is how full, of its budget, a rig's boot render is called
// near full: the point past which a new fact may well be sent to search only.
const NearFullPercent = 95

// NewestSearchOnly is how many of a rig's newest facts sit outside the boot
// render: the current facts demoted (boot: no) that are newer, by their since
// date, than every current fact in the render. A fact of the same day as the
// newest one in the render is not counted, and 0 means the newest fact is read
// at boot. A demoted old fact, older than something in the render, is no new
// fact and is not counted.
func NewestSearchOnly(facts []RigFact) int {
	newestBooted := ""
	for _, fact := range facts {
		if fact.Status == FactCurrent && !fact.Unbooted && fact.Since > newestBooted {
			newestBooted = fact.Since
		}
	}
	count := 0
	for _, fact := range facts {
		if fact.Status == FactCurrent && fact.Unbooted && fact.Since > newestBooted {
			count++
		}
	}
	return count
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
	if req.Current && req.Status != "" {
		return fmt.Errorf("--current and --status both choose which facts to list: use one")
	}
	if req.Current {
		req.Status = FactCurrent
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
		status := string(fact.Status)
		if fact.Unbooted {
			status += " boot:no"
		}
		fmt.Fprintf(m.Out, "%s  %s  %s  [%s]  %s  %s\n", fact.Slug, status, fact.Kind, fact.Subject, fact.Since, fact.Source)
	}
	for _, why := range skipped {
		fmt.Fprintf(m.Out, "skipped: %s\n", why)
	}
	size := len(RenderRigMemory(about, all))
	fmt.Fprintf(m.Out, "render %d/%d bytes\n", size, m.Budget)
	if unseen := NewestSearchOnly(all); unseen > 0 && m.Budget > 0 && size*100 >= m.Budget*NearFullPercent {
		fmt.Fprintf(m.Out, "%d of the newest facts are search-only: run `mw memory make-room %s --bytes N` (seats/mayor/pruning.md)\n", unseen, req.Rig)
	}
	return nil
}

// wordHas reports whether term starts a word of text: it is found there with
// nothing but a non-letter, non-digit (or the start) before it. Both are
// already lower-case, so a whole word and the start of one both match.
func wordHas(text, term string) bool {
	for from := 0; ; {
		at := strings.Index(text[from:], term)
		if at < 0 {
			return false
		}
		at += from
		if at == 0 {
			return true
		}
		if before, _ := utf8.DecodeLastRuneInString(text[:at]); !unicode.IsLetter(before) && !unicode.IsDigit(before) {
			return true
		}
		from = at + 1
	}
}

// matches reports whether every term starts a word of the fact's subject,
// sentence, slug or source.
func (f RigFact) matches(terms []string) bool {
	fields := []string{
		strings.ToLower(f.Subject), strings.ToLower(f.Sentence),
		strings.ToLower(f.Slug), strings.ToLower(f.Source),
	}
	for _, term := range terms {
		found := false
		for _, field := range fields {
			if wordHas(field, term) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// queryLine is a fact as mw memory query prints it: 'slug  status  [subject]
// sentence (source)', then, for a superseded fact, its successor and, for a
// retired one, when and why.
func queryLine(f RigFact) string {
	line := fmt.Sprintf("%s  %s  [%s]  %s (%s)", f.Slug, f.Status, f.Subject, f.Sentence, f.Source)
	switch f.Status {
	case FactSuperseded:
		if f.SupersededBy != "" {
			line += "  superseded by " + f.SupersededBy
		}
	case FactRetired:
		line += "  retired " + f.Retired
		if f.Reason != "" {
			line += ": " + f.Reason
		}
	}
	return line
}

// related is the facts, other than the hits, that share a hit's subject or lie
// along a hit's supersedes chain in either direction.
func related(hits []RigFact, all []RigFact, bySlug map[string]RigFact) []RigFact {
	hit := map[string]bool{}
	subjects := map[string]bool{}
	for _, f := range hits {
		hit[f.Slug] = true
		subjects[f.Subject] = true
	}
	near := map[string]bool{}
	for _, f := range hits {
		for _, next := range []func(RigFact) string{
			func(f RigFact) string { return f.SupersededBy },
			func(f RigFact) string { return f.Supersedes },
		} {
			for at, seen := f, map[string]bool{f.Slug: true}; ; {
				slug := next(at)
				fact, ok := bySlug[slug]
				if slug == "" || !ok || seen[slug] {
					break
				}
				seen[slug] = true
				near[slug] = true
				at = fact
			}
		}
	}
	var out []RigFact
	for _, f := range all {
		if !hit[f.Slug] && (subjects[f.Subject] || near[f.Slug]) {
			out = append(out, f)
		}
	}
	return out
}

// byReading puts the facts a Builder reads first: current, then by subject and slug.
func byReading(facts []RigFact) {
	sort.SliceStable(facts, func(i, j int) bool {
		a, b := facts[i], facts[j]
		if statusRank(a.Status) != statusRank(b.Status) {
			return statusRank(a.Status) < statusRank(b.Status)
		}
		if a.Subject != b.Subject {
			return a.Subject < b.Subject
		}
		return a.Slug < b.Slug
	})
}

// orderedFacts is every fact in the order a Builder reads them.
func orderedFacts(bySlug map[string]RigFact) []RigFact {
	all := make([]RigFact, 0, len(bySlug))
	for _, fact := range bySlug {
		all = append(all, fact)
	}
	byReading(all)
	return all
}

// Query prints the facts of a rig, of every status, in which every term starts
// a word of the subject, sentence, slug or source (case does not matter), then
// the facts related to them, each marked related. It reads files only. It is
// an error when nothing matches.
func (m Memory) Query(ctx context.Context, req MemoryQuery) error {
	terms := make([]string, 0, len(req.Terms))
	for _, term := range req.Terms {
		if term = strings.ToLower(strings.TrimSpace(term)); term != "" {
			terms = append(terms, term)
		}
	}
	if len(terms) == 0 {
		return fmt.Errorf("give at least one term to look for")
	}
	_, _, bySlug, err := m.load(ctx, req.Rig)
	if err != nil {
		return err
	}
	all := orderedFacts(bySlug)
	var hits []RigFact
	for _, fact := range all {
		if fact.matches(terms) {
			hits = append(hits, fact)
		}
	}
	m.logQuery(ctx, req.Rig, terms, hits)
	if len(hits) == 0 {
		return fmt.Errorf("no fact matches %s in the rig %s", strings.Join(terms, " "), req.Rig)
	}
	for _, fact := range hits {
		fmt.Fprintln(m.Out, queryLine(fact))
	}
	for _, fact := range related(hits, all, bySlug) {
		fmt.Fprintln(m.Out, queryLine(fact)+"  related")
	}
	return nil
}

// logQuery records a query in the host's log. A log that cannot be written is
// warned of and does not fail the query.
func (m Memory) logQuery(ctx context.Context, rig string, terms []string, hits []RigFact) {
	if m.Queries == nil {
		return
	}
	seen := map[string]bool{}
	var subjects []string
	for _, fact := range hits {
		if !seen[fact.Subject] {
			seen[fact.Subject] = true
			subjects = append(subjects, fact.Subject)
		}
	}
	sort.Strings(subjects)
	record := MemoryQueryRecord{At: m.clock().UTC(), Rig: rig, Terms: terms, Hits: len(hits), Subjects: subjects, Story: m.Story}
	if err := m.Queries.AppendMemoryQuery(ctx, record); err != nil && m.Warn != nil {
		fmt.Fprintf(m.Warn, "the query was not logged: %v\n", err)
	}
}

// SubjectQueryCounts is how many queries of a rig, at or after since (zero for
// all of them), hit each subject.
func SubjectQueryCounts(records []MemoryQueryRecord, rig string, since time.Time) map[string]int {
	counts := map[string]int{}
	for _, record := range records {
		if record.Rig != rig || record.At.Before(since) {
			continue
		}
		for _, subject := range record.Subjects {
			counts[subject]++
		}
	}
	return counts
}

// TopSubjectsLimit is how many subjects a Builder's boot names.
const TopSubjectsLimit = 10

// TopSubjects names at most n subjects of facts, the most queried first, then
// the one with the most facts, then by name. A subject no fact has is left out
// however often it was queried.
func TopSubjects(facts []RigFact, queries map[string]int, n int) []string {
	held := map[string]int{}
	for _, fact := range facts {
		held[fact.Subject]++
	}
	subjects := make([]string, 0, len(held))
	for subject := range held {
		subjects = append(subjects, subject)
	}
	sort.Slice(subjects, func(i, j int) bool {
		a, b := subjects[i], subjects[j]
		switch {
		case queries[a] != queries[b]:
			return queries[a] > queries[b]
		case held[a] != held[b]:
			return held[a] > held[b]
		}
		return a < b
	})
	if len(subjects) > n {
		subjects = subjects[:n]
	}
	return subjects
}

// MemoryStats is the request mw memory stats makes. An empty Since is a week.
type MemoryStats struct {
	Rig, Since string
}

// Stats prints how often a rig's memory was queried since a time, how many of
// the queries found nothing, and the subjects they hit, most first.
func (m Memory) Stats(ctx context.Context, req MemoryStats) error {
	if m.Queries == nil {
		return fmt.Errorf("there is no query log on this host")
	}
	if _, _, _, err := m.load(ctx, req.Rig); err != nil {
		return err
	}
	text := req.Since
	if text == "" {
		text = "7d"
	}
	from, err := ParseReportSince(text, m.clock())
	if err != nil {
		return err
	}
	records, err := m.Queries.ReadMemoryQueries(ctx)
	if err != nil {
		return err
	}
	queries, empty := 0, 0
	for _, record := range records {
		if record.Rig != req.Rig || record.At.Before(from) {
			continue
		}
		queries++
		if record.Hits == 0 {
			empty++
		}
	}
	noun := "queries"
	if queries == 1 {
		noun = "query"
	}
	fmt.Fprintf(m.Out, "%s: %d %s since %s, %d found nothing\n", req.Rig, queries, noun, from.Format(LedgerDate), empty)
	counts := SubjectQueryCounts(records, req.Rig, from)
	subjects := make([]string, 0, len(counts))
	for subject := range counts {
		subjects = append(subjects, subject)
	}
	sort.Slice(subjects, func(i, j int) bool {
		if counts[subjects[i]] != counts[subjects[j]] {
			return counts[subjects[i]] > counts[subjects[j]]
		}
		return subjects[i] < subjects[j]
	})
	for i, subject := range subjects {
		if i == 0 {
			fmt.Fprintln(m.Out, "subjects queried, most first:")
		}
		fmt.Fprintf(m.Out, "  %3d  %s\n", counts[subject], subject)
	}
	return nil
}

// MakeRoomKeepDays is how recent a fact's since may be for make-room to leave it
// in the boot render: a fact placed within this many days is not demoted.
const MakeRoomKeepDays = 2

// makeRoomQuestionWords is how many words of a fact's sentence make the eval
// question that finds it once it is demoted.
const makeRoomQuestionWords = 8

// MemoryMakeRoom is the request mw memory make-room makes: demote boot facts
// until the render has Bytes free under the budget. DryRun prints the plan and
// writes nothing.
type MemoryMakeRoom struct {
	Rig    string
	Bytes  int
	DryRun bool
}

// roomMove is one demotion make-room has planned, with the question that will
// find the fact in eval.md.
type roomMove struct {
	slug, question string
}

// MakeRoom demotes current boot facts, one by one, until the render has Bytes
// free under the budget. It chooses by a fixed rule: a gotcha, never a decision;
// never a sentence beginning 'His ' or 'Decided by the Governor'; never one with
// a since within MakeRoomKeepDays days; the oldest since first, then the subject
// least queried (as stats counts), then by slug. Each goes through Demote with
// the first words of its own sentence as the question; a fact that question
// would not put in the top EvalTop results is skipped for the next. When the
// facts that qualify cannot free enough, nothing is changed and it is an error.
func (m Memory) MakeRoom(ctx context.Context, req MemoryMakeRoom) error {
	if req.Bytes < 0 {
		return fmt.Errorf("--bytes must be 0 or more")
	}
	return m.makeRoom(ctx, req.Rig, req.Bytes, nil, req.DryRun)
}

// makeRoom is MakeRoom; extra is a fact about to be added, which the questions
// must still find their facts beside once it is there.
func (m Memory) makeRoom(ctx context.Context, rig string, bytes int, extra *RigFact, dryRun bool) error {
	if m.Budget <= 0 {
		return fmt.Errorf("there is no boot render budget to make room under")
	}
	about, files, facts, err := m.load(ctx, rig)
	if err != nil {
		return err
	}
	all, _ := LoadRigFacts(files)
	size := len(RenderRigMemory(about, all))
	if m.Budget-size >= bytes {
		fmt.Fprintf(m.Out, "render %d/%d bytes, %d free: nothing to demote\n", size, m.Budget, m.Budget-size)
		return nil
	}
	moves, skipped, after := m.planRoom(ctx, rig, about, all, facts, extra, bytes)
	for _, slug := range skipped {
		fmt.Fprintf(m.Out, "skipped %s: the first %d words of its sentence do not put it in the top %d of an eval question\n", slug, makeRoomQuestionWords, EvalTop)
	}
	if m.Budget-after < bytes {
		return fmt.Errorf("make-room can leave %d bytes free at most, not the %d asked (render %d/%d now): no more gotcha qualifies to demote; nothing was changed",
			m.Budget-after, bytes, size, m.Budget)
	}
	fmt.Fprintf(m.Out, "render before: %d/%d bytes\n", size, m.Budget)
	for _, move := range moves {
		if dryRun {
			fmt.Fprintf(m.Out, "would demote %s  (Q: %s)\n", move.slug, move.question)
			continue
		}
		if err := m.Demote(ctx, rig, move.slug, move.question); err != nil {
			return err
		}
	}
	fmt.Fprintf(m.Out, "render after: %d/%d bytes\n", after, m.Budget)
	return nil
}

// planRoom chooses the demotions that free bytes, without writing anything, and
// returns them, the facts it passed over because their own question would not
// find them, and the render size once the demotions are made.
func (m Memory) planRoom(ctx context.Context, rig, about string, all []RigFact, facts map[string]RigFact, extra *RigFact, bytes int) (moves []roomMove, skipped []string, size int) {
	counts := map[string]int{}
	if m.Queries != nil {
		if records, err := m.Queries.ReadMemoryQueries(ctx); err == nil {
			counts = SubjectQueryCounts(records, rig, m.clock().UTC().AddDate(0, 0, -7))
		}
	}
	// Questions are checked against every fact, the one about to be added too: a
	// demotion changes which facts are read at boot, never what a query finds.
	known := facts
	if extra != nil {
		known = withFact(facts, *extra)
	}
	queried := orderedFacts(known)

	var candidates []RigFact
	for _, fact := range all {
		if m.roomEligible(fact) {
			candidates = append(candidates, fact)
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		a, b := candidates[i], candidates[j]
		switch {
		case a.Since != b.Since:
			return a.Since < b.Since
		case counts[a.Subject] != counts[b.Subject]:
			return counts[a.Subject] < counts[b.Subject]
		}
		return a.Slug < b.Slug
	})

	working := append([]RigFact(nil), all...)
	size = len(RenderRigMemory(about, working))
	for _, fact := range candidates {
		if m.Budget-size >= bytes {
			break
		}
		question := firstWords(fact.Sentence, makeRoomQuestionWords)
		q := EvalQuestion{Question: question}
		if len(q.Terms()) == 0 || !q.finds(queried, fact.Slug) {
			skipped = append(skipped, fact.Slug)
			continue
		}
		for i := range working {
			if working[i].Slug == fact.Slug {
				working[i].Unbooted = true
			}
		}
		size = len(RenderRigMemory(about, working))
		moves = append(moves, roomMove{slug: fact.Slug, question: question})
	}
	return moves, skipped, size
}

// roomEligible reports whether make-room may demote a fact: a current gotcha in
// the boot render that is not the Governor's own word and was not placed within
// MakeRoomKeepDays days.
func (m Memory) roomEligible(fact RigFact) bool {
	if fact.Status != FactCurrent || fact.Unbooted || fact.Kind != FactGotcha {
		return false
	}
	if strings.HasPrefix(fact.Sentence, "His ") || strings.HasPrefix(fact.Sentence, "Decided by the Governor") {
		return false
	}
	placed, err := time.Parse(FactDateLayout, fact.Since)
	if err != nil {
		return false
	}
	newest := m.clock().UTC().AddDate(0, 0, -MakeRoomKeepDays).Format(FactDateLayout)
	return placed.Format(FactDateLayout) < newest
}
