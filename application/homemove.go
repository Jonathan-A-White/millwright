package application

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/domain"
)

// The user units a move starts, and how long it waits for what it starts.
const (
	// DoltBeadsUnit is the user unit that serves the beads database over the
	// network; a host without it stays embedded, in backup mode.
	DoltBeadsUnit = "dolt-beads"
	// PosternBackendUnit is the user unit of the postern backend. Its env file
	// is placed beforehand, with the issuer key off: a move only starts it.
	PosternBackendUnit = "postern-backend"
	// HomeMoveAnswerWait is how long the move waits for the old home to answer ssh.
	HomeMoveAnswerWait = 10 * time.Second
	// HomeMoveHandoffWait is how long a planned move waits for the old home's Mayor
	// to hand off before it gives up. It never kills the Mayor.
	HomeMoveHandoffWait = 15 * time.Minute
	// HomeMoveBeadsServerWait is how long the move waits for dolt-beads, started on
	// a fresh data dir, to answer before it bootstraps into it.
	HomeMoveBeadsServerWait = 30 * time.Second
	// HomeMoveBackendWait is how long the move waits for the backend to answer as
	// home. A backend on a host that is not home is in standby and re-reads
	// `mw home --check` every 30 seconds.
	HomeMoveBackendWait = 45 * time.Second
)

// HomeMoveHost is what mw home move asks of the machines: the old home's ssh,
// GitHub, bd, git, systemctl, the backend and the vault's bin/mayor-up. The real
// one runs them; a test stands in for them.
type HomeMoveHost interface {
	// OldHomeAnswers reports whether the host reached by the ssh prefix answers
	// within wait. A host that does not answer is (false, nil): that is the
	// finding, not a failure.
	OldHomeAnswers(ctx context.Context, ssh []string, wait time.Duration) (bool, error)

	// BackupTime is when GitHub's refs/dolt/data, the beads backup, was last
	// written. It reads nothing else and changes nothing.
	BackupTime(ctx context.Context) (time.Time, error)

	// SetBeadsAside moves the vault's embedded beads database to a dated
	// directory beside it, stamp in its name, and never deletes it. A vault with
	// none is the zero move, not an error.
	SetBeadsAside(ctx context.Context, stamp string) (AsideMove, error)

	// SetDoltAside does the same for the vault's .beads/dolt, the directory the
	// dolt-beads unit serves. The unit must not be running.
	SetDoltAside(ctx context.Context, stamp string) (AsideMove, error)

	// BootstrapBeads runs `bd bootstrap --yes`: a database cloned from GitHub's
	// refs/dolt/data.
	BootstrapBeads(ctx context.Context) error

	// RestoreBeadsConfig runs `git checkout -- .beads/config.yaml` in the vault:
	// bootstrap drops that file's trailing newline.
	RestoreBeadsConfig(ctx context.Context) error

	// BeadsCount is how many beads bd answers with.
	BeadsCount(ctx context.Context) (int, error)

	// UnitInstalled reports whether this host has the user unit.
	UnitInstalled(ctx context.Context, unit string) (bool, error)

	// UnitWorkingDirectory is the unit's WorkingDirectory, read from systemd: for
	// dolt-beads, the data directory its server serves.
	UnitWorkingDirectory(ctx context.Context, unit string) (string, error)

	// EmptyDataDir makes sure dir exists and is empty, so that a server started in
	// it has no database. A directory with anything in it is refused, never cleared.
	EmptyDataDir(ctx context.Context, dir string) error

	// BeadsServerAnswers waits up to wait for the beads database server bd is told
	// (BEADS_DOLT_SERVER_HOST on BEADS_DOLT_SERVER_PORT) to take a connection.
	BeadsServerAnswers(ctx context.Context, wait time.Duration) error

	// CreateBeadsUser makes, on the fresh server, the user bd logs in as
	// (BEADS_DOLT_SERVER_USER, with BEADS_DOLT_PASSWORD), with all privileges: a
	// server on an empty data directory knows only root. user is the name, for the
	// words around it; created is false when there was nothing to make (the user is
	// root, or none is set). The password is never returned, printed or put in an
	// argument.
	CreateBeadsUser(ctx context.Context) (user string, created bool, err error)

	// StartUnit starts the user unit, and reports whether it did: a unit that was
	// already running is left alone, and is (false, nil).
	StartUnit(ctx context.Context, unit string) (started bool, err error)

	// StopUnit stops the user unit, and reports whether it did: a unit that was
	// not running is left alone, and is (false, nil).
	StopUnit(ctx context.Context, unit string) (stopped bool, err error)

	// BackendServing waits up to wait for the backend at url to answer /healthz
	// as home: 200, and not in standby.
	BackendServing(ctx context.Context, url string, wait time.Duration) error

	// MayorUp runs the vault's bin/mayor-up. started is false when a Mayor
	// already sits here; said is what it printed last: the window it started.
	MayorUp(ctx context.Context) (started bool, said string, err error)
}

