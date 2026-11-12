package application

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// PosternNeedsCountsFile is the name of the plaintext counts file the view
// writes beside itself, in the directory of postern_view_path: how many needs
// wait on him and on the Mayor, by kind, so that mw status can ring on the
// number his badge shows although the view itself is sealed to his key.
const PosternNeedsCountsFile = "needs-counts.json"

// PosternNeedsCountsStale is how old the counts file may be before mw status
// says the count is stale instead of giving a number.
const PosternNeedsCountsStale = 30 * time.Minute

// PosternNeedsCountsPath is where the counts file lies for a view written at
// viewPath.
func PosternNeedsCountsPath(viewPath string) string {
	return filepath.Join(filepath.Dir(viewPath), PosternNeedsCountsFile)
}

// PosternNeedsWaiter is what waits on one waiter: the total and its count by
// kind. No bead id, title or text is ever held.
type PosternNeedsWaiter struct {
	Total int            `json:"total"`
	Kinds map[string]int `json:"kinds"`
}

// PosternNeedsCounts is the counts file: the needs waiting on "you" and on
// "mayor" that are not folded, as postern's badge counts them.
type PosternNeedsCounts struct {
	V         int                `json:"v"`
	WrittenAt string             `json:"written_at"`
	You       PosternNeedsWaiter `json:"you"`
	Mayor     PosternNeedsWaiter `json:"mayor"`
}

// CountPosternNeeds counts doc's needs that wait on him and on the Mayor, not
// folded, by kind, stamped now.
func CountPosternNeeds(doc PosternViewDoc, now time.Time) PosternNeedsCounts {
	counts := PosternNeedsCounts{
		V: 1, WrittenAt: now.UTC().Format(time.RFC3339),
		You: PosternNeedsWaiter{Kinds: map[string]int{}}, Mayor: PosternNeedsWaiter{Kinds: map[string]int{}},
	}
	for _, need := range doc.Needs {
		if need.Folded {
			continue
		}
		var waiter *PosternNeedsWaiter
		switch need.WaitsFor {
		case PosternWaitsYou:
			waiter = &counts.You
		case PosternWaitsMayor:
			waiter = &counts.Mayor
		default:
			continue
		}
		waiter.Total++
		waiter.Kinds[need.Kind]++
	}
	return counts
}

// PosternNeedsCountsJSON is the bytes of the counts file for doc.
func PosternNeedsCountsJSON(doc PosternViewDoc, now time.Time) ([]byte, error) {
	raw, err := json.Marshal(CountPosternNeeds(doc, now))
	if err != nil {
		return nil, fmt.Errorf("building the needs counts JSON: %w", err)
	}
	return append(raw, '\n'), nil
}

// needsYouAlarm judges the counts file's bytes: nothing for a missing file
// (nil) or a count at or under limit, the stale line for a file older than
// PosternNeedsCountsStale (or one that cannot be read), and the count by kind
// over limit.
func needsYouAlarm(raw []byte, now time.Time, limit int) (alarm DamperAlarm, count int, ring bool) {
	if raw == nil {
		return DamperAlarm{}, 0, false
	}
	var counts PosternNeedsCounts
	err := json.Unmarshal(raw, &counts)
	var written time.Time
	if err == nil {
		written, err = time.Parse(time.RFC3339, counts.WrittenAt)
	}
	if err != nil || now.Sub(written) > PosternNeedsCountsStale {
		when := "at a time that cannot be read"
		if err == nil {
			when = viewClock(written)
		}
		return DamperAlarm{Principle: DamperNeedsYou, Text: fmt.Sprintf("count is stale (written %s)", when)}, 0, true
	}
	if counts.You.Total <= limit {
		return DamperAlarm{}, counts.You.Total, false
	}
	return DamperAlarm{
		Principle: DamperNeedsYou,
		Text:      fmt.Sprintf("his line is %d: %s (limit %d)", counts.You.Total, needsYouKinds(counts.You), limit),
	}, counts.You.Total, true
}

// needsYouKinds names the kinds of his line in the order the alarm says them;
// any other kind is counted together as "other".
func needsYouKinds(you PosternNeedsWaiter) string {
	named := []struct{ kind, noun string }{
		{PosternNeedVerify, "verify"}, {PosternNeedStale, "stale"}, {PosternNeedApprove, "release"},
		{PosternNeedDemo, "demos"}, {PosternNeedQuestion, "questions"}, {PosternNeedHands, "hands"},
	}
	var parts []string
	seen := 0
	for _, n := range named {
		parts = append(parts, fmt.Sprintf("%d %s", you.Kinds[n.kind], n.noun))
		seen += you.Kinds[n.kind]
	}
	if other := you.Total - seen; other > 0 {
		parts = append(parts, fmt.Sprintf("%d other", other))
	}
	return strings.Join(parts, ", ")
}
