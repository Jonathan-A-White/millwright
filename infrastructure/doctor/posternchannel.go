package doctor

import (
	"context"
	"fmt"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
)

var _ application.DoctorCheck = (*PosternChannel)(nil)

// PosternChannelName is what the check is called: in the log, and on the
// command line as `mw doctor postern-channel`.
const PosternChannelName = "postern-channel"

// The postern-channel check's damper: there is no cure to retry, so one failed
// cure attempt is all an episode ever spends, as with beads-size.
const (
	PosternChannelDamperWait = 0 * time.Second
	PosternChannelDamperCap  = 1
)

// errPosternChannelNoCure is what Cure always returns: the doctor does not
// edit a person's config file.
var errPosternChannelNoCure = fmt.Errorf("no cure: a person adds `postern_channel = \"%s\"` to ~/%s", config.PosternChannelDirect, config.File)

// PosternChannel is the check that a home host's config sends the Mayor's
// postern messages directly. config.PosternChannel defaults to chain on
// purpose, so a host whose config lost the key sends every message as a chain
// transaction, and past WhatsOnChain's newest-100 history cap the Mayor is
// locked out (2026-09-28 to 2026-09-30). It never cures: it names the key and
// the file, and leaves the edit to a person.
type PosternChannel struct {
	// Home and Host are the vault's home file and this host's name. A host the
	// file says is not home sends nothing for the Mayor, so nothing is wrong
	// there. A nil Home, or a home that cannot be told, judges as before, as
	// MayorGone does.
	Home application.HomeFile
	Host string
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

// Cure implements application.DoctorCheck: there is none.
func (p *PosternChannel) Cure(context.Context) error { return errPosternChannelNoCure }

// Damper implements application.DoctorCheck.
func (p *PosternChannel) Damper() (time.Duration, int) {
	return PosternChannelDamperWait, PosternChannelDamperCap
}

// WayBack implements application.DoctorCheck: nothing ever changes, so there
// is nothing to undo.
func (p *PosternChannel) WayBack() string {
	return fmt.Sprintf("none: no cure runs; a person edits ~/%s", config.File)
}
