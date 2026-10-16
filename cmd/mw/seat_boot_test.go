package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// seatVault writes a vault for the seat boot commands into a temp directory,
// points the config at it, and returns it.
func seatVault(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, text := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mwConfig(t, "vault = \""+dir+"\"\nhost = \"laptop\"\n")
	return dir
}

// runSeat runs mw seat <args> and returns what it printed and its error.
func runSeat(t *testing.T, args ...string) (string, error) {
	t.Helper()
	out := &bytes.Buffer{}
	root := newRootCmd()
	root.SetOut(out)
	root.SetErr(out)
	root.SetArgs(append([]string{"seat"}, args...))
	err := root.Execute()
	return out.String(), err
}

const deputyHandoff = "# Deputy handoff 693\n\nRead the charter, then this. Mail worked this sitting: mw-oz7aa2.\n"

func TestSeatBootDeputyPrintsExactlyTheManifestsFilesWithinTheirCaps(t *testing.T) {
	long := "# Mail\n\n" + strings.Repeat("A line about mail that the Deputy reads.\n", 10)
	seatVault(t, map[string]string{
		"seats/deputy/boot.toml": "cap = 200\n\n[[boot]]\nnewest = \"seats/deputy/handoffs\"\n\n[[boot]]\nfile = \"seats/deputy/mail.md\"\n\n" +
			"[[boot]]\ncommand = \"printf 'mw-abc Landed: mw-gq6.1\\\\n'\"\ncap = 0\n",
		"seats/deputy/charter.md":                 "# Deputy — charter\n\nNOT PRINTED: the charter is the system prompt.\n",
		"seats/deputy/mail.md":                    long,
		"seats/deputy/handoffs/2026-10-15-692.md": "# Deputy handoff 692\n\nOLD.\n",
		"seats/deputy/handoffs/2026-10-16-693.md": deputyHandoff,
		"seats/deputy/ledger.md":                  "2026-10-01 NOT PRINTED either.\n",
	})
	out, err := runSeat(t, "boot", "deputy")
	if err != nil {
		t.Fatalf("mw seat boot deputy: %v\n%s", err, out)
	}
	kept := "# Mail\n\n" + strings.Repeat("A line about mail that the Deputy reads.\n", 4) + "[cut at 200 of " + strconv.Itoa(len(long)) + " bytes]\n"
	want := "# What the deputy seat reads at boot (seats/deputy/boot.toml)\n\n" +
		"==> seats/deputy/handoffs/2026-10-16-693.md <==\n\n" + deputyHandoff + "\n" +
		"==> seats/deputy/mail.md <==\n\n" + kept + "\n" +
		"==> $ printf 'mw-abc Landed: mw-gq6.1\\n' <==\n\nmw-abc Landed: mw-gq6.1\n"
	if out != want {
		t.Errorf("mw seat boot deputy printed\n%s\nwant\n%s", out, want)
	}
}

func TestSeatBootRefusesASeatWithNoManifest(t *testing.T) {
	seatVault(t, map[string]string{"seats/deputy/charter.md": "# Deputy\n"})
	out, err := runSeat(t, "boot", "deputy")
	if err == nil || !strings.Contains(err.Error(), "seats/deputy/boot.toml") {
		t.Errorf("mw seat boot deputy: %v\n%s", err, out)
	}
}

const mayorProcedures = "# Mayor procedures (one place)\n\n## Boot\n\n1. Charter, vision, the newest handoff.\n\n" +
	"## Handing off (at ~180K)\n\nWrite the handoff, start the successor.\n\n## Standing asks of the Governor\n\n- Verify before releasing.\n"

func TestSeatReadMayorPrintsOneHeadingsSectionAndRefusesAnUnknownTopic(t *testing.T) {
	seatVault(t, map[string]string{
		"seats/mayor/boot.toml":     "[[boot]]\nfile = \"seats/mayor/vision.md\"\n\n[[topic]]\nfile = \"seats/mayor/procedures.md\"\n",
		"seats/mayor/vision.md":     "# Vision\n\nWhere the factory is going.\n",
		"seats/mayor/procedures.md": mayorProcedures,
	})
	out, err := runSeat(t, "read", "mayor", "handing-off")
	if err != nil {
		t.Fatalf("mw seat read mayor handing-off: %v\n%s", err, out)
	}
	if out != "## Handing off (at ~180K)\n\nWrite the handoff, start the successor.\n" {
		t.Errorf("printed %q", out)
	}

	out, err = runSeat(t, "read", "mayor", "lunch")
	if err == nil || !strings.Contains(err.Error(), `the mayor seat has no topic "lunch"`) || !strings.Contains(err.Error(), "standing-asks-of-the-governor") {
		t.Errorf("an unknown topic: %v\n%s", err, out)
	}

	// The boot set ends with the same index, so a seat knows what it can ask for.
	out, err = runSeat(t, "boot", "mayor")
	if err != nil {
		t.Fatalf("mw seat boot mayor: %v\n%s", err, out)
	}
	if !strings.HasSuffix(out, "==> Read by need <==\n\nmw seat read mayor <topic> prints one of these:\n"+
		"- mayor-procedures: Mayor procedures (one place) (seats/mayor/procedures.md)\n"+
		"- boot: Boot (seats/mayor/procedures.md)\n"+
		"- handing-off: Handing off (at ~180K) (seats/mayor/procedures.md)\n"+
		"- standing-asks-of-the-governor: Standing asks of the Governor (seats/mayor/procedures.md)\n") {
		t.Errorf("the boot set's index:\n%s", out)
	}
}

func TestSeatReadReadsATopicOfARigsFile(t *testing.T) {
	dir := seatVault(t, map[string]string{
		"seats/mayor/boot.toml": "[[boot]]\nfile = \"seats/mayor/vision.md\"\n\n[[topic]]\nrig = \"millwright\"\nfile = \"CONTEXT.md\"\n",
		"seats/mayor/vision.md": "# Vision\n",
	})
	rigDir := filepath.Join(dir, "rig")
	if err := os.MkdirAll(rigDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rigDir, "CONTEXT.md"), []byte("# Millwright\n\n## Language\n\n### Governor\n\nThe one human the factory serves.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mwConfig(t, "vault = \""+dir+"\"\nhost = \"laptop\"\n\n[rigs]\nmillwright = \""+rigDir+"\"\n")
	out, err := runSeat(t, "read", "mayor", "governor")
	if err != nil || out != "### Governor\n\nThe one human the factory serves.\n" {
		t.Errorf("mw seat read mayor governor: %q, %v", out, err)
	}

	mwConfig(t, "vault = \""+dir+"\"\nhost = \"laptop\"\n")
	_, err = runSeat(t, "read", "mayor", "governor")
	if err == nil || !strings.Contains(err.Error(), "millwright") {
		t.Errorf("a rig not checked out here: %v", err)
	}
}
