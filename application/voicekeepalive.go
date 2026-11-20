package application

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

// The church's Google Voice number is reclaimed by Google when no call or text
// is made from it for a while. VoiceKeepAlive is the follower's job that texts
// "Testing" from it once a month: one email from the church's Gmail account to
// the number's Voice text address, which Google sends on as an SMS from the
// number. The app password and the address are read from a mode-600 file on the
// home (VoiceKeepAliveEnvFile), never from the repository or a bead.

// VoiceKeepAliveJobName is the follower job that sends the monthly text.
const VoiceKeepAliveJobName = "voice-keepalive"

// VoiceKeepAliveEvery is how often the job looks at whether this month's text
// is sent: often enough that a file put in place, or a failed send, is tried
// again within hours, rarely enough not to hammer Gmail.
const VoiceKeepAliveEvery = 6 * time.Hour

// VoiceKeepAliveAccount is the Gmail account the Voice number belongs to.
const VoiceKeepAliveAccount = "whiteplainsnac@gmail.com"

// VoiceKeepAliveText is the text sent.
const VoiceKeepAliveText = "Testing"

// VoiceKeepAliveEnvFile is the file, under the home directory, holding
// CHURCH_VOICE_APP_PASSWORD and CHURCH_VOICE_TEXT_ADDRESS (and, when it is not
// VoiceKeepAliveAccount, CHURCH_VOICE_ACCOUNT).
const VoiceKeepAliveEnvFile = ".config/mw/church-voice.env"

// ErrNoVoiceAppPassword is what the credentials read says when the file is
// missing, or holds no app password.
var ErrNoVoiceAppPassword = errors.New("no app password yet")

// VoiceKeepAliveConfig is what the job reads from the file.
type VoiceKeepAliveConfig struct {
	// Account is the Gmail account that sends, and signs in with AppPassword.
	Account     string
	AppPassword string
	// TextAddress is the Voice text-reply address, <...>@txt.voice.google.com.
	TextAddress string
}

// VoiceMail is the email that becomes the text.
type VoiceMail struct {
	From, To, Subject, Body string
}

// String is the mail as the dry run prints it.
func (m VoiceMail) String() string {
	return fmt.Sprintf("From: %s\nTo: %s\nSubject: %s\n\n%s\n", m.From, m.To, m.Subject, m.Body)
}

// ParseVoiceKeepAliveEnv reads the file's text: KEY=value lines, # comments,
// values optionally in quotes. A file with no CHURCH_VOICE_APP_PASSWORD is
// ErrNoVoiceAppPassword.
func ParseVoiceKeepAliveEnv(text string) (VoiceKeepAliveConfig, error) {
	cfg := VoiceKeepAliveConfig{Account: VoiceKeepAliveAccount}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		key, value, ok := strings.Cut(line, "=")
		if !ok || strings.HasPrefix(line, "#") {
			continue
		}
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		switch strings.TrimSpace(key) {
		case "CHURCH_VOICE_APP_PASSWORD":
			cfg.AppPassword = value
		case "CHURCH_VOICE_TEXT_ADDRESS":
			cfg.TextAddress = value
		case "CHURCH_VOICE_ACCOUNT":
			if value != "" {
				cfg.Account = value
			}
		}
	}
	if cfg.AppPassword == "" {
		return cfg, ErrNoVoiceAppPassword
	}
	return cfg, nil
}

// VoiceKeepAliveNotes is the part of the tracker's key-value store the job
// keeps its months in.
type VoiceKeepAliveNotes interface {
	Note(ctx context.Context, key string) (string, error)
	SetNote(ctx context.Context, key, value string) error
}

// VoiceKeepAliveSentKey is the note that keeps a month's text as sent.
func VoiceKeepAliveSentKey(month string) string { return "voice-keepalive-sent-" + month }

// VoiceKeepAliveAlertKey is the note that keeps a month's alert of one kind as
// posted, so a failing month alerts him once, not every pass.
func VoiceKeepAliveAlertKey(month, kind string) string {
	return "voice-keepalive-alert-" + month + "-" + kind
}

// VoiceKeepAlive sends the monthly text, on the home only, and posts him one
// alarm for each way a month fails.
type VoiceKeepAlive struct {
	Notes  VoiceKeepAliveNotes
	Sender PosternSender
	// Creds reads the file; it says ErrNoVoiceAppPassword when there is none.
	Creds func(ctx context.Context) (VoiceKeepAliveConfig, error)
	// Send sends the mail over Gmail's SMTP with cfg's account and app password.
	Send func(ctx context.Context, cfg VoiceKeepAliveConfig, mail VoiceMail) error
	// Home says whether this host is the home: only the home sends.
	Home func(ctx context.Context) (bool, error)
	// Now is the clock; nil is time.Now.
	Now func() time.Time
	// Out is where the dry run's mail and what was done are printed.
	Out io.Writer
}

func (k *VoiceKeepAlive) month() string {
	if k.Now != nil {
		return k.Now().UTC().Format("2006-01")
	}
	//mw:realclock the default clock, when no Now is injected: the host's time
	return time.Now().UTC().Format("2006-01")
}

