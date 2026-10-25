package application

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"
)

// fakeSeatBootFiles is a vault of seat manifests and files held in memory, and
// the rigs' docs beside it.
type fakeSeatBootFiles struct {
	manifests map[string]string // by seat
	files     map[string]string // by vault-relative path
	rigs      map[string]string // by "<rig>/<rel>"
	commands  map[string]string // by command: what it prints
	failing   map[string]error  // by command: how it fails
	ran       []string
}

func (f *fakeSeatBootFiles) Dir() string { return "/vault" }

func (f *fakeSeatBootFiles) SeatManifest(_ context.Context, seat string) (string, bool, error) {
	text, ok := f.manifests[seat]
	return text, ok, nil
}

func (f *fakeSeatBootFiles) VaultFile(_ context.Context, rel string) (string, error) {
	text, ok := f.files[rel]
	if !ok {
		return "", fmt.Errorf("%s: no such file in the vault", rel)
	}
	return text, nil
}

func (f *fakeSeatBootFiles) NewestVaultFile(_ context.Context, dir string) (string, string, error) {
	var names []string
	for name := range f.files {
		if strings.HasPrefix(name, dir+"/") && !strings.Contains(strings.TrimPrefix(name, dir+"/"), "/") {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return "", "", fmt.Errorf("%s holds no file", dir)
	}
	sort.Strings(names)
	newest := names[len(names)-1]
	return newest, f.files[newest], nil
}

func (f *fakeSeatBootFiles) RigFile(_ context.Context, rig, rel string) (string, error) {
	text, ok := f.rigs[rig+"/"+rel]
	if !ok {
		return "", fmt.Errorf("the rig %s has no %s here", rig, rel)
	}
	return text, nil
}

func (f *fakeSeatBootFiles) Run(_ context.Context, dir, command string) (string, error) {
	f.ran = append(f.ran, dir+": "+command)
	if err, failing := f.failing[command]; failing {
		return f.commands[command], err
	}
	out, ok := f.commands[command]
	if !ok {
		return "", fmt.Errorf("sh: %s: not found", command)
	}
	return out, nil
}

const (
	fixtureVision     = "# Vision\n\nWhere the factory is going.\n"
	fixtureProcedures = "# Mayor procedures (one place)\n\n## Boot\n\n1. Charter, vision, the newest handoff.\n2. CONTEXT.md.\n\n" +
		"## Rules that bite every sitting\n\n1. ONE bd at a time.\n\n```\n# not a heading: a comment in a fence\n```\n\n" +
		"## Handing off (at ~180K by `bin/mayor-context`, or at the end of a ticket)\n\nWrite the handoff, start the successor.\n\n" +
		"### A sub-step of handing off\n\nArm the reaper.\n"
	fixturePruning = "# Pruning a rig's memory\n\nMove lines, never delete them.\n"
	fixtureLedger  = "2026-09-01 mw-old worked and closed here.\n"
)

func fixtureSeatFiles() *fakeSeatBootFiles {
	return &fakeSeatBootFiles{
		manifests: map[string]string{},
		files: map[string]string{
			"seats/mayor/vision.md":                   fixtureVision,
			"seats/mayor/procedures.md":               fixtureProcedures,
			"seats/mayor/pruning.md":                  fixturePruning,
			"seats/mayor/charter.md":                  "# Mayor — charter\n\nNEVER IN THE BOOT SET: the charter is the system prompt.\n",
			"seats/mayor/ledger.md":                   fixtureLedger,
			"seats/mayor/handoffs/2026-10-15-414.md":  "# Handoff 414\n\nOLD HANDOFF.\n",
			"seats/mayor/handoffs/2026-10-16-415.md":  "# Handoff 415\n\nThe newest handoff: carry on from here.\n",
			"seats/deputy/handoffs/2026-10-16-693.md": "# Deputy handoff 693\n\nMail worked this sitting.\n",
			"seats/millhand/hosts/laptop/boot.md":     "# Millhand boot notes: the Laptop\n\nFirst steps.\n",
		},
		rigs: map[string]string{
			"millwright/CONTEXT.md": "# Millwright\n\n## Language\n\n### Governor\n\nThe one human the factory serves.\n",
		},
		commands: map[string]string{
			"mw mail inbox --as deputy": "mw-abc Landed: mw-gq6.1\n",
			"mw brief mw-6ww":           strings.Repeat("a line of the brief\n", 20),
		},
		failing: map[string]error{},
	}
}

const mayorManifest = `# What the Mayor reads at boot.
cap = 4000

[[boot]]
file = "seats/mayor/vision.md"

[[boot]]
newest = "seats/mayor/handoffs"

[[boot]]
file = "seats/mayor/procedures.md"
section = "Boot"

[[boot]]
command = "mw brief mw-6ww"
cap = 100

[[topic]]
file = "seats/mayor/procedures.md"

[[topic]]
file = "seats/mayor/pruning.md"

[[topic]]
rig = "millwright"
file = "CONTEXT.md"
`

func TestParseSeatManifestReadsPartsAndTopicsInOrder(t *testing.T) {
	m, err := ParseSeatManifest("mayor", mayorManifest)
	if err != nil {
		t.Fatal(err)
	}
	if m.Seat != "mayor" || m.Cap != 4000 {
		t.Errorf("seat %q cap %d", m.Seat, m.Cap)
	}
	want := []BootPart{
		{File: "seats/mayor/vision.md", Cap: 4000},
		{Newest: "seats/mayor/handoffs", Cap: 4000},
		{File: "seats/mayor/procedures.md", Section: "Boot", Cap: 4000},
		{Command: "mw brief mw-6ww", Cap: 100},
	}
	if len(m.Parts) != len(want) {
		t.Fatalf("%d parts, want %d: %+v", len(m.Parts), len(want), m.Parts)
	}
	for i, part := range want {
		if m.Parts[i] != part {
			t.Errorf("part %d is %+v, want %+v", i, m.Parts[i], part)
		}
	}
	topics := []BootPart{
		{File: "seats/mayor/procedures.md"},
		{File: "seats/mayor/pruning.md"},
		{File: "CONTEXT.md", Rig: "millwright"},
	}
	if len(m.Topics) != len(topics) {
		t.Fatalf("%d topics, want %d: %+v", len(m.Topics), len(topics), m.Topics)
	}
	for i, topic := range topics {
		if m.Topics[i] != topic {
			t.Errorf("topic %d is %+v, want %+v", i, m.Topics[i], topic)
		}
	}
}

func TestParseSeatManifestDefaultsTheCapAndTakesZeroAsNoCap(t *testing.T) {
	m, err := ParseSeatManifest("deputy", "[[boot]]\nfile = \"a.md\"\n\n[[boot]]\nfile = \"b.md\"\ncap = 0\n")
	if err != nil {
		t.Fatal(err)
	}
	if m.Cap != DefaultBootCap || m.Parts[0].Cap != DefaultBootCap || m.Parts[1].Cap != 0 {
		t.Errorf("caps: manifest %d, parts %d and %d; want %d, %d and 0", m.Cap, m.Parts[0].Cap, m.Parts[1].Cap, DefaultBootCap, DefaultBootCap)
	}
}

func TestParseSeatManifestRefusesWhatItCannotRead(t *testing.T) {
	for name, tc := range map[string]struct{ text, want string }{
		"not toml":           {"[[boot]\nfile = \"a.md\"\n", "line 1"},
		"no source":          {"[[boot]]\ncap = 10\n", "one of file, newest or command"},
		"two sources":        {"[[boot]]\nfile = \"a.md\"\ncommand = \"ls\"\n", "one of file, newest or command"},
		"section of a run":   {"[[boot]]\ncommand = \"ls\"\nsection = \"Boot\"\n", "section"},
		"rig of a run":       {"[[boot]]\ncommand = \"ls\"\nrig = \"millwright\"\n", "rig"},
		"unknown key":        {"[[boot]]\nfile = \"a.md\"\nbytes = 10\n", `"bytes"`},
		"bad cap":            {"[[boot]]\nfile = \"a.md\"\ncap = \"many\"\n", "cap"},
		"negative cap":       {"[[boot]]\nfile = \"a.md\"\ncap = -1\n", "cap"},
		"absolute path":      {"[[boot]]\nfile = \"/etc/passwd\"\n", "/etc/passwd"},
		"path outside":       {"[[boot]]\nfile = \"../other/a.md\"\n", "../other/a.md"},
		"topic with no file": {"[[topic]]\nrig = \"millwright\"\n", "file"},
		"topic with a run":   {"[[topic]]\ncommand = \"ls\"\n", `"command"`},
		"unknown root key":   {"caps = 4\n[[boot]]\nfile = \"a.md\"\n", `"caps"`},
		"nothing at all":     {"cap = 4\n", "names nothing"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := ParseSeatManifest("mayor", tc.text)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %v, want one saying %q", err, tc.want)
			}
		})
	}
}

