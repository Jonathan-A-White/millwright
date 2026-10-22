package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/grist"
	"github.com/Jonathan-A-White/millwright/infrastructure/postern"
	"github.com/Jonathan-A-White/millwright/infrastructure/rig"
)

// gristSmokeBook is the record of each app's last grist smoke, kept in the
// vault's tracker, and the alarm a failed one posts to the home's event log.
// The tracker also says which failing examples an open bead names, when it can.
func gristSmokeBook(notes application.GristSmokeNotes, host string) application.GristSmokeBook {
	filed, _ := notes.(application.GristSmokeFiled)
	comments, _ := notes.(application.GristSmokeCommenter)
	mailbox, _ := notes.(application.Mailbox)
	return application.GristSmokeBook{Notes: notes, Filed: filed, Comments: comments, Events: homeEventLog(), Mailbox: mailbox, Host: host}
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
	smoke := application.GristSmoke{
		Grinds: rig.NewGrinds(),
		Send:   application.GristSend{Postern: backend, Cipher: posternCipher(keys), Keys: keys, Now: posternClock},
		Apps:   apps,
		Wait:   wait,
		Out:    out,
	}
	// The talks of an example of two turns are the mill's, on this host's state
	// directory when the mill runs here; a host with no talks there has none to end.
	if stateDir, err := config.GristStateDir(); err == nil {
		smoke.Talks = application.GristTalkSweep{Store: grist.NewTalks(stateDir)}
		smoke.Record = grist.New(stateDir)
	}
	return smoke, nil
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
// on. A progress that is not nil is where the smoke says each example it sends,
// as it sends it, line by line under prefix (mw land's log: mw-gq6.626).
func hostGristSmoke(notes application.GristSmokeNotes, host string, rigs map[string]string, touches application.LandingTouches, progress io.Writer, prefix string) application.GristSmokeAfter {
	apps, err := config.GristApps()
	if err != nil || len(apps) == 0 {
		return application.GristSmokeAfter{}
	}
	paths, err := config.GristSmokePaths()
	if err != nil {
		paths = nil
	}
	var out io.Writer
	if progress != nil {
		out = &prefixedLines{to: progress, prefix: prefix}
	}
	var smoker application.GristAppSmoker
	if made, err := newSmoker(out, 0); err != nil {
		smoker = unavailableSmoker{err}
	} else {
		smoker = made
	}
	// A landing of the factory's own rig is smoked by the mw it builds, not by
	// the one this process is (mw-gq6.339).
	var built application.GristAppSmoker
	if dir := rigs[application.FactoryRig]; dir != "" {
		built = rig.NewBuiltSmoker(dir, smoker).WithProgress(out)
	}
	return application.GristSmokeAfter{
		Touches: touches, Smoke: smoker, Built: built, Book: gristSmokeBook(notes, host),
		Apps: apps, Rigs: rigs, ClientPaths: paths,
	}
}

// prefixedLines writes what it is given to a writer a line at a time, each line
// under a prefix, so that a smoke's progress reads as part of a log of its own.
type prefixedLines struct {
	to      io.Writer
	prefix  string
	pending []byte
}

func (p *prefixedLines) Write(data []byte) (int, error) {
	p.pending = append(p.pending, data...)
	for {
		end := bytes.IndexByte(p.pending, '\n')
		if end < 0 {
			return len(data), nil
		}
		if _, err := fmt.Fprintf(p.to, "%s%s\n", p.prefix, p.pending[:end]); err != nil {
			return len(data), err
		}
		p.pending = p.pending[end+1:]
	}
}

// newGristSmokeCmd builds `mw grist smoke <app>`: an app's grist tested end to
// end.
func newGristSmokeCmd() *cobra.Command {
	var kind, filed string
	var wait time.Duration
	var lift, asJSON bool

	cmd := &cobra.Command{
		Use:   "smoke <app> [<kind>/<example>...]",
		Short: "Send every example of an app's grinds through the live backend and check the answers",
		Long: "smoke sends each scenario of the app's grinds, grinds/examples/<kind>/<name>.json at its rig's\n" +
			"main (config [grist-apps]), through the postern backend as the test key (grist_test_key_file,\n" +
			"default ~/.config/mw/grist-test.key), with the photos beside it, and waits for the answer.\n" +
			"An example is a request (with its schemaVersion beside it, or in it), optional photo file names, and expect: for\n" +
			"each field of the answer (dotted, items.0.name) a value it equals or a check of equals,\n" +
			"is_null, one_of, between ([low, high]: a number within), contains, matches (a pattern), present and all (a list of such checks, each of\n" +
			"which must hold). It passes when the grist is\n" +
			"answered, the answer fits the grind's answer schema and meets every expect; a failure names\n" +
			"the example, the field, what was wanted and what came. An example whose answer is wrong is sent\n" +
			"twice more before it holds: a miss that does not repeat is \"flaky: <example> missed 1 of 3\", holds\n" +
			"nothing and is mailed to the Mayor, who is told \"already named on <bead>, nothing to file\" (and the miss\n" +
			"is added to that bead as a dated note) when an open bead already names the example.\nA kind with no example is a warning.\n" +
			"A smoke the mill would not take for a reason that is not the examples' (the key's daily limit,\n" +
			"its licence, the backend out of reach, the factory's usage limit or its model overloaded) is\n" +
			"\"not run\": it fails nothing and holds nothing.\n\n" +
			"What it finds is kept (mw status shows it): a failure after a landing holds the rig's open\n" +
			"stories until a smoke passes. --lift ends that hold without a smoke. It exits 1 on a failure.\n\n" +
			"A failure whose every failing example is already named on an open bead holds nothing: its line\n" +
			"says \"all filed on <bead>: not held\", mw status shows \"filed on <bead>, not held\" and each miss is added to\n" +
			"the naming bead as a dated note. Name them when\n" +
			"you file the cause, with --filed <bead> and the examples (<kind>/<example>, or <app>/<kind>/<example>):\n" +
			"  mw grist smoke lampas --filed mw-5r3p30.313 tutor/hello tutor/photo\n" +
			"which sets the bead's smoke_examples metadata (comma-separated <app>/<kind>/<example>), and ends the\n" +
			"hold of a failure it now names in full. The bead itself is never held by a failure it names, so the\n" +
			"fix can run. With no example, --filed names the examples the app's last smoke failed on:\n" +
			"  mw grist smoke lampas --filed mw-5r3p30.582\n" +
			"A held rig also runs an open story whose title or description names a failing example as\n" +
			"<app>/<kind>/<example>; mw dispatch says it ran over the hold. Closing the bead ends it: the next\n" +
			"failing smoke on those examples holds again.",
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if filed == "" && len(args) != 1 {
				return fmt.Errorf("mw grist smoke takes the app alone; examples go with --filed <bead>")
			}
			if filed != "" {
				dir, err := config.Vault()
				if err != nil {
					return err
				}
				host, err := config.Host()
				if err != nil {
					return err
				}
				gateway := mwGateway(dir, host)
				named, err := application.GristSmokeFile{Tracker: gateway, Book: gristSmokeBook(gateway, host)}.Run(cmd.Context(), filed, args[0], args[1:])
				if err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "grist smoke: %s names %s\n", filed, strings.Join(named, ", "))
				return nil
			}
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
			report = book.Mark(cmd.Context(), report)
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
	cmd.Flags().StringVar(&filed, "filed", "", "name the examples given after the app on this open bead, which holds their cause (smoke_examples), and run no smoke")
	cmd.Flags().BoolVar(&lift, "lift", false, "end the hold a failed smoke of the app put on its rig's stories, without a smoke")
	return cmd
}
