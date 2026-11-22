package application

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"
)

// What a fit Boost has (mw-6ww.185.9). The desktop was retired on 2026-11-11 and
// brought back by hand on 11-13, and guards were lost on the way with no alarm
// that named them: its doctor timer, the trim step, the Windows task, the tidy
// timer, an mw behind main, three zero-byte source files. HostFit is the one
// statement of what a fit Boost has, `mw host fit` says it on the host, and the
// home's boost-fit check asks each Boost for it every hour.

// HostFitUnits is the set of units a Boost runs, as scripts/install-units.sh
// --boost names them (BOOST_UNITS): each is a timer and a service.
var HostFitUnits = []string{"mw-dispatch", "mw-doctor", "mw-host-sweep", "mw-host-tidy"}

// What a fit Boost is held to.
const (
	// HostFitVerdictMaxAge is how old the newest doctor verdict may be.
	HostFitVerdictMaxAge = time.Hour
	// HostFitRenewMaxAge is how old the Windows half of give way's last renew may
	// be: the task renews every 5 minutes.
	HostFitRenewMaxAge = 10 * time.Minute
	// HostFitTrimMaxAge is how long fstrim may go without running on a WSL host:
	// the trim runs daily, and the timer's own check allows a week.
	HostFitTrimMaxAge = 8 * 24 * time.Hour
)

// The cures each gap names: a command the Mayor may run, or 'his hands' and
// which paste.
const (
	HostFitCureUnits    = "sh scripts/install-units.sh --boost --enable, from the rig's checkout on that host"
	HostFitCureUpdate   = "git pull --ff-only in the rig's checkout on that host, then make build there"
	HostFitCureDoctor   = "mw doctor, run on that host"
	HostFitCureBeadsEnv = "his hands: contrib/boost-bootstrap.sh writes ~/.config/mw/beads.env (mode 600); see docs/boost-bootstrap.md"
	HostFitCureWindows  = "his hands: paste the line `mw host wsl-flag --install-line` prints, in an admin PowerShell"
	HostFitCureTrim     = "his hands: paste the line `mw host trim-setup` prints, in a Windows PowerShell"
	HostFitCureGiveWay  = "his hands: paste the line `mw host giveway --install-line` prints, in an admin PowerShell"
)

// HostFitItem is one thing a fit Boost has: its name, whether it is so, what is
// true of it, and, when it is not, the cure.
type HostFitItem struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
	Cure   string `json:"cure,omitempty"`
}

// HostFitReport is what `mw host fit` says: this mw's commit and one item each.
type HostFitReport struct {
	Commit string        `json:"commit,omitempty"`
	Items  []HostFitItem `json:"items"`
}

// Gaps are the items that are not ok.
func (r HostFitReport) Gaps() []HostFitItem {
	var gaps []HostFitItem
	for _, item := range r.Items {
		if !item.OK {
			gaps = append(gaps, item)
		}
	}
	return gaps
}

// Fit is whether there is no gap.
func (r HostFitReport) Fit() bool { return len(r.Gaps()) == 0 }

// HostFitZeroBytes are the tracked files of one checkout that are empty on disk
// though the commit holds content: what an unclean death leaves behind.
type HostFitZeroBytes struct {
	// Where names the checkout: "the rig checkout" or "the vault".
	Where string
	// Dir is the checkout's directory.
	Dir   string
	Paths []string
}

// HostFitLimits are the host's own numbers: its caps and its memory.
type HostFitLimits struct {
	Cap, BuilderJobs        int
	MemTotalMB, SwapTotalMB int
}

// HostFitUnitState is one unit as the host has it: whether its timer is enabled,
// and when its service last ran (zero for never).
type HostFitUnitState struct {
	Enabled bool
	LastRun time.Time
}

// HostFitProbes is what HostFit reads of the machine it runs on. It changes
// nothing.
type HostFitProbes interface {
	// MwBehind is this mw's short commit and how many commits origin/main of its
	// own checkout has that this mw lacks.
	MwBehind(ctx context.Context) (commit string, behind int, err error)
	Unit(ctx context.Context, unit string) (HostFitUnitState, error)
	// DoctorVerdictAt is when the doctor last wrote a verdict, zero for never.
	DoctorVerdictAt(ctx context.Context) (time.Time, error)
	ZeroBytes(ctx context.Context) ([]HostFitZeroBytes, error)
	// BeadsEnv is the name of the beads env file and whether it is there; its
	// contents are never read.
	BeadsEnv(ctx context.Context) (name string, present bool, err error)
	WSL(ctx context.Context) (bool, error)
	// WindowsState is whether the windows-state check finds the Windows side as
	// it must be, and what it says.
	WindowsState(ctx context.Context) (ok bool, text string, err error)
	// FstrimLastRun is when fstrim last ran on this WSL host, zero for never.
	FstrimLastRun(ctx context.Context) (time.Time, error)
	// GiveWay says whether this host gives way, and when the Windows half last
	// renewed it.
	GiveWay(ctx context.Context) (gives bool, renewed time.Time, err error)
	Limits(ctx context.Context) (HostFitLimits, error)
}

