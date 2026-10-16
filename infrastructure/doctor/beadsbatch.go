package doctor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

var _ application.DoctorCheck = (*BeadsBatch)(nil)

// BeadsBatchName is what the check is called: in the log, and on the command
// line as `mw doctor beads-batch`.
const BeadsBatchName = "beads-batch"

// The beads-batch check's damper: there is no cure to retry (setting bd's mode
// is the Mayor's step), so the second faulty run finds the episode capped and
// writes the note that wakes the Millhand.
const (
	BeadsBatchDamperWait = 0 * time.Second
	BeadsBatchDamperCap  = 1
)

// beadsAutoCommitKey is bd's setting, as `bd config set` writes it into
// .beads/config.yaml.
const beadsAutoCommitKey = "dolt.auto-commit"

// VaultAutoCommit is what the vault's .beads/config.yaml says bd's
// dolt.auto-commit is: "on" (bd's default) when the file or the key is not
// there. It reads the flat key `bd config set` writes and the same key
// nested under dolt:.
func VaultAutoCommit(dir string) (string, error) {
	text, err := os.ReadFile(filepath.Join(dir, beadsSizeDir, "config.yaml"))
	if os.IsNotExist(err) {
		return application.AutoCommitOn, nil
	}
	if err != nil {
		return "", fmt.Errorf("reading the vault's .beads/config.yaml: %w", err)
	}
	inDolt := false
	for _, raw := range strings.Split(string(text), "\n") {
		if hash := strings.Index(raw, "#"); hash >= 0 {
			raw = raw[:hash]
		}
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		indented := raw[0] == ' ' || raw[0] == '\t'
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key, value = strings.TrimSpace(key), strings.ToLower(strings.Trim(strings.TrimSpace(value), `"'`))
		switch {
		case !indented && key == beadsAutoCommitKey && value != "":
			return value, nil
		case !indented:
			inDolt = key == "dolt" && value == ""
		case inDolt && key == "auto-commit" && value != "":
			return value, nil
		}
	}
	return application.AutoCommitOn, nil
}

// BeadsBatch is the check that a host's bd commits the way the vault says. In
// bd's batch mode a write waits in the working set for the home's flush, and
// one client left on `on` commits everyone's pending writes at its next write,
// which undoes the saving (mw-gq6.508). So once the vault's
// .beads/config.yaml says batch, a host whose bd answers anything else (a
// stale checkout, a BD_DOLT_AUTO_COMMIT in its environment) is faulty. While
// the vault says on, every host is ok. It never cures: setting bd's mode is
// the Mayor's step.
type BeadsBatch struct {
	// Dir is the vault's directory.
	Dir string
	// Host is what this host's bd answers for dolt.auto-commit.
	Host func(ctx context.Context) (string, error)
}

// NewBeadsBatch is the check over the vault at dir, asking host what this
// host's bd says.
func NewBeadsBatch(dir string, host func(ctx context.Context) (string, error)) *BeadsBatch {
	return &BeadsBatch{Dir: dir, Host: host}
}

// Name implements application.DoctorCheck.
func (b *BeadsBatch) Name() string { return BeadsBatchName }

// Probe implements application.DoctorCheck.
func (b *BeadsBatch) Probe(ctx context.Context) (application.Verdict, string) {
	vault, err := VaultAutoCommit(b.Dir)
	if err != nil {
		return application.DoctorCannotTell, err.Error()
	}
	if vault != application.AutoCommitBatch {
		return application.DoctorOK, ""
	}
	host, err := b.Host(ctx)
	if err != nil {
		return application.DoctorCannotTell, fmt.Sprintf("asking this host's bd for %s: %v", beadsAutoCommitKey, err)
	}
	if host != application.AutoCommitBatch {
		return application.DoctorFaulty, fmt.Sprintf(
			"the vault says %s is batch but this host's bd says %q: its writes commit everyone's pending ones; "+
				"pull the vault, and unset BD_DOLT_AUTO_COMMIT and any --dolt-auto-commit in what runs bd here",
			beadsAutoCommitKey, host)
	}
	return application.DoctorOK, ""
}

// Cure implements application.DoctorCheck: there is none.
func (b *BeadsBatch) Cure(context.Context) error {
	return fmt.Errorf("no cure: changed nothing; a person pulls the vault and clears BD_DOLT_AUTO_COMMIT here, " +
		"or turns batch off for every host with `bd config set dolt.auto-commit on` in the vault")
}

// Damper implements application.DoctorCheck.
func (b *BeadsBatch) Damper() (time.Duration, int) { return BeadsBatchDamperWait, BeadsBatchDamperCap }

// WayBack implements application.DoctorCheck: nothing ever changes.
func (b *BeadsBatch) WayBack() string { return "none: no cure runs" }
