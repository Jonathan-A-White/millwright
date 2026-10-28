package application

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
)

// The member's dispatch units. They are the holder's own, under the member's
// user manager: the timer is enabled, and it starts the service.
const (
	MemberDispatchService = "mw-dispatch.service"
	MemberDispatchTimer   = "mw-dispatch.timer"
)

// MemberDispatchTools are the programs a member's dispatch, and the Builders it
// starts, run from the PATH line of dispatch.env: mw and bd to drive the
// factory, git and tmux for the worktrees and sessions, codex for the Builder.
var MemberDispatchTools = []string{"mw", "bd", "git", "tmux", "codex"}

// memberSystemPath is what a PATH line ends with, after the tools' own directories.
const memberSystemPath = "/usr/local/bin:/usr/bin:/bin"

var memberDispatchEnv = filepath.Join(".config", "mw", "dispatch.env")

// MemberServe makes a member's dispatch timer, run as that member's Linux user:
// a dispatch.env saying where the tools are, the holder's dispatch service and
// timer written under the user's own systemd, and the timer enabled. A
// dispatcher under the holder's user could not use the member's Codex
// sign-in, since credentials do not cross users, so the member runs their own.
//
// It checks everything it can before it writes anything, and never overwrites.
type MemberServe struct {
	Host MemberHost
	// Service and Timer are the text of the holder's mw-dispatch.service and
	// mw-dispatch.timer (contrib/systemd), which are written as they are.
	Service string
	Timer   string
}

// MemberServeRequest is whose dispatcher to make.
type MemberServeRequest struct {
	// Home is the member's home directory, a full path.
	Home string
	// User is the member's Linux user, named in the hand step for linger.
	User string
}

// MemberServeReport is what MemberServe did.
type MemberServeReport struct {
	User        string
	EnvPath     string
	ServicePath string
	TimerPath   string
	// Path is the PATH line written to dispatch.env, or the one it already had.
	Path string
	// EnvKept is whether dispatch.env was there and so left as it was.
	EnvKept bool
	// LingerKnown is whether loginctl could say that linger is set.
	LingerKnown bool
}

// Run makes the dispatcher.
func (s MemberServe) Run(ctx context.Context, req MemberServeRequest) (MemberServeReport, error) {
	if s.Host == nil || s.Service == "" || s.Timer == "" {
		return MemberServeReport{}, fmt.Errorf("making a member's dispatcher: there is no place to make it")
	}
	if !filepath.IsAbs(req.Home) || strings.TrimSpace(req.User) == "" || strings.ContainsAny(req.User, " \t\r\n") {
		return MemberServeReport{}, fmt.Errorf("making a member's dispatcher: the home must be a full path and the user %q one word", req.User)
	}
	unitDir := filepath.Join(req.Home, ".config", "systemd", "user")
	report := MemberServeReport{
		User:        req.User,
		EnvPath:     filepath.Join(req.Home, memberDispatchEnv),
		ServicePath: filepath.Join(unitDir, MemberDispatchService),
		TimerPath:   filepath.Join(unitDir, MemberDispatchTimer),
	}

	config := filepath.Join(req.Home, memberConfigFile)
	if !s.Host.Exists(config) {
		return report, fmt.Errorf("no member factory here: %s is not there; mw member init makes one", config)
	}
	for _, path := range []string{report.ServicePath, report.TimerPath} {
		if s.Host.Exists(path) {
			return report, fmt.Errorf("already serving: %s is there; mw member serve --way-back says how to take the dispatcher away", path)
		}
	}

	var dirs, missing []string
	for _, tool := range MemberDispatchTools {
		program, err := s.Host.Program(tool)
		if err != nil {
			missing = append(missing, tool)
			continue
		}
		dirs = append(dirs, filepath.Dir(program))
	}
	if len(missing) > 0 {
		return report, fmt.Errorf("a member's dispatch needs %s on the PATH, and %s could not be found: install %s and run mw member serve again",
			strings.Join(MemberDispatchTools, ", "), strings.Join(missing, ", "), pluralTool(missing))
	}

	linger, err := s.Host.LingerEnabled(ctx)
	report.LingerKnown = err == nil
	if err == nil && !linger {
		return report, fmt.Errorf("linger is not set for %s, so the dispatch timer would stop when they log out: as root, loginctl enable-linger %s, then run mw member serve again", req.User, req.User)
	}

	if s.Host.Exists(report.EnvPath) {
		report.EnvKept = true
		text, err := s.Host.ReadFile(report.EnvPath)
		if err != nil {
			return report, err
		}
		for _, line := range strings.Split(text, "\n") {
			if value, ok := strings.CutPrefix(line, "PATH="); ok {
				report.Path = value
			}
		}
	} else {
		report.Path = strings.Join(uniqueStrings(dirs), ":") + ":" + memberSystemPath
		text := "# Where this user's dispatch finds mw, bd, git, tmux and codex: a user unit gets a bare PATH.\n" +
			"# Written by mw member serve; the user owns it.\n" +
			"PATH=" + report.Path + "\n"
		if err := s.Host.WriteFile(report.EnvPath, text, 0o644); err != nil {
			return report, err
		}
	}
	if err := s.Host.WriteFile(report.ServicePath, s.Service, 0o644); err != nil {
		return report, err
	}
	if err := s.Host.WriteFile(report.TimerPath, s.Timer, 0o644); err != nil {
		return report, err
	}
	if err := s.Host.EnableUnit(ctx, MemberDispatchTimer); err != nil {
		return report, fmt.Errorf("%w; the unit files are written: mw member serve --way-back prints how to remove them", err)
	}
	return report, nil
}

func pluralTool(tools []string) string {
	if len(tools) == 1 {
		return "it"
	}
	return "them"
}

// String says what was made and the way back.
func (r MemberServeReport) String() string {
	var out strings.Builder
	fmt.Fprintf(&out, "%s's dispatcher is set up, under their own user, so their Builders run on their own sign-in.\n\n", r.User)
	fmt.Fprintf(&out, "Wrote:\n")
	if r.EnvKept {
		fmt.Fprintf(&out, "  %s  (was there, and is left as it was; its PATH: %s)\n", r.EnvPath, orNone(r.Path))
	} else {
		fmt.Fprintf(&out, "  %s  (PATH: %s)\n", r.EnvPath, r.Path)
	}
	fmt.Fprintf(&out, "  %s  (the holder's dispatch service)\n", r.ServicePath)
	fmt.Fprintf(&out, "  %s  (enabled and started: a dispatch every hour)\n", r.TimerPath)
	if !r.LingerKnown {
		fmt.Fprintf(&out, "\nloginctl could not say whether linger is set; if it is not, as root: loginctl enable-linger %s\n", r.User)
	}
	fmt.Fprintf(&out, "\nThe way back: mw member serve --way-back prints the commands that remove it.\n")
	return out.String()
}

// MemberServeWayBack is the commands that take the member's dispatcher away,
// for a person to run; nothing is run for them. The dispatch.env and the rest of
// the factory stay where they are.
func MemberServeWayBack(home string) string {
	unitDir := filepath.Join(home, ".config", "systemd", "user")
	return "systemctl --user disable --now " + MemberDispatchTimer + "\n" +
		"rm " + filepath.Join(unitDir, MemberDispatchService) + " " + filepath.Join(unitDir, MemberDispatchTimer) + "\n" +
		"systemctl --user daemon-reload\n" +
		"# " + filepath.Join(home, memberDispatchEnv) + " and the rest of the factory are not touched; remove dispatch.env by hand if it is not wanted.\n"
}