// OldHome is what a planned move asks of the old home, which is up: the mw, the
// systemctl and the vault there, reached over the ssh prefix. The real one runs them
// over ssh; a test stands in for them.
type OldHome interface {
	// MailMayor sends mail to the Mayor in the old home's own beads database, signed
	// from, so that the Mayor there finds it where it is looking.
	MailMayor(ctx context.Context, ssh []string, from, subject, body string) error

	// MayorGone waits up to wait for the old home's Mayor to have handed off: its
	// .mayor-acting empty, or its Mayor process gone (bin/respawn-mayor's own
	// markers). A Mayor still there when wait is up is (false, what it sees, nil).
	// It never stops a Mayor.
	MayorGone(ctx context.Context, ssh []string, wait time.Duration) (gone bool, said string, err error)

	// Sync runs mw sync there: the final backup push of the beads and the vault.
	Sync(ctx context.Context, ssh []string) error

	// OldBeadsCount is how many beads bd counts on the old home, in its vault and
	// with the server mode it runs in.
	OldBeadsCount(ctx context.Context, ssh []string) (int, error)

	// OldUnitInstalled reports whether the old home has the user unit.
	OldUnitInstalled(ctx context.Context, ssh []string, unit string) (bool, error)

	// OldStopUnit stops the user unit there, and reports whether it did: a unit
	// that was not running is (false, nil).
	OldStopUnit(ctx context.Context, ssh []string, unit string) (stopped bool, err error)

	// OldDisableUnit disables the user unit there and stops it (`systemctl --user
	// disable --now`), so that a reboot of the old home does not bring it back. It
	// reports whether it did: a unit that was neither enabled nor running is
	// (false, nil).
	OldDisableUnit(ctx context.Context, ssh []string, unit string) (disabled bool, err error)

	// OldPush runs bd dolt push in the old home's vault, with the server mode it
	// runs in: the forced last flush of the beads database to refs/dolt/data, which
	// mw sync's backup only does when it is due. The server has to be running.
	OldPush(ctx context.Context, ssh []string) error

	// Mirror runs mw postern mirror there: the final copy of the backend's data to
	// this host.
	Mirror(ctx context.Context, ssh []string) error
}

// AsideMove is a database directory set aside: From is where it was, To where it
// is now. The zero value is that there was nothing to set aside.
type AsideMove struct{ From, To string }

// HomeWriter writes the vault's home file. It is the one place that file is
// written.
type HomeWriter interface {
	// WriteHome replaces the home file with record. It commits nothing.
	WriteHome(ctx context.Context, record domain.HomeRecord) error
}

// PosternIndexTime reads when the postern backend's index last had a record put
// in it. PosternMirrorer satisfies it.
type PosternIndexTime interface {
	LastIndexTime(ctx context.Context, ssh []string, dir string) (at time.Time, ok bool, err error)
}

// HomeMove is mw home move: run on the host that becomes home, it takes the home
// over from an old home that is dead.
type HomeMove struct {
	Files   HomeFile
	Writer  HomeWriter
	Vault   VaultFiles
	Machine HomeMoveHost
	// Old is the old home, for a planned move.
	Old     OldHome
	Mailbox Mailbox
	Index   PosternIndexTime
	// Lock, when set, is held while the beads database is swapped, so that no
	// sync on this host runs in the middle of it.
	Lock HostLock
	Out  io.Writer

	// Host is this host's name, and Target the host the move makes home: the two
	// must be the same.
	Host, Target string
	// Reach is how this host reaches each other host: an ssh prefix by host name.
	Reach map[string]string
	// VaultDir is the vault's directory, named in the ways back.
	VaultDir string
	// DataDir is the backend's POSTERN_DATA.
	DataDir string
	// BackendURL is where this host's own backend answers.
	BackendURL string
	// Actor signs the home file and the mail.
	Actor string
	// BeadsSync is this host's beads_sync setting; a move that finds it is not
	// auto says so at the end. Empty says nothing.
	BeadsSync BeadsSyncMode

	// OldHomeDead is the Governor's word that the old home is dead.
	OldHomeDead bool
	// Planned is a move with both hosts up: the old home hands off, flushes and
	// stands down first. It needs the old home to answer, and Old.
	Planned bool
	// DryRun prints the steps and their ways back and runs none of them.
	DryRun bool
	// Now is the clock; time.Now when nil.
	Now func() time.Time
}

// HomeMoveStopped is a move that stopped at a step. Changed says whether any step
// had changed anything by then: when it had not, the move is refused and there is
// nothing to undo.
type HomeMoveStopped struct {
	Step, Of int
	Title    string
	Err      error
	Changed  bool
}

