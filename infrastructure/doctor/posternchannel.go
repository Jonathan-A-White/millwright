package doctor

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
)

var _ application.DoctorCheck = (*PosternChannel)(nil)

// PosternChannelName is what the check is called: in the log, and on the
// command line as `mw doctor postern-channel`.
const PosternChannelName = "postern-channel"

// The postern-channel check's damper: the cure is one write that a second try
// does not make likelier to work, so one failed cure attempt is all an episode
// ever spends, as with beads-size.
const (
	PosternChannelDamperWait = 0 * time.Second
	PosternChannelDamperCap  = 1
)

// errPosternChannelNoCure is what Cure returns when the check was built with no
// config file to edit.
var errPosternChannelNoCure = fmt.Errorf("no cure: a person adds `postern_channel = \"%s\"` to ~/%s", config.PosternChannelDirect, config.File)

// PosternChannel is the check that a home host's config sends the Mayor's
// postern messages directly. config.PosternChannel defaults to chain on
// purpose, so a host whose config lost the key sends every message as a chain
// transaction, and past WhatsOnChain's newest-100 history cap the Mayor is
// locked out (2026-09-28 to 2026-09-30). Its cure is the one command a person
// would run, mw config set postern_channel direct: a dated backup of the file,
// then the one key.
type PosternChannel struct {
	// Home and Host are the vault's home file and this host's name. A host the
	// file says is not home sends nothing for the Mayor, so nothing is wrong
	// there. A nil Home, or a home that cannot be told, judges as before, as
	// MayorGone does.
	Home application.HomeFile
	Host string
	// Config is this host's ~/.config/mw/config.toml, which the cure edits as
	// mw config set does. Nil leaves the cure off.
	Config application.ConfigFile
	// Out is where the cure says what it changed and the way back; nil is
	// discarded.
	Out io.Writer
}

// NewPosternChannel is the check for the host named host, against the given
// home file.
func NewPosternChannel(home application.HomeFile, host string) *PosternChannel {
	return &PosternChannel{Home: home, Host: host}
}

// Name implements application.DoctorCheck.
func (p *PosternChannel) Name() string { return PosternChannelName }

// Probe implements application.DoctorCheck: ok on a host that is not home;
// otherwise cannot-tell when config.PosternChannel refuses the setting;
// faulty when it reads anything but direct, naming the key and the file; ok
// when it reads direct, from $MW_POSTERN_CHANNEL or the config file alike.
func (p *PosternChannel) Probe(ctx context.Context) (application.Verdict, string) {
	if p.Home != nil {
		if home, err := application.IsHome(ctx, p.Home, p.Host); err == nil && !home {
			return application.DoctorOK, ""
		}
	}

	channel, err := config.PosternChannel()
	if err != nil {
		return application.DoctorCannotTell, err.Error()
	}
	if channel != config.PosternChannelDirect {
		return application.DoctorFaulty, fmt.Sprintf(
			"postern_channel is %s (chain is also what a config without the key reads): every Mayor send is a transaction; add postern_channel = \"%s\" to ~/%s",
			channel, config.PosternChannelDirect, config.File)
	}
	return application.DoctorOK, ""
}

// Cure implements application.DoctorCheck: it sets postern_channel to direct
// through the writer mw config set uses (a dated backup first, every other
// byte kept). With no Config to edit there is no cure, and the error says what
// a person adds.
func (p *PosternChannel) Cure(ctx context.Context) error {
	if p.Config == nil {
		return errPosternChannelNoCure
	}
	out := p.Out
	if out == nil {
		out = io.Discard
	}
	err := application.ConfigSet{File: p.Config, Out: out}.
		Run(ctx, "postern_channel", config.PosternChannelDirect, false)
	if err != nil {
		return fmt.Errorf("mw config set postern_channel %s failed: %w", config.PosternChannelDirect, err)
	}
	return nil
}

// Damper implements application.DoctorCheck.
func (p *PosternChannel) Damper() (time.Duration, int) {
	return PosternChannelDamperWait, PosternChannelDamperCap
}

// WayBack implements application.DoctorCheck: the cure's backup of the config
// file, dated, beside it, which the cure's own words name.
func (p *PosternChannel) WayBack() string {
	return fmt.Sprintf("copy the dated ~/%s.bak-<time> the cure made back over ~/%s; where there was no config file, remove it", config.File, config.File)
}
