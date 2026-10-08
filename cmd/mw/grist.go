package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/claude"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/grist"
	"github.com/Jonathan-A-White/millwright/infrastructure/hostlock"
	"github.com/Jonathan-A-White/millwright/infrastructure/postern"
	"github.com/Jonathan-A-White/millwright/infrastructure/rig"
	"github.com/Jonathan-A-White/millwright/infrastructure/scorer"
)

// newGristCmd builds `mw grist`: the mill, the factory's side of the AI work
// apps send it (postern's docs/protocol.md section 18).
func newGristCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "grist",
		Short: "The mill: answer the AI work apps send the factory",
		Args:  cobra.NoArgs,
	}
	root.AddCommand(newGristKeyCmd())
	root.AddCommand(newGristGrindCmd())
	root.AddCommand(newGristSendCmd())
	root.AddCommand(newGristEvalCmd())
	root.AddCommand(newGristScoreCmd())
	root.AddCommand(newGristRunsCmd())
	root.AddCommand(newGristStatsCmd())
	return root
}

// gristKeys is the mill key file config points at.
func gristKeys() (*postern.KeyFile, error) {
	path, err := config.GristKeyFile()
	if err != nil {
		return nil, err
	}
	return postern.New(path), nil
}

// gristGrindSlots is this host's grind slot locks, which mw dispatch counts
// as one of its sessions each while they are held: the first, and the others
// up to the [grist] concurrency. A host whose grist state directory cannot be
// named has none, and counts none. A [grist] table that cannot be read counts
// the default number of slots: only the slots held are counted.
func gristGrindSlots() (first application.GristLock, more []application.GristLock) {
	dir, err := config.GristStateDir()
	if err != nil {
		return nil, nil
	}
	concurrency := config.DefaultGristConcurrency
	if settings, err := config.Grist(); err == nil {
		concurrency = settings.Concurrency
	}
	slots := hostlock.GrindSlots(dir, concurrency)
	return slots[0], slots[1:]
}

// newGristKeyCmd builds `mw grist key`: the mill key, made once, and its
// public half.
func newGristKeyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "key",
		Short: "Make the mill key if there is none, and print its public key and fingerprint",
		Long: "key generates the mill key (config grist_key_file, default ~/.config/mw/mill.key) when\n" +
			"there is none yet, 0600, a key of its own and never the Mayor's; then prints its public key\n" +
			"and its fingerprint. The postern backend is told the public key as POSTERN_MILL_KEY. It\n" +
			"never prints the private key, and never overwrites a key already there.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			keys, err := gristKeys()
			if err != nil {
				return err
			}
			_, err = application.GristKey{Keys: keys, Out: cmd.OutOrStdout()}.Run(cmd.Context())
			return err
		},
	}
}

// newGristGrindCmd builds `mw grist grind`: one pass of the mill.
func newGristGrindCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "grind",
		Short: "Answer every grist waiting for the mill key, then stop",
		Long: "grind reads what the postern backend holds for the mill key since its last pass and answers\n" +
			"each grist once, sealed to its sender: refused when the sender's licence does not open the\n" +
			"app, the app's grind (grinds/<kind>.json at its rig's local main, config [grist-apps]) is\n" +
			"unknown, or the grist is past a grind's or the factory's limits (config [grist]); otherwise\n" +
			"ground in one short Claude Code session with no seat, answered or failed. A grind takes one\n" +
			"of this host's cap, first come first served: with none free, the grist waits for the next\n" +
			"pass, or for the next mw dispatch tick on the host that is home. Each grist handled is one line of grinds.jsonl in grist_state_dir, and its photos are\n" +
			"deleted from the backend once it is answered. The backend's POSTERN_ON_GRIST runs it.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			mill, err := newMill(cmd.OutOrStdout())
			if err != nil {
				return err
			}
			_, err = mill.Run(cmd.Context())
			return err
		},
	}
}