func (s *HomeMoveStopped) Error() string {
	text := fmt.Sprintf("home move stopped at step %d of %d (%s): %v", s.Step, s.Of, s.Title, s.Err)
	if !s.Changed {
		return text + ". Nothing was changed."
	}
	return text + ". The ways back of what was done are printed above."
}

func (s *HomeMoveStopped) Unwrap() error { return s.Err }

// HomeMoveStoppedIn reports whether err is a move that stopped at a step.
func HomeMoveStoppedIn(err error) (*HomeMoveStopped, bool) {
	var stopped *HomeMoveStopped
	return stopped, errors.As(err, &stopped)
}

// homeMoveStep is one step of the move: what a dry run says of it, and what
// running it does.
type homeMoveStep struct {
	title string
	// plan is what a dry run says the step does, and back what undoes it.
	plan []string
	back string
	run  func(r *homeMoveRun, ctx context.Context) error
}

// homeMoveRun is one move, as it goes: what it has learned and what it has done.
type homeMoveRun struct {
	m      HomeMove
	old    string
	prefix []string
	// prev is the home file as it was, nil when there was none or it was not
	// understood; hadFile is whether there was a file at all.
	prev    *domain.HomeRecord
	hadFile bool
	at      time.Time
	// backupAt is when GitHub's refs/dolt/data was written, read by step 2.
	backupAt time.Time
	// oldCount is the old home's `bd count` after its final flush, taken by a
	// planned move's stand-down; haveOldCount is whether it was taken at all.
	oldCount     int
	haveOldCount bool
	// saidNoOldCount is whether a dead move has said already that it has no old count.
	saidNoOldCount bool
	// asides are the directories set aside, for the error of a count that differs.
	asides []AsideMove
	// backs are the ways back of the step that is running, the first thing done
	// first; nothing says why there is none to give in the dry-run's words.
	backs   []string
	nothing string
}

const homeMoveNothing = "nothing was changed, so there is nothing to undo."

func (m HomeMove) now() time.Time {
	if m.Now != nil {
		return m.Now().UTC()
	}
	return time.Now().UTC()
}

func (m HomeMove) out() io.Writer {
	if m.Out != nil {
		return m.Out
	}
	return io.Discard
}

// refuse checks what needs no machine: the move is to this host, it is a host
// that can be home and is not, and there is a way to reach the old one. It returns
// the old home, the way to reach it, the home file as it is and whether there was a
// file at all, one not understood included.
func (m HomeMove) refuse(ctx context.Context) (old string, prefix []string, prev *domain.HomeRecord, hadFile bool, err error) {
	if !isHomeHost(m.Target) {
		return "", nil, nil, false, fmt.Errorf("the home can only be %s, not %s. Nothing was changed", strings.Join(domain.HomeHosts, " or "), m.Target)
	}
	if m.Host != m.Target {
		return "", nil, nil, false, fmt.Errorf("this host is %s, not %s: mw home move runs on the host that becomes home, so run it on %s. Nothing was changed", m.Host, m.Target, m.Target)
	}
	if m.Planned && m.OldHomeDead {
		return "", nil, nil, false, errors.New("--planned is a move with the old home up and --old-home-dead says it is dead: give one of them. Nothing was changed")
	}
	if m.Planned && m.Old == nil {
		return "", nil, nil, false, errors.New("a planned move has no way to reach the old home. Nothing was changed")
	}
	for _, host := range domain.HomeHosts {
		if host != m.Target {
			old = host
		}
	}
	record, err := WhereIsHome(ctx, m.Files)
	unknown, isUnknown := HomeUnknownIn(err)
	if err != nil && !isUnknown {
		return "", nil, nil, false, err
	}
	hadFile = err == nil || !errors.Is(unknown.Why, ErrNoHomeFile)
	if err == nil {
		if record.Host == m.Target {
			return "", nil, nil, false, fmt.Errorf("%s is already home (changed %s by %s): there is nothing to move. If an earlier move stopped part-way, finish it by hand from its steps in docs/home-move.md. Nothing was changed",
				m.Target, record.At.UTC().Format(time.RFC3339), record.By)
		}
		prev = &record
	}
	prefix = strings.Fields(m.Reach[old])
	if len(prefix) < 2 {
		return "", nil, nil, false, fmt.Errorf("no way to reach %s, the old home: set `%s = \"ssh <alias>\"` under [hands_hosts] in the config file. Nothing was changed", old, old)
	}
	return old, prefix, prev, hadFile, nil
}

func isHomeHost(host string) bool {
	for _, known := range domain.HomeHosts {
		if host == known {
			return true
		}
	}
	return false
}

