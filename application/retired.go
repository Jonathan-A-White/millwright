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

// refusedRetiredFor is the refusal of a command that names a retired host as a
// place work goes: it names the config key and the host, and says what the
// command cannot do with it. The caller adds what was or was not written.
func refusedRetiredFor(host, cannot string, retired []string) string {
	return fmt.Sprintf("the host %s is retired (%s = [%s] in the config file): %s",
		host, RetiredHostsKey, strings.Join(retired, ", "), cannot)
}
