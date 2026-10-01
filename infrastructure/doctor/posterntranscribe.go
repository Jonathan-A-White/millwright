package doctor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

var _ application.DoctorCheck = (*PosternTranscribe)(nil)

// PosternTranscribeName is what the check is called: in the log, and on the
// command line as `mw doctor postern-transcribe`.
const PosternTranscribeName = "postern-transcribe"

// The postern-transcribe check's damper: there is no cure to retry, so one
// failed cure attempt is all an episode ever spends, as with beads-size.
const (
	PosternTranscribeDamperWait = 0 * time.Second
	PosternTranscribeDamperCap  = 1
)

// errPosternTranscribeNoCure is what Cure always returns: installing ffmpeg,
// whisper.cpp and a model is host work for a person, never something mw does
// unasked.
var errPosternTranscribeNoCure = errors.New(
	"no cure: a person sets postern_transcribe_cmd in ~/.config/mw/config.toml and installs " +
		"what contrib/postern-transcribe needs (ffmpeg, whisper-cli, a ggml model)")

// contribTranscribe is the base name of the script the check knows the needs
// of: contrib/postern-transcribe.
const contribTranscribe = "postern-transcribe"

// PosternTranscribe is the check that the home host can hear a voice note:
// mw postern inbox, run there, transcribes the Governor's voice notes with
// postern_transcribe_cmd, and with none set — or one that cannot run — a
// voice note arrives with no words and, before this check, nothing said so.
// Off the home nothing is wrong: no voice note is heard there. It never
// cures.
type PosternTranscribe struct {
	// Command is what config.PosternTranscribeCmd reads: the command, split
	// on whitespace, mw appends the audio's path to. Empty is unset.
	Command string
	// CommandErr is the error reading that setting gave, if any.
	CommandErr error
	// Home and Host are the vault's home file and this host's name. A host
	// the file says is not home is never faulty. A nil Home, or a home that
	// cannot be told, judges as on the home.
	Home application.HomeFile
	Host string
	// PathEnv is the PATH searched for a bare program name; empty reads $PATH.
	PathEnv string
	// ServicePath is the PATH the hook runs under in postern-backend.service,
	// a systemd user service's, with no ~/.local/bin; empty reads
	// DefaultServicePath. ffmpeg and whisper-cli must be found there (whisper-cli
	// also at ~/.local/bin, where the script looks), not only on PathEnv.
	ServicePath string
	// Getenv reads POSTERN_WHISPER_CLI and POSTERN_WHISPER_MODEL; nil reads
	// the process environment.
	Getenv func(string) string
	// HomeDir is where the default model lives under; empty reads the user's
	// home directory.
	HomeDir string
}

// DefaultServicePath is the PATH a systemd user service starts with.
const DefaultServicePath = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:/usr/games:/usr/local/games:/snap/bin"

// NewPosternTranscribe is the check over command, as this host's config
// reads it.
func NewPosternTranscribe(command string, commandErr error) *PosternTranscribe {
	return &PosternTranscribe{Command: command, CommandErr: commandErr}
}

// Name implements application.DoctorCheck.
func (p *PosternTranscribe) Name() string { return PosternTranscribeName }

