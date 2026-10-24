package main

import (
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/memoryqueries"
)

// newMemoryCmd builds `mw memory`: the Mayor places, replaces and retires the
// typed facts of a rig kept as facts in the Builder's seat.
func newMemoryCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "memory",
		Short: "Place, replace, retire, list, query, count queries of, eval, demote, promote and make room among the facts the Builder's seat keeps of a rig",
		Long: "A rig kept as facts has a folder in the Builder's seat, seats/builder/rigs/<rig>/, with an\n" +
			"about.md and facts/<slug>.md, one typed fact a file. These verbs are how the Mayor changes\n" +
			"them: each writes files, prints each file it wrote, commits those files alone and pushes\n" +
			"them (a push that does not go is said on stderr, the commit kept). None deletes a fact:\n" +
			"one that is replaced or retired keeps its sentence, and why. migrate turns a rig's one memory file into facts, once.",
		Args: cobra.NoArgs,
	}
	root.AddCommand(newMemoryAddCmd(), newMemorySupersedeCmd(), newMemoryRetireCmd(),
		newMemoryRecheckCmd(), newMemoryListCmd(), newMemoryMigrateCmd(), newMemoryQueryCmd(), newMemoryEvalCmd(),
		newMemoryDemoteCmd(), newMemoryPromoteCmd(), newMemoryStatsCmd(), newMemoryMakeRoomCmd())
	return root
}

// memoryFor reads the vault and the budget a memory verb needs.
func memoryFor(cmd *cobra.Command) (application.Memory, error) {
	dir, err := config.Vault()
	if err != nil {
		return application.Memory{}, err
	}
	budget, err := config.RigMemoryBytes()
	if err != nil {
		return application.Memory{}, err
	}
	files := recordingVault(dir)
	return application.Memory{
		Files: files, Vault: files, Legacy: files, Head: files.Head, Seat: BuilderSeat, Budget: budget, Out: cmd.OutOrStdout(),
		Queries: memoryQueryLog(), Story: os.Getenv("MW_STORY"), Warn: cmd.ErrOrStderr(),
	}, nil
}

// memoryQueryLog is this host's log of memory queries, ~/.local/state/mw/
// memory-queries.jsonl: host state, never the vault. nil when there is no home
// directory to keep it in, which leaves queries unlogged.
func memoryQueryLog() application.MemoryQueryLog {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	return memoryqueries.New(filepath.Join(home, ".local", "state", "mw", memoryqueries.File))
}

func newMemoryAddCmd() *cobra.Command {
	var kind, subject, source, slug string
	var makeRoom bool
	cmd := &cobra.Command{
		Use:   "add <rig> --kind gotcha|decision --subject <s> --source <src> [--slug <slug>] [--make-room] \"<sentence>\"",
		Short: "Write a new current fact for a rig",
		Long: "add writes facts/<slug>.md with status current and since today (UTC). The slug defaults to the\n" +
			"sentence's first five words, lower-case and hyphenated. The source is a bead, rig@sha or\n" +
			"mayor:<date>. After writing it prints 'near: <slug> <first words>' for each current fact of the\n" +
			"same subject, or sharing four long words with the sentence, so a repeated rule is seen, then says\n" +
			"'added <slug>' (with '(near: <slug>)' when it named near facts). A refusal prints 'not added: <why>' and\n" +
			"exits non-zero. It refuses a rig the seat has no folder for, a slug that already has a file\n" +
			"(one flagged recheck is to be superseded or retired), a sentence with a newline and a kind\n" +
			"that is not gotcha or decision. An add that takes the boot render over its budget still writes the\n" +
			"fact, then WARNs with the overage and the command `mw memory make-room <rig> --bytes 0`;\n" +
			"--make-room runs that first, for the new line's own size (see make-room), and adds only if it made the room.",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			memory, err := memoryFor(cmd)
			if err != nil {
				return err
			}
			return memory.Add(cmd.Context(), application.MemoryAdd{
				Rig: args[0], Slug: slug, Subject: subject, Source: source,
				Kind: application.FactKind(kind), Sentence: args[1], MakeRoom: makeRoom,
			})
		},
	}
	cmd.Flags().StringVar(&kind, "kind", "", "gotcha or decision")
	cmd.Flags().StringVar(&subject, "subject", "", "what the fact is about; facts are grouped by it")
	cmd.Flags().StringVar(&source, "source", "", "a bead, rig@sha or mayor:<date>")
	cmd.Flags().StringVar(&slug, "slug", "", "the file's name; default the sentence's first five words")
	cmd.Flags().BoolVar(&makeRoom, "make-room", false, "first demote old gotchas to make room in the boot render for this line (see make-room)")
	_ = cmd.MarkFlagRequired("kind")
	_ = cmd.MarkFlagRequired("subject")
	_ = cmd.MarkFlagRequired("source")
	return cmd
}

