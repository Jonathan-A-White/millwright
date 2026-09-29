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

	// StartUnit starts the user unit, and reports whether it did: a unit that was
	// already running is left alone, and is (false, nil).
	StartUnit(ctx context.Context, unit string) (started bool, err error)

	// BackendServing waits up to wait for the backend at url to answer /healthz
	// as home: 200, and not in standby.
	BackendServing(ctx context.Context, url string, wait time.Duration) error

	// MayorUp runs the vault's bin/mayor-up. started is false when a Mayor
	// already sits here; said is what it printed last: the window it started.
	MayorUp(ctx context.Context) (started bool, said string, err error)
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
	return []homeMoveStep{
		{
			title: "the old home",
			plan: []string{
				fmt.Sprintf("asks whether %s answers `%s` within %s.", r.old, strings.Join(r.prefix, " "), HomeMoveAnswerWait),
				"If it does, the move stops: old home is up: use --planned when it exists.",
				"If it does not, the move goes on only with --old-home-dead, the Governor's word that it is dead.",
			},
			back: "nothing is changed.",
			run:  (*homeMoveRun).oldHome,
		},
		{
			title: "beads, from GitHub",
			plan: []string{
				"reads when GitHub's refs/dolt/data, the beads backup, was written. If GitHub cannot be read the move stops there, with nothing touched.",
				fmt.Sprintf("takes this host's sync lock, and sets %s/.beads/embeddeddolt aside in a dated directory in the home directory (beads-embeddeddolt-aside-<time>): it is never deleted.", m.VaultDir),
				"runs `bd bootstrap --yes`, then `git checkout -- .beads/config.yaml` (bootstrap drops its trailing newline), and asks bd how many beads it holds: none is a stop.",
				fmt.Sprintf("starts the %s user unit if this host has one; if not, stays embedded, and beads_sync = auto reads home as backup mode.", DoltBeadsUnit),
			},
			back: fmt.Sprintf("stop %s if it started, move the new .beads/embeddeddolt away and move the dated directory back in its place.", DoltBeadsUnit),
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
}

func (r *homeMoveRun) oldHome(ctx context.Context) error {
	m := r.m
	r.say("asking whether %s answers `%s` (%s)", r.old, strings.Join(r.prefix, " "), HomeMoveAnswerWait)
	up, err := m.Machine.OldHomeAnswers(ctx, r.prefix, HomeMoveAnswerWait)
	if err != nil {
		return fmt.Errorf("asking whether %s answers: %w", r.old, err)
	}
	if up {
		return fmt.Errorf("old home is up: use --planned when it exists (%s answered `%s`)", r.old, strings.Join(r.prefix, " "))
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
	aside, err := m.Machine.SetBeadsAside(ctx, r.at.Format("20060102T150405Z"))
	if err != nil {
		return fmt.Errorf("setting the embedded database aside: %w", err)
	}
	if aside.From == "" {
		r.say("no embedded database here to set aside.")
	} else {
		r.say("set %s aside as %s (never deleted).", aside.From, aside.To)
		r.undo(fmt.Sprintf("mv %s %s.from-github (only if bootstrap got as far as making it)\nmv %s %s", aside.From, aside.From, aside.To, aside.From))
	}
	if err := m.Machine.BootstrapBeads(ctx); err != nil {
		return fmt.Errorf("bootstrapping the beads database: %w", err)
	}
	r.say("bd bootstrap --yes cloned refs/dolt/data.")
	if err := m.Machine.RestoreBeadsConfig(ctx); err != nil {
		return fmt.Errorf("restoring .beads/config.yaml: %w", err)
	}
	if err := r.beadsAnswer(ctx, "bd"); err != nil {
		return err
	}

	installed, err := m.Machine.UnitInstalled(ctx, DoltBeadsUnit)
	if err != nil {
		return fmt.Errorf("asking whether the %s unit is installed: %w", DoltBeadsUnit, err)
	}
	if !installed {
		r.say("no %s unit here: staying embedded; beads_sync = auto reads home as backup mode.", DoltBeadsUnit)
		return nil
	}
	started, err := m.Machine.StartUnit(ctx, DoltBeadsUnit)
	if err != nil {
		return fmt.Errorf("starting the %s unit: %w", DoltBeadsUnit, err)
	}
	if started {
		r.say("started the %s user unit.", DoltBeadsUnit)
		r.undo("systemctl --user stop " + DoltBeadsUnit)
	} else {
		r.say("the %s user unit was already running.", DoltBeadsUnit)
	}
	return r.beadsAnswer(ctx, "bd, with the "+DoltBeadsUnit+" unit up,")
}

// beadsAnswer is bd counting the beads: none is not a database to go on with.
func (r *homeMoveRun) beadsAnswer(ctx context.Context, who string) error {
	count, err := r.m.Machine.BeadsCount(ctx)
	if err != nil {
		return fmt.Errorf("asking bd how many beads there are: %w", err)
	}
	if count == 0 {
		return fmt.Errorf("%s holds no beads: the database is not one to make the home of", who)
	}
	r.say("%s answers with %d beads.", who, count)
	return nil
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
	r.say("GitHub's copy of the beads was written %s, %s ago: whatever %s wrote to beads after that is not here.",
		r.backupAt.UTC().Format(time.RFC3339), homeMoveAgo(r.at.Sub(r.backupAt)), r.old)
	at, ok, err := m.Index.LastIndexTime(ctx, nil, m.DataDir)
	switch {
	case err != nil:
		r.say("the age of the Postern data here is not known: %v", err)
	case !ok:
		r.say("this host has no postern index in %s: no message from the old home is here.", m.DataDir)
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
