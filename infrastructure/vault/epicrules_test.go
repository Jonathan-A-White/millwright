package vault_test

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/domain"
	"github.com/Jonathan-A-White/millwright/infrastructure/vault"
)

func writeRigFile(t *testing.T, dir, rig, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "rigs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "rigs", rig+".toml"), []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestEpicRequirementsAreReadFromTheRigsFileInTheVault(t *testing.T) {
	dir := aVault(t)
	v := vault.New(dir)
	writeRigFile(t, dir, "spell-forge", "# what a spell-forge epic needs\nepic_sections = [\"Demo\"]\nepic_last_story_labels = ['demo']  # last\n")

	got, err := v.EpicRequirements(context.Background(), "spell-forge")
	if err != nil {
		t.Fatalf("reading the rig's file: %v", err)
	}
	want := domain.EpicRequirements{Sections: []string{"Demo"}, LastStoryLabels: []string{"demo"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("requirements = %+v, want %+v", got, want)
	}

	// Another host reads the same clone and gets the same answer.
	again, err := vault.New(dir).EpicRequirements(context.Background(), "spell-forge")
	if err != nil || !reflect.DeepEqual(again, want) {
		t.Errorf("a second reader got %+v, %v", again, err)
	}
}

func TestAddingASectionNameEnforcesItWithNoCodeChange(t *testing.T) {
	dir := aVault(t)
	v := vault.New(dir)
	epic := domain.EpicShape{Description: "## Demo\nshow it"}

	writeRigFile(t, dir, "spell-forge", `epic_sections = ["Demo"]`)
	rules, _ := v.EpicRequirements(context.Background(), "spell-forge")
	if short := rules.Check(epic); len(short) != 0 {
		t.Fatalf("an epic with a Demo section meets [Demo]: %v", short)
	}

	// The Governor adds a second name to the list in the vault; nothing is rebuilt.
	writeRigFile(t, dir, "spell-forge", "epic_sections = [\n  \"Demo\",\n  \"Rollback\",\n]\n")
	rules, err := v.EpicRequirements(context.Background(), "spell-forge")
	if err != nil {
		t.Fatalf("reading the longer list: %v", err)
	}
	short := rules.Check(epic)
	if len(short) != 1 || short[0].Name != "Rollback" {
		t.Errorf("the same epic must now lack Rollback, got %v", short)
	}
}

func TestARigWithNoFileOrNoKeysIsUnchecked(t *testing.T) {
	dir := aVault(t)
	v := vault.New(dir)
	got, err := v.EpicRequirements(context.Background(), "millwright")
	if err != nil || got.Any() {
		t.Errorf("a rig with no file: %+v, %v", got, err)
	}
	writeRigFile(t, dir, "empty", "# nothing required\n")
	got, err = v.EpicRequirements(context.Background(), "empty")
	if err != nil || got.Any() {
		t.Errorf("a rig whose file lists nothing: %+v, %v", got, err)
	}
}

func TestAMisspeltKeyOrARigNameOutsideTheVaultIsRefused(t *testing.T) {
	dir := aVault(t)
	v := vault.New(dir)
	writeRigFile(t, dir, "spell-forge", `epic_section = ["Demo"]`)
	if _, err := v.EpicRequirements(context.Background(), "spell-forge"); err == nil || !strings.Contains(err.Error(), "epic_section") {
		t.Errorf("a misspelt key must be named in the refusal, got %v", err)
	}
	if _, err := v.EpicRequirements(context.Background(), "../seats"); err == nil {
		t.Error("a rig name reaching outside the vault must be refused")
	}
	writeRigFile(t, dir, "bare", `epic_sections = [Demo]`)
	if _, err := v.EpicRequirements(context.Background(), "bare"); err == nil {
		t.Error("an unquoted name must be refused")
	}
}

func TestAGuestRigsFileNamesItsOwnerAndStillRefusesAnUnknownKey(t *testing.T) {
	dir := t.TempDir()
	v := vault.New(dir)

	writeRigFile(t, dir, "argus", "# Luke's rig\nguest = \"Luke\"  # A32\nepic_sections = [\"Demo\"]\n")
	got, err := v.EpicRequirements(context.Background(), "argus")
	if err != nil {
		t.Fatalf("reading a guest rig's file: %v", err)
	}
	want := domain.EpicRequirements{Sections: []string{"Demo"}, Guest: "Luke"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}

	writeRigFile(t, dir, "argus", "guest = 'Luke (lukeaverywhite-alt)'\n")
	got, err = v.EpicRequirements(context.Background(), "argus")
	if err != nil || got.Guest != "Luke (lukeaverywhite-alt)" {
		t.Errorf("single quotes and parentheses: got %+v, %v", got, err)
	}

	writeRigFile(t, dir, "argus", "guest = Luke\n")
	if _, err := v.EpicRequirements(context.Background(), "argus"); err == nil {
		t.Error("expected an unquoted owner to be an error")
	}

	writeRigFile(t, dir, "argus", "gest = \"Luke\"\n")
	if _, err := v.EpicRequirements(context.Background(), "argus"); err == nil || !strings.Contains(err.Error(), "gest") {
		t.Errorf("expected an unknown key to be an error naming it, got %v", err)
	}
}

func TestARigsFileNamesTheFilesWhoseVersionLandingRaises(t *testing.T) {
	dir := aVault(t)
	v := vault.New(dir)
	writeRigFile(t, dir, "lampas", "version_files = [\"package.json\", \"package-lock.json\"]  # the patch at landing\n")

	got, err := v.EpicRequirements(context.Background(), "lampas")
	if err != nil {
		t.Fatalf("reading the rig's file: %v", err)
	}
	want := domain.EpicRequirements{VersionFiles: []string{"package.json", "package-lock.json"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("requirements = %+v, want %+v", got, want)
	}
	if got.Any() {
		t.Errorf("version_files is no requirement of an epic, but Any() = true")
	}

	// A list over several lines, beside the other keys.
	writeRigFile(t, dir, "whisper-hid", "epic_sections = [\"Demo\"]\nversion_files = [\n  \"pwa/package.json\",\n  \"pwa/package-lock.json\",\n]\n")
	got, err = v.EpicRequirements(context.Background(), "whisper-hid")
	if err != nil {
		t.Fatalf("reading the rig's file: %v", err)
	}
	want = domain.EpicRequirements{Sections: []string{"Demo"}, VersionFiles: []string{"pwa/package.json", "pwa/package-lock.json"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("requirements = %+v, want %+v", got, want)
	}
}

func TestVersionFilesStillRefusesAnUnknownKeyAndAPathOutsideTheRig(t *testing.T) {
	dir := aVault(t)
	v := vault.New(dir)

	writeRigFile(t, dir, "lampas", "version_files = [\"package.json\"]\nversion_file = [\"package.json\"]\n")
	if _, err := v.EpicRequirements(context.Background(), "lampas"); err == nil || !strings.Contains(err.Error(), "version_file") {
		t.Errorf("a misspelt key beside version_files: err = %v, want it to name the key", err)
	}

	for _, bad := range []string{`["/etc/passwd"]`, `["../package.json"]`, `["a/../../b.json"]`} {
		writeRigFile(t, dir, "lampas", "version_files = "+bad+"\n")
		if _, err := v.EpicRequirements(context.Background(), "lampas"); err == nil || !strings.Contains(err.Error(), "version_files") {
			t.Errorf("version_files = %s: err = %v, want a refusal naming version_files", bad, err)
		}
	}
}

func TestARigsFileNamesTheChangelogFilesALandingWrites(t *testing.T) {
	dir := aVault(t)
	v := vault.New(dir)
	writeRigFile(t, dir, "lampas", "version_files = [\"package.json\"]\nchangelog_files = [\"public/changelog.json\", \"CHANGELOG.md\"]\n")

	got, err := v.EpicRequirements(context.Background(), "lampas")
	if err != nil {
		t.Fatalf("reading the rig's file: %v", err)
	}
	want := domain.EpicRequirements{VersionFiles: []string{"package.json"}, ChangelogFiles: []string{"public/changelog.json", "CHANGELOG.md"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("requirements = %+v, want %+v", got, want)
	}
	if got.Any() {
		t.Errorf("changelog_files is no requirement of an epic, but Any() = true")
	}

	for _, bad := range []string{`["/etc/passwd.md"]`, `["../CHANGELOG.md"]`, `["changelog.txt"]`} {
		writeRigFile(t, dir, "lampas", "changelog_files = "+bad+"\n")
		if _, err := v.EpicRequirements(context.Background(), "lampas"); err == nil || !strings.Contains(err.Error(), "changelog_files") {
			t.Errorf("changelog_files = %s: err = %v, want a refusal naming changelog_files", bad, err)
		}
	}
}
