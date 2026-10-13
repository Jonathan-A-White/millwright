package domain

import (
	"strings"
	"testing"
)

func TestParseConfigTOMLAcceptsWhatTheFactoryWrites(t *testing.T) {
	text := `vault = "/v"
cap = 5 # a comment, with "quotes" and a = sign
ratio = 1.5e3
since = 2026-10-12T14:03:09Z
literal = 'C:\path'
"quoted-key" = true
dotted.key = 1

[tester]
rigs = ["lampas",
  "legend", # trailing
]
inline = { a = 1, b = "x" }
text = """
two
lines"""

[[hands]]
name = "a"

[cloud.caps]
desktop = 2
`
	file, err := ParseConfigTOML(text)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []struct{ table, key, value string }{
		{"", "cap", "5"},
		{"", "dotted.key", "1"},
		{"", "quoted-key", "true"},
		{"tester", "rigs", "[\"lampas\",\n  \"legend\", # trailing\n]"},
		{"tester", "inline", `{ a = 1, b = "x" }`},
		{"cloud.caps", "desktop", "2"},
	} {
		if got, ok := file.Lookup(want.table, want.key); !ok || got != want.value {
			t.Errorf("[%s] %s = %q (found %v), want %q", want.table, want.key, got, ok, want.value)
		}
	}
}

func TestParseConfigTOMLRefusesWhatIsNotTOML(t *testing.T) {
	for name, text := range map[string]string{
		"unclosed header":      "[rigs\na = 1\n",
		"no equals":            "[a]\nkey\n",
		"no value":             "[a]\nkey =\n",
		"open string":          "[a]\nkey = \"x\n",
		"bare word":            "[a]\nkey = make\n",
		"open array":           "[a]\nkey = [1,\n",
		"junk after value":     "[a]\nkey = 1 2\n",
		"bad escape":           "[a]\nkey = \"a\\qb\"\n",
		"twice key":            "[a]\nk = 1\nk = 2\n",
		"twice table":          "[a]\nk = 1\n[a]\nj = 2\n",
		"table named as a key": "cap = 5\n[cap]\nx = 1\n",
		"key named as a table": "[a.b]\nx = 1\n[a]\nb = 1\n",
		"leading zero":         "[a]\nk = 05\n",
		"open inline table":    "[a]\nk = { x = 1\n",
		"open multi-line":      "[a]\nk = \"\"\"abc\n",
		"empty header":         "[]\n",
		"unclosed array end":   "[a]\nk = [1, 2\n[b]\n",
	} {
		if _, err := ParseConfigTOML(text); err == nil {
			t.Errorf("%s: parsed %q", name, text)
		}
	}
}

func TestConfigTOMLSet(t *testing.T) {
	for name, tc := range map[string]struct{ in, table, key, literal, want string }{
		"replace keeps comment": {"[a]\nk = 1 # why\nj = 2\n", "a", "k", "9", "[a]\nk = 9 # why\nj = 2\n"},
		"replace keeps spacing": {"[a]\nk   =   1\n", "a", "k", "9", "[a]\nk   =   9\n"},
		"replace an array across lines": {"[a]\nk = [1,\n 2]\nj = 2\n", "a", "k", `"x"`,
			"[a]\nk = \"x\"\nj = 2\n"},
		"add to table end":              {"[a]\nk = 1\n\n[b]\nj = 2\n", "a", "n", "3", "[a]\nk = 1\nn = 3\n\n[b]\nj = 2\n"},
		"add to empty table":            {"[a]\n\n[b]\nj = 2\n", "a", "n", "3", "[a]\nn = 3\n\n[b]\nj = 2\n"},
		"add to last table, no newline": {"[a]\nk = 1", "a", "n", "3", "[a]\nk = 1\nn = 3"},
		"append table":                  {"[a]\nk = 1\n", "b", "n", "3", "[a]\nk = 1\n\n[b]\nn = 3\n"},
		"append table after blank":      {"[a]\nk = 1\n\n", "b", "n", "3", "[a]\nk = 1\n\n[b]\nn = 3\n"},
		"append table, no newline":      {"[a]\nk = 1", "b", "n", "3", "[a]\nk = 1\n\n[b]\nn = 3\n"},
		"empty file":                    {"", "b", "n", "3", "[b]\nn = 3\n"},
		"same key, other table":         {"[a]\nk = 1\n[b]\nk = 2\n", "b", "k", "9", "[a]\nk = 1\n[b]\nk = 9\n"},
		"crlf":                          {"[a]\r\nk = 1\r\n", "a", "n", "3", "[a]\r\nk = 1\r\nn = 3\r\n"},
		"crlf replace":                  {"[a]\r\nk = 1\r\n", "a", "k", "3", "[a]\r\nk = 3\r\n"},
	} {
		file, err := ParseConfigTOML(tc.in)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		got := file.Set(tc.table, tc.key, tc.literal)
		if got != tc.want {
			t.Errorf("%s: got %q, want %q", name, got, tc.want)
		}
		if _, err := ParseConfigTOML(got); err != nil {
			t.Errorf("%s: the result does not parse: %v", name, err)
		}
	}
}

