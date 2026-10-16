package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/todoindex"

	"github.com/spf13/cobra"
)

// todoIndexEnv names the file the To do index is kept in when it is not the
// default; todoIndexFile is that default, under the home directory.
const todoIndexEnv = "MW_TODO_INDEX"

var todoIndexFile = filepath.Join(".local", "state", "mw", "todo-search.db")

// todoIndexPath is where this host keeps the To do index: $MW_TODO_INDEX, else
// todoIndexFile under the home directory.
func todoIndexPath() (string, error) {
	if path := os.Getenv(todoIndexEnv); path != "" {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("there is no home directory to keep the To do index in: %w", err)
	}
	return filepath.Join(home, todoIndexFile), nil
}

func newTodoSearchCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "search <words>",
		Short: "Find a To do card by a word of its title, notes or comments, closed ones too",
		Long: "search finds the cards of the Governor's To do board (beads labelled hitl:todo), open or\n" +
			"closed, that hold every one of the words in their title, their notes (the description) or a\n" +
			"comment, best match first, at most " + fmt.Sprint(application.TodoSearchLimit) + ". It prints each card's id, status and\n" +
			"title, then the line that matched. A word matches whole, whatever its case or accents.\n\n" +
			"  mw todo search quarry road\n\n" +
			"Each run first rebuilds the index from beads: an SQLite file in ~/.local/state/mw (or the\n" +
			"file $" + todoIndexEnv + " names), which is only a cache and can be deleted. Nothing is\n" +
			"written to beads. When no card holds the words it says 'nothing found' and succeeds.",
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := config.Vault()
			if err != nil {
				return err
			}
			host, err := config.Host()
			if err != nil {
				return err
			}
			path, err := todoIndexPath()
			if err != nil {
				return err
			}
			gateway := mwGateway(dir, host)
			_, err = application.TodoSearch{
				Tracker: gateway, Closed: gateway, Index: todoindex.Index{Path: path}, Out: cmd.OutOrStdout(),
			}.Run(cmd.Context(), args)
			return err
		},
	}
}
