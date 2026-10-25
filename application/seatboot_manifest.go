package application

import (
	"context"
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"

	"github.com/Jonathan-A-White/millwright/domain"
)

// SeatManifestFileName is the file a seat keeps beside its charter saying what
// its session reads at boot, in order, and which of its files are read by
// need: seats/<seat>/boot.toml. `mw seat boot <seat>` prints the boot set it
// names and ends with an index of the by-need files' headings; `mw seat read
// <seat> <topic>` prints one of them (mw-gq6.445). Nothing a manifest does not
// name is read, so what a seat pays at boot is what its manifest says and no
// more; a seat with no manifest is not booted this way at all.
const SeatManifestFileName = "boot.toml"

// DefaultBootCap is how many bytes of one part of a boot set are printed when
// the manifest says nothing: the rig memory budget, since both are paid on
// every session. A manifest's own `cap` key replaces it, and a part's `cap`
// replaces that; 0 is no cap.
const DefaultBootCap = 8000

// SeatManifest is a seat's boot.toml, read.
type SeatManifest struct {
	Seat string
	// Cap is the manifest's own default cap, in bytes.
	Cap int
	// Ceiling is the most bytes the whole boot set may print, 0 for no
	// ceiling: a set past it is still printed, then said to be over and the
	// command fails, so that a part added later cannot quietly grow the
	// seat's boot (mw-gq6.755).
	Ceiling int
	// Parts are what the boot set prints, in order.
	Parts []BootPart
	// Topics are the files whose headings are read by need, in order; a topic
	// holds only File and Rig.
	Topics []BootPart
}

// BootPart is one [[boot]] or [[topic]] entry of a manifest: one of a file,
// the newest file of a directory, or a command, with the section of the file
// to print and the cap on how much of it is.
type BootPart struct {
	// File is a file by path from the vault's root, or from the rig's when Rig
	// names one. A path may hold {host} and {seat}.
	File string
	Rig  string
	// Newest is a directory of the vault whose newest .md file, by name, is
	// read: a seat's handoffs.
	Newest string
	// Command is a shell command line, run in the vault; what it prints is
	// the part.
	Command string
	// Section names one heading of the file, by its text or its slug, and
	// only that heading's section is printed.
	Section string
	// Cap is the most bytes of the part printed, 0 for all of it.
	Cap int
}

// ParseSeatManifest reads a seat's boot.toml, or says what is wrong with it.
func ParseSeatManifest(seat, text string) (SeatManifest, error) {
	file, err := domain.ParseConfigTOML(text)
	if err != nil {
		return SeatManifest{}, err
	}
	m := SeatManifest{Seat: seat, Cap: DefaultBootCap}
	for key, literal := range file.Table("") {
		switch key {
		case "cap":
			if m.Cap, err = bootCap(literal); err != nil {
				return SeatManifest{}, fmt.Errorf("the manifest's cap: %w", err)
			}
		case "ceiling":
			if m.Ceiling, err = bootCap(literal); err != nil {
				return SeatManifest{}, fmt.Errorf("the manifest's ceiling: %w", err)
			}
		default:
			return SeatManifest{}, fmt.Errorf("the key %q is not one mw reads at the top of a manifest (only cap and ceiling are)", key)
		}
	}
	for i, entry := range file.ArrayTables("boot") {
		part, err := bootPart(entry, m.Cap)
		if err != nil {
			return SeatManifest{}, fmt.Errorf("the [[boot]] entry %d: %w", i+1, err)
		}
		m.Parts = append(m.Parts, part)
	}
	for i, entry := range file.ArrayTables("topic") {
		topic, err := topicPart(entry)
		if err != nil {
			return SeatManifest{}, fmt.Errorf("the [[topic]] entry %d: %w", i+1, err)
		}
		m.Topics = append(m.Topics, topic)
	}
	if len(m.Parts) == 0 && len(m.Topics) == 0 {
		return SeatManifest{}, fmt.Errorf("the manifest names nothing to read: give at least one [[boot]] or [[topic]] entry")
	}
	return m, nil
}

