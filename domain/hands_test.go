package domain_test

import (
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/domain"
)

// The "hands" vector of postern's docs/fixtures/protocol-vectors.json
// (scripts/generate-fixture.ts, src/model/hands.ts), copied value for value:
// the TypeScript side's canonical bytes, their sha256 and the approval
// string it signs. Go must produce every one of them byte for byte. The run
// text holds multi-byte characters, so a length counted in runes instead of
// bytes would be caught here (52 bytes, 50 runes).
const (
	vectorBead       = "mw-f758y.8"
	vectorRun        = "loginctl enable-linger jwhite && echo \"ünïcode ok\""
	vectorCanonical  = "hands/v1\n10:mw-f758y.8\n6:linger\n7:desktop\n4:root\n52:loginctl enable-linger jwhite && echo \"ünïcode ok\"\n30:loginctl disable-linger jwhite\n"
	vectorSHA256     = "2d74974c1cc77dd9faa271dc7c6b18dd690aa369c2cba8c5067b3c56bf42fe14"
	vectorApprovedAt = int64(1790000000)
	vectorApproval   = "hands-approve/v1\n2d74974c1cc77dd9faa271dc7c6b18dd690aa369c2cba8c5067b3c56bf42fe14\n1790000000\n"
)

func vectorStep() domain.HandsStep {
	return domain.HandsStep{ID: "linger", Host: "desktop", As: "root", Run: vectorRun, WayBack: "loginctl disable-linger jwhite"}
}

func TestHandsCanonicalBytesMatchTheTypeScriptVector(t *testing.T) {
	if got := string(domain.HandsCanonical(vectorBead, vectorStep())); got != vectorCanonical {
		t.Fatalf("expected the vector's canonical bytes\n%q\ngot\n%q", vectorCanonical, got)
	}
	if len(vectorRun) != 52 || len([]rune(vectorRun)) != 50 {
		t.Fatalf("the vector's run text should be 52 bytes and 50 runes, is %d and %d", len(vectorRun), len([]rune(vectorRun)))
	}
}

func TestHandsSHA256MatchesTheTypeScriptVector(t *testing.T) {
	if got := domain.HandsSHA256(vectorBead, vectorStep()); got != vectorSHA256 {
		t.Fatalf("expected %s, got %s", vectorSHA256, got)
	}
}

func TestHandsApprovalMessageMatchesTheTypeScriptVector(t *testing.T) {
	if got := domain.HandsApprovalMessage(vectorSHA256, vectorApprovedAt); got != vectorApproval {
		t.Fatalf("expected %q, got %q", vectorApproval, got)
	}
}

// Every field is length-prefixed, so no two different steps share bytes: a
// newline moved from one field to the next changes the hash.
func TestHandsCanonicalBytesTellFieldsApart(t *testing.T) {
	a := domain.HandsStep{ID: "x", Host: "desktop", As: "user", Run: "echo a\n", WayBack: "b"}
	b := domain.HandsStep{ID: "x", Host: "desktop", As: "user", Run: "echo a", WayBack: "\nb"}
	if domain.HandsSHA256("mw-1", a) == domain.HandsSHA256("mw-1", b) {
		t.Fatal("expected two different steps to hash apart")
	}
}

// An approval is good for 5 minutes, and may be up to 2 minutes ahead of
// this host's clock; nothing else.
func TestHandsApprovalAge(t *testing.T) {
	approved := time.Unix(vectorApprovedAt, 0)
	for _, c := range []struct {
		now  time.Time
		good bool
	}{
		{approved, true},
		{approved.Add(5 * time.Minute), true},
		{approved.Add(5*time.Minute + time.Second), false},
		{approved.Add(-2 * time.Minute), true},
		{approved.Add(-2*time.Minute - time.Second), false},
	} {
		err := domain.CheckHandsApprovalAge(vectorApprovedAt, c.now)
		if (err == nil) != c.good {
			t.Errorf("at %s after the approval: expected good=%v, got %v", c.now.Sub(approved), c.good, err)
		}
	}
}

func TestHandsStepValidation(t *testing.T) {
	if err := domain.ValidateHandsStep(vectorBead, vectorStep()); err != nil {
		t.Fatalf("expected the vector's step valid, got %v", err)
	}
	for name, change := range map[string]func(*domain.HandsStep){
		"as neither user nor root": func(s *domain.HandsStep) { s.As = "admin" },
		"no run":                   func(s *domain.HandsStep) { s.Run = "  " },
		"an id with a space":       func(s *domain.HandsStep) { s.ID = "two words" },
		"an id starting with -":    func(s *domain.HandsStep) { s.ID = "-rf" },
		"no host":                  func(s *domain.HandsStep) { s.Host = "" },
		"a host with a space":      func(s *domain.HandsStep) { s.Host = "vps laptop" },
	} {
		step := vectorStep()
		change(&step)
		if err := domain.ValidateHandsStep(vectorBead, step); err == nil {
			t.Errorf("%s: expected a refusal", name)
		}
	}
	if err := domain.ValidateHandsStep("-bad", vectorStep()); err == nil || !strings.Contains(err.Error(), "bead") {
		t.Errorf("expected a bad bead id refused, got %v", err)
	}
}