// Run makes the move, or with DryRun says what it would do. Each step is printed
// with its way back. A step that fails stops the move, and the ways back of what
// was done, the failing step's own included, are printed last step first.
func (m HomeMove) Run(ctx context.Context) error {
	old, prefix, prev, hadFile, err := m.refuse(ctx)
	if err != nil {
		return err
	}
	r := &homeMoveRun{m: m, old: old, prefix: prefix, prev: prev, hadFile: hadFile, at: m.now()}
	steps := r.steps()
	if m.DryRun {
		r.dryRun(steps)
		return nil
	}
	return r.run(ctx, steps)
}

func (r *homeMoveRun) say(format string, args ...any) {
	fmt.Fprintf(r.m.out(), "  "+format+"\n", args...)
}

// undo notes the way back of what was just done. Text may run to several lines.
func (r *homeMoveRun) undo(text string) { r.backs = append(r.backs, text) }

func (r *homeMoveRun) dryRun(steps []homeMoveStep) {
	out := r.m.out()
	fmt.Fprintf(out, "Dry run: nothing below is run.\n")
	from := "the home file says no host is home"
	if r.prev != nil {
		from = fmt.Sprintf("the home file says %s is home (changed %s by %s)", r.prev.Host, r.prev.At.UTC().Format(time.RFC3339), r.prev.By)
	}
	fmt.Fprintf(out, "Moving the home from %s to %s, this host: %s.\n", r.old, r.m.Target, from)
	for n, step := range steps {
		fmt.Fprintf(out, "\nStep %d of %d: %s\n", n+1, len(steps), step.title)
		for _, line := range step.plan {
			r.say("%s", line)
		}
		r.say("Way back: %s", step.back)
	}
}

func (r *homeMoveRun) run(ctx context.Context, steps []homeMoveStep) error {
	out := r.m.out()
	fmt.Fprintf(out, "Moving the home from %s to %s, this host.\n", r.old, r.m.Target)
	type done struct {
		title string
		backs []string
	}
	var did []done
	for n, step := range steps {
		fmt.Fprintf(out, "\nStep %d of %d: %s\n", n+1, len(steps), step.title)
		r.backs, r.nothing = nil, homeMoveNothing
		err := step.run(r, ctx)
		did = append(did, done{fmt.Sprintf("Step %d (%s)", n+1, step.title), r.backs})
		if err != nil {
			changed := false
			for _, d := range did {
				changed = changed || len(d.backs) > 0
			}
			if changed {
				fmt.Fprintf(out, "\nThe move stopped. Ways back for what was already done, last step first:\n")
				for i := len(did) - 1; i >= 0; i-- {
					if len(did[i].backs) == 0 {
						continue
					}
					fmt.Fprintf(out, "  %s:\n", did[i].title)
					for j := len(did[i].backs) - 1; j >= 0; j-- {
						for _, line := range strings.Split(did[i].backs[j], "\n") {
							fmt.Fprintf(out, "    %s\n", line)
						}
					}
				}
			}
			return &HomeMoveStopped{Step: n + 1, Of: len(steps), Title: step.title, Err: err, Changed: changed}
		}
		if len(r.backs) == 0 {
			r.say("Way back: %s", r.nothing)
			continue
		}
		r.say("Way back:")
		for i := len(r.backs) - 1; i >= 0; i-- {
			for _, line := range strings.Split(r.backs[i], "\n") {
				fmt.Fprintf(out, "    %s\n", line)
			}
		}
	}
	fmt.Fprintf(out, "\nHome is now %s.\n", r.m.Target)
	if said := r.m.BeadsSync; said != "" && said != BeadsSyncAuto {
		fmt.Fprintf(out, "Note: beads_sync is %q here, not \"auto\": mw sync does not follow the home file. Set beads_sync = \"auto\" in the config file.\n", said)
	}
	return nil
}