func TestSectionFindsOneHeadingAndStopsAtTheNextOfItsLevel(t *testing.T) {
	got, ok := Section(fixtureProcedures, "Boot")
	if !ok || got != "## Boot\n\n1. Charter, vision, the newest handoff.\n2. CONTEXT.md." {
		t.Errorf("section Boot: %q (found %v)", got, ok)
	}
	// A heading is matched by its text whatever the case, or by its slug, and
	// a sub-heading of a lower level stays inside its section.
	for _, name := range []string{"handing off (at ~180K by `bin/mayor-context`, or at the end of a ticket)", "handing-off", "HANDING OFF"} {
		got, ok := Section(fixtureProcedures, name)
		if !ok || !strings.HasPrefix(got, "## Handing off") || !strings.Contains(got, "### A sub-step") || !strings.HasSuffix(got, "Arm the reaper.") {
			t.Errorf("section %q: %q (found %v)", name, got, ok)
		}
	}
	// A # inside a fenced block is not a heading, so the rules section runs on
	// through the fence.
	got, ok = Section(fixtureProcedures, "rules-that-bite-every-sitting")
	if !ok || !strings.Contains(got, "# not a heading") || strings.Contains(got, "Handing off") {
		t.Errorf("section through a fence: %q (found %v)", got, ok)
	}
	// The title's section is the whole file.
	got, ok = Section(fixtureProcedures, "mayor-procedures")
	if !ok || got != strings.TrimRight(fixtureProcedures, "\n") {
		t.Errorf("the title's section: %q (found %v)", got, ok)
	}
	if _, ok := Section(fixtureProcedures, "nowhere"); ok {
		t.Error("a heading the text lacks was found")
	}
}

