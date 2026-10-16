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
	want := BoostTables{
		"tests":         {"argus": "npm test", "lampas": "npm ci && TZ=UTC npm test"},
		"changed_specs": {"lampas": "npm run build && npx playwright test {specs}"},
	}
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
	tables := BoostTables{
		"tests": {"millwright": "make test", "argus": `npm test -- --grep "a\b"`, "weird": "true"},
	}
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
	if !reflect.DeepEqual(back["tests"], tables["tests"]) || len(back["changed_specs"]) != 0 {
		t.Fatalf("the text does not read back as the tables: %v", back)
	}
}

func TestBoostTablesTextKeepsACommandOnOneLine(t *testing.T) {
	text := BoostTables{"tests": {"argus": "npm ci\nnpm test"}}.Text()
	if !strings.Contains(text, `argus = "npm ci\u000Anpm test"`+"\n") {
		t.Fatalf("expected the command on one line, got %q", text)
	}
}

func TestBoostTablesDriftNamesEachRigMissingDifferentOrExtra(t *testing.T) {
	home := BoostTables{
		"tests":         {"argus": "npm test", "postern": "npm ci && npm test", "millwright": "make test"},
		"changed_specs": {"lampas": "npx playwright test {specs}"},
	}
	vault := BoostTables{
		"tests": {"postern": "npm test", "millwright": "make test", "gone": "make"},
	}
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
