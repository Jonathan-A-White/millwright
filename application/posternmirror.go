package application

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

// PosternIndexFile is the postern backend's index, one JSON record a line, in its
// data directory. The others are the files the backend keeps beside it there;
// PosternBlobsDir holds the attachments.
const (
	PosternIndexFile         = "postern-index.jsonl"
	PosternVapidFile         = "postern-vapid.json"
	PosternSubscriptionsFile = "postern-push-subscriptions.json"
	PosternBlobsDir          = "blobs"
)

// PosternMirrorer is what mw postern mirror asks of the machines: when a data
// directory's index last had a record put in it, and a copy of files to another
// place. The real one runs rsync and ssh.
type PosternMirrorer interface {
	// LastIndexTime reports the time of the last line of dir's index, on the host
	// reached by ssh (this host when ssh is empty). ok is false when there is no
	// index there, or it has no lines.
	LastIndexTime(ctx context.Context, ssh []string, dir string) (at time.Time, ok bool, err error)
	// Copy copies names, paths under dir here, with their directories, to dest, an
	// rsync destination (`host:/path/`). rsh is the remote shell rsync reaches dest
	// by; empty leaves that to rsync's own default. A name that is not there is
	// skipped.
	Copy(ctx context.Context, dir string, names []string, rsh []string, dest string) error
}

// PosternMirror is mw postern mirror: on the home, copy the backend's data to the
// boost, and the two files the VPS watchdog needs to the watchdog's directory.
type PosternMirror struct {
	Files    HomeFile
	Mirrorer PosternMirrorer
	Out      io.Writer
	// Host is this host's name.
	Host string
	// DataDir is the backend's POSTERN_DATA: a full path, the same on the boost.
	DataDir string
	// Reach is how this host reaches each other host: an ssh prefix by host name
	// (`ssh desktop`), the last word of it the host itself.
	Reach map[string]string
	// WatchdogTarget is where the watchdog's two files go, an rsync destination
	// (`root@vps:/var/lib/postern-watchdog/`). Empty copies nothing there.
	WatchdogTarget string
}

// MirrorRefused is the finding that a copy to a host was left alone because that
// host's index is newer than this one's: what is there is not to be overwritten
// by less.
type MirrorRefused struct {
	Host         string
	Theirs, Ours time.Time
	HasOurs      bool
}

func (r *MirrorRefused) Error() string {
	ours := "this host's index has no records"
	if r.HasOurs {
		ours = "this host's last is " + r.Ours.UTC().Format(time.RFC3339)
	}
	return fmt.Sprintf("left %s alone: its index is newer (its last record is %s; %s)", r.Host, r.Theirs.UTC().Format(time.RFC3339), ours)
}

// Run mirrors, once. A host that is not home does nothing and says so, and a home
// that cannot be told is a *HomeUnknown. A boost whose index is newer is left
// alone and the run says so in its error; the watchdog's copy is tried all the
// same.
func (m PosternMirror) Run(ctx context.Context) error {
	record, err := WhereIsHome(ctx, m.Files)
	if err != nil {
		return err
	}
	if record.Host != m.Host {
		fmt.Fprintf(m.Out, "this host is not home: %s is home and this host is %s: nothing to mirror\n", record.Host, m.Host)
		return nil
	}

	return errors.Join(m.toBoost(ctx), m.toWatchdog(ctx))
}

// boost is the other host that can be home.
func (m PosternMirror) boost() string {
	if m.Host == "desktop" {
		return "laptop"
	}
	return "desktop"
}

func (m PosternMirror) toBoost(ctx context.Context) error {
	boost := m.boost()
	prefix := strings.Fields(m.Reach[boost])
	if len(prefix) < 2 {
		return fmt.Errorf("no way to reach %s: set `%s = \"ssh <alias>\"` under [hands_hosts] in the config file", boost, boost)
	}
	host, rsh := prefix[len(prefix)-1], prefix[:len(prefix)-1]

	theirs, there, err := m.Mirrorer.LastIndexTime(ctx, prefix, m.DataDir)
	if err != nil {
		return fmt.Errorf("asking %s for its index: %w", boost, err)
	}
	if there {
		ours, has, err := m.Mirrorer.LastIndexTime(ctx, nil, m.DataDir)
		if err != nil {
			return fmt.Errorf("reading this host's index: %w", err)
		}
		if !has || theirs.After(ours) {
			return &MirrorRefused{Host: boost, Theirs: theirs, Ours: ours, HasOurs: has}
		}
	}
	// One rsync, so the order is rsync's: a blob can arrive after the record that
	// names it, and the next run makes it whole.
	names := []string{PosternIndexFile, PosternVapidFile, PosternSubscriptionsFile, PosternBlobsDir}
	if err := m.Mirrorer.Copy(ctx, m.DataDir, names, rsh, host+":"+m.DataDir+"/"); err != nil {
		return fmt.Errorf("copying to %s: %w", boost, err)
	}
	fmt.Fprintf(m.Out, "copied %s to %s\n", m.DataDir, boost)
	return nil
}

func (m PosternMirror) toWatchdog(ctx context.Context) error {
	if m.WatchdogTarget == "" {
		return nil
	}
	names := []string{PosternVapidFile, PosternSubscriptionsFile}
	if err := m.Mirrorer.Copy(ctx, m.DataDir, names, nil, m.WatchdogTarget); err != nil {
		return fmt.Errorf("copying to the watchdog at %s: %w", m.WatchdogTarget, err)
	}
	fmt.Fprintf(m.Out, "copied the vapid keys and push subscriptions to %s\n", m.WatchdogTarget)
	return nil
}
