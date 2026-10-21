package application

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	pathpkg "path"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/domain"
)

// ErrorRegisterDir is the folder of a seat that holds the common-errors
// register: one file a class (<slug>.md) and the kinds file. It is read by
// need, never at a session's boot (mw-gq6.420).
const ErrorRegisterDir = "common-errors"

// ErrorKindsFile is the register's file of what kinds each rig is: a class is
// checked in every rig of its kinds.
const ErrorKindsFile = "kinds.toml"

// ErrorRegisterTemplateDir is the Builder's register as the template a fresh
// vault is born from holds it: the seed classes.
const ErrorRegisterTemplateDir = SeatsDir + "/builder/" + ErrorRegisterDir

// SharedComponentAsk is the short form of SharedComponentQuestion, which mw
// status shows under a class found twice.
const SharedComponentAsk = "shared component?"

// SharedComponentQuestion is what a class found twice asks the Mayor to weigh.
// It is never answered for him and nothing is filed for it.
const SharedComponentQuestion = SharedComponentAsk + " would one shared component carry the fix to every rig of its kinds"

// ErrorFiles is the port the common-errors register is kept through: the
// register folder of a seat in the vault. Nothing is ever deleted through it.
type ErrorFiles interface {
	// ReadErrorRegister reads the seat's register: the text of every class
	// file by slug, and the text of its kinds file. A seat with no register,
	// or a register with no kinds file, has none, which is not an error.
	ReadErrorRegister(ctx context.Context, seat string) (classes map[string]string, kinds string, err error)

	// WriteErrorClass writes the file of one class, making the register's
	// folder if it is not there, and reports where it landed.
	WriteErrorClass(ctx context.Context, seat, slug, text string) (string, error)

	// WriteErrorKinds writes the register's kinds file and reports where.
	WriteErrorKinds(ctx context.Context, seat, text string) (string, error)
}

// ErrorClass is one class of error in the register: what is seen, why, the
// check over the whole class, the kinds of rig it can occur in, the shared
// component verdict and every rig it was checked in. Its file is a front
// matter of typed keys, then the checks one a line.
type ErrorClass struct {
	Slug    string
	Symptom string
	Cause   string
	Sweep   string
	Kinds   []string
	// Shared is the shared-component verdict: none, candidate or exists
	// <where>; empty while the Mayor has not weighed it.
	Shared string
	Since  string // YYYY-MM-DD
	Checks []ErrorCheck
}

// ErrorCheck is one rig checked for a class: when, and the bead of what was
// found there, or no bead when the rig was clean.
type ErrorCheck struct {
	Date  string
	Rig   string
	Found string
}

// Result is the check's result as mw errors checked takes it: ok or found:<bead>.
func (c ErrorCheck) Result() string {
	if c.Found == "" {
		return "ok"
	}
	return "found:" + c.Found
}

const (
	errorSymptom = "symptom"
	errorCause   = "cause"
	errorSweep   = "sweep"
	errorKinds   = "kinds"
	errorShared  = "shared"
	errorSince   = "since"

	errorChecksHeading = "## Checked"
)

