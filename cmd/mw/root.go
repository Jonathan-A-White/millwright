package main

import "github.com/spf13/cobra"

// newRootCmd builds the mw command tree.
func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "mw",
		Short:         "millwright: a personal software factory",
		Long:          "mw drives the millwright factory: it reads stories, dispatches the sessions that work them, and reports back.",
		SilenceUsage:  true,
		SilenceErrors: false,
	}

	root.AddCommand(newAfterLandingCmd())
	root.AddCommand(newAskCmd())
	root.AddCommand(newBdCmd())
	root.AddCommand(newBriefCmd())
	root.AddCommand(newCardCmd())
	root.AddCommand(newCheckCmd())
	root.AddCommand(newCloudCmd())
	root.AddCommand(newConfigCmd())
	root.AddCommand(newDigestCmd())
	root.AddCommand(newDispatchCmd())
	root.AddCommand(newDoctorCmd())
	root.AddCommand(newEventsCmd())
	root.AddCommand(newFileCmd())
	root.AddCommand(newFuelCmd())
	root.AddCommand(newGristCmd())
	root.AddCommand(newGoogleCmd())
	root.AddCommand(newGrillCmd())
	root.AddCommand(newContactsCmd())
	root.AddCommand(newHandsCmd())
	root.AddCommand(newHomeCmd())
	root.AddCommand(newIceCmd())
	root.AddCommand(newStuckCmd())
	root.AddCommand(newVerifyCmd())
	root.AddCommand(newLintCheckItCmd())
	root.AddCommand(newThawCmd())
	root.AddCommand(newIdeasOnlyCmd())
	root.AddCommand(newInitCmd())
	root.AddCommand(newLandCmd())
	root.AddCommand(newMailCmd())
	root.AddCommand(newMemberCmd())
	root.AddCommand(newMemoryCmd())
	root.AddCommand(newErrorsCmd())
	root.AddCommand(newMillhandCmd())
	root.AddCommand(newDeputyCmd())
	root.AddCommand(newNextCmd())
	root.AddCommand(newNoteCmd())
	root.AddCommand(newTocCmd())
	root.AddCommand(newNudgeCmd())
	root.AddCommand(newPeekCmd())
	root.AddCommand(newPosternCmd())
	root.AddCommand(newPromptCmd())
	root.AddCommand(newProveCmd())
	root.AddCommand(newQueueCmd())
	root.AddCommand(newFoldCmd())
	root.AddCommand(newStampCmd())
	root.AddCommand(newReleaseCmd())
	root.AddCommand(newCauseCmd())
	root.AddCommand(newTodoCmd())
	root.AddCommand(newPathCmd())
	root.AddCommand(newPauseCmd())
	root.AddCommand(newResumeCmd())
	root.AddCommand(newRetryCmd())
	root.AddCommand(newSeatCmd())
	root.AddCommand(newSecretsCmd())
	root.AddCommand(newShowCmd())
	root.AddCommand(newSleepCmd())
	root.AddCommand(newStatusCmd())
	root.AddCommand(newSuiteCmd())
	root.AddCommand(newSweepCmd())
	root.AddCommand(newSyncCmd())
	root.AddCommand(newTalkCmd())
	root.AddCommand(newTesterCmd())
	root.AddCommand(newReportCmd())
	root.AddCommand(newLoopCmd())
	root.AddCommand(newInvestCmd())
	root.AddCommand(newTidyCmd())
	root.AddCommand(newTriageCmd())
	root.AddCommand(newUsageCmd())
	root.AddCommand(newVersionCmd())
	root.AddCommand(newWatchCmd())

	return root
}
