package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/spf13/cobra"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/postern"
	"github.com/Jonathan-A-White/millwright/infrastructure/rig"
)

// gristSmokeBook is the record of each app's last grist smoke, kept in the
// vault's tracker, and the alarm a failed one posts to the home's event log.
func gristSmokeBook(notes application.GristSmokeNotes, host string) application.GristSmokeBook {
	return application.GristSmokeBook{Notes: notes, Events: homeEventLog(), Host: host}
}

// newSmoker is the grist smoke as this host is set up to make it: the grinds
// read from the apps' rigs ([grist-apps]) and every example sent through the
// postern backend as the test key (grist_test_key_file). A key that is not there
// is an error that says how to make one, not a key made: only a key the backend
// holds a licence for is of any use.
func newSmoker(out io.Writer, wait time.Duration) (application.GristSmoke, error) {
	apps, err := config.GristApps()
	if err != nil {
		return application.GristSmoke{}, err
	}
	path, err := config.GristTestKeyFile()
	if err != nil {
		return application.GristSmoke{}, err
	}
	keys := postern.New(path)
	if exists, err := keys.Exists(); err != nil {
		return application.GristSmoke{}, err
	} else if !exists {
		return application.GristSmoke{}, fmt.Errorf("there is no grist test key at %s (grist_test_key_file in %s): it is a key file the postern backend holds a licence for every app's grist", path, config.File)
	}
	backend, err := posternBackend(keys)
	if err != nil {
		return application.GristSmoke{}, err
	}
	return application.GristSmoke{
		Grinds: rig.NewGrinds(),
		Send:   application.GristSend{Postern: backend, Cipher: posternCipher(keys), Keys: keys, Now: posternClock},
		Apps:   apps,
		Wait:   wait,
		Out:    out,
	}, nil
}

// unavailableSmoker is the smoke of a host that is set up to smoke (it has
// [grist-apps]) but cannot make one: every app it is asked about fails, saying
// why, so that a landing there is not passed as tested.
type unavailableSmoker struct{ err error }

func (u unavailableSmoker) Run(_ context.Context, app, _ string) (application.GristSmokeReport, error) {
	return application.GristSmokeReport{App: app}, u.err
}

// hostGristSmoke is what a landing smokes with on this host: nothing on a host
// with no [grist-apps], and otherwise the smoke after the landing's changes. A
// host that cannot be read for it says so and smokes nothing: the landing goes
// on.
func hostGristSmoke(notes application.GristSmokeNotes, host string, rigs map[string]string, touches application.LandingTouches) application.GristSmokeAfter {
	apps, err := config.GristApps()
	if err != nil || len(apps) == 0 {
		return application.GristSmokeAfter{}
	}
	paths, err := config.GristSmokePaths()
	if err != nil {
		paths = nil
	}
	var smoker application.GristAppSmoker
	if made, err := newSmoker(nil, 0); err != nil {
		smoker = unavailableSmoker{err}
	} else {
		smoker = made
	}
	// A landing of the factory's own rig is smoked by the mw it builds, not by
	// the one this process is (mw-gq6.339).
	var built application.GristAppSmoker
	if dir := rigs[application.FactoryRig]; dir != "" {
		built = rig.NewBuiltSmoker(dir, smoker)
	}
	return application.GristSmokeAfter{
		Touches: touches, Smoke: smoker, Built: built, Book: gristSmokeBook(notes, host),
		Apps: apps, Rigs: rigs, ClientPaths: paths,
	}
}

// newGristSmokeCmd builds `mw grist smoke <app>`: an app's grist tested end to
// end.
func newGristSmokeCmd() *cobra.Command {
	var kind string
	var wait time.Duration
	var lift, asJSON bool

	cmd := &cobra.Command{
		Use:   "smoke <app>",
		Short: "Send every example of an app's grinds through the live backend and check the answers",
		Long: "smoke sends each scenario of the app's grinds, grinds/examples/<kind>/<name>.json at its rig's\n" +
			"main (config [grist-apps]), through the postern backend as the test key (grist_test_key_file,\n" +
			"default ~/.config/mw/grist-test.key), with the photos beside it, and waits for the answer.\n" +
			"An example is a request (with its schemaVersion beside it, or in it), optional photo file names, and expect: for\n" +
			"each field of the answer (dotted, items.0.name) a value it equals or a check of equals,\n" +
			"is_null, one_of, contains, matches (a pattern), present and all (a list of such checks, each of\n" +
			"which must hold). It passes when the grist is\n" +
			"answered, the answer fits the grind's answer schema and meets every expect; a failure names\n" +
			"the example, the field, what was wanted and what came. A kind with no example is a warning.\n\n" +
			"What it finds is kept (mw status shows it): a failure after a landing holds the rig's open\n" +
			"stories until a smoke passes. --lift ends that hold without a smoke. It exits 1 on a failure.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if asJSON {
				// What a landing in the factory's rig asks of the mw it just built
				// (mw-gq6.339): the smoke only, as one JSON report. The landing
				// records it against the rig it holds, so this records nothing.
				smoke, err := newSmoker(cmd.ErrOrStderr(), wait)
				if err != nil {
					return err
				}
				report, err := smoke.Run(cmd.Context(), args[0], kind)
				if err != nil {
					return err
				}
				return json.NewEncoder(cmd.OutOrStdout()).Encode(report)
			}
			dir, err := config.Vault()
			if err != nil {
				return err
			}
			host, err := config.Host()
			if err != nil {
				return err
			}
			book := gristSmokeBook(mwGateway(dir, host), host)
			if lift {
				lifted, err := book.Lift(cmd.Context(), args[0])
				if err != nil {
					return err
				}
				if lifted {
					fmt.Fprintf(cmd.OutOrStdout(), "grist smoke: the hold of %s is lifted\n", args[0])
				} else {
					fmt.Fprintf(cmd.OutOrStdout(), "grist smoke: %s holds nothing\n", args[0])
				}
				return nil
			}
			smoke, err := newSmoker(cmd.OutOrStdout(), wait)
			if err != nil {
				return err
			}
			report, err := smoke.Run(cmd.Context(), args[0], kind)
			if err != nil {
				return err
			}
			// A kind-only smoke does not stand for the whole app's.
			if kind == "" {
				if err := book.Record(cmd.Context(), "", report); err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "mw grist smoke: %v\n", err)
				}
			}
			fmt.Fprintln(cmd.OutOrStdout(), report.Line())
			for _, warning := range report.Warnings {
				fmt.Fprintf(cmd.OutOrStdout(), "warning: %s\n", warning)
			}
			if report.Failed() {
				return application.ErrGristSmokeFailed
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&kind, "kind", "", "smoke only this kind of grist (default: every grind of the app)")
	cmd.Flags().DurationVar(&wait, "wait", application.GristSmokeWait, "how long each example waits for its answer")
	cmd.Flags().BoolVar(&asJSON, "json", false, "print the report as one JSON object, and record nothing (what a landing asks of the mw it built)")
	cmd.Flags().BoolVar(&lift, "lift", false, "end the hold a failed smoke of the app put on its rig's stories, without a smoke")
	return cmd
}