func TestTopicSlugIsTheHeadingsFirstWordsBeforeAnyAside(t *testing.T) {
	for heading, want := range map[string]string{
		"Boot":                          "boot",
		"Rules that bite every sitting": "rules-that-bite-every-sitting",
		"Handing off (at ~180K by `bin/mayor-context`)":        "handing-off",
		"Since 2026-09-21: Builders propose, the Mayor places": "since-2026-09-21",
		"The VPS health rule (the Governor: \"yes\")":          "the-vps-health-rule",
		"Verifying a spell-forge landing (no npm on the VPS)":  "verifying-a-spell-forge-landing",
		"  Mayor — charter  ":                                  "mayor-charter",
	} {
		if got := TopicSlug(heading); got != want {
			t.Errorf("TopicSlug(%q) = %q, want %q", heading, got, want)
		}
	}
}

func bootSet(files *fakeSeatBootFiles, seat, host string) (string, error) {
	var out bytes.Buffer
	err := SeatBootSet{Files: files, Rigs: files, Commands: files, Seat: seat, Host: host, Out: &out}.Run(context.Background())
	return out.String(), err
}

func TestSeatBootSetPrintsTheManifestsPartsInOrderWithinTheirCaps(t *testing.T) {
	files := fixtureSeatFiles()
	files.manifests["mayor"] = mayorManifest
	out, err := bootSet(files, "mayor", "laptop")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, want := range []string{
		"==> seats/mayor/vision.md <==\n\n# Vision\n\nWhere the factory is going.\n",
		"==> seats/mayor/handoffs/2026-10-16-415.md <==\n\n# Handoff 415\n\nThe newest handoff: carry on from here.\n",
		"==> seats/mayor/procedures.md: Boot <==\n\n## Boot\n\n1. Charter, vision, the newest handoff.\n2. CONTEXT.md.\n",
		"==> $ mw brief mw-6ww <==\n\n" + strings.Repeat("a line of the brief\n", 5) + "[cut at 100 of 400 bytes]\n",
		"==> Read by need <==\n\nmw seat read mayor <topic> prints one of these:\n",
		"- seats/mayor/procedures.md: mayor-procedures, boot, rules-that-bite-every-sitting, handing-off, a-sub-step-of-handing-off\n",
		"- seats/mayor/pruning.md: pruning-a-rigs-memory\n",
		"- millwright: CONTEXT.md: millwright, language, governor\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the boot set lacks %q:\n%s", want, out)
		}
	}
	for _, never := range []string{"NEVER IN THE BOOT SET", "mw-old worked", "OLD HANDOFF", "Rules that bite every sitting\n\n1. ONE bd", "Write the handoff, start the successor"} {
		if strings.Contains(out, never) {
			t.Errorf("the boot set holds %q, which the manifest does not name:\n%s", never, out)
		}
	}
	// The parts come in the manifest's order, and the index last.
	order := []string{"==> seats/mayor/vision.md <==", "==> seats/mayor/handoffs/", "==> seats/mayor/procedures.md: Boot <==", "==> $ mw brief", "==> Read by need <=="}
	last := -1
	for _, mark := range order {
		at := strings.Index(out, mark)
		if at < last {
			t.Errorf("%q comes before what the manifest lists first:\n%s", mark, out)
		}
		last = at
	}
	if len(files.ran) != 1 || files.ran[0] != "/vault: mw brief mw-6ww" {
		t.Errorf("ran %v, want the one command in the vault", files.ran)
	}
	if !strings.HasPrefix(out, "# What the mayor seat reads at boot (seats/mayor/boot.toml)\n\n") {
		t.Errorf("the boot set starts %q", out[:min(len(out), 80)])
	}
}