// newGristSendCmd builds `mw grist send`: a grist sent from the terminal as
// the key it is given, and, with --wait, its answer.
func newGristSendCmd() *cobra.Command {
	var keyFile, app, kind, request, backend, version string
	var photos []string
	var wait time.Duration

	cmd := &cobra.Command{
		Use:   "send",
		Short: "Send a grist as a given key, with photos, and wait for the answer",
		Long: "send does what an app's phone does, from a terminal (postern's docs/protocol.md section 19):\n" +
			"it proves --key to the postern backend, asks it (GET /api/me) which key is the mill's, seals\n" +
			"each --photo and the --request to that key, uploads the photos (POST /api/blobs) and posts\n" +
			"the grist (class grist). Without --wait it prints the grist's id and stops. With --wait it\n" +
			"pages the backend for the answer whose re is that id, opens it and prints it as JSON, and\n" +
			"leaves with 0 when it is answered, 1 when it is refused or failed (saying why on standard\n" +
			"error), and 2 when no answer came in time; the grist keeps waiting at the backend.\n\n" +
			"It reads no key but --key, which has no default and is a file holding the key to send as\n" +
			"(a key the backend holds a licence for), and it never prints a private key.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			keys := postern.New(keyFile)
			if backend == "" {
				var err error
				if backend, err = config.PosternBackend(); err != nil {
					return err
				}
			}
			send := application.GristSend{
				Postern: postern.NewHTTP(backend, keys),
				Cipher:  posternCipher(keys),
				Keys:    keys,
				Now:     posternClock,
				Out:     cmd.OutOrStdout(),
				Log:     cmd.ErrOrStderr(),
			}
			_, err := send.Run(cmd.Context(), application.GristSendRequest{
				App: app, Kind: kind, Version: version, RequestFile: request, Photos: photos, Wait: wait,
			})
			return err
		},
	}
	cmd.Flags().StringVar(&keyFile, "key", "", "the key file to send as, a WIF key the backend holds a licence for (required; no default)")
	cmd.Flags().StringVar(&app, "app", "", "the app the grist is for, as its licence names it, e.g. cairn (required)")
	cmd.Flags().StringVar(&kind, "kind", "", "the kind of grist, which grind of the app to run, e.g. sweep (required)")
	cmd.Flags().StringVar(&request, "request", "", "a JSON file holding the app's request, in the app's own schema (required)")
	cmd.Flags().StringArrayVar(&photos, "photo", nil, "a photo to send with it, .jpg, .png or .webp, sealed to the mill key (repeatable, at most 4)")
	cmd.Flags().StringVar(&backend, "backend", "", "the postern backend's URL (default: postern_backend)")
	cmd.Flags().DurationVar(&wait, "wait", 0, "how long to wait for the answer, e.g. 5m; 0 sends and does not wait")
	cmd.Flags().StringVar(&version, "schema-version", "", "the version of the app's request schema (default: the request's schemaVersion)")
	for _, name := range []string{"key", "app", "kind", "request"} {
		_ = cmd.MarkFlagRequired(name)
	}
	return cmd
}

// newGristEvalCmd builds `mw grist eval`: a grind tried on photos whose
// answers are known, on each model named, and scored.
func newGristEvalCmd() *cobra.Command {
	var grind, photos, effort, out string
	var models []string

	cmd := &cobra.Command{
		Use:   "eval",
		Short: "Try a grind on photos with known answers, on each model named, and score it",
		Long: "eval runs one grind over a directory of photos, each with a <name>.txt beside it listing what\n" +
			"is really in it (one name a line; a \"Container: X\" line names a container whose indented\n" +
			"lines are its items), on each model named, one session at a time through the same grinder\n" +
			"and with the same prompt the mill gives one photo. Each answer is scored against its .txt:\n" +
			"hits, misses (expected, not answered), extras (answered, not expected), items it was unsure\n" +
			"of (still answered), seconds, cost and tokens, per photo and model, then per model. A grind\n" +
			"that fails is a row with its error and counts of zero. A photo with no .txt is skipped.\n\n" +
			"It writes eval-<UTC timestamp>.jsonl (every row, with the full answer) and .md (the tables)\n" +
			"under --out. It never touches the postern backend, the mill's state or the dispatch cap, and\n" +
			"the photos and answers never enter a bead or the vault.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ceilings, err := config.Grist()
			if err != nil {
				return err
			}
			if out == "" {
				stateDir, err := config.GristStateDir()
				if err != nil {
					return err
				}
				out = filepath.Join(stateDir, "eval")
			}
			_, err = application.GristEval{
				Grinder: claude.NewGrinder(),
				Ceilings: application.GristCeilings{
					Models: ceilings.Models, Efforts: ceilings.Efforts, MaxAttachments: ceilings.MaxAttachments,
					MaxAttachmentBytes: ceilings.MaxAttachmentBytes, DailyLimit: ceilings.DailyLimit,
					Timeout: ceilings.Timeout,
				},
				Out: cmd.OutOrStdout(),
			}.Run(cmd.Context(), application.GristEvalRequest{
				Grind: grind, Photos: photos, Models: models, Effort: effort, Out: out,
			})
			return err
		},
	}
	cmd.Flags().StringVar(&grind, "grind", "", "a grinds/<kind>.json in a rig checkout, read from its working tree; its instructions and schema are read from the same checkout (required)")
	cmd.Flags().StringVar(&photos, "photos", "", "a directory of .jpg, .jpeg, .png or .webp photos, each with a <name>.txt beside it (required)")
	cmd.Flags().StringSliceVar(&models, "models", nil, "the models to try, comma separated, each one the [grist] models allows (default: the grind file's model)")
	cmd.Flags().StringVar(&effort, "effort", "", "the effort to run at: low, medium, high, xhigh or max (default: the grind file's)")
	cmd.Flags().StringVar(&out, "out", "", "the directory the .jsonl and .md are written to (default: eval under grist_state_dir, ~/.local/state/mw/grist/eval)")
	for _, name := range []string{"grind", "photos"} {
		_ = cmd.MarkFlagRequired(name)
	}
	return cmd
}

