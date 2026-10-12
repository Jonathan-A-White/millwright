package steps

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"

	"github.com/cucumber/godog"
)

// clientTimingContext is what features/grist_client_timing.feature adds to a
// mill: the client_timing the phone's next grist carries, and the txids of the
// grists it has sent.
type clientTimingContext struct {
	c *gristContext
	// next is the client_timing the next grist sent carries; nil is none.
	next json.RawMessage
}

func registerGristClientTiming(ctx *godog.ScenarioContext, c *gristContext) {
	t := &c.clientTiming
	// After the reset of gristContext's own Before hook, which clears it.
	ctx.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		t.c = c
		return ctx, nil
	})

	ctx.Given(`^the phone's next grist carries the client timing (\{.*\}) for the grist it sent before$`, t.carriesForLast)
	ctx.Given(`^the phone's next grist carries the client timing (\{.*\}) for the grist it sent first$`, t.carriesForFirst)
	ctx.Given(`^the phone's next grist carries the client timing (\{.*\}) for a grist named "([^"]*)"$`, t.carriesForNamed)

	ctx.Then(`^the mill recorded no client timing$`, t.recordedNone)
	ctx.Then(`^the mill recorded for the first grist the client timing tap_to_sent (\S+), network (\S+), poll_network (\S+), delivered_to_seen (\S+) and seen_to_shown (\S+) milliseconds$`, t.recorded)
	ctx.Then(`^the mill's report says it ignored the client timing$`, t.reportIgnored)
	ctx.Then(`^the mill's report says nothing of client timing$`, t.reportSilent)
}

// carry sets the client_timing of the next grist: the JSON given with its txid.
func (t *clientTimingContext) carry(body, txid string) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(body), &fields); err != nil {
		return fmt.Errorf("the client timing %s is not JSON: %w", body, err)
	}
	quoted, _ := json.Marshal(txid)
	fields["txid"] = quoted
	raw, err := json.Marshal(fields)
	t.next = raw
	return err
}

// sentTxids reads the txids of the grists on the backend, in the order sent.
func (t *clientTimingContext) sentTxids() ([]string, error) {
	records, err := t.c.backend.Messages(context.Background(), 0)
	if err != nil {
		return nil, err
	}
	var txids []string
	for _, r := range records {
		if r.Class == application.GristClass && r.To == t.c.millKey {
			txids = append(txids, r.Txid)
		}
	}
	return txids, nil
}

func (t *clientTimingContext) carriesForLast(body string) error {
	txids, err := t.sentTxids()
	if err != nil || len(txids) == 0 {
		return fmt.Errorf("the phone has sent no grist before (%v)", err)
	}
	return t.carry(body, txids[len(txids)-1])
}

func (t *clientTimingContext) carriesForFirst(body string) error {
	txids, err := t.sentTxids()
	if err != nil || len(txids) == 0 {
		return fmt.Errorf("the phone has sent no grist before (%v)", err)
	}
	return t.carry(body, txids[0])
}

func (t *clientTimingContext) carriesForNamed(body, txid string) error { return t.carry(body, txid) }

func (t *clientTimingContext) recordedNone() error {
	got, err := t.c.state.ClientTimings(context.Background())
	if err != nil {
		return err
	}
	if len(got) != 0 {
		return fmt.Errorf("expected no client timing recorded, got %+v", got)
	}
	return nil
}

// milli is a step's number, or "-" for one the record must leave out.
func milli(name string, got *float64, want string) error {
	if want == "-" {
		if got != nil {
			return fmt.Errorf("expected %s left out, got %v", name, *got)
		}
		return nil
	}
	w, _ := strconv.ParseFloat(want, 64)
	if got == nil || *got != w {
		return fmt.Errorf("expected %s %s ms, got %v", name, want, got)
	}
	return nil
}

func (t *clientTimingContext) recorded(tap, network, poll, seen, shown string) error {
	got, err := t.c.state.ClientTimings(context.Background())
	if err != nil {
		return err
	}
	txids, err := t.sentTxids()
	if err != nil || len(txids) == 0 {
		return fmt.Errorf("no grist was sent (%v)", err)
	}
	if len(got) != 1 {
		return fmt.Errorf("expected one client timing recorded, got %+v", got)
	}
	r := got[0]
	if r.Txid != txids[0] {
		return fmt.Errorf("expected the client timing recorded against the first grist %s, got %s", txids[0], r.Txid)
	}
	for _, check := range []error{
		milli("tap_to_sent", r.TapToSent, tap), milli("network", r.Network, network),
		milli("poll_network", r.PollNetwork, poll), milli("delivered_to_seen", r.DeliveredToSeen, seen),
		milli("seen_to_shown", r.SeenToShown, shown),
	} {
		if check != nil {
			return check
		}
	}
	return nil
}

func (t *clientTimingContext) reportIgnored() error {
	for _, note := range t.c.report.Notes {
		if strings.Contains(note, "client_timing") && strings.Contains(note, "ignored") {
			return nil
		}
	}
	return fmt.Errorf("expected a note that the client_timing was ignored, got %q", t.c.report.Notes)
}

func (t *clientTimingContext) reportSilent() error {
	for _, note := range t.c.report.Notes {
		if strings.Contains(note, "client_timing") {
			return fmt.Errorf("expected no note of client_timing, got %q", note)
		}
	}
	return nil
}