// HostFit says what a fit Boost has, of the host it runs on.
type HostFit struct {
	Probes HostFitProbes
	// DiskFits is the wsl-disk-fits check on a WSL host; nil leaves it out (the
	// check is D5's, mw-6ww.185.5, and is not on every build).
	DiskFits func(ctx context.Context) (ok bool, text string, err error)
	// Now is the clock; nil is time.Now.
	Now func() time.Time
}

func (h HostFit) now() time.Time {
	if h.Now != nil {
		return h.Now()
	}
	return time.Now() //mw:realclock the default of the injected Now, used where none is set
}

// Run reads the host and returns one item each. Nothing is changed.
func (h HostFit) Run(ctx context.Context) HostFitReport {
	var report HostFitReport
	add := func(item HostFitItem) { report.Items = append(report.Items, item) }
	now := h.now()

	commit, behind, err := h.Probes.MwBehind(ctx)
	report.Commit = commit
	switch {
	case err != nil:
		add(unread("mw-commit", err, HostFitCureUpdate))
	case behind > 0:
		add(HostFitItem{Name: "mw-commit", Detail: fmt.Sprintf("mw %s is behind origin/main by %d", fitCommit(commit), behind), Cure: HostFitCureUpdate})
	default:
		add(HostFitItem{Name: "mw-commit", OK: true, Detail: fmt.Sprintf("mw %s is at origin/main", fitCommit(commit))})
	}

	for _, unit := range HostFitUnits {
		add(h.unitItem(ctx, unit))
	}

	switch at, err := h.Probes.DoctorVerdictAt(ctx); {
	case err != nil:
		add(unread("doctor-verdict", err, HostFitCureDoctor))
	case at.IsZero():
		add(HostFitItem{Name: "doctor-verdict", Detail: "the doctor has never written a verdict", Cure: HostFitCureDoctor + ", and " + HostFitCureUnits})
	case now.Sub(at) > HostFitVerdictMaxAge:
		add(HostFitItem{Name: "doctor-verdict", Detail: fmt.Sprintf("the newest doctor verdict is %s old (over 1 hour)", ageOf(now.Sub(at))), Cure: HostFitCureDoctor + "; if mw-doctor.timer is not enabled: " + HostFitCureUnits})
	default:
		add(HostFitItem{Name: "doctor-verdict", OK: true, Detail: fmt.Sprintf("the newest doctor verdict is %s old", ageOf(now.Sub(at)))})
	}

	zero, err := h.Probes.ZeroBytes(ctx)
	switch {
	case err != nil:
		add(unread("zero-byte-files", err, "git status, in the rig's checkout and the vault"))
	default:
		add(zeroItem(zero))
	}

	switch name, present, err := h.Probes.BeadsEnv(ctx); {
	case err != nil:
		add(unread("beads-env", err, HostFitCureBeadsEnv))
	case !present:
		add(HostFitItem{Name: "beads-env", Detail: fmt.Sprintf("%s is missing: bd would open an empty database of its own", fitCommit(name)), Cure: HostFitCureBeadsEnv})
	default:
		add(HostFitItem{Name: "beads-env", OK: true, Detail: name + " is there"})
	}

	if wsl, err := h.Probes.WSL(ctx); err != nil {
		add(unread("wsl", err, ""))
	} else if wsl {
		for _, item := range h.wslItems(ctx, now) {
			add(item)
		}
	}

	switch gives, renewed, err := h.Probes.GiveWay(ctx); {
	case err != nil:
		add(unread("give-way-renew", err, HostFitCureGiveWay))
	case !gives:
		// A host that does not give way has no Windows half to renew it.
	case renewed.IsZero() || now.Sub(renewed) > HostFitRenewMaxAge:
		detail := "the Windows half of give way has never renewed the marker"
		if !renewed.IsZero() {
			detail = fmt.Sprintf("the Windows half of give way last renewed %s ago (over 10 minutes)", ageOf(now.Sub(renewed)))
		}
		add(HostFitItem{Name: "give-way-renew", Detail: detail, Cure: HostFitCureGiveWay})
	default:
		add(HostFitItem{Name: "give-way-renew", OK: true, Detail: fmt.Sprintf("the Windows half of give way renewed %s ago", ageOf(now.Sub(renewed)))})
	}

	switch limits, err := h.Probes.Limits(ctx); {
	case err != nil:
		add(unread("limits", err, ""))
	default:
		add(HostFitItem{Name: "limits", OK: true, Detail: fmt.Sprintf("cap %d, builder_jobs %s, memory %d MB, swap %d MB",
			limits.Cap, jobsWords(limits.BuilderJobs), limits.MemTotalMB, limits.SwapTotalMB)})
	}
	return report
}