// newGristScoreCmd builds `mw grist score`: one reading scored word by word
// by one engine, the result printed as JSON.
func newGristScoreCmd() *cobra.Command {
	var engine, target, audio, lang string

	cmd := &cobra.Command{
		Use:   "score",
		Short: "Score a recording of someone reading a text aloud, word by word, and print it as JSON",
		Long: "score hands --audio, a recording of someone reading --target aloud, to the scoring engine\n" +
			"named by --engine and prints its ReadingResult as JSON: for each word the phonemes expected\n" +
			"and produced, whether it was read right, left out, added, mispronounced or hesitated over,\n" +
			"and its accuracy, then the reading's accuracy as a whole. The engines are the ones the\n" +
			"[scorers] table of the config file names (engines, default local); a name that is not one of\n" +
			"them is refused, saying which are. The local engine needs ffmpeg and the scorer of\n" +
			"contrib/scorer running at local_url; the azure engine needs ffmpeg, azure_key_file (mode 0600)\n" +
			"and azure_region, and sends the audio to Azure Speech. The contract is docs/scorers.md.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			scorers, err := newScorers()
			if err != nil {
				return err
			}
			_, err = application.GristScore{Scorers: scorers, Out: cmd.OutOrStdout()}.Run(cmd.Context(), application.GristScoreRequest{
				Engine: engine, Target: target, AudioFile: audio, Lang: lang,
			})
			return err
		},
	}
	cmd.Flags().StringVar(&engine, "engine", "", "the scoring engine, one of the [scorers] engines of the config file, e.g. local (required)")
	cmd.Flags().StringVar(&target, "target", "", "the text the reader was asked to read aloud (required)")
	cmd.Flags().StringVar(&audio, "audio", "", "the recording of the reading, e.g. clip.webm; anything ffmpeg reads (required)")
	cmd.Flags().StringVar(&lang, "lang", "en", "the language of the target, as a language code")
	for _, name := range []string{"engine", "target", "audio"} {
		_ = cmd.MarkFlagRequired(name)
	}
	return cmd
}

// newGristRunsCmd builds `mw grist runs`: the raw record the mill keeps of
// every grind, listed.
func newGristRunsCmd() *cobra.Command {
	var since string

	cmd := &cobra.Command{
		Use:   "runs",
		Short: "List the raw records the mill keeps of its grinds",
		Long: "runs lists every grind the mill has kept a record of, oldest first: its txid, the kind of grist,\n" +
			"the model, the seconds it took from the grist taken up to the answer, and whether it was\n" +
			"answered, refused or failed. Each is a directory runs/<txid>/ of grist_state_dir holding the\n" +
			"request the session was given, the attachments, what the scorers said, the answer and the\n" +
			"timing; mw never deletes them. --since is how far back to look: a duration (24h), a date\n" +
			"(2026-10-07) or a time (2026-10-07T09:00:00Z).",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			from, err := parseRunsSince(since, time.Now())
			if err != nil {
				return err
			}
			stateDir, err := config.GristStateDir()
			if err != nil {
				return err
			}
			_, err = application.GristRuns{Runs: grist.NewRuns(stateDir), Out: cmd.OutOrStdout()}.Run(cmd.Context(), from)
			return err
		},
	}
	cmd.Flags().StringVar(&since, "since", "", "list only the runs since this: a duration such as 24h, a date, or an RFC 3339 time (default: all)")
	return cmd
}

