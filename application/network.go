package application

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/domain/events"
)

// MeteredSetting is config `metered`: whether mw asks Windows if the network is
// metered (auto, the default), or is told it is (yes) or is not (no).
type MeteredSetting string

const (
	MeteredAuto MeteredSetting = "auto"
	MeteredYes  MeteredSetting = "yes"
	MeteredNo   MeteredSetting = "no"
)

// ParseMeteredSetting reads config `metered`; empty is auto.
func ParseMeteredSetting(said string) (MeteredSetting, error) {
	switch s := MeteredSetting(strings.ToLower(strings.TrimSpace(said))); s {
	case "", MeteredAuto:
		return MeteredAuto, nil
	case MeteredYes, MeteredNo:
		return s, nil
	}
	return "", fmt.Errorf("metered is %q: it is auto, yes or no", said)
}

// The places a NetworkReading comes from.
const (
	NetworkWindows = "Windows"
	NetworkConfig  = "config"
)

// NetworkCacheFor is how long a reading of Windows is kept: the call takes a few
// seconds, and the ticks that need the answer come minutes apart.
const NetworkCacheFor = time.Minute

// NetworkCondition is what Windows says of the connection it uses for the
// internet: the profile's name and its cost type (Unrestricted, Fixed,
// Variable or Unknown). Both are empty when there is no connection profile.
type NetworkCondition struct {
	Profile string
	Cost    string
}

// NetworkProbe asks Windows, from WSL, what the current connection costs. An
// error is a host with no Windows to ask, or a call that failed or timed out.
type NetworkProbe interface {
	Probe(ctx context.Context) (NetworkCondition, error)
}

// NetworkReader answers "is this host's network metered?", for everything in mw
// that goes easy on data. It never fails: what cannot be told is unmetered, with
// the reason in the reading.
type NetworkReader interface {
	Read(ctx context.Context) NetworkReading
}

// NetworkStore is this host's own memory of the network, kept between runs of
// mw: the last reading, so that it is asked about once a minute and not once a
// run, and what the Governor was last told. Nothing in it is synced.
type NetworkStore interface {
	Load(ctx context.Context) (NetworkMemory, error)
	Save(ctx context.Context, memory NetworkMemory) error
}

// NetworkMemory is what a NetworkStore keeps.
type NetworkMemory struct {
	// Reading is the last one taken from Windows.
	Reading NetworkReading
	// Told says an event has been written for a state of the network, and
	// ToldMetered which state it was: the next event is written when a reading
	// differs from it, so a change is said once and a tick that sees no change
	// says nothing. Alert is the seq of the metered event, which the one for
	// the way back clears.
	Told        bool
	ToldMetered bool
	Alert       uint64
}

// NetworkReading is whether the network is metered, and how that is known.
type NetworkReading struct {
	Metered bool
	// Source is NetworkWindows or NetworkConfig; empty when nothing could be asked.
	Source string
	// Profile and Cost are what Windows said, when it did.
	Profile string
	Cost    string
	// Reason is why the network reads as unmetered when it could not be told.
	Reason string
	At     time.Time
}

// Line is the NETWORK line mw status shows.
func (r NetworkReading) Line() string {
	switch {
	case r.Metered && r.Source == NetworkConfig:
		return `NETWORK metered (config: metered = "yes")`
	case r.Metered:
		return fmt.Sprintf("NETWORK metered (%s: %s, cost %s)", NetworkWindows, r.Profile, r.Cost)
	case r.Reason != "":
		return "NETWORK unmetered (" + r.Reason + ")"
	}
	return "NETWORK unmetered"
}

// Why is the reading as a clause: what a story passed over for it says.
func (r NetworkReading) Why() string {
	return strings.TrimPrefix(strings.TrimPrefix(r.Line(), "NETWORK "), "metered ")
}

// MeteredCost says whether a Windows cost type (NetworkCostType) is a metered
// connection: Fixed and Variable are, Unrestricted and Unknown are not.
func MeteredCost(cost string) bool {
	switch cost {
	case "Fixed", "Variable":
		return true
	}
	return false
}

