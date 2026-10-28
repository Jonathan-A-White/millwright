package doctor

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

var _ application.DoctorCheck = (*BeadsChurn)(nil)

// BeadsChurnName is what the check is called: in the log, and on the command
// line as `mw doctor beads-churn`.
const BeadsChurnName = "beads-churn"

// BeadsChurnLimitPerMinute is how many new connections a minute the beads
// server may answer before this check alarms, when the host does not say (the
// [doctor] table's beads_churn_per_minute). The follower alone, when it
// started a bd for every read, brought it to 514 a minute and Dolt to 1.6
// cores all day (mw-gq6.596); a quiet factory answers under 60 and a busy one
// 200-280 (mw-gq6.825). It is config.DefaultDoctorBeadsChurnPerMinute.
const BeadsChurnLimitPerMinute = 300

// beadsChurnTopClients is how many clients a fault names.
const beadsChurnTopClients = 5

// beadsChurnSampleName is the state file the check keeps its last sample in.
const beadsChurnSampleName = "beads-churn-sample"

// beadsChurnStale is how old a sample may be and still be judged against: a
// rate over hours or days says nothing of now.
const beadsChurnStale = time.Hour

// beadsChurnShortest is the least stretch a rate is judged over; a run this
// soon after the last leaves its sample where it was.
const beadsChurnShortest = 30 * time.Second

// BeadsChurn is the check that watches how fast the beads server answers new
// connections: each run reads the server's count and judges the rate since the
// run before. It never cures — a client that opens a connection per call has
// to be found and changed — so a fault is damped after one try, and its note is
// what wakes the Millhand to look.
type BeadsChurn struct {
	// Counter is how many connections the server has answered since it started.
	Counter func(ctx context.Context) (int64, error)
	// State holds the last sample between runs.
	State application.DoctorState
	// Limit is the rate, a minute, past which the check is faulty; zero is
	// BeadsChurnLimitPerMinute.
	Limit float64
	// Clients, when set, is asked at a fault for the commands whose bd processes
	// it saw, most first; nil names none. BdParents.Sample is the one a host
	// uses.
	Clients func(ctx context.Context) ([]ChurnClient, error)
	// Now is the clock; nil is time.Now.
	Now func() time.Time
}

// ChurnClient is a command whose children were bd processes, and how many.
type ChurnClient struct {
	Command string
	Count   int
}

// NewBeadsChurn is the check over a counter, keeping its sample in state.
func NewBeadsChurn(counter func(ctx context.Context) (int64, error), state application.DoctorState) *BeadsChurn {
	return &BeadsChurn{Counter: counter, State: state}
}

// Name implements application.DoctorCheck.
func (b *BeadsChurn) Name() string { return BeadsChurnName }

func (b *BeadsChurn) now() time.Time {
	if b.Now != nil {
		return b.Now()
	}
	return time.Now()
}

func (b *BeadsChurn) limit() float64 {
	if b.Limit > 0 {
		return b.Limit
	}
	return BeadsChurnLimitPerMinute
}

// Probe implements application.DoctorCheck. The first run, and the first after
// a restart of the server or a gap of over an hour, only take a sample; the
// next judges the connections a minute since it. The sample is kept as an
// episode: its time as FirstFaulty and its count as the one entry of SeenPaths.
func (b *BeadsChurn) Probe(ctx context.Context) (application.Verdict, string) {
	count, err := b.Counter(ctx)
	if err != nil {
		return application.DoctorCannotTell, fmt.Sprintf("reading the beads server's connection count: %v", err)
	}
	now := b.now()
	last, err := b.State.Load(ctx, beadsChurnSampleName)
	if err != nil {
		return application.DoctorCannotTell, err.Error()
	}
	var before int64 = -1
	if len(last.SeenPaths) == 1 {
		if n, err := strconv.ParseInt(last.SeenPaths[0], 10, 64); err == nil {
			before = n
		}
	}
	elapsed := now.Sub(last.FirstFaulty)
	if before >= 0 && !last.FirstFaulty.IsZero() && elapsed >= 0 && elapsed < beadsChurnShortest {
		return application.DoctorOK, ""
	}
	if err := b.State.Save(ctx, beadsChurnSampleName, application.DoctorEpisode{
		FirstFaulty: now, SeenPaths: []string{strconv.FormatInt(count, 10)},
	}); err != nil {
		return application.DoctorCannotTell, fmt.Sprintf("keeping the sample: %v", err)
	}
	if before < 0 || last.FirstFaulty.IsZero() || elapsed < 0 || elapsed > beadsChurnStale || count < before {
		return application.DoctorOK, ""
	}
	rate := float64(count-before) / elapsed.Minutes()
	if rate > b.limit() {
		return application.DoctorFaulty, fmt.Sprintf(
			"the beads server answered %.0f new connections a minute over the last %.0f minutes, past %.0f: "+
				"a client opens one per call (mw events follow, a seat loop, a timer); %s",
			rate, elapsed.Minutes(), b.limit(), b.whoSays(ctx))
	}
	return application.DoctorOK, ""
}

// whoSays is the sentence that names the clients: the top five parent
// commands of the bd processes sampled, or that no single one leads (the
// first holds under half of those seen, or none was seen), or why the sample
// could not be taken.
func (b *BeadsChurn) whoSays(ctx context.Context) string {
	const fallback = "find it with ss -tnp on the server's port"
	if b.Clients == nil {
		return fallback
	}
	clients, err := b.Clients(ctx)
	if err != nil {
		return fmt.Sprintf("the bd processes could not be sampled (%v); %s", err, fallback)
	}
	sort.SliceStable(clients, func(i, j int) bool { return clients[i].Count > clients[j].Count })
	total := 0
	for _, c := range clients {
		total += c.Count
	}
	if total == 0 {
		return "no bd processes were seen in the sample: no single client; " + fallback
	}
	if len(clients) > beadsChurnTopClients {
		clients = clients[:beadsChurnTopClients]
	}
	named := make([]string, len(clients))
	for i, c := range clients {
		named[i] = fmt.Sprintf("%s %d", c.Command, c.Count)
	}
	line := "top parents of the bd processes sampled: " + strings.Join(named, ", ")
	if clients[0].Count*2 < total {
		line += " (no single client)"
	}
	return line
}

// Cure implements application.DoctorCheck: there is none.
func (b *BeadsChurn) Cure(context.Context) error {
	return fmt.Errorf("no cure: changed nothing; a client that opens a connection per call is found by ss -tnp on the server's port and changed to keep one")
}

// Damper implements application.DoctorCheck: one failed cure ends an episode.
func (b *BeadsChurn) Damper() (time.Duration, int) { return 0, 1 }

// WayBack implements application.DoctorCheck.
func (b *BeadsChurn) WayBack() string { return "none: no cure runs" }