// newGristStatsCmd builds `mw grist stats`: where the wait of one kind of grist
// goes, from the timing the mill kept of its runs.
func newGristStatsCmd() *cobra.Command {
	var kind string
	var last int

	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Print the median and worst of each phase of the last runs of one kind of grist",
		Long: "stats reads the timing.json the mill kept of the last runs of --kind and prints, for each phase,\n" +
			"the median and the worst seconds and how many runs had it: queue (the grist sent to taken up),\n" +
			"scoring and each scorer engine within it, harness and total (taken up to answered).\n" +
			"A phase none of those runs had is left out. Only kinds, times and ids are read.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			stateDir, err := config.GristStateDir()
			if err != nil {
				return err
			}
			_, err = application.GristStats{Runs: grist.NewRuns(stateDir), Out: cmd.OutOrStdout()}.Run(cmd.Context(), kind, last)
			return err
		},
	}
	cmd.Flags().StringVar(&kind, "kind", "", "the kind of grist, e.g. tutor-turn (required)")
	cmd.Flags().IntVar(&last, "last", application.GristStatsLast, "how many of the latest runs of the kind to look at")
	_ = cmd.MarkFlagRequired("kind")
	return cmd
}

// parseRunsSince is --since as a time: empty is the beginning of time.
func parseRunsSince(text string, now time.Time) (time.Time, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return time.Time{}, nil
	}
	if d, err := time.ParseDuration(text); err == nil {
		return now.Add(-d), nil
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02"} {
		if t, err := time.Parse(layout, text); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("mw grist runs: --since %q is not a duration (24h), a date (2026-10-07) or an RFC 3339 time", text)
}

// azureEndpointEnv replaces the azure engine's address, so a test can stub
// Azure; it is not a setting.
const azureEndpointEnv = "MW_AZURE_ENDPOINT"

// newScorers is the registry of the engines the [scorers] table of the config
// file names. An engine this mw has no adapter for is an error, so a
// misspelt name in the config is not mistaken for one that is merely down.
func newScorers() (application.ScorerRegistry, error) {
	settings, err := config.Scorers()
	if err != nil {
		return application.ScorerRegistry{}, err
	}
	engines := map[string]application.Scorer{}
	for _, name := range settings.Engines {
		switch name {
		case "local":
			engines[name] = scorer.NewLocal(settings.LocalURL)
		case "azure":
			azure := scorer.NewAzure(settings.AzureKeyFile, settings.AzureRegion)
			azure.Endpoint = os.Getenv(azureEndpointEnv)
			engines[name] = azure
		default:
			return application.ScorerRegistry{}, fmt.Errorf("the [%s] table of %s names the engine %q: this mw has no such engine (it has local and azure)", config.ScorersTable, config.File, name)
		}
	}
	return application.NewScorerRegistry(engines), nil
}

// newMill is the mill as this host is configured to run it: what
// `mw grist grind` runs, and what the dispatch tick runs on the host that is
// home. It prints its report to out.
func newMill(out io.Writer) (application.GristGrind, error) {
	dir, err := config.Vault()
	if err != nil {
		return application.GristGrind{}, err
	}
	host, err := config.Host()
	if err != nil {
		return application.GristGrind{}, err
	}
	atOnce, err := config.Cap()
	if err != nil {
		return application.GristGrind{}, err
	}
	keys, err := gristKeys()
	if err != nil {
		return application.GristGrind{}, err
	}
	backend, err := posternBackend(keys)
	if err != nil {
		return application.GristGrind{}, err
	}
	stateDir, err := config.GristStateDir()
	if err != nil {
		return application.GristGrind{}, err
	}
	apps, err := config.GristApps()
	if err != nil {
		return application.GristGrind{}, err
	}
	ceilings, err := config.Grist()
	if err != nil {
		return application.GristGrind{}, err
	}
	governorKey, err := config.PosternGovernorKey()
	if err != nil {
		return application.GristGrind{}, err
	}
	scorers, err := newScorers()
	if err != nil {
		return application.GristGrind{}, err
	}
	slots := hostlock.GrindSlots(stateDir, ceilings.Concurrency)
	return application.GristGrind{
		Postern:      backend,
		Cipher:       postern.NewCipher(keys),
		Keys:         keys,
		State:        grist.New(stateDir),
		Scorers:      scorers,
		Runs:         grist.NewRuns(stateDir),
		Grinds:       rig.NewGrinds(),
		Grinder:      claude.NewGrinder(),
		Tracker:      mwGateway(dir, host),
		Pass:         hostlock.NewTry(stateDir, hostlock.PassFile),
		Grinding:     slots[0],
		MoreGrinding: slots[1:],
		Host:         host,
		Cap:          atOnce,
		Apps:         apps,
		GovernorKey:  governorKey,
		Ceilings: application.GristCeilings{
			Models: ceilings.Models, Efforts: ceilings.Efforts, MaxAttachments: ceilings.MaxAttachments,
			MaxAttachmentBytes: ceilings.MaxAttachmentBytes, DailyLimit: ceilings.DailyLimit,
			Timeout: ceilings.Timeout,
		},
		Out: out,
	}, nil
}
