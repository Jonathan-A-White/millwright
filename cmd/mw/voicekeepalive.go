package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/smtp"
	"os"
	"path/filepath"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"

	"github.com/spf13/cobra"
)

// voiceSMTPHost is Gmail's submission server; the mail goes over STARTTLS on 587.
const voiceSMTPHost = "smtp.gmail.com"

// voiceKeepAliveEnvPath is the file the church Voice number's app password is
// read from, under the user's home directory.
func voiceKeepAliveEnvPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, application.VoiceKeepAliveEnvFile), nil
}

// readVoiceKeepAliveEnv reads the file at path. A missing file is
// application.ErrNoVoiceEnvFile, which turns the job off; a file other users can read is refused,
// for it holds an app password.
func readVoiceKeepAliveEnv(path string) (application.VoiceKeepAliveConfig, error) {
	info, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return application.VoiceKeepAliveConfig{}, application.ErrNoVoiceEnvFile
	}
	if err != nil {
		return application.VoiceKeepAliveConfig{}, err
	}
	if info.Mode().Perm()&0o077 != 0 {
		return application.VoiceKeepAliveConfig{}, fmt.Errorf("%s is mode %o; it holds an app password, so chmod 600 it", path, info.Mode().Perm())
	}
	text, err := os.ReadFile(path)
	if err != nil {
		return application.VoiceKeepAliveConfig{}, err
	}
	return application.ParseVoiceKeepAliveEnv(string(text))
}

// sendVoiceMail sends mail through Gmail's SMTP as cfg's account.
func sendVoiceMail(ctx context.Context, cfg application.VoiceKeepAliveConfig, mail application.VoiceMail) error {
	dialer := net.Dialer{Timeout: 30 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(voiceSMTPHost, "587"))
	if err != nil {
		return err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Minute))
	client, err := smtp.NewClient(conn, voiceSMTPHost)
	if err != nil {
		return err
	}
	defer client.Close()
	if err := client.StartTLS(&tls.Config{ServerName: voiceSMTPHost}); err != nil {
		return err
	}
	if err := client.Auth(smtp.PlainAuth("", cfg.Account, cfg.AppPassword, voiceSMTPHost)); err != nil {
		return err
	}
	if err := client.Mail(mail.From); err != nil {
		return err
	}
	if err := client.Rcpt(mail.To); err != nil {
		return err
	}
	w, err := client.Data()
	if err != nil {
		return err
	}
	msg := "From: " + mail.From + "\r\nTo: " + mail.To + "\r\nSubject: " + mail.Subject +
		"\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n" + mail.Body + "\r\n"
	if _, err := io.WriteString(w, msg); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return client.Quit()
}

// voiceKeepAlive builds the use case for a host. envPath "" is the default file.
func voiceKeepAlive(host, envPath string, out io.Writer, dryRun bool) (*application.VoiceKeepAlive, error) {
	if envPath == "" {
		path, err := voiceKeepAliveEnvPath()
		if err != nil {
			return nil, err
		}
		envPath = path
	}
	keep := &application.VoiceKeepAlive{
		Creds: func(context.Context) (application.VoiceKeepAliveConfig, error) { return readVoiceKeepAliveEnv(envPath) },
		Send:  sendVoiceMail,
		Out:   out,
	}
	if dryRun {
		return keep, nil
	}
	day, hour, err := config.VoiceKeepAliveWhen()
	if err != nil {
		return nil, err
	}
	keep.When = &application.VoiceKeepAliveWhen{Day: day, Hour: hour}
	dir, err := config.Vault()
	if err != nil {
		return nil, err
	}
	gateway := mwGateway(dir, host)
	keep.Notes, keep.Sender = gateway, handsPush{gateway: gateway}
	keep.Home = func(ctx context.Context) (bool, error) {
		return application.IsHome(ctx, mwVault(dir, host), host)
	}
	return keep, nil
}

// voiceKeepAliveSpringJob is the follower's job that texts "Testing" from the
// church's Google Voice number once a month, on the home only.
func voiceKeepAliveSpringJob(host string, out io.Writer) (application.SpringJob, error) {
	keep, err := voiceKeepAlive(host, "", out, false)
	if err != nil {
		return application.SpringJob{}, err
	}
	return application.VoiceKeepAliveJob(func(ctx context.Context) error { return keep.Run(ctx, false) }), nil
}

// newVoiceKeepAliveCmd builds `mw voice-keepalive`.
func newVoiceKeepAliveCmd() *cobra.Command {
	var dryRun bool
	var envPath string
	cmd := &cobra.Command{
		Use:   "voice-keepalive [--dry-run] [--env <file>]",
		Short: "Text 'Testing' from the church's Google Voice number, as the monthly job does",
		Long: "voice-keepalive sends this month's text from the church's Google Voice number: one email from\n" +
			application.VoiceKeepAliveAccount + " over Gmail SMTP to the number's Voice text address, which Google\n" +
			"sends as an SMS. It sends once a month, at 9 pm Eastern (America/New_York) on the 1st, or at the first\n" +
			"check after that if the home was off then; the follower checks every hour on the home. The day and the\n" +
			"hour are the [voice_keepalive] keys day (1 to 28, default 1) and hour (0 to 23, default 21) of\n" +
			"~/.config/mw/config.toml: mw config set voice_keepalive.day 15 and mw config set voice_keepalive.hour 20.\n" +
			"The app password and the text address are read from the mode-600 file ~/" + application.VoiceKeepAliveEnvFile + "\n" +
			"(CHURCH_VOICE_APP_PASSWORD, CHURCH_VOICE_TEXT_ADDRESS), or --env <file>. The file turns the job on:\n" +
			"with no file the job is off ('off: no church-voice.env'), nothing is sent and nothing is posted. With a\n" +
			"file that holds no password or address, or a send that fails, the Governor gets one alarm a month.\n\n" +
			"  mw voice-keepalive --dry-run     print the email, send and post nothing, on any host\n" +
			"  mw voice-keepalive               send now if this month's text is due and not sent (the home only)",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			host, err := config.Host()
			if err != nil && !dryRun {
				return err
			}
			keep, err := voiceKeepAlive(host, envPath, cmd.OutOrStdout(), dryRun)
			if err != nil {
				return err
			}
			err = keep.Run(cmd.Context(), dryRun)
			var skipped *application.SkippedPassError
			if errors.As(err, &skipped) {
				fmt.Fprintf(cmd.OutOrStdout(), "mw voice-keepalive: %s\n", skipped.Why)
				return nil
			}
			return err
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print the email it would send and send nothing")
	cmd.Flags().StringVar(&envPath, "env", "", "read the app password from this file, not ~/"+application.VoiceKeepAliveEnvFile)
	return cmd
}