var (
	errorBead      = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*-[A-Za-z0-9._-]+$`)
	errorCheckLine = regexp.MustCompile(`^-\s+(\S+)\s+(\S+)\s+(\S+)$`)
)

// ParseErrorClass reads one class file. It refuses what is not a whole class:
// no symptom, no kinds, no since, an unknown key, a verdict or a check line it
// cannot read.
func ParseErrorClass(slug, text string) (ErrorClass, error) {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != frontMatterFence {
		return ErrorClass{}, fmt.Errorf("no front matter: the file must open with %s", frontMatterFence)
	}
	end := slices.IndexFunc(lines[1:], func(line string) bool { return strings.TrimSpace(line) == frontMatterFence })
	if end < 0 {
		return ErrorClass{}, fmt.Errorf("the front matter is never closed with %s", frontMatterFence)
	}
	end++

	class := ErrorClass{Slug: slug}
	seen := map[string]bool{}
	for _, line := range lines[1:end] {
		if strings.TrimSpace(line) == "" {
			continue
		}
		key, raw, ok := strings.Cut(line, ":")
		key = strings.TrimSpace(key)
		if !ok || key == "" {
			return ErrorClass{}, fmt.Errorf("front matter line %q is not key: value", line)
		}
		if seen[key] {
			return ErrorClass{}, fmt.Errorf("the key %q is given twice", key)
		}
		seen[key] = true
		value, err := factValue(raw)
		if err != nil {
			return ErrorClass{}, fmt.Errorf("the value of %q: %w", key, err)
		}
		switch key {
		case errorSymptom:
			class.Symptom = value
		case errorCause:
			class.Cause = value
		case errorSweep:
			class.Sweep = value
		case errorKinds:
			class.Kinds = splitKinds(value)
		case errorShared:
			class.Shared = value
		case errorSince:
			class.Since = value
		default:
			return ErrorClass{}, fmt.Errorf("unknown key %q", key)
		}
	}

	for _, line := range lines[end+1:] {
		line = strings.TrimSpace(line)
		if line == "" || line == errorChecksHeading {
			continue
		}
		m := errorCheckLine.FindStringSubmatch(line)
		if m == nil {
			return ErrorClass{}, fmt.Errorf("the check line %q is not '- <date> <rig> ok|found:<bead>'", line)
		}
		check := ErrorCheck{Date: m[1], Rig: m[2]}
		found, err := checkResult(m[3])
		if err != nil {
			return ErrorClass{}, fmt.Errorf("the check line %q: %w", line, err)
		}
		check.Found = found
		class.Checks = append(class.Checks, check)
	}

	if err := class.validate(); err != nil {
		return ErrorClass{}, err
	}
	return class, nil
}

// validate is what makes a class whole.
func (c ErrorClass) validate() error {
	if err := checkSlug(c.Slug); err != nil {
		return err
	}
	if strings.TrimSpace(c.Symptom) == "" {
		return fmt.Errorf("the key %q is missing", errorSymptom)
	}
	for _, one := range [][2]string{{errorSymptom, c.Symptom}, {errorCause, c.Cause}, {errorSweep, c.Sweep}, {errorShared, c.Shared}} {
		if strings.ContainsAny(one[1], "\r\n") {
			return fmt.Errorf("%s must be one line, with no newline", one[0])
		}
	}
	if err := checkKinds(c.Kinds); err != nil {
		return err
	}
	if err := checkVerdict(c.Shared); err != nil {
		return err
	}
	if _, err := time.Parse(FactDateLayout, c.Since); err != nil {
		return fmt.Errorf("since %q is not a YYYY-MM-DD date", c.Since)
	}
	for _, check := range c.Checks {
		if _, err := time.Parse(FactDateLayout, check.Date); err != nil {
			return fmt.Errorf("the check of %s: %q is not a YYYY-MM-DD date", check.Rig, check.Date)
		}
		if err := safeRig(check.Rig); err != nil {
			return err
		}
	}
	return nil
}

// String is the class as its file holds it. ParseErrorClass reads it back.
func (c ErrorClass) String() string {
	var b strings.Builder
	b.WriteString(frontMatterFence + "\n")
	for _, pair := range [][2]string{
		{errorSymptom, c.Symptom}, {errorCause, c.Cause}, {errorSweep, c.Sweep},
		{errorKinds, strings.Join(c.Kinds, ", ")}, {errorShared, c.Shared}, {errorSince, c.Since},
	} {
		if pair[1] != "" {
			fmt.Fprintf(&b, "%s: %s\n", pair[0], writeFactValue(pair[1]))
		}
	}
	b.WriteString(frontMatterFence + "\n")
	if len(c.Checks) > 0 {
		b.WriteString("\n" + errorChecksHeading + "\n\n")
		for _, check := range c.Checks {
			fmt.Fprintf(&b, "- %s %s %s\n", check.Date, check.Rig, check.Result())
		}
	}
	return b.String()
}

// Occurrences are the checks that found the class, one a bead, in the order
// they were recorded.
func (c ErrorClass) Occurrences() []ErrorCheck {
	var found []ErrorCheck
	seen := map[string]bool{}
	for _, check := range c.Checks {
		if check.Found != "" && !seen[check.Found] {
			seen[check.Found] = true
			found = append(found, check)
		}
	}
	return found
}

// AsksSharedQuestion reports whether the class is found twice or more and the
// Mayor has not yet given its shared-component verdict.
func (c ErrorClass) AsksSharedQuestion() bool {
	return len(c.Occurrences()) >= 2 && c.Shared == ""
}

// seenLine is how often the class was found, and where.
func (c ErrorClass) seenLine() string {
	var where []string
	for _, check := range c.Occurrences() {
		where = append(where, check.Found+" in "+check.Rig)
	}
	return fmt.Sprintf("%s is seen %d times (%s)", c.Slug, len(where), strings.Join(where, ", "))
}

// questionLine is the question for the Mayor, with the command that records
// his verdict.
func (c ErrorClass) questionLine() string {
	return fmt.Sprintf("%s: %s? weigh it, then: mw errors shared %s none|candidate|'exists <where>'",
		c.seenLine(), SharedComponentQuestion, c.Slug)
}

// Unchecked are the rigs of the class's kinds that hold no check of it, by name.
func (c ErrorClass) Unchecked(kinds RigKinds) []string {
	checked := map[string]bool{}
	for _, check := range c.Checks {
		checked[check.Rig] = true
	}
	var rigs []string
	for _, rig := range kinds.Rigs() {
		if checked[rig] {
			continue
		}
		for _, kind := range kinds[rig] {
			if slices.Contains(c.Kinds, kind) {
				rigs = append(rigs, rig)
				break
			}
		}
	}
	return rigs
}

// UncheckedClass is a class and the rigs of its kinds not yet checked for it.
type UncheckedClass struct {
	Class ErrorClass
	Rigs  []string
}

// Lines are the class as mw status lists it under UncheckedErrorsHeading: the
// rigs not yet checked, and, for a class found twice with no verdict, the
// shared-component question in short.
func (u UncheckedClass) Lines() []string {
	lines := []string{fmt.Sprintf("%s: not checked in %s", u.Class.Slug, strings.Join(u.Rigs, ", "))}
	if u.Class.AsksSharedQuestion() {
		lines = append(lines, fmt.Sprintf("  seen %d times: %s", len(u.Class.Occurrences()), SharedComponentAsk))
	}
	return lines
}

// UncheckedClasses are the classes not yet checked in every rig of their
// kinds, in slug order: the register's standing sweep, which mw status shows.
func UncheckedClasses(classes []ErrorClass, kinds RigKinds) []UncheckedClass {
	var unchecked []UncheckedClass
	for _, class := range classes {
		if rigs := class.Unchecked(kinds); len(rigs) > 0 {
			unchecked = append(unchecked, UncheckedClass{Class: class, Rigs: rigs})
		}
	}
	sort.SliceStable(unchecked, func(i, j int) bool { return unchecked[i].Class.Slug < unchecked[j].Class.Slug })
	return unchecked
}

// LoadErrorClasses parses the class files of a register, by slug. A file that
// is not a whole class is left out and named, with why, in the second result.
// Both results are in slug order.
func LoadErrorClasses(files map[string]string) (classes []ErrorClass, skipped []string) {
	slugs := make([]string, 0, len(files))
	for slug := range files {
		slugs = append(slugs, slug)
	}
	sort.Strings(slugs)
	for _, slug := range slugs {
		class, err := ParseErrorClass(slug, files[slug])
		if err != nil {
			skipped = append(skipped, fmt.Sprintf("%s: %v", slug, err))
			continue
		}
		classes = append(classes, class)
	}
	return classes, skipped
}

// RigKinds is what kinds each rig is (pwa, go-cli, grind...), as the register's
// kinds file holds it: `<rig> = ["<kind>", ...]` a line.
type RigKinds map[string][]string

// ParseRigKinds reads the kinds file: a line a rig, blank lines and # comments
// skipped.
func ParseRigKinds(text string) (RigKinds, error) {
	kinds := RigKinds{}
	for i, raw := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		rig, value, ok := strings.Cut(line, "=")
		rig, value = strings.TrimSpace(rig), strings.TrimSpace(value)
		if !ok || !strings.HasPrefix(value, "[") || !strings.HasSuffix(value, "]") {
			return nil, fmt.Errorf("line %d: expected `<rig> = [\"<kind>\", ...]`, got %q", i+1, raw)
		}
		if err := safeRig(rig); err != nil {
			return nil, fmt.Errorf("line %d: %w", i+1, err)
		}
		var list []string
		for _, item := range strings.Split(strings.Trim(value, "[]"), ",") {
			item = strings.TrimSpace(item)
			if item == "" {
				continue
			}
			kind, err := strconv.Unquote(item)
			if err != nil {
				return nil, fmt.Errorf("line %d: %s is not a quoted kind", i+1, item)
			}
			list = append(list, kind)
		}
		if err := checkKinds(list); err != nil {
			return nil, fmt.Errorf("line %d: %w", i+1, err)
		}
		kinds[rig] = list
	}
	return kinds, nil
}

