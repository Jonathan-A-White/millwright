package domain

import (
	"reflect"
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