func (k *VoiceKeepAlive) say(format string, args ...any) {
	if k.Out != nil {
		fmt.Fprintf(k.Out, format, args...)
	}
}

// VoiceKeepAliveJob is the job the follower runs: no event springs it, it runs
// every VoiceKeepAliveEvery on the follower's own clock, and a pass that finds
// the month done is a skipped one.
func VoiceKeepAliveJob(run func(context.Context) error) SpringJob {
	job := ClockJob(VoiceKeepAliveJobName, VoiceKeepAliveEvery, run)
	job.TellFailure = true
	return job
}

// Run sends this month's text if it is not sent yet. With dryRun it prints the
// mail it would send, or the alert it would post, and writes, sends and posts
// nothing, on any host. A month whose text is already sent, or a host that is
// not the home, is a skipped pass.
func (k *VoiceKeepAlive) Run(ctx context.Context, dryRun bool) error {
	month := k.month()
	if !dryRun {
		if at, err := k.Home(ctx); err != nil {
			return err
		} else if !at {
			return SkippedPass("the Voice text is sent by the home only")
		}
		sent, err := k.Notes.Note(ctx, VoiceKeepAliveSentKey(month))
		if err != nil {
			return fmt.Errorf("reading the note of the %s text: %w", month, err)
		}
		if sent != "" {
			return SkippedPass("the Voice text of " + month + " is sent")
		}
	}
	cfg, err := k.Creds(ctx)
	if errors.Is(err, ErrNoVoiceAppPassword) {
		return k.fail(ctx, dryRun, month, "no-password", nil, "Church Voice number keep-alive: no app password yet, so no text was sent. Put CHURCH_VOICE_APP_PASSWORD (and CHURCH_VOICE_TEXT_ADDRESS) in ~/"+VoiceKeepAliveEnvFile+" on the home.")
	}
	if err != nil {
		return k.fail(ctx, dryRun, month, "creds", err, "Church Voice number keep-alive: the file ~/"+VoiceKeepAliveEnvFile+" could not be read ("+err.Error()+"), so no text was sent.")
	}
	if strings.TrimSpace(cfg.TextAddress) == "" {
		return k.fail(ctx, dryRun, month, "no-address", nil, "Church Voice number keep-alive: no Voice text address, so no text was sent. Set CHURCH_VOICE_TEXT_ADDRESS in ~/"+VoiceKeepAliveEnvFile+" (the <...>@txt.voice.google.com address on a forwarded text).")
	}
	mail := VoiceMail{From: cfg.Account, To: cfg.TextAddress, Body: VoiceKeepAliveText}
	if dryRun {
		k.say("mw voice-keepalive --dry-run: would send, and sent nothing:\n%s", mail)
		return nil
	}
	if err := k.Send(ctx, cfg, mail); err != nil {
		what := strings.ReplaceAll(err.Error(), cfg.AppPassword, "[app password]")
		return k.fail(ctx, false, month, "send", errors.New(what), "Church Voice number keep-alive: the monthly 'Testing' text did not send: "+what+". It is tried again within hours.")
	}
	// Noted after the send, so a note that fails is a second text, not a missed one.
	if err := k.Notes.SetNote(ctx, VoiceKeepAliveSentKey(month), k.stamp()); err != nil {
		return fmt.Errorf("the %s text was sent, but noting it failed: %w", month, err)
	}
	k.say("mw voice-keepalive: sent %q to %s\n", VoiceKeepAliveText, cfg.TextAddress)
	return nil
}

func (k *VoiceKeepAlive) stamp() string {
	if k.Now != nil {
		return k.Now().UTC().Format(time.RFC3339)
	}
	//mw:realclock the default clock, when no Now is injected: the host's time
	return time.Now().UTC().Format(time.RFC3339)
}

// fail posts the alarm for a way the month failed, once for each kind of
// failure in a month, and returns the pass's error: cause when there is one (a
// failed send), a skipped pass otherwise. A dry run prints the alarm instead.
func (k *VoiceKeepAlive) fail(ctx context.Context, dryRun bool, month, kind string, cause error, text string) error {
	ended := cause
	if ended == nil {
		ended = SkippedPass(text)
	}
	if dryRun {
		k.say("mw voice-keepalive --dry-run: would post the alert, and sent nothing:\n%s\n", text)
		return nil
	}
	key := VoiceKeepAliveAlertKey(month, kind)
	posted, err := k.Notes.Note(ctx, key)
	if err != nil {
		return fmt.Errorf("reading the note of the %s alert: %w", kind, err)
	}
	if posted != "" {
		return ended
	}
	if _, err := k.Sender.Run(ctx, PosternSendRequest{Class: "alarm", Text: text, Summary: "Church Voice number keep-alive failed"}); err != nil {
		return fmt.Errorf("%v (and the alert could not be posted: %w)", ended, err)
	}
	if err := k.Notes.SetNote(ctx, key, k.stamp()); err != nil {
		return fmt.Errorf("the %s alert was posted, but noting it failed: %w", kind, err)
	}
	return ended
}
