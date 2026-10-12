package application

import (
	"encoding/json"
	"fmt"
	"math"
	"time"
)

// GristClientTimingMax is the most milliseconds one phase of a client_timing
// may claim: ten minutes. A longer one is a bug in the app, or a clock that
// was set back, and is not recorded.
const GristClientTimingMax = 10 * 60 * 1000

// GristClientTiming is the phone's part of a grist's round trip, as the app
// reported it on its next grist (section 18's client_timing), recorded against
// the earlier grist whose answer it measured. Each field is milliseconds and is
// left out where the app did not report it.
//
// TapToSent is the user's tap to the request accepted by the backend, upload
// included; DeliveredToSeen the answer delivered on the backend to the app's
// poll seeing it; SeenToShown that to the answer drawn on screen; Network the
// round trip of the poll that saw the answer plus that of the request that
// sent it, summed (so it lies inside TapToSent and DeliveredToSeen, and is not
// added to them); PollNetwork, optional, the poll's round trip alone, which
// makes the wait between checks (DeliveredToSeen less PollNetwork) known.
type GristClientTiming struct {
	Time            time.Time `json:"time"`
	Txid            string    `json:"txid"`
	TapToSent       *float64  `json:"tap_to_sent_ms,omitempty"`
	Network         *float64  `json:"network_ms,omitempty"`
	PollNetwork     *float64  `json:"poll_network_ms,omitempty"`
	DeliveredToSeen *float64  `json:"delivered_to_seen_ms,omitempty"`
	SeenToShown     *float64  `json:"seen_to_shown_ms,omitempty"`
}

// parseGristClientTiming reads the client_timing a grist carries. An error says
// in one plain sentence why it is not to be trusted: the caller ignores it and
// logs that, and never refuses the grist for it. at is when it was received.
func parseGristClientTiming(raw json.RawMessage, at time.Time) (GristClientTiming, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return GristClientTiming{}, fmt.Errorf("it is not an object")
	}
	timing := GristClientTiming{Time: at.UTC()}
	if err := json.Unmarshal(fields["txid"], &timing.Txid); err != nil || timing.Txid == "" {
		return GristClientTiming{}, fmt.Errorf("it names no txid")
	}
	known := 0
	for name, into := range map[string]**float64{
		"tap_to_sent": &timing.TapToSent, "network": &timing.Network, "poll_network": &timing.PollNetwork,
		"delivered_to_seen": &timing.DeliveredToSeen, "seen_to_shown": &timing.SeenToShown,
	} {
		value, present := fields[name]
		if !present || string(value) == "null" {
			continue
		}
		var ms float64
		if err := json.Unmarshal(value, &ms); err != nil || math.IsNaN(ms) || math.IsInf(ms, 0) {
			return GristClientTiming{}, fmt.Errorf("%s is not a number of milliseconds", name)
		}
		if ms < 0 || ms > GristClientTimingMax {
			return GristClientTiming{}, fmt.Errorf("%s, %v milliseconds, is outside 0 to %d", name, ms, GristClientTimingMax)
		}
		*into = &ms
		known++
	}
	if known == 0 {
		return GristClientTiming{}, fmt.Errorf("it holds no timing")
	}
	return timing, nil
}

// msSeconds is the milliseconds of one phase as seconds, or nil when it is not
// known.
func msSeconds(ms *float64) *float64 {
	if ms == nil {
		return nil
	}
	s := *ms / 1000
	return &s
}

// phaseSeconds is the seconds of the phase named, or nil when the report does
// not have it. wait and round_trip need the mill's line as well: wait is
// DeliveredToSeen less PollNetwork, and round_trip the tap to the answer
// shown, the sum of TapToSent, the mill's total, DeliveredToSeen and
// SeenToShown.
func (c GristClientTiming) phaseSeconds(name string, line GrindLine) *float64 {
	switch name {
	case "tap_to_sent":
		return msSeconds(c.TapToSent)
	case "network":
		return msSeconds(c.Network)
	case "delivered_to_seen":
		return msSeconds(c.DeliveredToSeen)
	case "seen_to_shown":
		return msSeconds(c.SeenToShown)
	case "wait":
		if c.DeliveredToSeen == nil || c.PollNetwork == nil {
			return nil
		}
		wait := max(*c.DeliveredToSeen-*c.PollNetwork, 0) / 1000
		return &wait
	case "round_trip":
		if c.TapToSent == nil || c.DeliveredToSeen == nil || c.SeenToShown == nil || line.TotalSeconds == nil {
			return nil
		}
		total := (*c.TapToSent+*c.DeliveredToSeen+*c.SeenToShown)/1000 + *line.TotalSeconds
		return &total
	}
	return nil
}