func TestSeatBootSetCutsAFileAtItsCapOnALineAndSaysSo(t *testing.T) {
	files := fixtureSeatFiles()
	files.manifests["deputy"] = "[[boot]]\nfile = \"seats/mayor/procedures.md\"\ncap = 90\n\n[[boot]]\nfile = \"seats/mayor/pruning.md\"\ncap = 0\n"
	out, err := bootSet(files, "deputy", "laptop")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	want := "==> seats/mayor/procedures.md <==\n\n# Mayor procedures (one place)\n\n## Boot\n\n1. Charter, vision, the newest handoff.\n" +
		fmt.Sprintf("[cut at 90 of %d bytes]\n", len(fixtureProcedures)) +
		"\n==> seats/mayor/pruning.md <==\n\n" + fixturePruning
	if !strings.Contains(out, want) {
		t.Errorf("want\n%s\nin\n%s", want, out)
	}
	if strings.Contains(out, "2. CONTEXT.md") {
		t.Errorf("the cut kept a line past the cap:\n%s", out)
	}
	if strings.Contains(out, "==> Read by need <==") {
		t.Errorf("a manifest with no topics printed an index:\n%s", out)
	}
}

func TestSeatBootSetFillsInTheHostAndTheSeat(t *testing.T) {
	files := fixtureSeatFiles()
	files.manifests["millhand"] = "[[boot]]\nfile = \"seats/{seat}/hosts/{host}/boot.md\"\n"
	out, err := bootSet(files, "millhand", "laptop")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(out, "==> seats/millhand/hosts/laptop/boot.md <==\n\n# Millhand boot notes: the Laptop\n") {
		t.Errorf("the host's file was not read:\n%s", out)
	}
	// With no host named, the part is said not read in its place, and the
	// run says so at the end.
	out, err = bootSet(files, "millhand", "")
	if err == nil || !strings.Contains(out, "[not read: the path \"seats/millhand/hosts/{host}/boot.md\" names {host}, and this host has no name]") {
		t.Errorf("a {host} with no host named: %v\n%s", err, out)
	}
}