func (h HostFit) unitItem(ctx context.Context, unit string) HostFitItem {
	name := "unit " + unit
	state, err := h.Probes.Unit(ctx, unit)
	switch {
	case err != nil:
		return unread(name, err, HostFitCureUnits)
	case !state.Enabled:
		return HostFitItem{Name: name, Detail: unit + ".timer is not enabled; last run " + h.lastRun(state.LastRun), Cure: HostFitCureUnits}
	}
	return HostFitItem{Name: name, OK: true, Detail: unit + ".timer is enabled; last run " + h.lastRun(state.LastRun)}
}

func (h HostFit) lastRun(at time.Time) string {
	if at.IsZero() {
		return "never"
	}
	return ageOf(h.now().Sub(at)) + " ago"
}

// wslItems are what a WSL host has besides: the Windows side, fstrim, the disk.
func (h HostFit) wslItems(ctx context.Context, now time.Time) []HostFitItem {
	var items []HostFitItem
	switch ok, text, err := h.Probes.WindowsState(ctx); {
	case err != nil:
		items = append(items, unread("windows-state", err, HostFitCureWindows))
	case !ok:
		items = append(items, HostFitItem{Name: "windows-state", Detail: text, Cure: HostFitCureWindows})
	default:
		items = append(items, HostFitItem{Name: "windows-state", OK: true, Detail: text})
	}
	switch at, err := h.Probes.FstrimLastRun(ctx); {
	case err != nil:
		items = append(items, unread("fstrim", err, HostFitCureTrim))
	case at.IsZero():
		items = append(items, HostFitItem{Name: "fstrim", Detail: "fstrim has never run", Cure: HostFitCureTrim})
	case now.Sub(at) > HostFitTrimMaxAge:
		items = append(items, HostFitItem{Name: "fstrim", Detail: fmt.Sprintf("fstrim last ran %s ago (over 8 days)", ageOf(now.Sub(at))), Cure: HostFitCureTrim})
	default:
		items = append(items, HostFitItem{Name: "fstrim", OK: true, Detail: fmt.Sprintf("fstrim last ran %s ago", ageOf(now.Sub(at)))})
	}
	if h.DiskFits != nil {
		switch ok, text, err := h.DiskFits(ctx); {
		case err != nil:
			items = append(items, unread("wsl-disk-fits", err, ""))
		case !ok:
			items = append(items, HostFitItem{Name: "wsl-disk-fits", Detail: text, Cure: "mw doctor wsl-disk-fits names it, on that host"})
		default:
			items = append(items, HostFitItem{Name: "wsl-disk-fits", OK: true, Detail: text})
		}
	}
	return items
}

// zeroItem is the one item for the empty tracked files of every checkout.
func zeroItem(zero []HostFitZeroBytes) HostFitItem {
	var said, cures []string
	total := 0
	for _, z := range zero {
		if len(z.Paths) == 0 {
			continue
		}
		total += len(z.Paths)
		shown := z.Paths
		if len(shown) > 5 {
			shown = shown[:5]
		}
		said = append(said, fmt.Sprintf("%s: %d (%s)", z.Where, len(z.Paths), strings.Join(shown, ", ")))
		cures = append(cures, fmt.Sprintf("git -C %s checkout -- %s", z.Dir, strings.Join(z.Paths, " ")))
	}
	if total == 0 {
		return HostFitItem{Name: "zero-byte-files", OK: true, Detail: "no tracked file of the rig checkout or the vault is empty on disk"}
	}
	return HostFitItem{
		Name:   "zero-byte-files",
		Detail: fmt.Sprintf("%d tracked files are empty on disk, so the checkout stays at an old commit: %s", total, strings.Join(said, "; ")),
		Cure:   strings.Join(cures, "; "),
	}
}

// unread is the item for a thing the host could not read: a gap, since a fit
// host can be read.
func unread(name string, err error, cure string) HostFitItem {
	return HostFitItem{Name: name, Detail: "could not be read: " + oneLineFit(err.Error()), Cure: cure}
}

func oneLineFit(s string) string { return strings.Join(strings.Fields(s), " ") }

func fitCommit(s string) string {
	if s == "" {
		return "(unknown commit)"
	}
	return s
}

func jobsWords(n int) string {
	if n == 0 {
		return "unlimited"
	}
	return fmt.Sprint(n)
}

// ageOf says a span in the unit that fits it.
func ageOf(d time.Duration) string {
	switch {
	case d < 0:
		return "0 minutes"
	case d >= 48*time.Hour:
		return fmt.Sprintf("%d days", int(d/(24*time.Hour)))
	case d >= 2*time.Hour:
		return fmt.Sprintf("%d hours", int(d/time.Hour))
	}
	return fmt.Sprintf("%d minutes", int(d/time.Minute))
}

// WriteText prints one line a item: ok or the gap, and its cure.
func (r HostFitReport) WriteText(w io.Writer) {
	for _, item := range r.Items {
		if item.OK {
			fmt.Fprintf(w, "ok   %s: %s\n", item.Name, item.Detail)
			continue
		}
		line := fmt.Sprintf("GAP  %s: %s", item.Name, item.Detail)
		if item.Cure != "" {
			line += " (cure: " + item.Cure + ")"
		}
		fmt.Fprintln(w, line)
	}
}