func TestTOMLLiteral(t *testing.T) {
	for in, want := range map[string]string{
		"make build": `"make build"`,
		"5":          "5",
		"true":       "true",
		"1.5":        "1.5",
		`["a"]`:      `["a"]`,
		`"x"`:        `"x"`,
		`say "hi"`:   `"say \"hi\""`,
		`C:\dir`:     `"C:\\dir"`,
		"":           `""`,
		"2026-10-12": "2026-10-12",
		"05":         `"05"`,
		"/home/x":    `"/home/x"`,
		"a\tb":       `"a\tb"`,
		"  padded  ": `"  padded  "`,
	} {
		got, err := TOMLLiteral(in)
		if err != nil || got != want {
			t.Errorf("%q: got %q (%v), want %q", in, got, err, want)
		}
	}
	if _, err := TOMLLiteral("a\nb"); err == nil {
		t.Error("a value over two lines was accepted")
	}
}

func TestTOMLString(t *testing.T) {
	for in, want := range map[string]string{
		`"a \"b\" c"`:     `a "b" c`,
		`'a "b"'`:         `a "b"`,
		`"""x"""`:         "x",
		"\"\"\"\nx\"\"\"": "x",
	} {
		if got, is := TOMLString(in); !is || got != want {
			t.Errorf("%q: got %q (%v), want %q", in, got, is, want)
		}
	}
	if _, is := TOMLString("5"); is {
		t.Error("a number was read as a string")
	}
	if SameTOMLValue(`"5"`, "5") || !SameTOMLValue(`'x'`, `"x"`) {
		t.Error("SameTOMLValue is wrong")
	}
}

func TestSplitConfigKey(t *testing.T) {
	table, key, err := SplitConfigKey("cloud.caps.desktop")
	if err != nil || table != "cloud.caps" || key != "desktop" {
		t.Errorf("got %q %q %v", table, key, err)
	}
	if table, key, err := SplitConfigKey("cap"); err != nil || table != "" || key != "cap" {
		t.Errorf("cap: got %q %q %v", table, key, err)
	}
	if _, _, err := SplitConfigKey("nokey"); err == nil || !strings.Contains(err.Error(), "builder_jobs") {
		t.Errorf("an unknown root key must be refused naming the known ones: %v", err)
	}
	for _, bad := range []string{".k", "t.", "a b.k", "a..k", `a."b"`} {
		if _, _, err := SplitConfigKey(bad); err == nil {
			t.Errorf("%q: %v", bad, err)
		}
	}
}

func TestSetARootKeyThatIsNotThereYet(t *testing.T) {
	for name, c := range map[string]struct{ in, want string }{
		"empty file":       {"", "cap = 2\n"},
		"only a table":     {"[rigs]\na = \"b\"\n", "cap = 2\n\n[rigs]\na = \"b\"\n"},
		"a comment at top": {"# hi\n[rigs]\n", "cap = 2\n\n# hi\n[rigs]\n"},
		"after a root key": {"vault = \"/v\"\n\n[rigs]\n", "vault = \"/v\"\ncap = 2\n\n[rigs]\n"},
	} {
		f, err := ParseConfigTOML(c.in)
		if err != nil {
			t.Fatal(name, err)
		}
		got := f.Set("", "cap", "2")
		if got != c.want {
			t.Errorf("%s: got %q want %q", name, got, c.want)
		}
		if re, err := ParseConfigTOML(got); err != nil {
			t.Errorf("%s: does not parse: %v", name, err)
		} else if v, ok := re.Lookup("", "cap"); !ok || v != "2" {
			t.Errorf("%s: reads back %q %v", name, v, ok)
		}
	}
}
