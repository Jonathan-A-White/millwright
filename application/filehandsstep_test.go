package application_test

import (
	"context"
	"strings"
	"testing"
)

// mw-gq6.796: an acceptance or description that needs a shell on another host
// is warned of, with the advice to split that half into a hands step.
func TestAStoryThatNeedsAShellOnAnotherHostIsWarnedOfAndFiledAnyway(t *testing.T) {
	for name, text := range map[string]string{
		"ssh":     "ssh root@vps.example.com systemctl status nginx prints active",
		"certbot": "`certbot certificates` lists the kestrel domain",
		"nginx":   "the site file exists at /etc/nginx/sites-enabled/kestrel",
	} {
		for field, plan := range map[string]string{"acceptance": text + "\nExpect: none, because a test plan", "description": ""} {
			p := gristAcceptancePlan("make test passes\nExpect: none, because a test plan")
			if field == "acceptance" {
				p = gristAcceptancePlan(plan)
			} else {
				p.Stories[0].Description = "Then: " + text
			}
			var out, warn strings.Builder
			if err := gristFile(&out, &warn).Check(context.Background(), p); err != nil {
				t.Fatalf("%s/%s: expected a warning and not a refusal, got %v", name, field, err)
			}
			for _, want := range []string{"module", "hands step"} {
				if !strings.Contains(warn.String(), want) {
					t.Errorf("%s/%s: expected the warning to say %q, got %q", name, field, want, warn.String())
				}
			}
		}
	}
}

func TestAStoryThatNeedsNoOtherHostIsNotWarnedOfHandsSteps(t *testing.T) {
	p := gristAcceptancePlan("make test passes; an ssh key setting is documented, and ssh.go still builds\nExpect: none, because a test plan")
	p.Stories[0].Description = "Add a flag."
	var out, warn strings.Builder
	if err := gristFile(&out, &warn).Check(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(warn.String(), "hands step") {
		t.Errorf("expected no warning, got %q", warn.String())
	}
}