// bootPart reads one [[boot]] entry.
func bootPart(entry map[string]string, cap int) (BootPart, error) {
	part := BootPart{Cap: cap}
	for key, literal := range entry {
		value, _ := domain.TOMLString(literal)
		var err error
		switch key {
		case "file":
			part.File, err = bootPath(key, value)
		case "rig":
			part.Rig = value
		case "newest":
			part.Newest, err = bootPath(key, value)
		case "command":
			part.Command = strings.TrimSpace(value)
		case "section":
			part.Section = strings.TrimSpace(value)
		case "cap":
			part.Cap, err = bootCap(literal)
		default:
			return BootPart{}, fmt.Errorf("has a key %q mw does not read (its keys are file, rig, newest, command, section and cap)", key)
		}
		if err != nil {
			return BootPart{}, err
		}
	}
	sources := 0
	for _, source := range []string{part.File, part.Newest, part.Command} {
		if source != "" {
			sources++
		}
	}
	switch {
	case sources != 1:
		return BootPart{}, fmt.Errorf("names %d of file, newest and command: give one of file, newest or command", sources)
	case part.Command != "" && part.Section != "":
		return BootPart{}, fmt.Errorf("runs a command, so it has no section: a section is a heading of a file")
	case part.File == "" && part.Rig != "":
		return BootPart{}, fmt.Errorf("names a rig, which only a file is read from")
	}
	return part, nil
}

// topicPart reads one [[topic]] entry: a file, maybe of a rig.
func topicPart(entry map[string]string) (BootPart, error) {
	var topic BootPart
	for key, literal := range entry {
		value, _ := domain.TOMLString(literal)
		var err error
		switch key {
		case "file":
			topic.File, err = bootPath(key, value)
		case "rig":
			topic.Rig = value
		default:
			return BootPart{}, fmt.Errorf("has a key %q mw does not read (its keys are file and rig)", key)
		}
		if err != nil {
			return BootPart{}, err
		}
	}
	if topic.File == "" {
		return BootPart{}, fmt.Errorf("names no file")
	}
	return topic, nil
}

// bootCap reads a cap: a whole number of bytes, 0 for none.
func bootCap(literal string) (int, error) {
	cap, err := strconv.Atoi(strings.TrimSpace(literal))
	if err != nil || cap < 0 {
		return 0, fmt.Errorf("the cap %s is not a whole number of bytes (0 is no cap)", literal)
	}
	return cap, nil
}

// bootPath checks a path of a manifest: relative to a root, reaching nowhere
// above it, with {host} and {seat} the only holes in it.
func bootPath(what, value string) (string, error) {
	value = strings.TrimSpace(value)
	if err := safeBootPath(value); err != nil {
		return "", fmt.Errorf("the %s %q %w", what, value, err)
	}
	rest := value
	for {
		open := strings.IndexByte(rest, '{')
		if open < 0 {
			break
		}
		closing := strings.IndexByte(rest[open:], '}')
		if closing < 0 {
			return "", fmt.Errorf("the %s %q has a { that is not closed", what, value)
		}
		if hole := rest[open : open+closing+1]; hole != "{host}" && hole != "{seat}" {
			return "", fmt.Errorf("the %s %q holds %s: a path may hold {host} and {seat} only", what, value, hole)
		}
		rest = rest[open+closing+1:]
	}
	return value, nil
}