func (r *homeMoveRun) steps() []homeMoveStep {
	m := r.m
	dead := []homeMoveStep{
		{
			title: "the old home",
			plan: []string{
				fmt.Sprintf("asks whether %s answers `%s` within %s.", r.old, strings.Join(r.prefix, " "), HomeMoveAnswerWait),
				"If it does, the move stops: old home is up: use --planned.",
				"If it does not, the move goes on only with --old-home-dead, the Governor's word that it is dead.",
			},
			back: "nothing is changed.",
			run:  (*homeMoveRun).oldHome,
		},
		{
			title: "beads, from GitHub",
			plan: []string{
				"reads when GitHub's refs/dolt/data, the beads backup, was written. If GitHub cannot be read the move stops there, with nothing touched.",
				fmt.Sprintf("takes this host's sync lock, stops the %s unit here if it is running (it holds %s/.beads/dolt open), and sets %s/.beads/embeddeddolt and %s/.beads/dolt aside in dated directories in the home directory (beads-embeddeddolt-aside-<time>, beads-dolt-aside-<time>): neither is ever deleted, so a stale .beads/dolt is never served.", DoltBeadsUnit, m.VaultDir, m.VaultDir, m.VaultDir),
				fmt.Sprintf("if this host has the %s unit: reads its WorkingDirectory from systemd, makes sure that directory is empty, starts the unit on it and waits for the server to answer, because bootstrap makes no .beads/dolt of its own and clones into the server it is told. A host without the unit stays embedded, and beads_sync = auto reads home as backup mode.", DoltBeadsUnit),
				"runs `bd bootstrap --yes`, then `git checkout -- .beads/config.yaml` (bootstrap drops its trailing newline), and asks bd how many beads it holds: none is a stop, and so is a count that differs from the old home's own (a planned move takes that over ssh; a dead old home has none to compare).",
			},
			back: fmt.Sprintf("stop %s if the move started it, move the new .beads/embeddeddolt and .beads/dolt (the fresh data directory) away, move both dated directories back in their places, then start %s again if it was running before.", DoltBeadsUnit, DoltBeadsUnit),
			run:  (*homeMoveRun).beads,
		},
		{
			title: "the vault",
			plan: []string{
				fmt.Sprintf("brings the vault level (git pull --rebase), writes the home file (`%s <UTC time> %s`), commits it and pushes.", m.Target, m.Actor),
				"That is the fence: an old home that comes back reads the file, sees it is not home and stays quiet (no Mayor, no backend serving, no beads writes).",
			},
			back: fmt.Sprintf("git -C %s revert --no-edit <the commit>, then push: the home file is as it was.", m.VaultDir),
			run:  (*homeMoveRun).vault,
		},
		{
			title: "the Postern backend",
			plan: []string{
				fmt.Sprintf("starts the %s user unit. Its env file is placed beforehand, issuer key off; a host that is not home already runs it in standby, and it leaves standby by itself within 30 s of the home file changing.", PosternBackendUnit),
				"never mw postern serve on this host: it writes POSTERN_ISSUER_KEY back into the env file and the Mayor loses postern (decision B).",
				fmt.Sprintf("waits up to %s for %s/healthz to answer as home.", HomeMoveBackendWait, m.BackendURL),
			},
			back: fmt.Sprintf("systemctl --user stop %s if the move started it; a backend that was already running goes back to standby by itself once step 3 is undone.", PosternBackendUnit),
			run:  (*homeMoveRun).backend,
		},
		{
			title: "the Mayor",
			plan: []string{
				"sends mail to the Mayor: 'Home moved to " + m.Target + " at <time>: the old home is dead; beads from GitHub as of <time of refs/dolt/data>'.",
				"runs the vault's bin/mayor-up, which starts a Mayor here (it refuses on a host that is not home: step 3 made this one home).",
			},
			back: "tmux kill-window -t '<the window bin/mayor-up prints>'; the message stays sent.",
			run:  (*homeMoveRun).mayor,
		},
		{
			title: "what was lost",
			plan: []string{
				"prints how old GitHub's beads backup is and how old the Postern data here is (the last record of its index).",
			},
			back: "nothing: it only reads.",
			run:  (*homeMoveRun).lost,
		},
	}
	if m.Planned {
		return r.plannedSteps(dead)
	}
	return dead
}

func (r *homeMoveRun) oldHome(ctx context.Context) error {
	m := r.m
	r.say("asking whether %s answers `%s` (%s)", r.old, strings.Join(r.prefix, " "), HomeMoveAnswerWait)
	up, err := m.Machine.OldHomeAnswers(ctx, r.prefix, HomeMoveAnswerWait)
	if err != nil {
		return fmt.Errorf("asking whether %s answers: %w", r.old, err)
	}
	if up {
		return fmt.Errorf("old home is up: use --planned (%s answered `%s`)", r.old, strings.Join(r.prefix, " "))
	}
	if !m.OldHomeDead {
		return fmt.Errorf("old home %s did not answer `%s` within %s. If it is dead, say so: mw home move %s --old-home-dead",
			r.old, strings.Join(r.prefix, " "), HomeMoveAnswerWait, m.Target)
	}
	r.say("%s did not answer; going on, as --old-home-dead says it is dead.", r.old)
	return nil
}

