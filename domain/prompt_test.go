package domain_test

import (
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/domain"
)

func TestParsePromptOptionReadsTypeDefaultAndRequired(t *testing.T) {
	for spec, want := range map[string]domain.PromptOption{
		"count:int=5":           {Flag: "count", Type: "int", Default: "5"},
		"since:string:required": {Flag: "since", Type: "string", Required: true},
		"all:bool=true":         {Flag: "all", Type: "bool", Default: "true"},
		"note:string":           {Flag: "note", Type: "string"},
		"where:string=a:b":      {Flag: "where", Type: "string", Default: "a:b"},
	} {
		got, err := domain.ParsePromptOption(spec)
		if err != nil || got != want {
			t.Errorf("%q: expected %+v, got %+v (%v)", spec, want, got, err)
		}
		if got.String() != spec {
			t.Errorf("%q: expected the spec back, got %q", spec, got.String())
		}
	}
}

func TestParsePromptOptionRefusesWhatIsNotASpec(t *testing.T) {
	for spec, why := range map[string]string{
		"count":                "is not <flag>:<type>=<default>",
		"Count:int=5":          "is not a flag name",
		"count:float=5":        "is not a type",
		"count:int=many":       "is not an int",
		"all:bool=maybe":       "is not a bool",
		"count:int=5:required": "required, so it has no default",
	} {
		if _, err := domain.ParsePromptOption(spec); err == nil || !strings.Contains(err.Error(), why) {
			t.Errorf("%q: expected an error saying %q, got %v", spec, why, err)
		}
	}
}

func TestFillReplacesEachFlagAndNamesTheSignatureWhenTheCallIsWrong(t *testing.T) {
	p := domain.Prompt{Name: "top", Signature: []string{"count:int=5", "since:string:required", "all:bool=false"}, Body: "<count> since <since> (<all>) <count>"}

	got, err := p.Fill([]domain.PromptArg{{Flag: "since", Value: "monday"}, {Flag: "all", Value: "true"}})
	if err != nil || got != "5 since monday (true) 5" {
		t.Fatalf("expected the default and the values filled, got %q (%v)", got, err)
	}
	for _, c := range []struct {
		args []domain.PromptArg
		why  string
	}{
		{[]domain.PromptArg{{Flag: "since", Value: "x"}, {Flag: "bogus", Value: "1"}}, "--bogus is not an option"},
		{[]domain.PromptArg{{Flag: "since", Value: "x"}, {Flag: "count", Value: "many"}}, `--count: "many" is not an int`},
		{[]domain.PromptArg{{Flag: "since", Value: "x"}, {Flag: "since", Value: "y"}}, "--since is given twice"},
		{nil, "--since is required"},
	} {
		_, err := p.Fill(c.args)
		if err == nil || !strings.Contains(err.Error(), c.why) || !strings.Contains(err.Error(), "its signature is --count:int=5 --since:string:required --all:bool=false") {
			t.Errorf("expected %q naming the signature, got %v", c.why, err)
		}
	}
}