// safeBootPath is why a path cannot be read from a root, or nil.
func safeBootPath(value string) error {
	switch {
	case value == "":
		return fmt.Errorf("is empty")
	case strings.HasPrefix(value, "/"), strings.Contains(value, `\`), strings.Contains(value, ".."):
		return fmt.Errorf("would reach outside the vault: a path is relative to the vault's root (or the rig's), with no ..")
	}
	return nil
}

// fill puts the host and the seat into a path's holes, or says why it cannot.
func (p BootPart) fill(seat, host string) (BootPart, error) {
	for _, field := range []*string{&p.File, &p.Newest} {
		if !strings.Contains(*field, "{") {
			continue
		}
		*field = strings.ReplaceAll(*field, "{seat}", seat)
		if strings.Contains(*field, "{host}") {
			if host == "" {
				return p, fmt.Errorf("the path %q names {host}, and this host has no name", *field)
			}
			*field = strings.ReplaceAll(*field, "{host}", host)
		}
		if err := safeBootPath(*field); err != nil {
			return p, fmt.Errorf("the path %q, filled in, %w", *field, err)
		}
	}
	return p, nil
}

// Heading is one markdown heading of a file.
type Heading struct {
	Level int
	Text  string
	// Line is the line it is on, from 0.
	Line int
}

// Headings is every heading of a markdown text, in order: a line of # marks
// then a space outside a fenced code block.
func Headings(text string) []Heading {
	var found []Heading
	fenced := false
	for i, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			fenced = !fenced
			continue
		}
		if fenced || !strings.HasPrefix(line, "#") {
			continue
		}
		level := len(line) - len(strings.TrimLeft(line, "#"))
		if level > 6 || len(line) <= level || line[level] != ' ' && line[level] != '\t' {
			continue
		}
		if heading := strings.TrimSpace(line[level:]); heading != "" {
			found = append(found, Heading{Level: level, Text: heading, Line: i})
		}
	}
	return found
}

// Section is the section of text under the heading named — by its text, case
// aside, or by its slug — from the heading line to the line before the next
// heading of its level or higher, trailing blank lines left off; and whether
// the text has such a heading.
func Section(text, name string) (string, bool) {
	lines := strings.Split(text, "\n")
	headings := Headings(text)
	want := strings.ToLower(strings.TrimSpace(name))
	slug := TopicSlug(name)
	for i, heading := range headings {
		if strings.ToLower(heading.Text) != want && TopicSlug(heading.Text) != slug {
			continue
		}
		end := len(lines)
		for _, next := range headings[i+1:] {
			if next.Level <= heading.Level {
				end = next.Line
				break
			}
		}
		return strings.TrimRight(strings.Join(lines[heading.Line:end], "\n"), "\n"), true
	}
	return "", false
}

// topicSlugWords is how many words of a heading make its slug.
const topicSlugWords = 5

// TopicSlug is the name a heading is asked for by: its first five words before
// any aside in parentheses or after a colon, lower-case, hyphenated, each word
// kept to its letters, digits and hyphens.
func TopicSlug(heading string) string {
	if cut := strings.IndexAny(heading, "(:"); cut >= 0 {
		heading = heading[:cut]
	}
	var words []string
	for _, word := range strings.Fields(strings.ToLower(heading)) {
		kept := strings.Trim(strings.Map(func(r rune) rune {
			if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' {
				return r
			}
			return -1
		}, word), "-")
		if kept == "" {
			continue
		}
		words = append(words, kept)
		if len(words) == topicSlugWords {
			break
		}
	}
	return strings.Join(words, "-")
}

// SeatBootFiles is the port a seat's manifest and the vault files it names are
// read through. One adapter is the vault directory on disk.
type SeatBootFiles interface {
	// Dir is the vault's directory on this host: where a manifest's commands
	// run.
	Dir() string

	// SeatManifest reads seats/<seat>/boot.toml, and whether the seat has one.
	SeatManifest(ctx context.Context, seat string) (text string, found bool, err error)

	// VaultFile reads one file by path from the vault's root.
	VaultFile(ctx context.Context, rel string) (string, error)

	// NewestVaultFile reads the newest .md file, by name, of a directory of
	// the vault: its path from the vault's root and its text. A directory
	// with none is an error.
	NewestVaultFile(ctx context.Context, dir string) (name, text string, err error)
}

// RigDocs is the port a file of a rig's checkout on this host is read through,
// for a manifest entry that names a rig: a rig's own docs are the rig's, and
// the vault only points at them.
type RigDocs interface {
	// RigFile reads one file by path from the rig's root. A rig not checked
	// out here is an error that names it.
	RigFile(ctx context.Context, rig, rel string) (string, error)
}

// BootCommands is the port a manifest's commands run through: one shell
// command line in a directory, and what it printed, out and err together.
type BootCommands interface {
	Run(ctx context.Context, dir, command string) (string, error)
}

// SeatBootSet prints what a seat reads at boot: every part of its manifest, in
// order, within its cap, then the index of what it can read by need. One
// command in place of a run of reads, so that a seat's boot is one tool call
// and what it costs is what the manifest says.
type SeatBootSet struct {
	Files    SeatBootFiles
	Rigs     RigDocs
	Commands BootCommands
	Seat     string
	// Host fills {host} in a manifest's paths.
	Host string
	Out  io.Writer
}

// Run prints the boot set: a title line, then each part under a `==> <part> <==`
// line (the form head and tail use for many files, which no markdown heading
// of a part's own can be mistaken for), then the index. A part that cannot be
// read is said in its place and the rest is printed all the same; then the
// error counts them, so that a seat sees both what it has and what it lacks.
func (b SeatBootSet) Run(ctx context.Context) error {
	m, err := loadSeatManifest(ctx, b.Files, b.Seat)
	if err != nil {
		return err
	}
	out := &countingWriter{w: b.Out}
	b.Out = out
	fmt.Fprintf(b.Out, "# What the %s seat reads at boot (%s)\n\n", b.Seat, seatManifestPath(b.Seat))
	failed := 0
	for i, part := range m.Parts {
		if i > 0 {
			fmt.Fprintln(b.Out)
		}
		if !b.part(ctx, part) {
			failed++
		}
	}
	if len(m.Topics) > 0 {
		if len(m.Parts) > 0 {
			fmt.Fprintln(b.Out)
		}
		fmt.Fprintln(b.Out, "==> Read by need <==")
		fmt.Fprintln(b.Out)
		topics, problems := seatTopics(ctx, b.Files, b.Rigs, m, b.Host)
		printTopicIndex(b.Out, b.Seat, topics)
		for _, problem := range problems {
			fmt.Fprintf(b.Out, "[not read: %v]\n", problem)
		}
		failed += len(problems)
	}
	if failed > 0 {
		return fmt.Errorf("%d of %d parts of the %s seat's boot set could not be read", failed, len(m.Parts)+len(m.Topics), b.Seat)
	}
	if m.Ceiling > 0 && out.n > m.Ceiling {
		over := fmt.Sprintf("the %s seat's boot set is %d bytes, over its manifest's ceiling of %d bytes: cut a part or move it to a topic", b.Seat, out.n, m.Ceiling)
		fmt.Fprintf(b.Out, "\n[over the manifest's ceiling: %d of %d bytes]\n", out.n, m.Ceiling)
		return fmt.Errorf("%s", over)
	}
	return nil
}

// countingWriter counts the bytes written through it.
type countingWriter struct {
	w io.Writer
	n int
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += n
	return n, err
}

// part prints one part of the boot set, and whether it could be read.
func (b SeatBootSet) part(ctx context.Context, part BootPart) bool {
	part, err := part.fill(b.Seat, b.Host)
	if err != nil {
		fmt.Fprintf(b.Out, "==> %s <==\n\n[not read: %v]\n", part.File+part.Newest, err)
		return false
	}
	var title, text string
	switch {
	case part.Command != "":
		title = "$ " + part.Command
		if b.Commands == nil {
			err = fmt.Errorf("nothing here runs a command")
		} else {
			text, err = b.Commands.Run(ctx, b.Files.Dir(), part.Command)
		}
		if err != nil {
			fmt.Fprintf(b.Out, "==> %s <==\n\n", title)
			if text = strings.TrimRight(text, "\n"); text != "" {
				fmt.Fprintln(b.Out, text)
			}
			fmt.Fprintf(b.Out, "[failed: %v]\n", err)
			return false
		}
	case part.Newest != "":
		title, text, err = b.Files.NewestVaultFile(ctx, part.Newest)
		if err != nil {
			title = part.Newest
		}
	default:
		title = partTitle(part)
		text, err = readBootFile(ctx, b.Files, b.Rigs, part)
	}
	if err != nil {
		fmt.Fprintf(b.Out, "==> %s <==\n\n[not read: %v]\n", title, err)
		return false
	}
	if part.Section != "" {
		title += ": " + part.Section
		section, ok := Section(text, part.Section)
		if !ok {
			fmt.Fprintf(b.Out, "==> %s <==\n\n[not read: %s has no heading %q]\n", title, partTitle(part), part.Section)
			return false
		}
		text = section
	}
	fmt.Fprintf(b.Out, "==> %s <==\n\n", title)
	kept, cut := capText(text, part.Cap)
	if kept = strings.TrimRight(kept, "\n"); kept != "" {
		fmt.Fprintln(b.Out, kept)
	}
	if cut {
		fmt.Fprintf(b.Out, "[cut at %d of %d bytes]\n", part.Cap, len(text))
	}
	return true
}

// partTitle names a file part: its path, with the rig when it is a rig's.
func partTitle(part BootPart) string {
	if part.Rig != "" {
		return part.Rig + ": " + part.File
	}
	return part.File
}

// readBootFile reads a file part from the vault or the rig it names.
func readBootFile(ctx context.Context, files SeatBootFiles, rigs RigDocs, part BootPart) (string, error) {
	if part.Rig == "" {
		return files.VaultFile(ctx, part.File)
	}
	if rigs == nil {
		return "", fmt.Errorf("no rig is checked out here to read %s of %s from", part.File, part.Rig)
	}
	return rigs.RigFile(ctx, part.Rig, part.File)
}

// capText is the first cap bytes of text, cut back to the end of a line so
// that no line is printed in part, and whether anything was cut. A cap of 0
// cuts nothing.
func capText(text string, cap int) (string, bool) {
	if cap <= 0 || len(text) <= cap {
		return text, false
	}
	kept := text[:cap]
	if i := strings.LastIndexByte(kept, '\n'); i > 0 {
		kept = kept[:i]
	}
	return kept, true
}

// seatManifestPath is where a seat's manifest is, from the vault's root.
func seatManifestPath(seat string) string {
	return path.Join(SeatsDir, seat, SeatManifestFileName)
}

// loadSeatManifest reads and parses a seat's manifest, naming the file in
// every refusal.
func loadSeatManifest(ctx context.Context, files SeatBootFiles, seat string) (SeatManifest, error) {
	if files == nil {
		return SeatManifest{}, fmt.Errorf("reading a seat's boot set needs the vault")
	}
	if !plainSeatName(seat) {
		return SeatManifest{}, fmt.Errorf("%q is not a seat: a seat is named in letters, digits, dashes and underscores", seat)
	}
	text, found, err := files.SeatManifest(ctx, seat)
	if err != nil {
		return SeatManifest{}, err
	}
	if !found {
		return SeatManifest{}, fmt.Errorf("the %s seat has no %s: nothing says what it reads at boot", seat, seatManifestPath(seat))
	}
	m, err := ParseSeatManifest(seat, text)
	if err != nil {
		return SeatManifest{}, fmt.Errorf("%s: %w", seatManifestPath(seat), err)
	}
	return m, nil
}

// Topic is one heading of a seat's by-need files: what `mw seat read` prints.
type Topic struct {
	Slug    string
	Heading string
	// File is the file it is in, as the manifest names it, and Rig the rig
	// when it is a rig's.
	File string
	Rig  string
	// Text is the file's whole text, which the section is cut from.
	Text string
}

// Where names the topic's file in the index.
func (t Topic) Where() string {
	if t.Rig != "" {
		return t.Rig + ": " + t.File
	}
	return t.File
}

// seatTopics reads every topic file of a manifest and lists their headings as
// topics, each slug made unique by a count when a heading repeats, and the
// files that could not be read.
func seatTopics(ctx context.Context, files SeatBootFiles, rigs RigDocs, m SeatManifest, host string) ([]Topic, []error) {
	var topics []Topic
	var problems []error
	seen := map[string]int{}
	for _, file := range m.Topics {
		file, err := file.fill(m.Seat, host)
		if err != nil {
			problems = append(problems, err)
			continue
		}
		text, err := readBootFile(ctx, files, rigs, file)
		if err != nil {
			problems = append(problems, err)
			continue
		}
		for _, heading := range Headings(text) {
			slug := TopicSlug(heading.Text)
			if slug == "" {
				continue
			}
			seen[slug]++
			if n := seen[slug]; n > 1 {
				slug = fmt.Sprintf("%s-%d", slug, n)
			}
			topics = append(topics, Topic{Slug: slug, Heading: heading.Text, File: file.File, Rig: file.Rig, Text: text})
		}
	}
	return topics, problems
}

// printTopicIndex prints the one-screen index a seat reads by need from: one
// line for each file, its topics' slugs in order, since a slug is the heading's
// own words and the file is said once rather than on every line (mw-gq6.755).
func printTopicIndex(out io.Writer, seat string, topics []Topic) {
	fmt.Fprintf(out, "mw seat read %s <topic> prints one of these:\n", seat)
	for i := 0; i < len(topics); {
		where := topics[i].Where()
		slugs := []string{topics[i].Slug}
		i++
		for ; i < len(topics) && topics[i].Where() == where; i++ {
			slugs = append(slugs, topics[i].Slug)
		}
		fmt.Fprintf(out, "- %s: %s\n", where, strings.Join(slugs, ", "))
	}
}

// SeatRead prints one topic of a seat's by-need files: the section under one
// heading, whole, from the file the manifest names. It is the other half of
// SeatBootSet: boot prints the index, read prints the entry.
type SeatRead struct {
	Files SeatBootFiles
	Rigs  RigDocs
	Seat  string
	Host  string
	Out   io.Writer
}

// Run prints the topic, or with no topic the index. A topic is matched by its
// slug, whole or as a prefix no other slug shares; an unknown or ambiguous one
// is refused with what there is.
func (r SeatRead) Run(ctx context.Context, topic string) error {
	m, err := loadSeatManifest(ctx, r.Files, r.Seat)
	if err != nil {
		return err
	}
	topics, problems := seatTopics(ctx, r.Files, r.Rigs, m, r.Host)
	if len(problems) > 0 {
		return fmt.Errorf("a by-need file of the %s seat could not be read: %v", r.Seat, problems[0])
	}
	topic = strings.TrimSpace(topic)
	if topic == "" {
		if len(topics) == 0 {
			fmt.Fprintf(r.Out, "the %s seat names no topics in %s\n", r.Seat, seatManifestPath(r.Seat))
			return nil
		}
		printTopicIndex(r.Out, r.Seat, topics)
		return nil
	}
	var found []Topic
	for _, t := range topics {
		if t.Slug == topic {
			found = []Topic{t}
			break
		}
		if strings.HasPrefix(t.Slug, topic) {
			found = append(found, t)
		}
	}
	switch len(found) {
	case 0:
		return fmt.Errorf("the %s seat has no topic %q: it has %s", r.Seat, topic, slugsOf(topics))
	case 1:
	default:
		return fmt.Errorf("%q names more than one topic of the %s seat: %s", topic, r.Seat, slugsOf(found))
	}
	section, ok := Section(found[0].Text, found[0].Heading)
	if !ok {
		return fmt.Errorf("the heading %q of %s has gone", found[0].Heading, found[0].Where())
	}
	fmt.Fprintln(r.Out, section)
	return nil
}

// slugsOf lists topics' slugs in a sentence.
func slugsOf(topics []Topic) string {
	if len(topics) == 0 {
		return "none"
	}
	slugs := make([]string, len(topics))
	for i, t := range topics {
		slugs[i] = t.Slug
	}
	return strings.Join(slugs, ", ")
}
