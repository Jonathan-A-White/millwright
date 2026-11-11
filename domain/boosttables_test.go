package domain

import (
	"reflect"
	"slices"
	"strings"
	"testing"
)

func mustParse(t *testing.T, text string) *ConfigTOML {
	t.Helper()
	file, err := ParseConfigTOML(text)
	if err != nil {
		t.Fatal(err)
	}
	return file
}

func TestBoostTablesOfReadsOnlyTestsAndChangedSpecs(t *testing.T) {
	home := mustParse(t, `vault = "/v"

[rigs]
argus = "/h/argus"

[tests]
argus = "npm test"
lampas = 'npm ci && TZ=UTC npm test'

[changed_specs]
lampas = "npm run build && npx playwright test {specs}"

[after_landing]
millwright = "make build"
`)
	got, err := BoostTablesOf(home)
	if err != nil {
		t.Fatal(err)
	}
	want := BoostTables{Lines: map[string]map[string]string{
		"tests":         {"argus": "npm test", "lampas": "npm ci && TZ=UTC npm test"},
		"changed_specs": {"lampas": "npm run build && npx playwright test {specs}"},
	}, Parts: map[string][]BoostPart{}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestBoostTablesOfRefusesALineThatIsNotAString(t *testing.T) {
	_, err := BoostTablesOf(mustParse(t, "[tests]\nargus = 5\n"))
	if err == nil || !strings.Contains(err.Error(), "argus") {
		t.Fatalf("expected an error naming argus, got %v", err)
	}
}

func TestBoostTablesTextIsOneLinePerRigInEachTable(t *testing.T) {
	tables := BoostTables{Lines: map[string]map[string]string{
		"tests": {"millwright": "make test", "argus": `npm test -- --grep "a\b"`, "weird": "true"},
	}}
	text := tables.Text()
	if !strings.HasPrefix(text, "#") {
		t.Fatalf("expected the file to open with its header comment, got %q", text)
	}
	body := text[strings.Index(text, "\n[tests]\n")+1:]
	want := "[tests]\n" +
		`argus = "npm test -- --grep \"a\\b\""` + "\n" +
		`millwright = "make test"` + "\n" +
		`weird = "true"` + "\n" +
		"\n[changed_specs]\n"
	if body != want {
		t.Fatalf("got\n%s\nwant\n%s", body, want)
	}
	back, err := BoostTablesOf(mustParse(t, text))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(back.Lines["tests"], tables.Lines["tests"]) || len(back.Lines["changed_specs"]) != 0 {
		t.Fatalf("the text does not read back as the tables: %v", back)
	}
}

func TestBoostTablesTextKeepsACommandOnOneLine(t *testing.T) {
	text := BoostTables{Lines: map[string]map[string]string{"tests": {"argus": "npm ci\nnpm test"}}}.Text()
	if !strings.Contains(text, `argus = "npm ci\u000Anpm test"`+"\n") {
		t.Fatalf("expected the command on one line, got %q", text)
	}
}

func TestBoostTablesDriftNamesEachRigMissingDifferentOrExtra(t *testing.T) {
	home := BoostTables{Lines: map[string]map[string]string{
		"tests":         {"argus": "npm test", "postern": "npm ci && npm test", "millwright": "make test"},
		"changed_specs": {"lampas": "npx playwright test {specs}"},
	}}
	vault := BoostTables{Lines: map[string]map[string]string{
		"tests": {"postern": "npm test", "millwright": "make test", "gone": "make"},
	}}
	want := []string{
		"[tests] argus: missing",
		"[tests] gone: not on the home",
		"[tests] postern: differs",
		"[changed_specs] lampas: missing",
	}
	if got := BoostTablesDrift(home, vault); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
	if got := BoostTablesDrift(home, home); len(got) != 0 {
		t.Fatalf("level tables drift: %q", got)
	}
}

const homeWithParts = `[tests]
lampas = "npm test"

[test_parts.lampas]
"install" = "npm ci"
"vitest 2/2" = "npx vitest run --shard 2/2"
"vitest 1/2" = "npx vitest run --shard 1/2"

[test_parts.postern]
"shots" = 'npm run gate:shots'

[heavy_net]
postern = true
`

func TestBoostTablesOfReadsTestPartsInTheOrderTheFileHasThem(t *testing.T) {
	got, err := BoostTablesOf(mustParse(t, homeWithParts))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][]BoostPart{
		"lampas": {
			{"install", "npm ci"},
			{"vitest 2/2", "npx vitest run --shard 2/2"},
			{"vitest 1/2", "npx vitest run --shard 1/2"},
		},
		"postern": {{"shots", "npm run gate:shots"}},
	}
	if !reflect.DeepEqual(got.Parts, want) {
		t.Fatalf("got %v, want %v", got.Parts, want)
	}
}

func TestBoostTablesOfRefusesATestPartThatIsNotAString(t *testing.T) {
	_, err := BoostTablesOf(mustParse(t, "[test_parts.lampas]\n\"install\" = 5\n"))
	if err == nil || !strings.Contains(err.Error(), "install") {
		t.Fatalf("expected an error naming install, got %v", err)
	}
}

func TestBoostTablesTextWritesEachRigsPartsAfterTheLinesAndReadsBack(t *testing.T) {
	tables, err := BoostTablesOf(mustParse(t, homeWithParts))
	if err != nil {
		t.Fatal(err)
	}
	text := tables.Text()
	body := text[strings.Index(text, "\n[test_parts.lampas]\n")+1:]
	want := "[test_parts.lampas]\n" +
		`"install" = "npm ci"` + "\n" +
		`"vitest 2/2" = "npx vitest run --shard 2/2"` + "\n" +
		`"vitest 1/2" = "npx vitest run --shard 1/2"` + "\n" +
		"\n[test_parts.postern]\n" +
		`"shots" = "npm run gate:shots"` + "\n"
	if body != want {
		t.Fatalf("got\n%s\nwant\n%s", body, want)
	}
	back, err := BoostTablesOf(mustParse(t, text))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(back.Parts, tables.Parts) {
		t.Fatalf("the text does not read back as the parts: %v", back.Parts)
	}
}

func TestBoostTablesDriftNamesATestPartsTableMissingDifferentOrExtra(t *testing.T) {
	home := BoostTables{Parts: map[string][]BoostPart{
		"lampas":  {{"a", "1"}, {"b", "2"}},
		"postern": {{"a", "1"}},
		"argus":   {{"a", "1"}},
	}}
	vault := BoostTables{Parts: map[string][]BoostPart{
		"lampas": {{"b", "2"}, {"a", "1"}},
		"argus":  {{"a", "1"}},
		"gone":   {{"a", "1"}},
	}}
	want := []string{
		"[test_parts.gone]: not on the home",
		"[test_parts.lampas]: differs",
		"[test_parts.postern]: missing",
	}
	if got := BoostTablesDrift(home, vault); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

const levelVaultText = `# header
[tests]
lampas = "npm ci && npm test"
postern = "make test"

[changed_specs]
lampas = "npx playwright test {specs}"

[test_parts.lampas]
"unit" = "npm test"
"e2e" = "npx playwright test"

[test_parts.postern]
"gate" = "make gate"
`

func levelVault(t *testing.T) BoostTables {
	t.Helper()
	tables, err := BoostTablesOf(mustParse(t, levelVaultText))
	if err != nil {
		t.Fatal(err)
	}
	return tables
}

func TestLevelBoostConfigAppendsATableAndLinesTheConfigLacksAndChangesNoOtherByte(t *testing.T) {
	config := "vault = \"/v\" # mine\nhost = \"cloud1\"\n\n[rigs]\nmillwright = \"/m\"\nlampas = \"/l\"\npostern = \"/p\"\n\n[tests]\nlampas = \"npm ci && npm test\"\n\n[test_parts.lampas]\n\"unit\" = \"npm test\"\n\"e2e\" = \"npx playwright test\"\n"
	out, appended, differing, err := LevelBoostConfig(config, levelVault(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(differing) != 0 {
		t.Errorf("differing = %v, want none", differing)
	}
	wantAppended := []string{"[tests] postern", "[changed_specs] lampas", "[test_parts.postern]"}
	if !reflect.DeepEqual(appended, wantAppended) {
		t.Errorf("appended = %v, want %v", appended, wantAppended)
	}
	for _, line := range strings.Split(strings.TrimSuffix(config, "\n"), "\n") {
		if !strings.Contains(out, line+"\n") {
			t.Errorf("the line %q was lost from\n%s", line, out)
		}
	}
	if !strings.HasSuffix(out, "\n[test_parts.postern]\n\"gate\" = \"make gate\"\n") {
		t.Errorf("the postern parts table is not appended whole at the end:\n%s", out)
	}
	parsed := mustParse(t, out)
	got, err := BoostTablesOf(parsed)
	if err != nil {
		t.Fatal(err)
	}
	if got.Lines["tests"]["postern"] != "make test" || got.Lines["changed_specs"]["lampas"] != "npx playwright test {specs}" {
		t.Errorf("the lines were not appended: %+v", got.Lines)
	}

	again, appended, differing, err := LevelBoostConfig(out, levelVault(t))
	if err != nil || again != out || len(appended) != 0 || len(differing) != 0 {
		t.Errorf("a second run changed %q (appended %v, differing %v, err %v)", again, appended, differing, err)
	}
}

func TestLevelBoostConfigSaysADifferentLineAndLeavesIt(t *testing.T) {
	config := "[rigs]\nlampas = \"/l\"\n\n[tests]\nlampas = \"make test\"\n\n[test_parts.lampas]\n\"unit\" = \"npm test\"\n"
	out, appended, differing, err := LevelBoostConfig(config, levelVault(t))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "lampas = \"make test\"\n") || strings.Contains(out, "npm ci") {
		t.Errorf("a differing line was overwritten:\n%s", out)
	}
	want := []string{"[tests] lampas: differs", "[test_parts.lampas]: differs"}
	if !reflect.DeepEqual(differing, want) {
		t.Errorf("differing = %v, want %v", differing, want)
	}
	if !reflect.DeepEqual(appended, []string{"[changed_specs] lampas"}) {
		t.Errorf("appended = %v", appended)
	}
}

func TestLevelBoostConfigTakesOnlyTheRigsTheBoostHas(t *testing.T) {
	config := "[rigs]\nmillwright = \"/m\"\n"
	out, appended, _, err := LevelBoostConfig(config, levelVault(t))
	if err != nil || out != config || len(appended) != 0 {
		t.Errorf("a Boost with no lampas or postern took %q, %v (err %v)", out, appended, err)
	}
}

const homeWithSession = "[tests]\nargus = \"npm test\"\n\n[session]\ndeny = [\"Bash(scp *)\", 'Read(~/.ssh/**)']\n"

func TestBoostTablesOfReadsTheSessionDenyRulesInOrder(t *testing.T) {
	got, err := BoostTablesOf(mustParse(t, homeWithSession))
	if err != nil {
		t.Fatal(err)
	}
	if !got.HasSessionDeny || !reflect.DeepEqual(got.SessionDeny, []string{"Bash(scp *)", "Read(~/.ssh/**)"}) {
		t.Errorf("got %+v", got)
	}
	none, err := BoostTablesOf(mustParse(t, "[tests]\nargus = \"npm test\"\n"))
	if err != nil || none.HasSessionDeny {
		t.Errorf("a config with no [session] deny should hold none, got %+v, %v", none, err)
	}
	if _, err := BoostTablesOf(mustParse(t, "[session]\ndeny = 5\n")); err == nil || !strings.Contains(err.Error(), "session.deny") {
		t.Errorf("a deny that is not an array of strings should be refused naming it, got %v", err)
	}
}

func TestBoostTablesTextWritesTheSessionDenyOnOneLineAndReadsBack(t *testing.T) {
	tables, err := BoostTablesOf(mustParse(t, homeWithSession))
	if err != nil {
		t.Fatal(err)
	}
	text := tables.Text()
	if want := "\n[session]\ndeny = [\"Bash(scp *)\", \"Read(~/.ssh/**)\"]\n"; !strings.HasSuffix(text, want) {
		t.Errorf("expected the text to end with %q, got:\n%s", want, text)
	}
	back, err := BoostTablesOf(mustParse(t, text))
	if err != nil || !reflect.DeepEqual(back, tables) {
		t.Errorf("the text did not read back as the tables: %+v vs %+v (%v)", back, tables, err)
	}
	// A home with no deny writes no [session] table at all.
	none, _ := BoostTablesOf(mustParse(t, "[tests]\nargus = \"npm test\"\n"))
	if strings.Contains(none.Text(), "\n[session]\n") {
		t.Errorf("expected no [session] table, got:\n%s", none.Text())
	}
}

func TestBoostTablesDriftNamesTheSessionDenyMissingDifferentOrGone(t *testing.T) {
	home, _ := BoostTablesOf(mustParse(t, homeWithSession))
	other, _ := BoostTablesOf(mustParse(t, "[session]\ndeny = [\"Bash(scp *)\"]\n"))
	reordered, _ := BoostTablesOf(mustParse(t, "[session]\ndeny = [\"Read(~/.ssh/**)\", \"Bash(scp *)\"]\n"))
	for name, c := range map[string]struct {
		home, vault BoostTables
		want        string
	}{
		"missing":   {home, BoostTables{}, "[session] deny: missing"},
		"differs":   {home, other, "[session] deny: differs"},
		"order":     {home, reordered, "[session] deny: differs"},
		"not there": {BoostTables{}, home, "[session] deny: not on the home"},
	} {
		if got := BoostTablesDrift(c.home, c.vault); !slices.Contains(got, c.want) {
			t.Errorf("%s: expected %q in %v", name, c.want, got)
		}
	}
	if got := BoostTablesDrift(home, home); len(got) != 0 {
		t.Errorf("a level deny should be no drift, got %v", got)
	}
}

func TestBoostTablesHeldDifferentlyNamesADifferingSessionDeny(t *testing.T) {
	vault, _ := BoostTablesOf(mustParse(t, homeWithSession))
	other, _ := BoostTablesOf(mustParse(t, "[session]\ndeny = [\"Bash(scp *)\"]\n"))
	reordered, _ := BoostTablesOf(mustParse(t, "[session]\ndeny = [\"Read(~/.ssh/**)\", \"Bash(scp *)\"]\n"))
	for name, here := range map[string]BoostTables{"fewer rules": other, "other order": reordered} {
		got := BoostTablesHeldDifferently(here, vault)
		want := []BoostHeld{{Key: "session.deny", Here: SessionDenyLiteral(here.SessionDeny), Vault: SessionDenyLiteral(vault.SessionDeny)}}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s: got %+v, want %+v", name, got, want)
		}
	}
	// A level list, a Boost without the key and a vault without it are left out:
	// sync levels what a Boost lacks.
	for name, c := range map[string]struct{ here, vault BoostTables }{
		"level":       {vault, vault},
		"boost lacks": {BoostTables{}, vault},
		"vault lacks": {vault, BoostTables{}},
		"both lack":   {BoostTables{}, BoostTables{}},
	} {
		if got := BoostTablesHeldDifferently(c.here, c.vault); len(got) != 0 {
			t.Errorf("%s: expected nothing held differently, got %+v", name, got)
		}
	}
}
