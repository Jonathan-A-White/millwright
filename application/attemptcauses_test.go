package application

import (
	"testing"
	"time"
)

// TestOwnAttemptsCountsCodeAndUnnamed is mw-gq6.450: only the attempts mw cause
// named code, or named nothing, are the story's own.
func TestOwnAttemptsCountsCodeAndUnnamed(t *testing.T) {
	d := StoryDetail{Attempts: 4, AttemptCauses: ParseAttemptCauses("1:process, 2:test,4:code,7:tooling,x:test,3:")}
	own, uncounted := OwnAttempts(d)
	if own != 2 {
		t.Errorf("expected attempts 3 (unnamed) and 4 (code) to count, got %d", own)
	}
	if len(uncounted) != 2 || uncounted[0] != (UncountedAttempt{1, "process"}) || uncounted[1] != (UncountedAttempt{2, "test"}) {
		t.Errorf("expected attempts 1 (process) and 2 (test) not counted, got %+v", uncounted)
	}
	if got := attemptCausesText(d.AttemptCauses); got != "1:process,2:test,4:code,7:tooling" {
		t.Errorf("expected the causes kept oldest first, got %q", got)
	}
}

// TestANewAttemptClearsTheCausesOfTheAttemptsItRenumbers: a count lowered (a
// refund, a harness stop) or reset by hand makes the next attempt reuse a
// number, and the cause named for the earlier session of that number is not
// the new one's.
func TestANewAttemptClearsTheCausesOfTheAttemptsItRenumbers(t *testing.T) {
	d := StoryDetail{Attempts: 1, AttemptCauses: map[int]string{1: "process", 2: "test", 3: "code"}}
	fields := attemptFields(d, 2, time.Time{})
	if got, ok := fields[AttemptCausesField]; !ok || got != "1:process" {
		t.Errorf("expected attempt 2's and 3's causes cleared, leaving 1:process, got %q (written: %v)", got, ok)
	}
	if _, ok := attemptFields(StoryDetail{Attempts: 2, AttemptCauses: map[int]string{1: "test", 2: "tooling"}}, 3, time.Time{})[AttemptCausesField]; ok {
		t.Errorf("expected nothing written of causes when none is stale")
	}
}
