package application

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// BatchRoom is what another host says when asked whether it could land a rig's
// batch now (mw land <rig> --room, over ssh): it has the rig checked out, its
// gate lock is free, and its load is under its cores (mw-gq6.1616).
type BatchRoom struct {
	Host string
	// HasRig says the rig is checked out there.
	HasRig bool
	// GateHeldBy is whose gate holds the host's gate lock, empty when it is free.
	GateHeldBy string
	Load       LoadReading
	// Trouble is why the host could not be asked, when it could not: such a host
	// has no room, whatever else the room says.
	Trouble string
}

// Free says the host can land the batch: asked, the rig checked out, the gate
// lock free, and the load, with the part workers' share left in it as the host
// reads it, under the cores. A host whose load was not read has no room.
func (r BatchRoom) Free() bool {
	return r.Trouble == "" && r.HasRig && r.GateHeldBy == "" && r.Load.Cores > 0 && r.Load.Load < float64(r.Load.Cores)
}

// share is the load per core, which picks the freest of the hosts with room.
func (r BatchRoom) share() float64 {
	if r.Load.Cores <= 0 {
		return 0
	}
	return r.Load.Load / float64(r.Load.Cores)
}

// The lines a host's room is told in: what mw land --room prints, and
// ParseBatchRoom reads.
const (
	batchRoomRig  = "rig: "
	batchRoomGate = "gate: "
	batchRoomLoad = "load: "

	batchRoomGateFree = "free"
	batchRoomHeldBy   = "held by "
)

// String is the room as the far end prints it and ParseBatchRoom reads it.
func (r BatchRoom) String() string {
	var b strings.Builder
	rig := "no"
	if r.HasRig {
		rig = "yes"
	}
	fmt.Fprintf(&b, "%s%s\n", batchRoomRig, rig)
	gate := batchRoomGateFree
	if r.GateHeldBy != "" {
		gate = batchRoomHeldBy + r.GateHeldBy
	}
	fmt.Fprintf(&b, "%s%s\n", batchRoomGate, gate)
	fmt.Fprintf(&b, "%s%g %d %d\n", batchRoomLoad, r.Load.Load, r.Load.Cores, r.Load.PartWorkers)
	return b.String()
}

// ParseBatchRoom reads what a far end printed. Text with no rig, gate and load
// line is an error, not a free host: a host that printed something else (a login
// banner, an error) has not said it has room.
func ParseBatchRoom(host, text string) (BatchRoom, error) {
	room := BatchRoom{Host: host}
	var rig, gate, load bool
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, batchRoomRig):
			room.HasRig, rig = strings.TrimPrefix(line, batchRoomRig) == "yes", true
		case strings.HasPrefix(line, batchRoomGate):
			said := strings.TrimPrefix(line, batchRoomGate)
			if by, held := strings.CutPrefix(said, batchRoomHeldBy); held {
				room.GateHeldBy = by
			}
			gate = true
		case strings.HasPrefix(line, batchRoomLoad):
			fields := strings.Fields(strings.TrimPrefix(line, batchRoomLoad))
			if len(fields) != 3 {
				continue
			}
			l, err1 := strconv.ParseFloat(fields[0], 64)
			cores, err2 := strconv.Atoi(fields[1])
			workers, err3 := strconv.Atoi(fields[2])
			if err1 != nil || err2 != nil || err3 != nil {
				continue
			}
			room.Load = LoadReading{Load: l, Cores: cores, PartWorkers: workers}
			load = true
		}
	}
	if !rig || !gate || !load {
		return BatchRoom{}, fmt.Errorf("%s did not say whether it has room to land a batch (its answer: %q)", host, clippedTo(strings.TrimSpace(text), 120))
	}
	return room, nil
}

// BatchElsewhere is the host book of the follower's batch-land job and how it
// lands on another host: the far end of an ssh, which asks the host what room it
// has and runs mw land <rig> there. The adapter is cmd/mw's sshBatchHosts, over
// peekremote and apartproc; the fake is apptest.FakeBatchElsewhere.
type BatchElsewhere interface {
	// Rooms is what each host but this one that can be reached says of its room to
	// land rig's batch. A host that could not be asked is a room with its Trouble.
	Rooms(ctx context.Context, rig string) ([]BatchRoom, error)
	// Land runs mw land <rig> on host over ssh, with --one, --only and --without
	// as the scope names them (the zero scope is the whole queue), apart from the
	// follower as a landing here is (DetachedLand), and waits for it.
	Land(ctx context.Context, host, rig string, scope LandScope) error
}

// homeBusy says why the home's own landing would wait (homeBusyWhy).
func (b *BatchLandings) homeBusy(ctx context.Context) string {
	return homeBusyWhy(ctx, b.Load, b.GateHolder)
}

// homeBusyWhy says why a run on the home would wait: its load has reached its
// cores (LoadReading.Busy), or its one gate lock is held by another's gate. Empty
// when neither, and when neither can be read: a home that cannot say it is
// busy runs as it did.
func homeBusyWhy(ctx context.Context, load HostLoad, gateHolder func() string) string {
	if load != nil {
		if reading, err := load.Load(ctx); err == nil && reading.Busy() {
			return "its " + reading.loadSaid()
		}
	}
	if gateHolder != nil {
		if holder := gateHolder(); holder != "" {
			return "its gate lock is held by " + holder
		}
	}
	return ""
}

// freeHost is the host a due batch of rig is landed on instead of the home's wait
// (mw-gq6.1616): when the home is busy, the host of the book with room that has
// the lowest load for its cores. None, and so the home's own landing as before,
// when the home is not busy, the book is not there or cannot be read, or no host
// has room.
func (b *BatchLandings) freeHost(ctx context.Context, d batchDue) (string, bool) {
	rig := d.rig
	if b.Elsewhere == nil || d.adopted || b.homeBusy(ctx) == "" {
		return "", false
	}
	rooms, err := b.Elsewhere.Rooms(ctx, rig)
	if err != nil {
		return "", false
	}
	return freestHost(rooms, b.Host)
}

// freestHost is the host of rooms, but self, that has room and the lowest load
// for its cores.
func freestHost(rooms []BatchRoom, self string) (string, bool) {
	var room []BatchRoom
	for _, r := range rooms {
		if r.Host != "" && r.Host != self && r.Free() {
			room = append(room, r)
		}
	}
	if len(room) == 0 {
		return "", false
	}
	sort.SliceStable(room, func(i, j int) bool {
		if room[i].share() != room[j].share() {
			return room[i].share() < room[j].share()
		}
		return room[i].Host < room[j].Host
	})
	return room[0].Host, true
}