// Network is the one place that says whether this host's network is metered:
// Windows' answer on WSL, or config's. A fresh reading of Windows that differs
// from the last the Governor was told of is one event, once: an emergency job
// event, running to failed, when the network turns metered, and one in the
// normal lane, running to done, clearing it, when it turns unmetered again.
type Network struct {
	Setting MeteredSetting
	// Probe asks Windows; nil on a host with none, which is then unmetered.
	Probe NetworkProbe
	// Store keeps the last reading between runs; nil keeps none, and every read
	// asks Windows.
	Store NetworkStore
	// Events is where a change is written; nil writes none.
	Events EventLog
	Host   string
	// Quiet reads the kept answer or asks Windows but writes nothing: no kept
	// reading and no event, for a dry run and for mw status.
	Quiet bool
	// TTL is how long a reading is kept; zero reads NetworkCacheFor.
	TTL time.Duration
	Now func() time.Time
}

var _ NetworkReader = (*Network)(nil)

func (n *Network) now() time.Time {
	if n.Now == nil {
		return time.Now()
	}
	return n.Now()
}

// Read implements NetworkReader.
func (n *Network) Read(ctx context.Context) NetworkReading {
	switch n.Setting {
	case MeteredYes:
		return NetworkReading{Metered: true, Source: NetworkConfig}
	case MeteredNo:
		return NetworkReading{Source: NetworkConfig}
	}
	if n.Probe == nil {
		return NetworkReading{Reason: "no Windows to ask"}
	}

	var memory NetworkMemory
	if n.Store != nil {
		memory, _ = n.Store.Load(ctx)
		ttl := n.TTL
		if ttl <= 0 {
			ttl = NetworkCacheFor
		}
		if at := memory.Reading.At; !at.IsZero() && n.now().Sub(at) < ttl && n.now().Sub(at) >= 0 {
			return memory.Reading
		}
	}

	reading := NetworkReading{Source: NetworkWindows, At: n.now()}
	condition, err := n.Probe.Probe(ctx)
	switch {
	case err != nil:
		reading.Reason = "Windows could not be asked: " + firstLine(err.Error())
	case condition.Profile == "" && condition.Cost == "":
		reading.Reason = "Windows has no connection"
	default:
		reading.Profile, reading.Cost = condition.Profile, condition.Cost
		reading.Metered = MeteredCost(condition.Cost)
	}

	if n.Quiet {
		return reading
	}
	memory.Reading = reading
	// A call that failed says nothing of the network, so it is no change.
	if err == nil {
		n.tell(ctx, &memory)
	}
	if n.Store != nil {
		_ = n.Store.Save(ctx, memory)
	}
	return reading
}

// tell writes the event for a change from what the Governor was last told. An
// event that cannot be written leaves memory as it was, so that the next fresh
// reading tries again.
func (n *Network) tell(ctx context.Context, memory *NetworkMemory) {
	reading := memory.Reading
	if memory.Told && memory.ToldMetered == reading.Metered {
		return
	}
	if !memory.Told && !reading.Metered {
		// The first sight of an unmetered network is no change.
		memory.Told, memory.ToldMetered = true, false
		return
	}
	if n.Events == nil {
		return
	}
	emit := EventEmit{Log: n.Events, Now: n.now}
	emit.Event = events.Event{Kind: events.KindJob, Actor: "network@" + n.Host, From: events.JobRunning}
	if reading.Metered {
		emit.Emergency = true
		emit.Event.To = events.JobFailed
		emit.Event.Detail = fmt.Sprintf("the network is metered (%s: %s, cost %s): mw skips the beads backup and starts no story that installs dependencies until it is not",
			NetworkWindows, reading.Profile, reading.Cost)
	} else {
		emit.Event.To = events.JobDone
		emit.Event.Clears = memory.Alert
		emit.Event.Detail = "the network is no longer metered: the beads backup and dependency-installing stories go on as before"
	}
	written, err := emit.Run(ctx)
	if err != nil {
		return
	}
	memory.Told, memory.ToldMetered = true, reading.Metered
	if reading.Metered {
		memory.Alert = written.Seq
	}
}