func newMemorySupersedeCmd() *cobra.Command {
	var source, slug string
	cmd := &cobra.Command{
		Use:   "supersede <rig> <slug> --source <src> [--slug <new>] \"<sentence>\"",
		Short: "Replace a fact with a new current one, linking the two",
		Long: "supersede writes a new current fact (with supersedes: <old>, the old one's subject and kind)\n" +
			"and marks the old one superseded, with superseded-by: <new>. It refuses a slug that is\n" +
			"unknown or already superseded or retired. A demoted old fact (boot: no) gives a demoted new\n" +
			"one. The eval.md questions that expect the old one are run with the old one superseded (it\n" +
			"ranks after every current fact): one that finds the new one expects it too, and when it no\n" +
			"longer finds the old one the new one replaces it; one that does not find the new one is\n" +
			"named in a 'WARN: eval question at line N' line, since it fails until an expect is added.\n" +
			"It prints the render size against the budget, and WARNs when it is over. A slug made\n" +
			"from the sentence that is taken is refused, saying to pass --slug <new>.",
		Args: cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			memory, err := memoryFor(cmd)
			if err != nil {
				return err
			}
			return memory.Supersede(cmd.Context(), application.MemorySupersede{
				Rig: args[0], Old: args[1], Slug: slug, Source: source, Sentence: args[2],
			})
		},
	}
	cmd.Flags().StringVar(&source, "source", "", "a bead, rig@sha or mayor:<date>")
	cmd.Flags().StringVar(&slug, "slug", "", "the new fact's file name; default its sentence's first five words")
	_ = cmd.MarkFlagRequired("source")
	return cmd
}

func newMemoryRetireCmd() *cobra.Command {
	var reason string
	cmd := &cobra.Command{
		Use:   "retire <rig> <slug> --reason <why>",
		Short: "Retire a fact, keeping its sentence and the reason",
		Long: "retire sets status retired, retired: today and the reason. The file stays. It refuses a slug\n" +
			"that is unknown or already retired.",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			memory, err := memoryFor(cmd)
			if err != nil {
				return err
			}
			return memory.Retire(cmd.Context(), args[0], args[1], reason)
		},
	}
	cmd.Flags().StringVar(&reason, "reason", "", "why the fact is retired")
	_ = cmd.MarkFlagRequired("reason")
	return cmd
}

func newMemoryRecheckCmd() *cobra.Command {
	var why string
	cmd := &cobra.Command{
		Use:   "recheck <rig> <slug> [--why <text>]",
		Short: "Flag a fact as in doubt, so that it is no longer read at boot",
		Long: "recheck sets status recheck, appending --why to the reason. A Builder reads current facts\n" +
			"only; supersede or retire settles a fact in doubt, and add refuses its slug meanwhile.",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			memory, err := memoryFor(cmd)
			if err != nil {
				return err
			}
			return memory.Recheck(cmd.Context(), args[0], args[1], why)
		},
	}
	cmd.Flags().StringVar(&why, "why", "", "what is in doubt")
	return cmd
}

func newMemoryListCmd() *cobra.Command {
	var status string
	var oldest, current bool
	cmd := &cobra.Command{
		Use:   "list <rig> [--current | --status current|recheck|superseded|retired] [--oldest]",
		Short: "List a rig's facts and the size of what a Builder reads against the budget",
		Long: "list prints one line a fact, 'slug  status  kind  [subject]  since  source', current facts\n" +
			"first and by subject (--oldest: by since, oldest first), then a line with the size of the\n" +
			"render a Builder reads against the budget (config rig_memory_bytes). --current lists only the\n" +
			"current facts, leaving out the recheck, superseded and retired ones (the render size is still\n" +
			"printed); it cannot be combined with --status. Exit 0.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			memory, err := memoryFor(cmd)
			if err != nil {
				return err
			}
			return memory.List(cmd.Context(), application.MemoryList{
				Rig: args[0], Status: application.FactStatus(status), Oldest: oldest, Current: current,
			})
		},
	}
	cmd.Flags().StringVar(&status, "status", "", "only facts of this status")
	cmd.Flags().BoolVar(&oldest, "oldest", false, "oldest first, by since")
	cmd.Flags().BoolVar(&current, "current", false, "only current facts: no recheck, superseded or retired one")
	return cmd
}

