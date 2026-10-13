package application

import "strconv"

// WorkerLimitEnv is the environment that caps the test workers a Builder's
// tests may start at n on a host that is also someone's PC (mw-gq6.399): JOBS,
// which the millwright Makefile turns into go's -p and GOMAXPROCS; GOMAXPROCS
// itself, which caps go on a rig whose Makefile does not read JOBS; and
// VITEST_MAX_WORKERS, which vitest reads. An n below 1 is no limit and adds
// nothing.
func WorkerLimitEnv(n int) map[string]string {
	if n < 1 {
		return nil
	}
	said := strconv.Itoa(n)
	return map[string]string{"JOBS": said, "GOMAXPROCS": said, "VITEST_MAX_WORKERS": said}
}
