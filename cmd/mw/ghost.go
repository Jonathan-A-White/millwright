package main

import (
	"fmt"
	"os"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/ghost"

	"github.com/spf13/cobra"
)

// newGhostCmd builds `mw ghost`: the factory's one way into the blog, drafts
// only. It has no command that publishes, schedules, sends or deletes.
func newGhostCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ghost",
		Short: "Push a post to the blog as a Ghost draft, never publish it",
		Long: "ghost is the factory's way into the blog, and it only makes drafts: the Governor\n" +
			"publishes them from Ghost's editor. The Admin API key is kept with\n" +
			"mw secrets put " + application.GhostKeyName + " (docs/secrets.md); the blog's address is ghost_url\n" +
			"in the config file, or $" + config.GhostURLEnv + ".",
	}
	cmd.AddCommand(newGhostDraftCmd())
	return cmd
}

func newGhostDraftCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "draft <post.md>",
		Short: "Create or update the Ghost draft of a markdown post, and print its editor address",
		Long: "draft reads a post's markdown file: its frontmatter gives the title, the slug and the excerpt\n" +
			"(custom_excerpt, else excerpt), and the body goes to Ghost as one markdown card, so links and\n" +
			"[^1] footnotes render. It makes a draft, whatever the frontmatter's status says. A second run\n" +
			"with the same slug updates that draft; a post of that slug that is not a draft is refused and\n" +
			"left as it is. It prints the draft's editor address.\n\n" +
			"  mw ghost draft posts/who-are-the-dead-who-hear-john-5-25.md",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			text, err := os.ReadFile(args[0])
			if err != nil {
				return err
			}
			base, err := config.GhostURL()
			if err != nil {
				return err
			}
			store, _, _, err := secretsStore()
			if err != nil {
				return fmt.Errorf("opening the vault's secrets: %w", err)
			}
			return application.GhostDraft{
				Secrets: store,
				Open: func(key string) (application.GhostBlog, error) {
					return ghost.New(base, key)
				},
				Out: cmd.OutOrStdout(),
			}.Run(cmd.Context(), text)
		},
	}
}