func (r *homeMoveRun) beads(ctx context.Context) error {
	m := r.m
	backupAt, err := m.Machine.BackupTime(ctx)
	if err != nil {
		return fmt.Errorf("reading when GitHub's refs/dolt/data was written: %w", err)
	}
	r.backupAt = backupAt
	r.say("GitHub's refs/dolt/data was written %s (%s ago).", backupAt.UTC().Format(time.RFC3339), homeMoveAgo(r.at.Sub(backupAt)))

	if m.Lock != nil {
		release, err := m.Lock.Take(ctx)
		if err != nil {
			return fmt.Errorf("taking this host's sync lock: %w", err)
		}
		defer release()
	}
	// Whether this host serves beads decides the order: bootstrap makes no
	// .beads/dolt of its own, it clones into the server bd is told, so a host with
	// the unit starts it on a fresh data dir first.
	installed, err := m.Machine.UnitInstalled(ctx, DoltBeadsUnit)
	if err != nil {
		return fmt.Errorf("asking whether the %s unit is installed: %w", DoltBeadsUnit, err)
	}
	// A running server holds .beads/dolt open: stop it first, and start it in the way back.
	stopped, err := m.Machine.StopUnit(ctx, DoltBeadsUnit)
	if err != nil {
		return fmt.Errorf("stopping the %s unit before its directory is set aside: %w", DoltBeadsUnit, err)
	}
	if stopped {
		r.say("stopped the %s user unit here: it holds .beads/dolt open.", DoltBeadsUnit)
		r.undo("systemctl --user start " + DoltBeadsUnit + " (it was running before the move; start it once the directories above are back)")
	}
	stamp := r.at.Format("20060102T150405Z")
	aside, err := m.Machine.SetBeadsAside(ctx, stamp)
	if err != nil {
		return fmt.Errorf("setting the embedded database aside: %w", err)
	}
	r.setAside(aside, "embedded database")
	dolt, err := m.Machine.SetDoltAside(ctx, stamp)
	if err != nil {
		return fmt.Errorf("setting the .beads/dolt directory aside: %w", err)
	}
	r.setAside(dolt, ".beads/dolt directory")
	if installed {
		if err := r.serveFresh(ctx, dolt.From); err != nil {
			return err
		}
	}
	if err := m.Machine.BootstrapBeads(ctx); err != nil {
		if installed {
			return fmt.Errorf("bootstrapping the beads database into the %s server: %w. The server runs on a fresh data directory, the old ones are set aside. The ways back are below", DoltBeadsUnit, err)
		}
		return fmt.Errorf("bootstrapping the beads database: %w", err)
	}
	r.say("bd bootstrap --yes cloned refs/dolt/data.")
	if err := m.Machine.RestoreBeadsConfig(ctx); err != nil {
		return fmt.Errorf("restoring .beads/config.yaml: %w", err)
	}
	if installed {
		return r.beadsAnswer(ctx, "bd, with the "+DoltBeadsUnit+" unit up,")
	}
	r.say("no %s unit here: staying embedded; beads_sync = auto reads home as backup mode.", DoltBeadsUnit)
	return r.beadsAnswer(ctx, "bd")
}

// serveFresh starts the dolt-beads unit on an empty data directory and waits for it
// to answer, so that bootstrap has a server to clone into. dolt is where the
// .beads/dolt that was set aside stood, "" if there was none.
func (r *homeMoveRun) serveFresh(ctx context.Context, dolt string) error {
	m := r.m
	dir, err := m.Machine.UnitWorkingDirectory(ctx, DoltBeadsUnit)
	if err != nil {
		return fmt.Errorf("reading the %s unit's WorkingDirectory: %w", DoltBeadsUnit, err)
	}
	if err := m.Machine.EmptyDataDir(ctx, dir); err != nil {
		return fmt.Errorf("making an empty data directory %s for the %s unit: %w", dir, DoltBeadsUnit, err)
	}
	r.say("%s serves %s: a fresh, empty data directory.", DoltBeadsUnit, dir)
	if dir != dolt {
		r.undo(fmt.Sprintf("rmdir %s (the empty data directory the move made for %s; only if it is still empty)", dir, DoltBeadsUnit))
	}
	// Noted before the start: a unit that fails to start is left failed, and stopped in the way back.
	r.undo("systemctl --user stop " + DoltBeadsUnit)
	if _, err := m.Machine.StartUnit(ctx, DoltBeadsUnit); err != nil {
		return fmt.Errorf("starting the %s unit on the fresh data directory %s: %w. %s", DoltBeadsUnit, dir, err, r.asideText())
	}
	r.say("started the %s user unit.", DoltBeadsUnit)
	if err := m.Machine.BeadsServerAnswers(ctx, HomeMoveBeadsServerWait); err != nil {
		return fmt.Errorf("waiting for the %s server to answer: %w", DoltBeadsUnit, err)
	}
	r.say("the %s server answers.", DoltBeadsUnit)
	user, created, err := m.Machine.CreateBeadsUser(ctx)
	if err != nil {
		return fmt.Errorf("making the beads user %q on the fresh %s server, which knows only root: %w. bd cannot log in without it, so the move stops before bootstrap. %s; the ways back are below",
			user, DoltBeadsUnit, err, r.asideText())
	}
	switch {
	case created:
		r.say("made the beads user %q on the %s server, with all privileges.", user, DoltBeadsUnit)
	case user == "":
		r.say("BEADS_DOLT_SERVER_USER is not set: no user is made on the fresh server.")
	default:
		r.say("BEADS_DOLT_SERVER_USER is %s: the fresh server has that user already.", user)
	}
	return nil
}

