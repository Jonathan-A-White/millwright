package doctor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/claude"
)

var _ application.DoctorCheck = (*ClaudeLogin)(nil)

// ClaudeLoginName is what the check is called: in the log, and on the command
// line as `mw doctor claude-login`.
const ClaudeLoginName = "claude-login"

// ClaudeTokenWarnDays is the age of the token in ~/.config/mw/claude.env at
// which the check starts to warn: the token lasts a year, so this is thirty
// days before it runs out.
const ClaudeTokenWarnDays = 335

// The claude-login check's damper: the alarm is sent again a day on, and for
// thirty days, the span between the first warning and the token's end.
const (
	ClaudeLoginDamperWait = 24 * time.Hour
	ClaudeLoginDamperCap  = 30
)

// claudeLoginFix is the cure the alarm names, a person's to do (docs/secrets.md).
const claudeLoginFix = "Run `claude setup-token`, then write the token with " +
	"`printf 'CLAUDE_CODE_OAUTH_TOKEN=%s\\n' \"$token\" > ~/.config/mw/claude.env` (umask 077; docs/secrets.md)"

// notLoggedInScreen is the words a Claude Code pane shows, with /login, when
// its login is gone.
const notLoggedInScreen = "Not logged in"

// ClaudeLogin is the check that warns before the factory's Claude login dies
// (mw-gq6.830). It is faulty when the token in the claude env file is
// ClaudeTokenWarnDays old or more, judged by the file's mtime (the day it was
// last written, which is the day the token was made); when `claude auth status`
// says the login is gone; or when a pane shows "Not logged in". A host with no
// env file has no token to age and a host with no claude program has nothing to
// judge: both read ok. Its cure is the alarm to the Governor; the new token is
// a person's to make.
type ClaudeLogin struct {
	// EnvFile is the host's claude env file: ~/.config/mw/claude.env.
	EnvFile string
	// Now is the clock; nil reads time.Now.
	Now func() time.Time
	// AuthStatus says whether claude is logged in. An error wrapping
	// exec.ErrNotFound means this host has no claude program. Nil skips the
	// question.
	AuthStatus func(ctx context.Context) (bool, error)
	// Screens names the windows whose pane shows "Not logged in". Nil skips it.
	Screens func(ctx context.Context) ([]string, error)
	// Alarm sends the Governor the text and gives the seq of the emergency
	// event it was written as, 0 for none. A nil Alarm cannot cure.
	Alarm func(ctx context.Context, text string) (uint64, error)
}

// NewClaudeLogin is the check over the env file at envFile, asking the real
// `claude auth status` with that file's token in its environment.
func NewClaudeLogin(envFile string) *ClaudeLogin {
	return &ClaudeLogin{EnvFile: envFile, AuthStatus: ClaudeAuthStatus("claude", envFile)}
}

// Name implements application.DoctorCheck.
func (c *ClaudeLogin) Name() string { return ClaudeLoginName }

func (c *ClaudeLogin) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

// problems is every reason to alarm, in words that end in the fix. cannotTell
// is why something could not be read, when nothing alarming was found.
func (c *ClaudeLogin) problems(ctx context.Context) (found []string, cannotTell error) {
	if c.EnvFile != "" {
		if info, err := os.Stat(c.EnvFile); err == nil {
			if age := int(c.now().Sub(info.ModTime()) / (24 * time.Hour)); age >= ClaudeTokenWarnDays {
				found = append(found, fmt.Sprintf("the Claude token in %s is %d days old and lasts a year", c.EnvFile, age))
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			cannotTell = err
		}
	}
	if c.AuthStatus != nil {
		switch in, err := c.AuthStatus(ctx); {
		case errors.Is(err, exec.ErrNotFound):
		case err != nil:
			cannotTell = err
		case !in:
			found = append(found, "`claude auth status` says this host is not logged in")
		}
	}
	if c.Screens != nil {
		windows, err := c.Screens(ctx)
		if err != nil {
			cannotTell = err
		} else if len(windows) > 0 {
			found = append(found, fmt.Sprintf("%s shows %q", strings.Join(windows, ", "), notLoggedInScreen))
		}
	}
	return found, cannotTell
}

// Probe implements application.DoctorCheck.
func (c *ClaudeLogin) Probe(ctx context.Context) (application.Verdict, string) {
	found, cannotTell := c.problems(ctx)
	if len(found) > 0 {
		return application.DoctorFaulty, alarmClaudeLogin(found)
	}
	if cannotTell != nil {
		return application.DoctorCannotTell, cannotTell.Error()
	}
	return application.DoctorOK, ""
}

func alarmClaudeLogin(found []string) string {
	return "Claude login warning: " + strings.Join(found, "; ") + ". " + claudeLoginFix
}

// Cure implements application.DoctorCheck: send the alarm.
func (c *ClaudeLogin) Cure(ctx context.Context) error {
	found, _ := c.problems(ctx)
	if len(found) == 0 {
		return nil
	}
	if c.Alarm == nil {
		return fmt.Errorf("there is no way to send the Claude login alarm")
	}
	if _, err := c.Alarm(ctx, alarmClaudeLogin(found)); err != nil {
		return fmt.Errorf("the Claude login alarm could not be sent: %w", err)
	}
	return nil
}

// Damper implements application.DoctorCheck.
func (c *ClaudeLogin) Damper() (time.Duration, int) {
	return ClaudeLoginDamperWait, ClaudeLoginDamperCap
}

// WayBack implements application.DoctorCheck.
func (c *ClaudeLogin) WayBack() string {
	return "none needed: the alarm only tells the Governor; make a new token as docs/secrets.md says"
}

// ClaudeAuthStatus is the question `<program> auth status` answers, with the
// env file's assignments (the token) on top of this process's environment, so
// the answer is the factory's login and not an interactive one. The program
// says its state as JSON with a loggedIn member, and leaves non-zero when it
// is not logged in; the JSON decides. A program that is not there gives an
// error wrapping exec.ErrNotFound.
func ClaudeAuthStatus(program, envFile string) func(ctx context.Context) (bool, error) {
	return func(ctx context.Context) (bool, error) {
		path, err := exec.LookPath(program)
		if err != nil {
			return false, err
		}
		ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		var out bytes.Buffer
		cmd := exec.CommandContext(ctx, path, "auth", "status")
		cmd.Env = append(os.Environ(), claude.ClaudeEnv(envFile)...)
		cmd.Stdout = &out
		runErr := cmd.Run()
		var status struct {
			LoggedIn *bool `json:"loggedIn"`
		}
		if err := json.Unmarshal(out.Bytes(), &status); err != nil || status.LoggedIn == nil {
			if runErr != nil {
				return false, fmt.Errorf("`claude auth status` failed: %w", runErr)
			}
			return false, fmt.Errorf("`claude auth status` gave no loggedIn answer")
		}
		return *status.LoggedIn, nil
	}
}
