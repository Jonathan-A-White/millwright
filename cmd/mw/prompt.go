package main

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/Jonathan-A-White/millwright/application"
)

// promptClock stamps the facts mw prompt run reads against. A test fixes it.
var promptClock = time.Now

// newPromptCmd builds `mw prompt`: the prompts the Mayor saves to the postern
// backend, for the Governor to run by name from the app.
func newPromptCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "prompt",
		Short: "Save prompts to the postern backend and run one by name",
		Args:  cobra.NoArgs,
	}
	root.AddCommand(newPromptSaveCmd())
	root.AddCommand(newPromptListCmd())
	root.AddCommand(newPromptShowCmd())
	root.AddCommand(newPromptRunCmd())
	return root
}

// promptsBackend is the postern backend prompts are kept in, authenticated as
// the home's postern key is for /api/messages.
func promptsBackend() (application.Prompts, error) {
	keys, err := posternKeys()
	if err != nil {
		return nil, err
	}
	return posternBackend(keys)
}

// newPromptSaveCmd builds `mw prompt save`.
func newPromptSaveCmd() *cobra.Command {
	var summary, bodyFile string
	var options []string

	cmd := &cobra.Command{
		Use:   "save <name> --summary <text> --body-file <path> [--option '<flag>:<type>=<default>']...",
		Short: "Save a prompt from a draft file to the postern backend",
		Long: "save keeps the prompt <name> on the postern backend, whole, in place of one of that name:\n" +
			"its --summary, its signature (each --option, repeatable) and its body, read from\n" +
			"--body-file, a draft in the vault. An option is <flag>:<type>=<default>, the type string,\n" +
			"int, bool or duration (30m); <flag>:<type>:required has no default and must be given. In the body each\n" +
			"<flag> is replaced by the option's value when the prompt is run.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if bodyFile == "" {
				return fmt.Errorf("mw prompt save: which file holds the body? give --body-file <path>")
			}
			body, err := os.ReadFile(bodyFile)
			if err != nil {
				return fmt.Errorf("mw prompt save: reading the body: %w", err)
			}
			backend, err := promptsBackend()
			if err != nil {
				return err
			}
			_, err = application.PromptSave{Prompts: backend, Out: cmd.OutOrStdout()}.Run(cmd.Context(), application.PromptSaveRequest{
				Name: args[0], Summary: summary, Options: options, Body: string(body),
			})
			return err
		},
	}
	cmd.Flags().StringVar(&summary, "summary", "", "the line the list shows (required)")
	cmd.Flags().StringArrayVar(&options, "option", nil, "an option the prompt takes, <flag>:<type>=<default>, or <flag>:<type>:required (repeatable)")
	cmd.Flags().StringVar(&bodyFile, "body-file", "", "the file holding the prompt's body (required)")
	return cmd
}

// newPromptListCmd builds `mw prompt list`.
func newPromptListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List the saved prompts: name, summary and signature, one to a line",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			backend, err := promptsBackend()
			if err != nil {
				return err
			}
			_, err = application.PromptList{Prompts: backend, Out: cmd.OutOrStdout()}.Run(cmd.Context())
			return err
		},
	}
}

// newPromptShowCmd builds `mw prompt show`.
func newPromptShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <name>",
		Short: "Print a saved prompt whole: summary, signature and body",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			backend, err := promptsBackend()
			if err != nil {
				return err
			}
			_, err = application.PromptShow{Prompts: backend, Out: cmd.OutOrStdout()}.Run(cmd.Context(), args[0])
			return err
		},
	}
}

// newPromptRunCmd builds `mw prompt run`. Its flags are the prompt's own, which
// only the backend knows, so cobra does not parse them: the use case does,
// against the signature.
func newPromptRunCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run <name> [--<flag> <value>]...",
		Short: "Print a saved prompt with its options filled, then the facts it answers from",
		Long: "run checks the call against the prompt's signature (an option it does not name, a value\n" +
			"that is not of its type or a required option left out is refused, naming the signature), then\n" +
			"prints PROMPT /<name> and the options as given, the body with each <flag> replaced by its\n" +
			"value, and FACTS: what waits for the Governor, what landed and is not yet VERIFIED (with\n" +
			"its HOW TO CHECK IT), the open cards, the open demos and the hands steps that wait.\n" +
			"Every fact is read from the tracker and its notes: it spends no tokens.",
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 && (args[0] == "--help" || args[0] == "-h") {
				return cmd.Help()
			}
			if len(args) == 0 {
				return fmt.Errorf("mw prompt run: which prompt? give its name; mw prompt list says which there are")
			}
			backend, err := promptsBackend()
			if err != nil {
				return err
			}
			gateway, host, err := posternGateway()
			if err != nil {
				return err
			}
			return application.PromptRun{
				Prompts: backend, Tracker: gateway, Notes: gateway, Host: host,
				Now: promptClock, Out: cmd.OutOrStdout(), Err: cmd.ErrOrStderr(),
			}.Run(cmd.Context(), args[0], args[1:])
		},
	}
	return cmd
}
