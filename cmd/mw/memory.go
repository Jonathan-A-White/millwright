package main

import (
	"github.com/spf13/cobra"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/vault"
)

// newMemoryCmd builds `mw memory`: the Mayor places, replaces and retires the
// typed facts of a rig kept as facts in the Builder's seat.
func newMemoryCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "memory",
		Short: "Place, replace, retire, list and query the facts the Builder's seat keeps of a rig",
		Long: "A rig kept as facts has a folder in the Builder's seat, seats/builder/rigs/<rig>/, with an\n" +
			"about.md and facts/<slug>.md, one typed fact a file. These verbs are how the Mayor changes\n" +
			"them: each writes files, prints each file it wrote, and runs no git command (the Mayor\n" +
			"commits). None deletes a fact: one that is replaced or retired keeps its sentence, and\n" +
			"why. migrate turns a rig's one memory file into facts, once.",
		Args: cobra.NoArgs,
	}
	root.AddCommand(newMemoryAddCmd(), newMemorySupersedeCmd(), newMemoryRetireCmd(),
		newMemoryRecheckCmd(), newMemoryListCmd(), newMemoryMigrateCmd(), newMemoryQueryCmd())
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
	files := vault.New(dir)
	return application.Memory{
		Files: files, Legacy: files, Head: files.Head, Seat: BuilderSeat, Budget: budget, Out: cmd.OutOrStdout(),
	}, nil
}

func newMemoryAddCmd() *cobra.Command {
	var kind, subject, source, slug string
	cmd := &cobra.Command{
		Use:   "add <rig> --kind gotcha|decision --subject <s> --source <src> [--slug <slug>] \"<sentence>\"",
		Short: "Write a new current fact for a rig",
		Long: "add writes facts/<slug>.md with status current and since today (UTC). The slug defaults to the\n" +
			"sentence's first five words, lower-case and hyphenated. The source is a bead, rig@sha or\n" +
			"mayor:<date>. It refuses a rig the seat has no folder for, a slug that already has a file\n" +
			"(one flagged recheck is to be superseded or retired), a sentence with a newline and a kind\n" +
			"that is not gotcha or decision.",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			memory, err := memoryFor(cmd)
			if err != nil {
				return err
			}
			return memory.Add(cmd.Context(), application.MemoryAdd{
				Rig: args[0], Slug: slug, Subject: subject, Source: source,
				Kind: application.FactKind(kind), Sentence: args[1],
			})
		},
	}
	cmd.Flags().StringVar(&kind, "kind", "", "gotcha or decision")
	cmd.Flags().StringVar(&subject, "subject", "", "what the fact is about; facts are grouped by it")
	cmd.Flags().StringVar(&source, "source", "", "a bead, rig@sha or mayor:<date>")
	cmd.Flags().StringVar(&slug, "slug", "", "the file's name; default the sentence's first five words")
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
			"unknown or already superseded or retired.",
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
	var oldest bool
	cmd := &cobra.Command{
		Use:   "list <rig> [--status current|recheck|superseded|retired] [--oldest]",
		Short: "List a rig's facts and the size of what a Builder reads against the budget",
		Long: "list prints one line a fact, 'slug  status  kind  [subject]  since  source', current facts\n" +
			"first and by subject (--oldest: by since, oldest first), then a line with the size of the\n" +
			"render a Builder reads against the budget (config rig_memory_bytes). Exit 0.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			memory, err := memoryFor(cmd)
			if err != nil {
				return err
			}
			return memory.List(cmd.Context(), application.MemoryList{
				Rig: args[0], Status: application.FactStatus(status), Oldest: oldest,
			})
		},
	}
	cmd.Flags().StringVar(&status, "status", "", "only facts of this status")
	cmd.Flags().BoolVar(&oldest, "oldest", false, "oldest first, by since")
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
			"text; each '- ' line is a current fact, and each of the archive's a retired one, with the\n" +
			"date and name of the heading it sat under. A fact's source is the last bead id in brackets\n" +
			"on its line, else mayor:<file>@<vault HEAD>; its kind decision under a heading with 'Before\n" +
			"you start' or 'Decided' in it, else gotcha; its subject the first path-like or backticked\n" +
			"token, else general; its since a date in the line, else today. A line it cannot place is\n" +
			"printed as 'not placed' and left out. --dry-run prints every fact and writes nothing.\n" +
			"Otherwise it removes the two files it read (no git command is run: the Mayor commits). It\n" +
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
			"Builder finds what boot does not show. It reads files and runs no git command, so it may be\n" +
			"run from a worktree. Exit 1, saying 'no fact matches', when nothing hits.",
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
