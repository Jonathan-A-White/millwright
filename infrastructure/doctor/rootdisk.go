package doctor

import (
	"context"
	"fmt"
	"syscall"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

var _ application.DoctorCheck = (*RootDiskBudget)(nil)

// RootDiskBudgetName is what the check is called: in the log, and on the
// command line as `mw doctor root-disk-budget`.
const RootDiskBudgetName = "root-disk-budget"

// DefaultRootDiskMarginBytes is the headroom, in bytes, this check keeps
// between the root's used bytes and the budget before it is faulty, when
// nothing says otherwise: 10 GB. It is the same figure
// infrastructure/config.DefaultDoctorRootDiskMarginBytes reads as.
const DefaultRootDiskMarginBytes int64 = 10_000_000_000

// The root-disk-budget check's damper. Its cure is one note to the Mayor and
// nothing on the host, so there is no cure to cap: the damper's wait alone
// says how often the Mayor hears of it. A cap would only add a second note
// when the episode reaches it, written by the doctor itself over the first.
const (
	RootDiskBudgetDamperWait = 6 * time.Hour
	RootDiskBudgetDamperCap  = 0
)

// RootDiskBudget is the check that watches the desktop's WSL root against the
// Windows drive that holds its vhdx. The vhdx is sparse: it grows as ext4
// allocates blocks and never shrinks by itself, and when the Windows drive is
// full WSL's root goes read-only (mw-6ww.60, 2026-10-03). WSL on the desktop
// is sealed from Windows, so the doctor cannot read that drive's free space;
// the root's own used bytes is the nearest signal from inside, a lower bound of
// the vhdx's size. Budget is the drive's free space at the time the vhdx was
// placed there; faulty once used bytes pass Budget less Margin. It changes
// nothing on the host: its cure is one note to the Mayor, a person's to act on.
// With no Budget set it is inert.
type RootDiskBudget struct {
	// Budget is the bytes the Windows drive had free when the vhdx was put on
	// it. Zero is inert: the check can say nothing.
	Budget int64
	// Margin is the headroom kept under Budget. Zero reads
	// DefaultRootDiskMarginBytes.
	Margin int64
	// UsedBytes reads the root filesystem's used bytes. Nil reads the real /.
	UsedBytes func() (int64, error)
	// Note puts text in the Mayor's way: mw doctor sets it to this check's own
	// doctor note, the one mw millhand tick wakes the Millhand for. Nil
	// writes nothing.
	Note func(ctx context.Context, text string) error
}

// NewRootDiskBudget is the check over the real /, inert until a budget is set.
func NewRootDiskBudget() *RootDiskBudget { return &RootDiskBudget{} }

// Name implements application.DoctorCheck.
func (r *RootDiskBudget) Name() string { return RootDiskBudgetName }

// Probe implements application.DoctorCheck: used bytes of the root against
// Budget less Margin. Inert with no budget set, and reported as an ok that
// says so rather than cannot-tell: mw doctor writes a note, and so wakes the
// Millhand, for every cannot-tell, and a host with no vhdx to watch has
// nothing to tell. Cannot-tell when the root's usage cannot be read.
func (r *RootDiskBudget) Probe(context.Context) (application.Verdict, string) {
	if r.Budget <= 0 {
		return application.DoctorOK, "n/a: no root_disk_budget_bytes set"
	}
	used, err := r.used()
	if err != nil {
		return application.DoctorCannotTell, err.Error()
	}
	if used > r.Budget-r.margin() {
		return application.DoctorFaulty, r.figures(used)
	}
	return application.DoctorOK, r.figures(used)
}

// Cure implements application.DoctorCheck: one note to the Mayor, nothing on
// the host changed. A note that cannot be written fails the cure, so the
// damper tries again.
func (r *RootDiskBudget) Cure(ctx context.Context) error {
	used, err := r.used()
	if err != nil {
		return err
	}
	text := fmt.Sprintf("root-disk-budget: the WSL root uses %s of a %s budget on the Windows drive that holds its vhdx; under %s headroom. "+
		"Hands at the desk: free space on that drive or compact the vhdx (Optimize-VHD) or move it; "+
		"the check stays faulty until root_disk_budget_bytes is raised.",
		gb(used), gb(r.Budget), gb(r.margin()))
	if r.Note == nil {
		return nil
	}
	return r.Note(ctx, text)
}

// Damper implements application.DoctorCheck.
func (r *RootDiskBudget) Damper() (time.Duration, int) {
	return RootDiskBudgetDamperWait, RootDiskBudgetDamperCap
}

// WayBack implements application.DoctorCheck: the cure changes nothing, so the
// way back is only how to stop the check.
func (r *RootDiskBudget) WayBack() string {
	return "set root_disk_budget_bytes = 0 in [doctor] (the check goes inert)"
}

func (r *RootDiskBudget) figures(used int64) string {
	return fmt.Sprintf("the WSL root uses %s of a %s budget, %s margin", gb(used), gb(r.Budget), gb(r.margin()))
}

func (r *RootDiskBudget) used() (int64, error) {
	if r.UsedBytes != nil {
		return r.UsedBytes()
	}
	return RootUsedBytes()
}

func (r *RootDiskBudget) margin() int64 {
	if r.Margin > 0 {
		return r.Margin
	}
	return DefaultRootDiskMarginBytes
}

// RootUsedBytes is the used bytes of the root filesystem, as statfs of /
// reports them: (blocks - free blocks) x block size.
func RootUsedBytes() (int64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs("/", &st); err != nil {
		return 0, fmt.Errorf("statfs /: %w", err)
	}
	return int64(st.Blocks-st.Bfree) * int64(st.Bsize), nil
}

// gb is bytes as gigabytes (10^9), one decimal.
func gb(bytes int64) string { return fmt.Sprintf("%.1f GB", float64(bytes)/1e9) }