func TestSeatBootSetSaysWhatItCouldNotReadPrintsTheRestAndFails(t *testing.T) {
	files := fixtureSeatFiles()
	files.failing["mw brief mw-6ww"] = errors.New("exit status 2")
	files.commands["mw brief mw-6ww"] = "bd: the server is away\n"
	files.manifests["mayor"] = "[[boot]]\nfile = \"seats/mayor/gone.md\"\n\n[[boot]]\ncommand = \"mw brief mw-6ww\"\n\n[[boot]]\nfile = \"seats/mayor/vision.md\"\n"
	out, err := bootSet(files, "mayor", "laptop")
	if err == nil || !strings.Contains(err.Error(), "2 of 3 parts") {
		t.Errorf("error %v, want one counting the parts not read", err)
	}
	for _, want := range []string{
		"==> seats/mayor/gone.md <==\n\n[not read: seats/mayor/gone.md: no such file in the vault]\n",
		"==> $ mw brief mw-6ww <==\n\nbd: the server is away\n[failed: exit status 2]\n",
		"==> seats/mayor/vision.md <==\n\n# Vision\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the boot set lacks %q:\n%s", want, out)
		}
	}
}

func TestSeatBootSetRefusesASeatWithNoManifestOrABadOne(t *testing.T) {
	files := fixtureSeatFiles()
	_, err := bootSet(files, "mayor", "laptop")
	if err == nil || !strings.Contains(err.Error(), "seats/mayor/boot.toml") {
		t.Errorf("no manifest: %v", err)
	}
	files.manifests["mayor"] = "[[boot]]\ncap = 1\n"
	_, err = bootSet(files, "mayor", "laptop")
	if err == nil || !strings.Contains(err.Error(), "seats/mayor/boot.toml") || !strings.Contains(err.Error(), "one of file, newest or command") {
		t.Errorf("a bad manifest: %v", err)
	}
	for _, seat := range []string{"", "../mayor", "a/b"} {
		if _, err := bootSet(files, seat, "laptop"); err == nil {
			t.Errorf("the seat %q was accepted", seat)
		}
	}
}

func seatRead(files *fakeSeatBootFiles, seat, topic string) (string, error) {
	var out bytes.Buffer
	err := SeatRead{Files: files, Rigs: files, Seat: seat, Host: "laptop", Out: &out}.Run(context.Background(), topic)
	return out.String(), err
}

func TestSeatReadPrintsOneHeadingsSectionAndRefusesAnUnknownTopic(t *testing.T) {
	files := fixtureSeatFiles()
	files.manifests["mayor"] = mayorManifest
	out, err := seatRead(files, "mayor", "handing-off")
	if err != nil {
		t.Fatal(err)
	}
	want := "## Handing off (at ~180K by `bin/mayor-context`, or at the end of a ticket)\n\nWrite the handoff, start the successor.\n\n### A sub-step of handing off\n\nArm the reaper.\n"
	if out != want {
		t.Errorf("read handing-off:\n%q\nwant\n%q", out, want)
	}
	// A topic of a rig's file reads from the rig's checkout.
	out, err = seatRead(files, "mayor", "governor")
	if err != nil || out != "### Governor\n\nThe one human the factory serves.\n" {
		t.Errorf("read governor: %q, %v", out, err)
	}
	// Nothing of the boot set alone is a topic: vision.md is read at boot, not by need.
	out, err = seatRead(files, "mayor", "vision")
	if err == nil || !strings.Contains(err.Error(), `no topic "vision"`) {
		t.Errorf("read vision: %q, %v", out, err)
	}
	// An unknown topic is refused, naming what there is.
	out, err = seatRead(files, "mayor", "lunch")
	if err == nil || !strings.Contains(err.Error(), `the mayor seat has no topic "lunch"`) || !strings.Contains(err.Error(), "handing-off") || !strings.Contains(err.Error(), "pruning-a-rigs-memory") {
		t.Errorf("read lunch: %q, %v", out, err)
	}
	if out != "" {
		t.Errorf("a refused read printed %q", out)
	}
}

