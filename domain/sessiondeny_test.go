package domain

import (
	"reflect"
	"strings"
	"testing"
)

func TestAPermissionRuleIsANameOrANameAndText(t *testing.T) {
	for _, ok := range []string{"Bash", "Bash(mw release *)", "Read(~/.ssh/**)", "mcp__github__get_issue", `Bash(echo "a (b)")`} {
		if err := CheckPermissionRule(ok); err != nil {
			t.Errorf("expected %q to be a rule, got %v", ok, err)
		}
	}
	for _, bad := range []string{"", "Bash(scp", "Bash()", "(x)", "Bash (x)", "Bash(x)y", "Bash(a\nb)", "9Bash", "Ba sh"} {
		err := CheckPermissionRule(bad)
		if err == nil {
			t.Errorf("expected %q to be refused", bad)
		} else if !strings.Contains(err.Error(), "permission rule") {
			t.Errorf("expected the error to name the rule, got %v", err)
		}
	}
}

func TestTheStringsOfAnArrayLiteral(t *testing.T) {
	got, err := TOMLStringArray("[\n  \"Bash(scp *)\", # no copying\n  'Read(~/.ssh/**)',\n]")
	if err != nil || !reflect.DeepEqual(got, []string{"Bash(scp *)", "Read(~/.ssh/**)"}) {
		t.Errorf("expected the two rules, got %v, %v", got, err)
	}
	if got, err := TOMLStringArray("[]"); err != nil || len(got) != 0 {
		t.Errorf("expected no rules, got %v, %v", got, err)
	}
	for _, bad := range []string{`"x"`, `[1, 2]`, `["a" "b"]`, `["a"`, `["a"] x`} {
		if _, err := TOMLStringArray(bad); err == nil || !strings.Contains(err.Error(), "not an array of strings") {
			t.Errorf("expected %q to be refused as not an array of strings, got %v", bad, err)
		}
	}
}

func TestOnlySessionDenyIsJudgedAfterASet(t *testing.T) {
	file, err := ParseConfigTOML("[session]\ndeny = [\"Bash(scp\"]\n[tests]\nx = \"y\"\n")
	if err != nil {
		t.Fatal(err)
	}
	if err := CheckSessionDenyChange(file, "tests", "x"); err != nil {
		t.Errorf("expected another key to be none of its business, got %v", err)
	}
	if err := CheckSessionDenyChange(file, "session", "deny"); err == nil || !strings.Contains(err.Error(), "Bash(scp") {
		t.Errorf("expected the bad rule to be refused by name, got %v", err)
	}
}
