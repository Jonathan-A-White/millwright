package main

import (
	"context"
	"fmt"
	"io"
	"path/filepath"

	millwright "github.com/Jonathan-A-White/millwright"
	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/beads"
	"github.com/Jonathan-A-White/millwright/infrastructure/claude"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"

	"github.com/spf13/cobra"
)

// newTodoCmd builds `mw todo`: the Mayor's hand on the Governor's To do board.
func newTodoCmd() *cobra.Command {
	todo := &cobra.Command{
		Use:   "todo",
		Short: "Work the Governor's To do board",
	}
	todo.AddCommand(newTodoDueCmd())
	todo.AddCommand(newTodoSizeCmd())
	todo.AddCommand(newTodoThenCmd())
	todo.AddCommand(newTodoRankCmd())
	todo.AddCommand(newTodoSearchCmd())
	return todo
}

func newTodoDueCmd() *cobra.Command {
	var clear bool
	cmd := &cobra.Command{
		Use:   "due <bead> (<time> | --clear)",
		Short: "Set or clear when a To do card is due",
		Long: "due gives a To do card (an open bead labelled hitl:todo) the due time <time>, RFC 3339,\n" +
			"written as the card's todo_due metadata, or with --clear takes it off. The view carries\n" +
			"it, and once it has passed the follower sends the Governor one Postern push naming the card.\n" +
			"A card is reminded once for each due time it is given.\n\n" +
			"  mw todo due mw-ab12 2026-10-15T17:00:00-04:00\n" +
			"  mw todo due mw-ab12 --clear\n\n" +
			"A bead that is not an open card, and a time that is not RFC 3339, are refused and nothing\n" +
			"is written.",
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			when := ""
			switch {
			case clear && len(args) == 2:
				return fmt.Errorf("mw todo due: give a time or --clear, not both")
			case !clear && len(args) == 1:
				return fmt.Errorf("mw todo due: give a time, or --clear to take the due time off")
			case len(args) == 2:
				when = args[1]
			}
			gateway, err := beads.FromConfig()
			if err != nil {
				return err
			}
			_, err = application.TodoDue{Tracker: gateway, Out: cmd.OutOrStdout()}.Run(cmd.Context(), args[0], when)
			return err
		},
	}
	cmd.Flags().BoolVar(&clear, "clear", false, "take the due time off the card")
	return cmd
}

func newTodoSizeCmd() *cobra.Command {
	var clear bool
	cmd := &cobra.Command{
		Use:   "size <bead> (quick|hour|half-day | --clear)",
		Short: "Set or clear how big a To do card is",
		Long: "size gives a To do card (an open bead labelled hitl:todo) a size, quick, hour or half-day,\n" +
			"written as the card's todo_size metadata, or with --clear takes it off. The view carries it.\n\n" +
			"  mw todo size mw-ab12 hour\n" +
			"  mw todo size mw-ab12 --clear\n\n" +
			"A bead that is not an open card, and a size that is not one of the three, are refused and\n" +
			"nothing is written.",
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			size := ""
			switch {
			case clear && len(args) == 2:
				return fmt.Errorf("mw todo size: give a size or --clear, not both")
			case !clear && len(args) == 1:
				return fmt.Errorf("mw todo size: give a size (quick, hour or half-day), or --clear to take it off")
			case len(args) == 2:
				size = args[1]
			}
			gateway, err := beads.FromConfig()
			if err != nil {
				return err
			}
			_, err = application.TodoSize{Tracker: gateway, Out: cmd.OutOrStdout()}.Run(cmd.Context(), args[0], size)
			return err
		},
	}
	cmd.Flags().BoolVar(&clear, "clear", false, "take the size off the card")
	return cmd
}

func newTodoThenCmd() *cobra.Command {
	var clear bool
	cmd := &cobra.Command{
		Use:   "then <bead> (<text> | --clear)",
		Short: "Set or clear what follows a To do card",
		Long: "then gives a To do card (an open bead labelled hitl:todo) a Then line, free text of at most\n" +
			"200 characters, written as the card's todo_then metadata, or with --clear takes it off. The\n" +
			"view carries it, and when the Governor marks the card Done the Then line is filed as a card\n" +
			"of its own on the same list.\n\n" +
			"  mw todo then mw-ab12 \"Fit the hose to the tap\"\n" +
			"  mw todo then mw-ab12 --clear\n\n" +
			"A bead that is not an open card, and a line over 200 characters, are refused and nothing is\n" +
			"written.",
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			text := ""
			switch {
			case clear && len(args) == 2:
				return fmt.Errorf("mw todo then: give a line or --clear, not both")
			case !clear && len(args) == 1:
				return fmt.Errorf("mw todo then: give a line, or --clear to take the Then line off")
			case len(args) == 2:
				text = args[1]
			}
			gateway, err := beads.FromConfig()
			if err != nil {
				return err
			}
			_, err = application.TodoThen{Tracker: gateway, Out: cmd.OutOrStdout()}.Run(cmd.Context(), args[0], text)
			return err
		},
	}
	cmd.Flags().BoolVar(&clear, "clear", false, "take the Then line off the card")
	return cmd
}