// Rigs are the rigs named, by name.
func (k RigKinds) Rigs() []string {
	rigs := make([]string, 0, len(k))
	for rig := range k {
		rigs = append(rigs, rig)
	}
	sort.Strings(rigs)
	return rigs
}

// String is the kinds file. ParseRigKinds reads it back.
func (k RigKinds) String() string {
	var b strings.Builder
	b.WriteString("# The kinds of each rig, for the common-errors register: a class is checked in every rig of its kinds.\n")
	b.WriteString("# Kept with mw errors kinds <rig> <kind>...\n")
	for _, rig := range k.Rigs() {
		quoted := make([]string, len(k[rig]))
		for i, kind := range k[rig] {
			quoted[i] = strconv.Quote(kind)
		}
		fmt.Fprintf(&b, "%s = [%s]\n", rig, strings.Join(quoted, ", "))
	}
	return b.String()
}

// splitKinds reads kinds written with commas, spaces or both.
func splitKinds(text string) []string {
	return strings.FieldsFunc(text, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' })
}

// checkKinds refuses no kinds, or a kind that is not a slug.
func checkKinds(kinds []string) error {
	if len(kinds) == 0 {
		return fmt.Errorf("the key %q is missing: name the kinds of rig the class can occur in (pwa, go-cli, grind...)", errorKinds)
	}
	for _, kind := range kinds {
		if err := checkSlug(kind); err != nil {
			return fmt.Errorf("the kind %q may hold only a-z, 0-9 and hyphens", kind)
		}
	}
	return nil
}

// checkVerdict refuses a shared-component verdict that is not none, candidate
// or exists <where>. Empty is the verdict not yet given.
func checkVerdict(verdict string) error {
	switch {
	case verdict == "", verdict == "none", verdict == "candidate":
		return nil
	case strings.HasPrefix(verdict, "exists ") && strings.TrimSpace(strings.TrimPrefix(verdict, "exists ")) != "":
		return nil
	}
	return fmt.Errorf("the shared-component verdict %q is not none, candidate or exists <where>", verdict)
}

// checkResult reads a check's result: ok, or found:<bead>.
func checkResult(result string) (found string, err error) {
	if result == "ok" {
		return "", nil
	}
	if bead, ok := strings.CutPrefix(result, "found:"); ok && errorBead.MatchString(bead) {
		return bead, nil
	}
	return "", fmt.Errorf("the result %q is not ok or found:<bead>", result)
}

// safeRig refuses a rig name that is not a slug.
func safeRig(rig string) error {
	if err := checkSlug(rig); err != nil {
		return fmt.Errorf("the rig %q may hold only a-z, 0-9 and hyphens", rig)
	}
	return nil
}

// RecordVault is what a verb that writes tracked vault files commits and
// pushes them through; VaultFiles' Commit and Push, narrowed. A verb given none
// leaves its files for a person to commit.
type RecordVault interface {
	Commit(ctx context.Context, message string, paths []string) ([]string, error)
	Push(ctx context.Context) (int, error)
}

// ErrVaultNoUpstream is what VaultFiles' Push says of a vault whose branch
// tracks no remote branch: nothing to push to, which is not a push that failed.
var ErrVaultNoUpstream = errors.New("the vault's branch tracks no remote branch")

// VaultCommitBound bounds a commit that is made once its files are written: long
// enough for a local git commit on a loaded box, short enough that a git stuck on
// a lock does not hold a stopped mw for ever.
const VaultCommitBound = 2 * time.Minute

// vaultCommitter is what commitWritten commits through; VaultFiles' Commit, narrowed.
type vaultCommitter interface {
	Commit(ctx context.Context, message string, paths []string) ([]string, error)
}

// commitWritten commits paths of the vault that were just written, and the
// commit runs to its end even when ctx has ended (mw told to stop, a caller's
// timeout): a file written and not committed is a tracked change every host's
// dispatch refuses on until someone commits it, and killing git part-way
// through leaves the index locked (mw-gq6.462, mw-gq6.502). Every write to the
// vault is followed by this, in the same step, rather than by a Commit on the
// caller's ctx.
func commitWritten(ctx context.Context, vault vaultCommitter, message string, paths []string) ([]string, error) {
	commit, cancel := context.WithTimeout(context.WithoutCancel(ctx), VaultCommitBound)
	defer cancel()
	return vault.Commit(commit, message, paths)
}

// recordInVault commits exactly the files a verb wrote, under a message that
// names the command and its subject, and pushes. A vault that holds someone
// else's uncommitted change is not swept into the commit: only the paths are.
// A commit that fails is an error, the files being written and not recorded; so
// is a push that fails after Push has rebased onto the remote and tried again
// (mw-gq6.583), the commit being kept for the next sync to carry. A vault that
// tracks no remote has nothing to push to: that is said on warn and is no error
// (mw-gq6.501).
func recordInVault(ctx context.Context, vault RecordVault, warn io.Writer, message string, paths []string) error {
	if vault == nil {
		return nil
	}
	recorded, err := commitWritten(ctx, vault, message, paths)
	if err != nil {
		return fmt.Errorf("%s: the files are written but not committed in the vault: %w", message, err)
	}
	if len(recorded) == 0 {
		return nil
	}
	if _, err := vault.Push(ctx); err != nil {
		said := fmt.Errorf("%s: committed in the vault, not pushed (mw sync carries it): %w", message, err)
		if errors.Is(err, ErrVaultNoUpstream) {
			if warn != nil {
				fmt.Fprintln(warn, said)
			}
			return nil
		}
		return said
	}
	return nil
}

// Errors is mw errors: the Mayor and the seats keep the common-errors
// register. Each verb writes files and spends no fuel, files no bead and sends
// no mail; none deletes a class. With a Vault it commits the files it wrote,
// those alone, and pushes them.
type Errors struct {
	Files ErrorFiles
	Seat  string
	// Vault commits and pushes what a verb wrote; nil leaves that to a person.
	// Warn is where a push that did not go is said.
	Vault RecordVault
	Warn  io.Writer
	// Seed is the register a fresh vault is born with, its class files at its
	// root: what seed writes into a register that lacks them.
	Seed fs.FS
	Now  func() time.Time
	Out  io.Writer
}

// ErrorAdd is the request mw errors add makes. Found, when given, is the
// first find, <rig>:<bead>.
type ErrorAdd struct {
	Slug, Symptom, Cause, Sweep, Shared, Found string
	Kinds                                      []string
}

func (e Errors) today() string {
	now := time.Now
	if e.Now != nil {
		now = e.Now
	}
	return now().UTC().Format(FactDateLayout)
}

// load reads the register: its whole classes by slug, the text of every file
// by slug, and the kinds.
func (e Errors) load(ctx context.Context) (classes map[string]ErrorClass, files map[string]string, kinds RigKinds, err error) {
	files, kindsText, err := e.Files.ReadErrorRegister(ctx, e.Seat)
	if err != nil {
		return nil, nil, nil, err
	}
	if kinds, err = ParseRigKinds(kindsText); err != nil {
		return nil, nil, nil, fmt.Errorf("the register's %s: %w", ErrorKindsFile, err)
	}
	classes = map[string]ErrorClass{}
	list, _ := LoadErrorClasses(files)
	for _, class := range list {
		classes[class.Slug] = class
	}
	return classes, files, kinds, nil
}

// existingClass finds the class a verb acts on, refusing a slug that is not
// there or not whole.
func existingClass(slug string, files map[string]string, classes map[string]ErrorClass) (ErrorClass, error) {
	if class, ok := classes[slug]; ok {
		return class, nil
	}
	if text, ok := files[slug]; ok {
		_, err := ParseErrorClass(slug, text)
		return ErrorClass{}, fmt.Errorf("the class %q is not whole: %v", slug, err)
	}
	return ErrorClass{}, fmt.Errorf("no class %q in the register (mw errors list names them)", slug)
}

// put writes a class and reports its path from the vault's root.
func (e Errors) put(ctx context.Context, class ErrorClass) (string, error) {
	if err := class.validate(); err != nil {
		return "", err
	}
	path, err := e.Files.WriteErrorClass(ctx, e.Seat, class.Slug, class.String())
	if err != nil {
		return "", err
	}
	fmt.Fprintln(e.Out, path)
	return pathpkg.Join(SeatsDir, e.Seat, ErrorRegisterDir, class.Slug+MemoryExt), nil
}

// record commits what the verb wrote.
func (e Errors) record(ctx context.Context, subject string, paths ...string) error {
	return recordInVault(ctx, e.Vault, e.Warn, "mw errors "+subject, paths)
}

// Add writes a new class. It refuses a slug already in the register.
func (e Errors) Add(ctx context.Context, req ErrorAdd) error {
	class := ErrorClass{
		Slug: req.Slug, Symptom: strings.TrimSpace(req.Symptom), Cause: strings.TrimSpace(req.Cause),
		Sweep: strings.TrimSpace(req.Sweep), Kinds: req.Kinds, Shared: strings.TrimSpace(req.Shared), Since: e.today(),
	}
	if req.Found != "" {
		rig, bead, ok := strings.Cut(req.Found, ":")
		if !ok {
			return fmt.Errorf("--found %q is not <rig>:<bead>", req.Found)
		}
		found, err := checkResult("found:" + bead)
		if err != nil {
			return fmt.Errorf("--found %q: %w", req.Found, err)
		}
		class.Checks = []ErrorCheck{{Date: class.Since, Rig: rig, Found: found}}
	}
	if err := class.validate(); err != nil {
		return err
	}
	_, files, _, err := e.load(ctx)
	if err != nil {
		return err
	}
	if _, taken := files[req.Slug]; taken {
		return fmt.Errorf("the class %q is already in the register: record a new find with mw errors checked %s <rig> found:<bead>", req.Slug, req.Slug)
	}
	written, err := e.put(ctx, class)
	if err != nil {
		return err
	}
	return e.record(ctx, "add "+class.Slug, written)
}

// Checked records that a rig was checked for a class, dated today. A find
// (found:<bead>) in a rig that shares none of the class's kinds adds the rig's
// first kind (kinds.toml) to the class and says so; a find in a rig that shares
// a kind, and an ok, change no kinds (mw-gq6.635). When that find makes the
// class seen twice, it says so and asks the shared-component question. It files
// nothing.
func (e Errors) Checked(ctx context.Context, slug, rig, result string) error {
	if err := safeRig(rig); err != nil {
		return err
	}
	found, err := checkResult(result)
	if err != nil {
		return err
	}
	classes, files, kinds, err := e.load(ctx)
	if err != nil {
		return err
	}
	class, err := existingClass(slug, files, classes)
	if err != nil {
		return err
	}
	before := len(class.Occurrences())
	class.Checks = append(class.Checks, ErrorCheck{Date: e.today(), Rig: rig, Found: found})
	var added []string
	if found != "" && len(kinds[rig]) > 0 {
		shares := false
		for _, kind := range kinds[rig] {
			shares = shares || slices.Contains(class.Kinds, kind)
		}
		if !shares {
			class.Kinds = append(class.Kinds, kinds[rig][0])
			added = append(added, kinds[rig][0])
		}
	}
	written, err := e.put(ctx, class)
	if err != nil {
		return err
	}
	if len(added) > 0 {
		fmt.Fprintf(e.Out, "added kinds to %s: %s (the class was found in %s)\n", slug, strings.Join(added, ", "), rig)
	}
	if err := e.record(ctx, fmt.Sprintf("checked %s %s %s", slug, rig, result), written); err != nil {
		return err
	}
	if after := len(class.Occurrences()); after > before && after >= 2 {
		// Said, never filed: the Mayor weighs it (mw-gq6.420).
		if class.Shared == "" {
			fmt.Fprintln(e.Out, class.questionLine())
		} else {
			fmt.Fprintf(e.Out, "%s; shared component: %s\n", class.seenLine(), class.Shared)
		}
	}
	return nil
}

// Shared records the Mayor's shared-component verdict on a class.
func (e Errors) Shared(ctx context.Context, slug, verdict string) error {
	verdict = strings.Join(strings.Fields(verdict), " ")
	if verdict == "" {
		return fmt.Errorf("give a verdict: none, candidate or exists <where>")
	}
	if err := checkVerdict(verdict); err != nil {
		return err
	}
	classes, files, _, err := e.load(ctx)
	if err != nil {
		return err
	}
	class, err := existingClass(slug, files, classes)
	if err != nil {
		return err
	}
	class.Shared = verdict
	written, err := e.put(ctx, class)
	if err != nil {
		return err
	}
	return e.record(ctx, "shared "+slug, written)
}

// List prints one line a class, by slug: its kinds, how often it was found,
// the rigs of its kinds not yet checked, its verdict, its symptom, and the
// shared-component question for a class found twice that has no verdict. A
// file that is not a whole class is named last.
func (e Errors) List(ctx context.Context) error {
	files, kindsText, err := e.Files.ReadErrorRegister(ctx, e.Seat)
	if err != nil {
		return err
	}
	kinds, err := ParseRigKinds(kindsText)
	if err != nil {
		return fmt.Errorf("the register's %s: %w", ErrorKindsFile, err)
	}
	classes, skipped := LoadErrorClasses(files)
	for _, class := range classes {
		fmt.Fprintln(e.Out, listLine(class, kinds))
	}
	for _, bad := range skipped {
		fmt.Fprintf(e.Out, "NOT WHOLE: %s\n", bad)
	}
	if len(classes) == 0 && len(skipped) == 0 {
		fmt.Fprintln(e.Out, "the register is empty")
	}
	return nil
}

func listLine(class ErrorClass, kinds RigKinds) string {
	parts := []string{class.Slug, "[" + strings.Join(class.Kinds, ", ") + "]", fmt.Sprintf("seen %d", len(class.Occurrences()))}
	if rigs := class.Unchecked(kinds); len(rigs) > 0 {
		parts = append(parts, "unchecked: "+strings.Join(rigs, ", "))
	}
	if class.Shared != "" {
		parts = append(parts, "shared: "+class.Shared)
	}
	line := strings.Join(parts, "  ") + "  " + class.Symptom
	if class.AsksSharedQuestion() {
		line += "  — " + SharedComponentQuestion + "?"
	}
	return line
}

// Show prints a class's file, then what is derived from it: the rigs of its
// kinds not yet checked, and the shared-component question when it asks it.
func (e Errors) Show(ctx context.Context, slug string) error {
	classes, files, kinds, err := e.load(ctx)
	if err != nil {
		return err
	}
	class, err := existingClass(slug, files, classes)
	if err != nil {
		return err
	}
	fmt.Fprint(e.Out, class.String())
	fmt.Fprintln(e.Out)
	if rigs := class.Unchecked(kinds); len(rigs) > 0 {
		fmt.Fprintf(e.Out, "not checked in: %s\n", strings.Join(rigs, ", "))
	}
	if class.AsksSharedQuestion() {
		fmt.Fprintln(e.Out, class.questionLine())
	}
	return nil
}

// Kinds sets the kinds of a rig, keeping every other rig's. With no kinds it
// prints the rig's; with no rig it prints every rig's.
func (e Errors) Kinds(ctx context.Context, rig string, kinds []string) error {
	_, kindsText, err := e.Files.ReadErrorRegister(ctx, e.Seat)
	if err != nil {
		return err
	}
	all, err := ParseRigKinds(kindsText)
	if err != nil {
		return fmt.Errorf("the register's %s: %w", ErrorKindsFile, err)
	}
	if rig == "" || len(kinds) == 0 {
		for _, name := range all.Rigs() {
			if rig == "" || name == rig {
				fmt.Fprintf(e.Out, "%s: %s\n", name, strings.Join(all[name], ", "))
			}
		}
		return nil
	}
	if err := safeRig(rig); err != nil {
		return err
	}
	var list []string
	for _, kind := range kinds {
		list = append(list, splitKinds(kind)...)
	}
	if err := checkKinds(list); err != nil {
		return err
	}
	all[rig] = list
	path, err := e.Files.WriteErrorKinds(ctx, e.Seat, all.String())
	if err != nil {
		return err
	}
	fmt.Fprintln(e.Out, path)
	return e.record(ctx, "kinds "+rig, pathpkg.Join(SeatsDir, e.Seat, ErrorRegisterDir, ErrorKindsFile))
}

// SeedRegister writes each seed class the register does not hold yet, and
// never one it does: run once on a vault born before the register was.
func (e Errors) SeedRegister(ctx context.Context) error {
	if e.Seed == nil {
		return fmt.Errorf("this mw holds no seed register")
	}
	entries, err := fs.ReadDir(e.Seed, ".")
	if err != nil {
		return fmt.Errorf("reading the seed register: %w", err)
	}
	_, files, _, err := e.load(ctx)
	if err != nil {
		return err
	}
	var written []string
	for _, entry := range entries {
		slug, isClass := strings.CutSuffix(entry.Name(), MemoryExt)
		if entry.IsDir() || !isClass {
			continue
		}
		if _, there := files[slug]; there {
			fmt.Fprintf(e.Out, "kept %s: already in the register\n", slug)
			continue
		}
		text, err := fs.ReadFile(e.Seed, entry.Name())
		if err != nil {
			return fmt.Errorf("reading the seed %s: %w", slug, err)
		}
		class, err := ParseErrorClass(slug, string(text))
		if err != nil {
			return fmt.Errorf("the seed %s is not whole: %w", slug, err)
		}
		path, err := e.put(ctx, class)
		if err != nil {
			return err
		}
		written = append(written, path)
	}
	if len(written) == 0 {
		return nil
	}
	return e.record(ctx, "seed", written...)
}

// CommonErrorsMarker heads the section of a bug's closing comment that names
// the class of its fault, beside 'For the rig memory:'. The Mayor places it.
const CommonErrorsMarker = "For common errors:"

// CommonErrorsAsk is what a bug's Builder is told at boot, after the rest of
// its kickoff.
const CommonErrorsAsk = " This story is a bug: just before 'For the rig memory:' in your closing comment, add a section headed exactly '" +
	CommonErrorsMarker + "' holding one line, '<slug>: <one line on the class of the fault>': the class's own slug when " +
	"`mw errors list` already holds it (read `mw errors show <slug>` by need), a new lower-case hyphenated one when it does not; " +
	"or the single word nothing. The Mayor places it in the common-errors register: you do not run mw errors add or checked."

// CommonErrorsHeld heads the list of register classes a bug's Builder is shown
// after CommonErrorsAsk.
const CommonErrorsHeld = "Classes the register holds for this rig's kinds (name one of these if it fits; a new slug only when none does):"

const (
	// commonErrorsListCap is about how many bytes the list of classes may take.
	commonErrorsListCap = 3000
	// commonErrorSymptomCap is how many characters of a class's symptom the list keeps.
	commonErrorSymptomCap = 140
	// commonErrorsSeparator ends an entry: a kickoff holds no newline, since its
	// command is one shell line.
	commonErrorsSeparator = " | "
)

// CommonErrorsFor is what a bug's Builder is shown of the register at boot,
// after CommonErrorsAsk: the classes whose kinds meet the rig's, one
// `<slug>: <symptom>` entry each (the symptom cut to commonErrorSymptomCap),
// the most recently found first, and, when they would pass commonErrorsListCap,
// '... and N more: mw errors list'. It is empty for a rig with no kinds, a
// register with no class of them, or a register that cannot be read: the ask
// then stands as it was.
func CommonErrorsFor(ctx context.Context, register ErrorFiles, seat, rig string) string {
	if register == nil {
		return ""
	}
	files, kindsText, err := register.ReadErrorRegister(ctx, seat)
	if err != nil {
		return ""
	}
	rigKinds, err := ParseRigKinds(kindsText)
	if err != nil || len(rigKinds[rig]) == 0 {
		return ""
	}
	classes, _ := LoadErrorClasses(files)
	var meet []ErrorClass
	for _, class := range classes {
		if slices.ContainsFunc(class.Kinds, func(kind string) bool { return slices.Contains(rigKinds[rig], kind) }) {
			meet = append(meet, class)
		}
	}
	if len(meet) == 0 {
		return ""
	}
	sort.SliceStable(meet, func(i, j int) bool { return meet[i].lastFound() > meet[j].lastFound() })

	var b strings.Builder
	b.WriteString(" " + CommonErrorsHeld)
	kept := 0
	for _, class := range meet {
		entry := class.Slug + ": " + cutSymptom(class.Symptom)
		if kept == 0 {
			entry = " " + entry
		} else {
			entry = commonErrorsSeparator + entry
		}
		if kept > 0 && b.Len()+len(entry) > commonErrorsListCap {
			break
		}
		b.WriteString(entry)
		kept++
	}
	if more := len(meet) - kept; more > 0 {
		fmt.Fprintf(&b, "%s... and %d more: mw errors list", commonErrorsSeparator, more)
	}
	return b.String()
}

// lastFound is the date a class was last found in a rig, or when it was first
// seen when no check found it again.
func (c ErrorClass) lastFound() string {
	last := c.Since
	for _, check := range c.Checks {
		if check.Found != "" && check.Date > last {
			last = check.Date
		}
	}
	return last
}

// cutSymptom is a symptom on one line, cut to commonErrorSymptomCap characters.
func cutSymptom(symptom string) string {
	symptom = strings.Join(strings.Fields(symptom), " ")
	if runes := []rune(symptom); len(runes) > commonErrorSymptomCap {
		return string(runes[:commonErrorSymptomCap-3]) + "..."
	}
	return symptom
}

// isBugStory is a story a bug by its type, its [bug] title or its bug label.
func isBugStory(d StoryDetail) bool {
	return domain.IsBugStory(d.Type, d.Story.Title) || slices.Contains(d.Labels, "bug")
}

// CommonErrorLine is one line of the section: a class's slug and its one line,
// the word nothing, or, when it could not be read, the line as written.
type CommonErrorLine struct {
	Slug      string
	Line      string
	Nothing   bool
	Malformed string
}

// CommonErrorProposals is what a closing comment's 'For common errors:'
// section says. Found is false when it has none.
type CommonErrorProposals struct {
	Found bool
	Lines []CommonErrorLine
}

var commonErrorForm = regexp.MustCompile(`^([a-z0-9][a-z0-9-]*):\s*(.+)$`)

// ParseCommonErrors reads the 'For common errors:' section of a closing
// comment: from its heading to the 'For the rig memory:' heading, or to the
// end. The text after the heading's colon counts as the first line; blank
// lines are skipped and list markers ignored. When the heading is given more
// than once, the last is the section.
func ParseCommonErrors(comment string) CommonErrorProposals {
	lines := strings.Split(strings.ReplaceAll(comment, "\r\n", "\n"), "\n")
	start, first := -1, ""
	for i, line := range lines {
		if at := strings.Index(strings.ToLower(line), strings.ToLower(CommonErrorsMarker)); at >= 0 {
			start, first = i, line[at+len(CommonErrorsMarker):]
		}
	}
	if start < 0 {
		return CommonErrorProposals{}
	}
	found := CommonErrorProposals{Found: true}
	for _, raw := range append([]string{first}, lines[start+1:]...) {
		if strings.Contains(strings.ToLower(raw), strings.ToLower(RigMemoryMarker)) {
			break
		}
		text := strings.TrimSpace(domain.TrimListMarker(raw))
		switch m := commonErrorForm.FindStringSubmatch(text); {
		case text == "":
		case strings.EqualFold(strings.Trim(text, " .*_`"), "nothing"):
			found.Lines = append(found.Lines, CommonErrorLine{Nothing: true})
		case m != nil:
			found.Lines = append(found.Lines, CommonErrorLine{Slug: m[1], Line: strings.Join(strings.Fields(m[2]), " ")})
		default:
			found.Lines = append(found.Lines, CommonErrorLine{Malformed: strings.TrimSpace(raw)})
		}
	}
	return found
}

// UnknownKinds stands in for a new class's kinds when its rig has none in the
// register: mw errors add refuses it, so the Mayor names them.
const UnknownKinds = "<the kinds of rig it can occur in>"

// Block is what a Landed mail says of the section: 'For common errors (<rig>):'
// and a line each, an mw errors command for the Mayor to paste — checked for a
// class the register holds (known), add for a new one, with the rig's own
// kinds (rigKinds) to narrow — 'nothing', or 'MALFORMED: <line>'. A bug whose
// comment has no section is named as not given; any other story's is empty.
func (p CommonErrorProposals) Block(rig, story string, bug bool, known map[string]bool, rigKinds []string) string {
	head := "For common errors (" + rig + "):"
	if !p.Found {
		if bug {
			return head + " not given: a bug's closing comment names the class of its fault, or says nothing"
		}
		return ""
	}
	out := []string{head}
	kinds := shellQuoted(UnknownKinds)
	if len(rigKinds) > 0 {
		kinds = strings.Join(rigKinds, ",")
	}
	for _, line := range p.Lines {
		switch {
		case line.Malformed != "":
			out = append(out, "MALFORMED: "+line.Malformed)
		case line.Nothing:
			out = append(out, "nothing")
		case known[line.Slug]:
			out = append(out, fmt.Sprintf("mw errors checked %s %s found:%s", line.Slug, rig, story))
		default:
			out = append(out, fmt.Sprintf("mw errors add %s --found %s:%s --kinds %s --symptom %s",
				line.Slug, rig, story, kinds, shellQuoted(line.Line)))
		}
	}
	if len(p.Lines) == 0 {
		out = append(out, "MALFORMED: the section is empty")
	}
	return strings.Join(out, "\n")
}
