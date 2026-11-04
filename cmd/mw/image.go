package main

import (
	"fmt"
	"os"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/gemini"

	"github.com/spf13/cobra"
)

// newImageCmd builds `mw image`: one picture from a prompt, made with the
// Gemini API (Nano Banana) and written to a file.
func newImageCmd() *cobra.Command {
	var out string
	cmd := &cobra.Command{
		Use:   "image <prompt> --out <file.png>",
		Short: "Make one picture with the Gemini API (Nano Banana) and write it to a file",
		Long: "image makes one picture from a prompt with the Gemini API, and writes it to --out (.png, .jpg,\n" +
			"or .webp). It sends one request a run: no retry, no batch. The key is kept with\n" +
			"mw secrets put " + application.GeminiKeyName + " (docs/secrets.md); the model is gemini_model in the config\n" +
			"file, or $" + config.GeminiModelEnv + " (default " + config.DefaultGeminiModel + ").\n\n" +
			"If Gemini refuses over quota or billing, image says so and exits non-zero: it never touches billing.\n\n" +
			"  mw image \"a lighthouse at dusk, oil painting\" --out lighthouse.png\n" +
			"  mw ghost draft posts/my-post.md --feature-image lighthouse.png",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			model, err := config.GeminiModel()
			if err != nil {
				return err
			}
			store, _, _, err := secretsStore()
			if err != nil {
				return fmt.Errorf("opening the vault's secrets: %w", err)
			}
			return application.Image{
				Secrets: store,
				Open: func(key string) (application.ImageMaker, error) {
					return gemini.New(gemini.DefaultURL, model, key)
				},
				Write: func(path string, data []byte) error {
					return os.WriteFile(path, data, 0o644)
				},
				Out: cmd.OutOrStdout(),
			}.Run(cmd.Context(), args[0], out)
		},
	}
	cmd.Flags().StringVar(&out, "out", "", "the file to write the picture to (.png, .jpg or .webp)")
	_ = cmd.MarkFlagRequired("out")
	return cmd
}