// Probe implements application.DoctorCheck: ok off the home; on it, faulty
// naming postern_transcribe_cmd when it is unset, its program when that is
// not an executable, and — when the program is contrib/postern-transcribe —
// whichever of ffmpeg, the whisper CLI or the model file it lacks.
func (p *PosternTranscribe) Probe(ctx context.Context) (application.Verdict, string) {
	if p.Home != nil {
		if home, err := application.IsHome(ctx, p.Home, p.Host); err == nil && !home {
			return application.DoctorOK, ""
		}
	}
	if p.CommandErr != nil {
		return application.DoctorCannotTell, fmt.Sprintf("reading postern_transcribe_cmd: %v", p.CommandErr)
	}
	words := strings.Fields(p.Command)
	if len(words) == 0 {
		return application.DoctorFaulty, "postern_transcribe_cmd is not set: no voice note is transcribed on this host"
	}
	program := words[0]
	if !p.executable(program) {
		return application.DoctorFaulty, fmt.Sprintf("postern_transcribe_cmd's program %s is not an executable", program)
	}
	if filepath.Base(program) != contribTranscribe {
		return application.DoctorOK, ""
	}
	var missing []string
	if !p.executable("ffmpeg") {
		missing = append(missing, "ffmpeg is not on PATH")
	} else if !p.onServicePath("ffmpeg") {
		missing = append(missing, "ffmpeg is on your login PATH but not on the service PATH ("+p.servicePath()+")")
	}
	if cli := p.getenv("POSTERN_WHISPER_CLI"); cli != "" {
		if !p.executable(cli) {
			missing = append(missing, cli+" is not on PATH")
		}
	} else if !isExecutable(filepath.Join(p.homeDir(), ".local", "bin", "whisper-cli")) && !p.onServicePath("whisper-cli") {
		if p.executable("whisper-cli") {
			missing = append(missing, "whisper-cli is on your login PATH but not on the service PATH ("+p.servicePath()+") nor at "+filepath.Join(p.homeDir(), ".local", "bin", "whisper-cli"))
		} else {
			missing = append(missing, "whisper-cli is not on PATH")
		}
	}
	if model := p.model(); !readable(model) {
		missing = append(missing, "no whisper model at "+model)
	}
	if len(missing) > 0 {
		return application.DoctorFaulty, fmt.Sprintf("postern_transcribe_cmd runs %s, which needs more: %s", program, strings.Join(missing, "; "))
	}
	return application.DoctorOK, ""
}

// Cure implements application.DoctorCheck: there is none.
func (p *PosternTranscribe) Cure(context.Context) error { return errPosternTranscribeNoCure }

// Damper implements application.DoctorCheck.
func (p *PosternTranscribe) Damper() (time.Duration, int) {
	return PosternTranscribeDamperWait, PosternTranscribeDamperCap
}

// WayBack implements application.DoctorCheck: nothing ever changes.
func (p *PosternTranscribe) WayBack() string {
	return "none: no cure runs; a person sets up voice transcription on this host by hand"
}

func (p *PosternTranscribe) getenv(key string) string {
	if p.Getenv != nil {
		return p.Getenv(key)
	}
	return os.Getenv(key)
}

// model is the ggml model file contrib/postern-transcribe reads.
func (p *PosternTranscribe) model() string {
	if model := p.getenv("POSTERN_WHISPER_MODEL"); model != "" {
		return model
	}
	return filepath.Join(p.homeDir(), ".local", "share", "whisper", "ggml-base.en.bin")
}

func (p *PosternTranscribe) homeDir() string {
	if p.HomeDir != "" {
		return p.HomeDir
	}
	home, _ := os.UserHomeDir()
	return home
}

func (p *PosternTranscribe) servicePath() string {
	if p.ServicePath != "" {
		return p.ServicePath
	}
	return DefaultServicePath
}

// onServicePath reports whether the bare name program is found on the
// service PATH.
func (p *PosternTranscribe) onServicePath(program string) bool {
	return lookPath(p.servicePath(), program)
}

// executable reports whether program — a path, or a bare name looked up on
// PATH — is a regular file with an execute bit.
func (p *PosternTranscribe) executable(program string) bool {
	if strings.Contains(program, "/") {
		return isExecutable(program)
	}
	path := p.PathEnv
	if path == "" {
		path = os.Getenv("PATH")
	}
	return lookPath(path, program)
}

func lookPath(path, program string) bool {
	for _, dir := range filepath.SplitList(path) {
		if dir != "" && isExecutable(filepath.Join(dir, program)) {
			return true
		}
	}
	return false
}

func isExecutable(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular() && info.Mode()&0o111 != 0
}

func readable(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	_ = f.Close()
	return true
}