// setAside says what was set aside and notes the way back of it; the zero move is
// nothing to set aside.
func (r *homeMoveRun) setAside(aside AsideMove, what string) {
	if aside.From == "" {
		r.say("no %s here to set aside.", what)
		return
	}
	r.asides = append(r.asides, aside)
	r.say("set %s aside as %s (never deleted).", aside.From, aside.To)
	r.undo(fmt.Sprintf("mv %s %s.from-github (only if bootstrap got as far as making it)\nmv %s %s", aside.From, aside.From, aside.To, aside.From))
}

// beadsAnswer is bd counting the beads: none is not a database to go on with, and
// neither is one whose count is not the old home's, which a stale .beads/dolt, served
// as it stood, would be. A dead old home has no count to compare, and the output says so.
func (r *homeMoveRun) beadsAnswer(ctx context.Context, who string) error {
	count, err := r.m.Machine.BeadsCount(ctx)
	if err != nil {
		return fmt.Errorf("asking bd how many beads there are: %w", err)
	}
	if count == 0 {
		return fmt.Errorf("%s holds no beads: the database is not one to make the home of", who)
	}
	r.say("%s answers with %d beads.", who, count)
	switch {
	case !r.haveOldCount:
		if r.saidNoOldCount {
			return nil
		}
		r.saidNoOldCount = true
		r.say("no old count to compare: the old home is dead, so what it held is not known, and %d is not checked against it.", count)
	case count != r.oldCount:
		return fmt.Errorf("%s answers with %d beads, but the old home held %d after its final flush: what is served here is not the old home's database (a stale .beads/dolt, or a clone that is not level). %s. Put them back as the ways back below say, or look inside them, before running the move again",
			who, count, r.oldCount, r.asideText())
	default:
		r.say("the old home counted %d too: the count agrees.", r.oldCount)
	}
	return nil
}

// asideText says where what was set aside is now.
func (r *homeMoveRun) asideText() string {
	if len(r.asides) == 0 {
		return "Nothing was set aside on this host"
	}
	var told []string
	for _, a := range r.asides {
		told = append(told, fmt.Sprintf("%s is at %s", a.From, a.To))
	}
	return "Set aside, never deleted: " + strings.Join(told, "; ")
}

func (r *homeMoveRun) vault(ctx context.Context) error {
	m := r.m
	pulled, err := m.Vault.Pull(ctx)
	if err != nil {
		return fmt.Errorf("bringing the vault level (git pull --rebase): %w", err)
	}
	r.say("the vault is level (%d commits came in).", pulled)

	record := domain.HomeRecord{Host: m.Target, At: r.at, By: m.Actor}
	if err := m.Writer.WriteHome(ctx, record); err != nil {
		return fmt.Errorf("writing the home file: %w", err)
	}
	if r.hadFile {
		r.undo(fmt.Sprintf("git -C %s checkout -- %s", m.VaultDir, HomeFileName))
	} else {
		r.undo(fmt.Sprintf("rm %s/%s", m.VaultDir, HomeFileName))
	}
	r.say("wrote the home file: %s", strings.TrimSpace(record.String()))

	message := fmt.Sprintf("home: %s (moved from %s: the old home is dead)", m.Target, r.old)
	if m.Planned {
		message = fmt.Sprintf("home: %s (planned move from %s)", m.Target, r.old)
	}
	committed, err := m.Vault.Commit(ctx, message, []string{HomeFileName})
	if err != nil {
		return fmt.Errorf("committing the home file: %w", err)
	}
	if len(committed) == 0 {
		return errors.New("committing the home file: git found nothing to commit")
	}
	sha, err := m.Vault.Head(ctx)
	if err != nil {
		sha = "HEAD"
	}
	r.backs = []string{fmt.Sprintf("git -C %s revert --no-edit %s && git -C %s push", m.VaultDir, sha, m.VaultDir)}
	r.say("committed it as %s.", sha)

	if _, err := m.Vault.Push(ctx); err != nil {
		return fmt.Errorf("pushing the vault: %w. The commit is only here: GitHub does not have it, so the old home has no fence yet", err)
	}
	r.say("pushed. The fence is up: an old home that comes back reads the home file, sees it is not home and stays quiet.")
	return nil
}

