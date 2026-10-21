package application

import "strings"

// TimingSkip is the mode that leaves a rig's timing tests out of a run: the
// landing gate, mw check, mw suite and a Builder's own test runs.
const TimingSkip = "skip"

// TimingEnv is the environment that tells a rig's test command which of its
// tests need a real clock and what to do with them: MW_TIMING=<mode> and
// MW_TIMING_TESTS, the rig's timing_tests globs joined by commas. A rig with no
// globs gets nothing, so its command is run as it always was.
func TimingEnv(mode string, globs []string) map[string]string {
	if len(globs) == 0 {
		return nil
	}
	return map[string]string{"MW_TIMING": mode, "MW_TIMING_TESTS": strings.Join(globs, ",")}
}