func newMemoryMigrateCmd() *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "migrate <rig> [--dry-run]",
		Short: "Turn a rig's one memory file and its archive into an about text and facts",
		Long: "migrate reads seats/builder/rigs/<rig>.md and <rig>-archive.md and writes about.md and one\n" +
			"fact file each under seats/builder/rigs/<rig>/. The head of the memory file (the whole of it\n" +
			"under 600 bytes, else the first 600 cut at a sentence end, with a warning) is the about\n" +
			"text; each '- ' line, with the lines indented under it joined to it, is a current fact, and each of the archive's a retired one, with the\n" +
			"date and name of the heading it sat under. A fact's source is the last bead id in brackets\n" +
			"on its line, else mayor:<file>@<vault HEAD>; its kind decision under a heading with 'Before\n" +
			"you start' or 'Decided' in it, else gotcha; its subject the first path-like token (a <placeholder> kept\n" +
			"whole) or one-word backticked token of at most 40 characters, else general; its since a date in the line, else today. A line it cannot place is\n" +
			"printed as 'not placed' and left out. --dry-run prints every fact and writes nothing.\n" +
			"Otherwise it removes the two files it read, then commits what it wrote and removed (message\n" +
			"'mw memory migrate <rig>') and pushes it itself. It\n" +
			"refuses a rig that already has a facts folder, and one with no memory file.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			memory, err := memoryFor(cmd)
			if err != nil {
				return err
			}
			return memory.Migrate(cmd.Context(), application.MemoryMigrate{Rig: args[0], DryRun: dryRun})
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print every fact it would write, and write nothing")
	return cmd
}

func newMemoryQueryCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "query <rig> <term>...",
		Short: "Find a rig's facts by term, of every status, with the facts related to them",
		Long: "query prints each fact of the rig in which every term starts a word (or is a word) of its\n" +
			"subject, sentence, slug or source, case aside, whatever its status: 'slug  status  [subject]\n" +
			"sentence (source)', a superseded fact adding 'superseded by <slug>' and a retired one\n" +
			"'retired <date>: <reason>'. Current facts come first. After the hits come the facts with a\n" +
			"hit's subject and those along a hit's supersedes chain, each marked 'related'. It is how a\n" +
			"Builder finds what boot does not show. Each run is logged (rig, terms, hits, the story from\n" +
			"$MW_STORY) to ~/.local/state/mw/memory-queries.jsonl, host state and not the vault: see\n" +
			"stats. It reads files and runs no git command, so it may be run from a worktree. Exit 1, saying 'no fact matches', when nothing hits.",
		Args: cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			memory, err := memoryFor(cmd)
			if err != nil {
				return err
			}
			return memory.Query(cmd.Context(), application.MemoryQuery{Rig: args[0], Terms: args[1:]})
		},
	}
}

func newMemoryEvalCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "eval <rig>",
		Short: "Run a rig's scripted questions against its facts: the pass bar for its memory",
		Long: "eval reads seats/builder/rigs/<rig>/eval.md, blocks of 'Q: <what a Builder would ask>' and\n" +
			"'expect: <slug>[, <slug>...]' separated by blank lines ('#' lines are comments), and runs\n" +
			"each Q as a query (its words as terms, the stop words a, an, the, to, of, in, on, is, does,\n" +
			"how, what, when and where dropped). It prints 'PASS <rank> <slug>  Q' for each expected fact\n" +
			"found in the top three results and 'FAIL <slug> not in top 3  Q' for each that is not, and\n" +
			"exits 1 when any question fails. A rig with no eval.md prints 'no eval' and exits 0. It reads\n" +
			"files and runs no git command. mw status says 'eval failing' for a rig that fails.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			memory, err := memoryFor(cmd)
			if err != nil {
				return err
			}
			return memory.Eval(cmd.Context(), args[0])
		},
	}
}