func TestSeatReadTakesAUniquePrefixAndRefusesAnAmbiguousOne(t *testing.T) {
	files := fixtureSeatFiles()
	files.manifests["mayor"] = mayorManifest
	out, err := seatRead(files, "mayor", "pruning")
	if err != nil || !strings.HasPrefix(out, "# Pruning a rig's memory\n") {
		t.Errorf("read by prefix: %q, %v", out, err)
	}
	_, err = seatRead(files, "mayor", "m")
	if err == nil || !strings.Contains(err.Error(), "more than one topic") || !strings.Contains(err.Error(), "mayor-procedures") || !strings.Contains(err.Error(), "millwright") {
		t.Errorf("an ambiguous prefix: %v", err)
	}
	files.files["seats/mayor/pruning.md"] = fixturePruning + "\n## Boot\n\nA second Boot heading, in another file.\n"
	_, err = seatRead(files, "mayor", "boot")
	if err != nil {
		t.Errorf("an exact slug wins over its twin: %v", err)
	}
	out, err = seatRead(files, "mayor", "boot-2")
	if err != nil || !strings.Contains(out, "A second Boot heading") {
		t.Errorf("the twin is boot-2: %q, %v", out, err)
	}
}

func TestSeatReadWithNoTopicPrintsTheIndex(t *testing.T) {
	files := fixtureSeatFiles()
	files.manifests["mayor"] = mayorManifest
	out, err := seatRead(files, "mayor", "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, "mw seat read mayor <topic> prints one of these:\n- seats/mayor/procedures.md: mayor-procedures, boot, ") {
		t.Errorf("the index:\n%s", out)
	}
	files.manifests["deputy"] = "[[boot]]\nnewest = \"seats/deputy/handoffs\"\n"
	out, err = seatRead(files, "deputy", "")
	if err != nil || out != "the deputy seat names no topics in seats/deputy/boot.toml\n" {
		t.Errorf("no topics: %q, %v", out, err)
	}
}

func TestSeatBootSetOverItsManifestsCeilingSaysSoAndFails(t *testing.T) {
	files := fixtureSeatFiles()
	files.files["seats/mayor/big.md"] = "# Big\n\n" + strings.Repeat("A line the Mayor does not need at boot.\n", 100)
	files.manifests["mayor"] = "ceiling = 1000\n\n[[boot]]\nfile = \"seats/mayor/vision.md\"\n\n[[boot]]\nfile = \"seats/mayor/big.md\"\ncap = 0\n"
	out, err := bootSet(files, "mayor", "laptop")
	if err == nil || !strings.Contains(err.Error(), "ceiling of 1000 bytes") {
		t.Fatalf("a boot set over its ceiling: %v", err)
	}
	if !strings.Contains(out, "Where the factory is going.") || !strings.Contains(out, "[over the manifest's ceiling: ") {
		t.Errorf("the boot set is still printed, with the overage said:\n%s", out)
	}
	if len(out) <= 1000 {
		t.Errorf("printed %d bytes, expected the whole over-ceiling set", len(out))
	}

	// Within the ceiling, or with none, the set prints as before.
	files.manifests["mayor"] = strings.Replace(files.manifests["mayor"], "ceiling = 1000", "ceiling = 9000", 1)
	if out, err = bootSet(files, "mayor", "laptop"); err != nil || strings.Contains(out, "over the manifest's ceiling") {
		t.Errorf("under its ceiling: %v\n%s", err, out)
	}
	files.manifests["mayor"] = strings.Replace(files.manifests["mayor"], "ceiling = 9000\n\n", "", 1)
	if out, err = bootSet(files, "mayor", "laptop"); err != nil || strings.Contains(out, "over the manifest's ceiling") {
		t.Errorf("with no ceiling: %v\n%s", err, out)
	}
}

func TestParseSeatManifestReadsTheCeilingAndRefusesABadOne(t *testing.T) {
	m, err := ParseSeatManifest("mayor", "ceiling = 15000\n[[boot]]\nfile = \"a.md\"\n")
	if err != nil || m.Ceiling != 15000 {
		t.Errorf("ceiling: %d, %v", m.Ceiling, err)
	}
	if m, err = ParseSeatManifest("mayor", "[[boot]]\nfile = \"a.md\"\n"); err != nil || m.Ceiling != 0 {
		t.Errorf("no ceiling: %d, %v", m.Ceiling, err)
	}
	if _, err = ParseSeatManifest("mayor", "ceiling = -5\n[[boot]]\nfile = \"a.md\"\n"); err == nil || !strings.Contains(err.Error(), "ceiling") {
		t.Errorf("a bad ceiling: %v", err)
	}
}