func (r *homeMoveRun) backend(ctx context.Context) error {
	m := r.m
	installed, err := m.Machine.UnitInstalled(ctx, PosternBackendUnit)
	if err != nil {
		return fmt.Errorf("asking whether the %s unit is installed: %w", PosternBackendUnit, err)
	}
	if !installed {
		return fmt.Errorf("this host has no %s user unit: place it with its env file, issuer key off, and never make it with mw postern serve, which writes POSTERN_ISSUER_KEY back", PosternBackendUnit)
	}
	started, err := m.Machine.StartUnit(ctx, PosternBackendUnit)
	if err != nil {
		return fmt.Errorf("starting the %s unit: %w", PosternBackendUnit, err)
	}
	if started {
		r.say("started the %s user unit.", PosternBackendUnit)
		r.undo("systemctl --user stop " + PosternBackendUnit)
	} else {
		r.say("the %s user unit was already running (in standby): it leaves standby by itself, within 30 s.", PosternBackendUnit)
		r.nothing = "the backend was already running; it goes back to standby by itself once step 3 is undone."
	}
	r.say("never mw postern serve here: it writes POSTERN_ISSUER_KEY back into the env file and the Mayor loses postern.")
	if err := m.Machine.BackendServing(ctx, m.BackendURL, HomeMoveBackendWait); err != nil {
		return fmt.Errorf("the backend did not answer %s/healthz as home within %s: %w", m.BackendURL, HomeMoveBackendWait, err)
	}
	r.say("the backend answers as home at %s.", m.BackendURL)
	return nil
}

func (r *homeMoveRun) mayor(ctx context.Context) error {
	m := r.m
	body := fmt.Sprintf("Home moved to %s at %s: the old home is dead; beads from GitHub as of %s",
		m.Target, r.at.Format(time.RFC3339), r.backupAt.UTC().Format(time.RFC3339))
	if m.Planned {
		body = fmt.Sprintf("Home moved to %s at %s: planned move; %s handed off and flushed first, beads from its final push (GitHub's refs/dolt/data as of %s)",
			m.Target, r.at.Format(time.RFC3339), r.old, r.backupAt.UTC().Format(time.RFC3339))
	}
	id, err := m.Mailbox.Send(ctx, NewMessage{From: m.Actor, To: "mayor", Subject: "Home moved to " + m.Target, Body: body})
	if err != nil {
		return fmt.Errorf("sending the Mayor mail: %w", err)
	}
	r.say("sent the Mayor mail %s.", id)
	r.undo(fmt.Sprintf("the message to the Mayor (%s) is already sent and stays; if the move is undone, send the Mayor a note.", id))

	started, said, err := m.Machine.MayorUp(ctx)
	if err != nil {
		return fmt.Errorf("bin/mayor-up: %w", err)
	}
	if !started {
		r.say("bin/mayor-up: a Mayor already sits here (%s).", said)
		return nil
	}
	r.say("bin/mayor-up started a Mayor in window %s.", said)
	r.undo(fmt.Sprintf("tmux kill-window -t '%s'", said))
	return nil
}

func (r *homeMoveRun) lost(ctx context.Context) error {
	m := r.m
	if m.Planned {
		r.say("planned move: %s flushed before it stood down, so nothing is lost. GitHub's beads were written %s (%s ago), the old home's final push.",
			r.old, r.backupAt.UTC().Format(time.RFC3339), homeMoveAgo(r.at.Sub(r.backupAt)))
	} else {
		r.say("GitHub's copy of the beads was written %s, %s ago: whatever %s wrote to beads after that is not here.",
			r.backupAt.UTC().Format(time.RFC3339), homeMoveAgo(r.at.Sub(r.backupAt)), r.old)
	}
	at, ok, err := m.Index.LastIndexTime(ctx, nil, m.DataDir)
	switch {
	case err != nil:
		r.say("the age of the Postern data here is not known: %v", err)
	case !ok:
		r.say("this host has no postern index in %s: no message from the old home is here.", m.DataDir)
	case m.Planned:
		r.say("the Postern data here ends at %s, %s ago, as of the old home's final mirror.", at.UTC().Format(time.RFC3339), homeMoveAgo(r.at.Sub(at)))
	default:
		r.say("the Postern data here ends at %s, %s ago: whatever %s got after that is not here (it copies here every ten minutes).",
			at.UTC().Format(time.RFC3339), homeMoveAgo(r.at.Sub(at)), r.old)
	}
	r.say("%s keeps its own copies on its disk. When it comes back it stays quiet, and nothing merges them: see docs/home-move.md.", r.old)
	return nil
}

// homeMoveAgo is an age in words, minutes rounded down.
func homeMoveAgo(age time.Duration) string {
	minutes := int(age / time.Minute)
	switch {
	case minutes < 1:
		return "under a minute"
	case minutes == 1:
		return "1 minute"
	case minutes < 120:
		return fmt.Sprintf("%d minutes", minutes)
	case minutes < 48*60:
		return fmt.Sprintf("%d hours %d minutes", minutes/60, minutes%60)
	default:
		return fmt.Sprintf("%d days %d hours", minutes/(24*60), minutes/60%24)
	}
}