func newMemoryDemoteCmd() *cobra.Command {
	var question string
	cmd := &cobra.Command{
		Use:   "demote <rig> <slug> [--question \"<terms>\"]",
		Short: "Keep a current fact out of the boot render, as long as the rig's eval still finds it",
		Long: "demote sets boot: no on a current fact, so that boot does not show it. The fact stays\n" +
			"current, and query shows it and list marks it 'boot:no'. It is refused, saying 'no eval\n" +
			"question finds <slug>; add one to eval.md first', unless some question in the rig's eval.md\n" +
			"has the slug in its expect and that question passes (see eval). It also refuses a slug that\n" +
			"is unknown, not current or already demoted. --question appends 'Q: <terms>' and\n" +
			"'expect: <slug>' to eval.md first, checks that the question passes (else nothing is\n" +
			"written), demotes the fact, and commits eval.md and the fact in one commit.",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			memory, err := memoryFor(cmd)
			if err != nil {
				return err
			}
			return memory.Demote(cmd.Context(), args[0], args[1], question)
		},
	}
	cmd.Flags().StringVar(&question, "question", "", "a question that should find the fact; appended to eval.md with the fact as its expect")
	return cmd
}

func newMemoryPromoteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "promote <rig> <slug>",
		Short: "Put a demoted fact back into the boot render",
		Long: "promote removes boot: no from a fact that demote kept out of boot. It refuses a slug that\n" +
			"is unknown or not demoted.",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			memory, err := memoryFor(cmd)
			if err != nil {
				return err
			}
			return memory.Promote(cmd.Context(), args[0], args[1])
		},
	}
}

func newMemoryStatsCmd() *cobra.Command {
	var since string
	cmd := &cobra.Command{
		Use:   "stats <rig> [--since 7d]",
		Short: "Count how often a rig's memory was queried, and which subjects",
		Long: "stats reads this host's log of mw memory query (~/.local/state/mw/memory-queries.jsonl) and\n" +
			"prints, for the rig since --since (a number of days such as 7d, the default, or a day), how\n" +
			"many queries there were and how many found nothing, then the subjects of the facts they hit,\n" +
			"the most queried first. A Builder's boot names the ten most queried subjects.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			memory, err := memoryFor(cmd)
			if err != nil {
				return err
			}
			return memory.Stats(cmd.Context(), application.MemoryStats{Rig: args[0], Since: since})
		},
	}
	cmd.Flags().StringVar(&since, "since", "7d", "a number of days (7d), a day (2026-10-09) or a UTC time")
	return cmd
}

func newMemoryMakeRoomCmd() *cobra.Command {
	var bytes int
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "make-room <rig> --bytes N [--dry-run]",
		Short: "Demote the oldest eligible boot gotchas until the render has N bytes free under the budget",
		Long: "make-room chooses by a fixed rule, at no cost in tokens: only a current gotcha in the boot\n" +
			"render, never a decision, never a sentence beginning 'His ' or 'Decided by the Governor', never\n" +
			"one whose since is within the last 2 days; the oldest since first (the subject `mw memory stats`\n" +
			"shows least queried among equals, then slug order). Each goes through demote with the first eight\n" +
			"words of its own sentence as the question; one that question would not put in the top 3 of eval is\n" +
			"skipped (named 'skipped <slug>') for the next. It prints the render before, each demotion and the\n" +
			"render after. When the facts that qualify cannot free N bytes it changes nothing and exits 1.\n" +
			"--bytes 0 gets the render under the budget. --dry-run prints 'would demote <slug>' for each\n" +
			"and the render before and after, and writes nothing.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			memory, err := memoryFor(cmd)
			if err != nil {
				return err
			}
			return memory.MakeRoom(cmd.Context(), application.MemoryMakeRoom{Rig: args[0], Bytes: bytes, DryRun: dryRun})
		},
	}
	cmd.Flags().IntVar(&bytes, "bytes", 0, "how many bytes the boot render should have free under the budget")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print what would be demoted, and write nothing")
	_ = cmd.MarkFlagRequired("bytes")
	return cmd
}
