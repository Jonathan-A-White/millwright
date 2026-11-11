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
