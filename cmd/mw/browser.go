package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	mwbrowser "github.com/Jonathan-A-White/millwright/infrastructure/browser"

	"github.com/spf13/cobra"
)

// browserLauncher starts and stops the browser's programs; a test puts a fake
// in its place.
var browserLauncher mwbrowser.Launcher = mwbrowser.ExecLauncher{}

// browserPrograms finds Xtigervnc and Chromium; a test names its own.
var browserPrograms = func(home string) (string, string, error) {
	x, err := exec.LookPath("Xtigervnc")
	if err != nil {
		return "", "", fmt.Errorf("Xtigervnc is not installed: sudo apt install -y tigervnc-standalone-server")
	}
	c, err := mwbrowser.FindChromium(home, exec.LookPath)
	return x, c, err
}

// browserStateDir is where the state file is kept, under the home.
var browserStateDir = filepath.Join(".local", "state", "mw", "browser")

func newBrowser() (mwbrowser.Browser, string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return mwbrowser.Browser{}, "", fmt.Errorf("there is no home directory for mw browser's state: %w", err)
	}
	return mwbrowser.Browser{
		Launcher:     browserLauncher,
		StateDir:     filepath.Join(home, browserStateDir),
		Display:      mwbrowser.DefaultDisplay,
		DevToolsPort: mwbrowser.DefaultDevToolsPort,
	}, home, nil
}

// newBrowserCmd builds `mw browser`: a Chromium on a VNC display of this host
// that he signs into himself through his ssh tunnel, that a script then drives
// over the DevTools protocol, and that forgets the session when closed.
func newBrowserCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "browser",
		Short: "A browser he signs into himself over VNC, driven by scripts, forgotten on close (open, status, close)",
		Long: "browser starts Xtigervnc on display :5, listening on 127.0.0.1 only with no VNC password\n" +
			"(only his ssh tunnel reaches it), and a Chromium there on a fresh profile in a new temp\n" +
			"directory of mode 700, its DevTools port on 127.0.0.1 only. He signs in himself through\n" +
			"his VNC app; a script then connects with chromium.connectOverCDP. The profile turns\n" +
			"password saving off, and mw browser close deletes it, cookies, storage and all. The only\n" +
			"file mw keeps is ~/.local/state/mw/browser/browser.json: pids, ports and the profile's path.",
	}
	cmd.AddCommand(newBrowserOpenCmd(), newBrowserStatusCmd(), newBrowserCloseCmd())
	return cmd
}

func newBrowserOpenCmd() *cobra.Command {
	var display, vncPort, devtoolsPort int
	var sshHost, geometry string
	cmd := &cobra.Command{
		Use:   "open <url>",
		Short: "Start the VNC display and a Chromium on a fresh profile at url; print the ssh -L line and the VNC address",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			b, home, err := newBrowser()
			if err != nil {
				return err
			}
			b.Display, b.VNCPort, b.DevToolsPort, b.Geometry = display, vncPort, devtoolsPort, geometry
			if b.Xtigervnc, b.Chromium, err = browserPrograms(home); err != nil {
				return err
			}
			o, err := b.Open(context.Background(), args[0])
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), o.Say(sshHost))
			return nil
		},
	}
	cmd.Flags().StringVar(&sshHost, "ssh-host", "lap", "the name his phone's ssh knows this host by, for the ssh -L line")
	cmd.Flags().IntVar(&display, "display", mwbrowser.DefaultDisplay, "the X display number")
	cmd.Flags().IntVar(&vncPort, "vnc-port", 0, "the VNC port on 127.0.0.1 (0: 5900 plus the display)")
	cmd.Flags().IntVar(&devtoolsPort, "devtools-port", mwbrowser.DefaultDevToolsPort, "Chromium's DevTools port on 127.0.0.1")
	cmd.Flags().StringVar(&geometry, "geometry", mwbrowser.DefaultGeometry, "the screen's size, WIDTHxHEIGHT")
	return cmd
}

func newBrowserStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Say whether the browser is up, the URL of each tab, and the CDP endpoint a script connects to",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			b, _, err := newBrowser()
			if err != nil {
				return err
			}
			s, err := b.Status(context.Background())
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), s)
			return nil
		},
	}
}

func newBrowserCloseCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "close",
		Short: "Stop Chromium and the VNC display and delete the profile, cookies, storage and all",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			b, _, err := newBrowser()
			if err != nil {
				return err
			}
			c, err := b.Close(context.Background())
			fmt.Fprintln(cmd.OutOrStdout(), c)
			return err
		},
	}
}