func newTodoRankCmd() *cobra.Command {
	var dryRun bool
	var about string
	cmd := &cobra.Command{
		Use:   "rank [--dry-run] [--about <file>]",
		Short: "Put every To do card in a row, now, by one model pass",
		Long: "rank runs one short model pass over the Governor's To do board: every open card (an open bead\n" +
			"labelled hitl:todo), the cards he closed in the last 30 days and his about file, the vault's\n" +
			application.TodoAboutFile + " (or --about <file>). The model puts each open card in a row, now,\n" +
			"make-time, later or let-go, with a one-line why, and mw writes them as the card's todo_row and\n" +
			"todo_row_why where they changed, marks a starred card past its due time todo_slipped once, and\n" +
			"writes the pass log runs/todo-rank/<date>.json in the vault. The model and its effort are\n" +
			"[todo] rank_model and rank_effort in the config (sonnet and medium).\n\n" +
			"  mw todo rank\n" +
			"  mw todo rank --dry-run\n\n" +
			"An answer that misses a card, names one it was not given, says another row, puts more than\n" +
			"three cards in now or make-time, or lets a starred card go is refused and nothing is written.\n" +
			"--dry-run prints the rows and writes nothing.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := config.Vault()
			if err != nil {
				return err
			}
			host, err := config.Host()
			if err != nil {
				return err
			}
			settings, err := config.Todo()
			if err != nil {
				return err
			}
			gateway := mwGateway(dir, host)
			pass := application.TodoRankPass{
				Tracker: gateway, Closed: gateway, Grinder: claude.NewGrinder(), Runs: mwVault(dir, host),
				Instructions: millwright.TodoRankInstructions, AboutFile: filepath.Join(dir, application.TodoAboutFile),
				Model: settings.RankModel, Effort: settings.RankEffort, Zone: settings.Zone, Out: cmd.OutOrStdout(),
			}
			if about != "" {
				pass.AboutFile, pass.AboutRequired = about, true
			}
			_, err = pass.Run(cmd.Context(), dryRun)
			return err
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print the rows and write nothing")
	cmd.Flags().StringVar(&about, "about", "", "read his about file from here, not the vault's "+application.TodoAboutFile)
	return cmd
}

// todoReminderSpringJob is the job that, each minute, sends the Governor one
// Postern push for every To do card past its due time and not yet reminded of
// it. The push goes out only when a card is due, so a host with no postern key
// says nothing until one is.
func todoReminderSpringJob(host string, out io.Writer) (application.SpringJob, error) {
	dir, err := config.Vault()
	if err != nil {
		return application.SpringJob{}, err
	}
	reminder := application.TodoReminder{
		Tracker: mwGateway(dir, host), Sender: handsPush{gateway: mwGateway(dir, host)}, Err: out,
	}
	return application.TodoReminderJob(application.TodoReminderEvery, func(ctx context.Context) error {
		sent, err := reminder.Run(ctx)
		if sent > 0 {
			fmt.Fprintf(out, "mw events follow: %d To do reminder(s) sent\n", sent)
		}
		return err
	}), nil
}

// todoRankSpringJob is the job that makes the night's ranking pass over the To
// do board (mw todo rank), once a UTC day from [todo] rank_hour, on the home
// only; tells him once, in the card's own thread, of a starred card that slipped
// past its due time; and on the first pass of the week posts the weekly Let go
// card. Its pass is the very one mw todo rank makes.
func todoRankSpringJob(host string, out io.Writer) (application.SpringJob, error) {
	dir, err := config.Vault()
	if err != nil {
		return application.SpringJob{}, err
	}
	settings, err := config.Todo()
	if err != nil {
		return application.SpringJob{}, err
	}
	gateway := mwGateway(dir, host)
	pass := application.TodoRankPass{
		Tracker: gateway, Closed: gateway, Grinder: claude.NewGrinder(), Runs: mwVault(dir, host),
		Instructions: millwright.TodoRankInstructions, AboutFile: filepath.Join(dir, application.TodoAboutFile),
		Model: settings.RankModel, Effort: settings.RankEffort, Zone: settings.Zone, Out: out,
	}
	nightly := &application.TodoNightly{
		Hour: settings.RankHour, WeekStart: settings.WeekStart, Zone: settings.Zone,
		Rank: func(ctx context.Context) error {
			_, err := pass.Run(ctx, false)
			return err
		},
		Tracker: gateway, Notes: gateway, Sender: handsPush{gateway: gateway}, Err: out,
		Home: func(ctx context.Context) (bool, error) {
			return application.IsHome(ctx, mwVault(dir, host), host)
		},
	}
	return nightly.Job(), nil
}
