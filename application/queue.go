package application

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
)

// MostBlockedShown is how many blocked stories mw queue lists after the ready
// ones: the next few, in the order they would start once unblocked.
const MostBlockedShown = 5

// Queue is `mw queue`: the stories ready on a host in the exact order a
// dispatch starts them, why each sits where it does, and the next blocked
// stories with what they wait on. It reads and writes nothing, and it never
// decides an order of its own: the order is inStartOrder's, the one Dispatch
// sorts by.
type Queue struct {
	Tracker WorkTracker
	Host    string
	// Rig narrows the lists to one rig; empty is every rig.
	Rig string
	// Rules, when set, says which rigs expect a files key, as the shared-files
	// guard needs; nil expects none.
	Rules EpicRules
	// JSON prints the report as JSON, not as lines.
	JSON bool
	Now  func() time.Time
	Out  io.Writer
}

// QueueRow is one ready story and the reason it sits where it does.
type QueueRow struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Rig      string `json:"rig,omitempty"`
	Priority int    `json:"priority"`
	Age      string `json:"age,omitempty"`
	Size     string `json:"size,omitempty"`
	// Why is the story's place in words: its priority, age and size.
	Why string `json:"why"`
	// Held is why a dispatch would pass it over for now, when one would: the
	// shared-files guard, or the Governor's presence being needed.
	Held string `json:"held,omitempty"`
}

// QueueBlocked is one blocked story and the stories it waits on.
type QueueBlocked struct {
	ID       string   `json:"id"`
	Title    string   `json:"title"`
	Rig      string   `json:"rig,omitempty"`
	Priority int      `json:"priority"`
	WaitsOn  []string `json:"waits_on"`
}

// QueueReport is what mw queue found.
type QueueReport struct {
	Host    string         `json:"host"`
	Ready   []QueueRow     `json:"ready"`
	Blocked []QueueBlocked `json:"blocked"`
}

// Run reads the host's ready and blocked stories and prints them.
func (q Queue) Run(ctx context.Context) (QueueReport, error) {
	report := QueueReport{Host: q.Host, Ready: []QueueRow{}, Blocked: []QueueBlocked{}}
	ready, err := q.Tracker.ReadyForHost(ctx, q.Host)
	if err != nil {
		return report, fmt.Errorf("reading what is ready on %s: %w", q.Host, err)
	}
	blocked, err := q.Tracker.BlockedForHost(ctx, q.Host)
	if err != nil {
		return report, fmt.Errorf("reading what is blocked on %s: %w", q.Host, err)
	}
	waiting := map[string]bool{}
	for _, detail := range blocked {
		waiting[detail.Story.ID] = true
	}

	shares := fileShares{rules: q.Rules}
	running, err := q.Tracker.RunningStories(ctx, q.Host)
	if err != nil {
		return report, fmt.Errorf("reading what %s is running: %w", q.Host, err)
	}
	for _, detail := range running {
		shares.add(detail)
	}
	away, err := q.Tracker.RunningElsewhere(ctx, q.Host)
	if err != nil {
		return report, fmt.Errorf("reading what the other hosts are running: %w", err)
	}
	for _, detail := range away {
		if !detail.Hitl() {
			shares.addAway(detail, detail.Merged().Host)
		}
	}

	var unblocked []StoryDetail
	for _, detail := range ready {
		if !waiting[detail.Story.ID] && q.inRig(detail) {
			unblocked = append(unblocked, detail)
		}
	}
	for _, detail := range inStartOrder(unblocked) {
		row := q.row(detail)
		if detail.Hitl() {
			why := LabelHitl
			if !hasHitlLabel(detail.Labels) {
				why = LabelDemo
			}
			row.Held = "the Governor must be present for it (" + why + ")"
		} else if share, sharing := shares.shared(ctx, detail); sharing {
			row.Held = share.String()
		}
		report.Ready = append(report.Ready, row)
	}

	var mine []StoryDetail
	for _, detail := range blocked {
		if q.inRig(detail) {
			mine = append(mine, detail)
		}
	}
	for _, detail := range inStartOrder(mine) {
		if len(report.Blocked) == MostBlockedShown {
			break
		}
		report.Blocked = append(report.Blocked, QueueBlocked{
			ID: detail.Story.ID, Title: detail.Story.Title, Rig: detail.Merged().Rig,
			Priority: detail.Priority, WaitsOn: append([]string{}, detail.Needs...),
		})
	}

	if q.Out != nil {
		if q.JSON {
			enc := json.NewEncoder(q.Out)
			enc.SetIndent("", "  ")
			err = enc.Encode(report)
		} else {
			_, err = io.WriteString(q.Out, report.String())
		}
		if err != nil {
			return report, fmt.Errorf("writing the queue: %w", err)
		}
	}
	return report, nil
}

func (q Queue) inRig(detail StoryDetail) bool {
	return q.Rig == "" || detail.Merged().Rig == q.Rig
}

func (q Queue) row(detail StoryDetail) QueueRow {
	now := time.Now()
	if q.Now != nil {
		now = q.Now()
	}
	merged := detail.Merged()
	row := QueueRow{
		ID: detail.Story.ID, Title: detail.Story.Title, Rig: merged.Rig,
		Priority: detail.Priority, Size: string(merged.Size),
	}
	why := []string{fmt.Sprintf("P%d", detail.Priority)}
	if !detail.Created.IsZero() {
		row.Age = queueAge(now.Sub(detail.Created))
		why = append(why, "filed "+row.Age+" ago")
	} else {
		why = append(why, "filing time unknown, after every dated story of its priority")
	}
	if row.Size != "" {
		why = append(why, "size "+row.Size)
	}
	row.Why = strings.Join(why, ", ")
	return row
}

// queueAge is d in whole days, or whole hours under a day.
func queueAge(d time.Duration) string {
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh", int(max(d, 0)/time.Hour))
	}
	return fmt.Sprintf("%dd", int(d/(24*time.Hour)))
}

// String is the report as lines a phone fits.
func (r QueueReport) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "READY on %s (%d), in the order a dispatch starts them\n", r.Host, len(r.Ready))
	if len(r.Ready) == 0 {
		b.WriteString("  nothing\n")
	}
	for i, row := range r.Ready {
		fmt.Fprintf(&b, "%2d. %s  %s\n", i+1, row.ID, row.Title)
		fmt.Fprintf(&b, "    %s\n", row.Why)
		if row.Held != "" {
			fmt.Fprintf(&b, "    held: %s\n", row.Held)
		}
	}
	fmt.Fprintf(&b, "BLOCKED on %s (next %d)\n", r.Host, len(r.Blocked))
	if len(r.Blocked) == 0 {
		b.WriteString("  nothing\n")
	}
	for _, row := range r.Blocked {
		fmt.Fprintf(&b, "  %s  %s\n    P%d, waits on %s\n", row.ID, row.Title, row.Priority, strings.Join(row.WaitsOn, ", "))
	}
	return b.String()
}
