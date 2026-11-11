package application

import (
	"fmt"
	"slices"
	"strings"
)

// RetiredHostsKey is the root-table key of the home's config file that names
// the hosts the Governor has retired (infrastructure/config.RetiredHosts): a
// retired host is no place work or copies can go.
const RetiredHostsKey = "retired_hosts"

// IsRetired says whether host is one the Governor retired.
func IsRetired(retired []string, host string) bool {
	return host != "" && slices.Contains(retired, host)
}

// refusedRetired is the refusal of a path naming a retired host, for mw path
// set: it names the config key, and says nothing was written.
func refusedRetired(host string, retired []string) error {
	return fmt.Errorf("the host %s is retired (%s = [%s] in the config file): no story is pathed to it, "+
		"so name a host that is awake, or host=auto: nothing was written",
		host, RetiredHostsKey, strings.Join(retired, ", "))
}

// refusedRetiredFor is the refusal of a command that names a retired host as a
// place work goes: it names the config key and the host, and says what the
// command cannot do with it. The caller adds what was or was not written.
func refusedRetiredFor(host, cannot string, retired []string) string {
	return fmt.Sprintf("the host %s is retired (%s = [%s] in the config file): %s",
		host, RetiredHostsKey, strings.Join(retired, ", "), cannot)
}
