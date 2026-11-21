package application

import (
	"context"
	"fmt"
	"slices"
	"time"
)

// SkipRigsGate is the name of the gate of a host that declines a rig.
const SkipRigsGate = "skip-rigs"

// DefaultSkipRigsWait is how long a host that declines a rig passes over its
// auto stories before it takes one itself, when the config file does not say.
const DefaultSkipRigsWait = 20 * time.Minute

// SkipRigs is the rigs a host declines at dispatch ([dispatch] skip_rigs, mw-6ww.119.11.1.3):
// a ready story of one of them pathed auto is left to the other hosts, so that this
// host's gate and merge slot stay free for another rig's. A story pathed to the
// host by name is still taken. Wait is the way out: a story the host has passed
// over for that long, which no other host took, is taken here, so none starves
// while the other hosts are down or full. Zero is DefaultSkipRigsWait.
type SkipRigs struct {
	Rigs []string
	Wait time.Duration
}

func (s SkipRigs) wait() time.Duration {
	if s.Wait <= 0 {
		return DefaultSkipRigsWait
	}
	return s.Wait
}

// skipRigsWhy is the reason this host passes a ready auto story of rig over, and
// "" when it takes it: the rig is not declined, the way out has come, or the host
// cannot say since when (no Memory, or its note cannot be read), since a host that
// cannot promise the way out must not decline. The time is the one the host's own
// note keeps of when it first passed the story here (keepHostPassed), so it is
// counted from the first tick that met the story, not from its filing.
func (d Dispatch) skipRigsWhy(ctx context.Context, id, rig string, auto bool) string {
	if !auto || !slices.Contains(d.SkipRigs.Rigs, rig) || d.Memory == nil {
		return ""
	}
	note, err := d.Memory.Note(ctx, HostPassedKey(d.Host))
	if err != nil {
		return ""
	}
	wait := d.SkipRigs.wait()
	for _, p := range ParseHostPassed(note) {
		if p.Story == id && p.Gate == SkipRigsGate && d.now().Sub(p.Since) >= wait {
			return ""
		}
	}
	return fmt.Sprintf(SkipRigsGate+": %s declines the rig %s (skip_rigs in the config file): the other hosts take it, and %s takes it itself if none has in %s",
		d.Host, rig, d.Host, elapsed(wait))
}
