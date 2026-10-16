package doctor

import (
	"context"
	"fmt"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

var _ application.DoctorCheck = (*BeadsFlush)(nil)

// BeadsFlushName is what the check is called: in the log, and on the command
// line as `mw doctor beads-flush`.
const BeadsFlushName = "beads-flush"

// The beads-flush check's damper: as with beads-batch there is no cure to
// retry.
const (
	BeadsFlushDamperWait = 0 * time.Second
	BeadsFlushDamperCap  = 1
)

// BeadsFlush is the check that the home's flush of the beads database is
// getting through while bd is in batch mode: an unflushed working set is the
// one thing batch adds to the failure modes, in no backup and no push until
// it is committed (mw-gq6.508). Faulty when the last flush failed or is older
// than application.BeadsFlushLate, or when none is on record. Ok on a host that
// is not home, which flushes nothing, and while the vault says on. It never
// cures: the job tries again every fifteen minutes by itself.
type BeadsFlush struct {
	// Dir is the vault's directory.
	Dir string
	// Home says whether this host is the home.
	Home func(ctx context.Context) (bool, error)
	// Log holds the home's last flush.
	Log application.BeadsFlushLog
	// Now is the clock; nil is time.Now.
	Now func() time.Time
}

// NewBeadsFlush is the check over the vault at dir.
func NewBeadsFlush(dir string, home func(ctx context.Context) (bool, error), log application.BeadsFlushLog) *BeadsFlush {
	return &BeadsFlush{Dir: dir, Home: home, Log: log}
}

// Name implements application.DoctorCheck.
func (b *BeadsFlush) Name() string { return BeadsFlushName }

// Probe implements application.DoctorCheck.
func (b *BeadsFlush) Probe(ctx context.Context) (application.Verdict, string) {
	vault, err := VaultAutoCommit(b.Dir)
	if err != nil {
		return application.DoctorCannotTell, err.Error()
	}
	if vault != application.AutoCommitBatch {
		return application.DoctorOK, ""
	}
	home, err := b.Home(ctx)
	if err != nil {
		return application.DoctorCannotTell, err.Error()
	}
	if !home {
		return application.DoctorOK, ""
	}
	last, err := b.Log.Last(ctx)
	if err != nil {
		return application.DoctorCannotTell, err.Error()
	}
	now := time.Now
	if b.Now != nil {
		now = b.Now
	}
	switch {
	case last.At.IsZero():
		return application.DoctorFaulty, "bd is in batch mode and the home has no flush on record: its writes are in no commit, so in no backup (is mw events follow running?)"
	case last.Failed != "":
		return application.DoctorFaulty, fmt.Sprintf("the home's last beads flush, at %s, failed: %s", last.At.UTC().Format(time.RFC3339), last.Failed)
	case now().Sub(last.At) > application.BeadsFlushLate:
		return application.DoctorFaulty, fmt.Sprintf("the home's last beads flush was at %s, over %s ago: its writes since are in no commit, so in no backup (is mw events follow running?)",
			last.At.UTC().Format(time.RFC3339), application.BeadsFlushLate)
	}
	return application.DoctorOK, ""
}

// Cure implements application.DoctorCheck: there is none.
func (b *BeadsFlush) Cure(context.Context) error {
	return fmt.Errorf("no cure: changed nothing; the follower's beads-flush job runs again by itself; " +
		"a person can run `bd dolt commit` in the vault to flush now")
}

// Damper implements application.DoctorCheck.
func (b *BeadsFlush) Damper() (time.Duration, int) { return BeadsFlushDamperWait, BeadsFlushDamperCap }

// WayBack implements application.DoctorCheck: nothing ever changes.
func (b *BeadsFlush) WayBack() string { return "none: no cure runs" }
