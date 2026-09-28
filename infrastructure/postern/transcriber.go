package postern

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

// DefaultTranscribeTimeout is how long a voice note's transcription may run
// before it is stopped: five minutes, far past the half hour of Opus voice
// the 8 MiB attachment cap allows on whisper.cpp's base model.
const DefaultTranscribeTimeout = 5 * time.Minute

var _ application.PosternTranscriber = (*CommandTranscriber)(nil)

// CommandTranscriber hears a voice note by running a command on this host —
// config postern_transcribe_cmd, split on whitespace, with the audio file's
// path appended — and reading what it prints: postern's docs/protocol.md
// section 14, never a third party. contrib/postern-transcribe is the
// command this rig ships, around ffmpeg and whisper.cpp.
type CommandTranscriber struct {
	argv    []string
	timeout time.Duration
}

// TranscribeOption is a setting of a CommandTranscriber.
type TranscribeOption func(*CommandTranscriber)

// WithTranscribeTimeout sets how long a transcription may run.
func WithTranscribeTimeout(timeout time.Duration) TranscribeOption {
	return func(t *CommandTranscriber) { t.timeout = timeout }
}

// NewCommandTranscriber is the transcriber that runs command, split on
// whitespace.
func NewCommandTranscriber(command string, opts ...TranscribeOption) *CommandTranscriber {
	t := &CommandTranscriber{argv: strings.Fields(command), timeout: DefaultTranscribeTimeout}
	for _, opt := range opts {
		opt(t)
	}
	return t
}

// Transcribe implements application.PosternTranscriber. The command runs in
// a process group of its own, so that a transcription stopped at its
// timeout takes with it the ffmpeg or whisper it started underneath.
func (t *CommandTranscriber) Transcribe(ctx context.Context, audioPath string) (string, error) {
	if len(t.argv) == 0 {
		return "", fmt.Errorf("no transcriber is configured: set postern_transcribe_cmd")
	}
	ctx, cancel := context.WithTimeout(ctx, t.timeout)
	defer cancel()
	args := append(append([]string(nil), t.argv[1:]...), audioPath)
	cmd := exec.CommandContext(ctx, t.argv[0], args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = time.Second
	var out, errs bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errs
	if err := cmd.Run(); err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return "", fmt.Errorf("%s gave up on %s after %s", t.argv[0], audioPath, t.timeout)
		}
		if said := strings.TrimSpace(errs.String()); said != "" {
			return "", fmt.Errorf("%s %s: %w: %s", t.argv[0], audioPath, err, said)
		}
		return "", fmt.Errorf("%s %s: %w", t.argv[0], audioPath, err)
	}
	return strings.TrimSpace(out.String()), nil
}
