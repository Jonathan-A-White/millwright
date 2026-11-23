package doctor

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

var _ application.DoctorCheck = (*VaultGitignore)(nil)

// VaultGitignoreName is what the check is called: in the log, and on the
// command line as `mw doctor vault-gitignore`.
const VaultGitignoreName = "vault-gitignore"

// The vault-gitignore check's damper: a cure appends lines and commits them, so
// a second one in the same episode means something keeps taking them out.
const (
	VaultGitignoreDamperWait = 5 * time.Minute
	VaultGitignoreDamperCap  = 3
)

// VaultGitignore is the check that the vault's .gitignore holds every line of
// the template's. mw init copies template/.gitignore into a vault once, so a
// line the template gains later (mw-gq6.1683's heartbeat log, mw-gq6.1688's
// check files) never reaches a vault that exists, and what the factory writes
// there shows as untracked for good (mw-gq6.1689). A line counts as held when
// the vault has it, spaces at either end aside; comments and blank lines are
// not lines. Its cure appends the lines the vault lacks, after everything the
// vault already holds, and commits that one file; it never pushes (the next
// sync does) and never takes a line out.
type VaultGitignore struct {
	// Dir is the vault's directory.
	Dir string
	// Host names this host, written into the cure's commit message.
	Host string
	// Template holds the .gitignore a fresh vault is born with, at its root.
	Template fs.FS
	// Program is the program run for git. Empty reads "git".
	Program string

	// commit is what the last Cure committed, read by WayBack.
	commit string
}

// NewVaultGitignore is the check over the vault at dir, its cure attributed to
// host, against the .gitignore in template.
func NewVaultGitignore(dir, host string, template fs.FS) *VaultGitignore {
	return &VaultGitignore{Dir: dir, Host: host, Template: template}
}

// Name implements application.DoctorCheck.
func (v *VaultGitignore) Name() string { return VaultGitignoreName }

// Probe implements application.DoctorCheck: ok when the vault's .gitignore
// holds every line of the template's, faulty naming the lines it lacks (all of
// them when there is no .gitignore), cannot-tell when either cannot be read.
func (v *VaultGitignore) Probe(context.Context) (application.Verdict, string) {
	lacking, _, err := v.lacking()
	if err != nil {
		return application.DoctorCannotTell, err.Error()
	}
	if len(lacking) == 0 {
		return application.DoctorOK, ""
	}
	return application.DoctorFaulty, fmt.Sprintf("the vault's .gitignore lacks %d line(s) of the template's: %s",
		len(lacking), strings.Join(lacking, ", "))
}

// lacking is the template's lines the vault's .gitignore does not hold, in the
// template's order, and the vault's .gitignore text as it is (empty when there
// is none).
func (v *VaultGitignore) lacking() (lines []string, have string, err error) {
	want, err := fs.ReadFile(v.Template, application.VaultIgnoreFile)
	if err != nil {
		return nil, "", fmt.Errorf("reading the template's %s: %w", application.VaultIgnoreFile, err)
	}
	got, err := os.ReadFile(filepath.Join(v.Dir, application.VaultIgnoreFile))
	if err != nil && !os.IsNotExist(err) {
		return nil, "", fmt.Errorf("reading the vault's %s: %w", application.VaultIgnoreFile, err)
	}
	held := map[string]bool{}
	for _, line := range strings.Split(string(got), "\n") {
		held[strings.TrimSpace(line)] = true
	}
	for _, line := range ignoreLines(string(want)) {
		if !held[line] {
			lines = append(lines, line)
			held[line] = true
		}
	}
	return lines, string(got), nil
}

// ignoreLines is the lines of a .gitignore that name something: not blank, not
// a comment.
func ignoreLines(text string) []string {
	var lines []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		lines = append(lines, line)
	}
	return lines
}

// Cure implements application.DoctorCheck: appends the lacking lines to the
// vault's .gitignore, under a comment saying where they came from, and commits
// the file alone when git tracks it.
func (v *VaultGitignore) Cure(ctx context.Context) error {
	lacking, have, err := v.lacking()
	if err != nil {
		return err
	}
	if len(lacking) == 0 {
		return nil
	}
	var text strings.Builder
	text.WriteString(have)
	if have != "" && !strings.HasSuffix(have, "\n") {
		text.WriteString("\n")
	}
	if have != "" {
		text.WriteString("\n")
	}
	text.WriteString("# Lines template/.gitignore gained after this vault was made (mw doctor, " + v.Host + ")\n")
	for _, line := range lacking {
		text.WriteString(line + "\n")
	}
	path := filepath.Join(v.Dir, application.VaultIgnoreFile)
	if err := os.WriteFile(path, []byte(text.String()), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	if _, err := v.git(ctx, "ls-files", "--error-unmatch", application.VaultIgnoreFile); err != nil {
		return nil // not tracked: nothing to commit
	}
	message := fmt.Sprintf("doctor: %s gains %d line(s) of the template's (host %s)", application.VaultIgnoreFile, len(lacking), v.Host)
	if _, err := v.git(ctx, "commit", "-q", "-m", message, "--", application.VaultIgnoreFile); err != nil {
		return fmt.Errorf("committing %s: %w", application.VaultIgnoreFile, err)
	}
	hash, err := v.git(ctx, "rev-parse", "HEAD")
	if err != nil {
		return fmt.Errorf("reading the commit made for %s: %w", application.VaultIgnoreFile, err)
	}
	v.commit = strings.TrimSpace(hash)
	return nil
}

// Damper implements application.DoctorCheck.
func (v *VaultGitignore) Damper() (time.Duration, int) {
	return VaultGitignoreDamperWait, VaultGitignoreDamperCap
}

// WayBack implements application.DoctorCheck.
func (v *VaultGitignore) WayBack() string {
	if v.commit == "" {
		return "none committed yet: nothing to revert"
	}
	return fmt.Sprintf("git -C %s revert %s", v.Dir, v.commit)
}

// git runs one git command against the vault and returns its standard output.
func (v *VaultGitignore) git(ctx context.Context, args ...string) (string, error) {
	program := v.Program
	if program == "" {
		program = "git"
	}
	full := append([]string{"-C", v.Dir}, args...)
	cmd := exec.CommandContext(ctx, program, full...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	var out, errs bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errs
	if err := cmd.Run(); err != nil {
		said := strings.TrimSpace(errs.String() + "\n" + out.String())
		return "", fmt.Errorf("%s %s: %w: %s", program, strings.Join(full, " "), err, said)
	}
	return out.String(), nil
}
