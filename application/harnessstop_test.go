package application_test

import (
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

// mw-gq6.434: a session the harness stopped for a reason that is not the
// work's — a usage limit, a rate limit, an overloaded or failing API — is told
// apart from a session that failed, with the time it is worth trying again.

func TestHarnessStopOfReadsTheResetTimeAUsageLimitNames(t *testing.T) {
	newYork, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("loading the zone: %v", err)
	}
	cases := []struct {
		name  string
		said  string
		ended time.Time
		want  time.Time
	}{
		{"weekly, dated", "You've hit your weekly limit · resets Oct 16, 9am (America/New_York)",
			time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC), time.Date(2026, 10, 16, 9, 0, 0, 0, newYork)},
		{"session, today", "You've hit your session limit · resets 3pm (America/New_York)",
			time.Date(2026, 10, 15, 17, 0, 0, 0, time.UTC), time.Date(2026, 10, 15, 15, 0, 0, 0, newYork)},
		{"session, tomorrow", "You've hit your session limit · resets 3pm (America/New_York)",
			time.Date(2026, 10, 15, 20, 0, 0, 0, time.UTC), time.Date(2026, 10, 16, 15, 0, 0, 0, newYork)},
		{"minutes", "You've hit your limit · resets 9:30pm (America/New_York)",
			time.Date(2026, 10, 15, 20, 0, 0, 0, time.UTC), time.Date(2026, 10, 15, 21, 30, 0, 0, newYork)},
		{"over the new year", "You've hit your weekly limit · resets Jan 2, 9am (UTC)",
			time.Date(2026, 12, 31, 9, 0, 0, 0, time.UTC), time.Date(2027, 1, 2, 9, 0, 0, 0, time.UTC)},
		{"no zone named is UTC", "You've hit your weekly limit · resets Oct 16, 9am",
			time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC), time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC)},
		{"the older epoch form", "Claude AI usage limit reached|1760620000",
			time.Date(2025, 10, 16, 9, 0, 0, 0, time.UTC), time.Unix(1760620000, 0)},
		{"no time named waits an hour", "5-hour limit reached",
			time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC), time.Date(2026, 10, 15, 10, 0, 0, 0, time.UTC)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stop, ok := application.HarnessStopOf(application.SessionResult{IsError: true, Subtype: "success", Said: tc.said}, tc.ended)
			if !ok {
				t.Fatalf("expected %q read as a harness stop", tc.said)
			}
			if stop.Reason != application.ReasonUsageLimit {
				t.Fatalf("expected %q read as %s, got %s", tc.said, application.ReasonUsageLimit, stop.Reason)
			}
			if !stop.Until.Equal(tc.want) {
				t.Fatalf("expected %q to reset at %s, got %s", tc.said, tc.want.UTC(), stop.Until.UTC())
			}
			if stop.Said != tc.said {
				t.Fatalf("expected the harness's words kept, got %q", stop.Said)
			}
		})
	}
}

func TestHarnessStopOfWaitsAQuarterHourAfterARateLimitOrAnOverloadedOrFailingAPI(t *testing.T) {
	ended := time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)
	cases := []struct {
		said string
		want application.Reason
	}{
		{`API Error: 429 {"type":"error","error":{"type":"rate_limit_error","message":"Number of request tokens has exceeded your per-minute rate limit"}}`, application.ReasonRateLimit},
		{`API Error: 529 {"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`, application.ReasonOverloaded},
		{`API Error: 500 {"type":"error","error":{"type":"api_error","message":"Internal server error"}}`, application.ReasonServerError},
		{`API Error: 503 Service Unavailable`, application.ReasonServerError},
	}
	for _, tc := range cases {
		stop, ok := application.HarnessStopOf(application.SessionResult{IsError: true, Said: tc.said}, ended)
		if !ok || stop.Reason != tc.want {
			t.Fatalf("expected %q read as %s, got %+v (%v)", tc.said, tc.want, stop, ok)
		}
		if want := ended.Add(application.HarnessStopWait); !stop.Until.Equal(want) {
			t.Fatalf("expected %q to wait until %s, got %s", tc.said, want, stop.Until)
		}
	}
}

func TestHarnessStopOfIsNoneForAFinishedSessionOrTheWorksOwnFailure(t *testing.T) {
	ended := time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)
	for _, result := range []application.SessionResult{
		{Subtype: "success", Said: "Done. You've hit your weekly limit of nothing: the rate limit test passes"},
		{IsError: true, Subtype: "error_during_execution", Said: "the session ran out of fuel"},
		{IsError: true, Subtype: "error_max_turns"},
		{IsError: true, Said: "the rig's tests fail: TestRateLimiter"},
	} {
		if stop, ok := application.HarnessStopOf(result, ended); ok {
			t.Fatalf("expected %+v not read as a harness stop, got %+v", result, stop)
		}
	}
}
