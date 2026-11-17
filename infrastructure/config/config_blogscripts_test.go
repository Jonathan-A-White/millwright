package config_test

import (
	"reflect"
	"testing"

	"github.com/Jonathan-A-White/millwright/infrastructure/config"
)

// The blog's list of hosts is the blog's own domain and jsDelivr's /ghost/ paths
// until the config file names another.
func TestBlogScriptHostsAreTheBlogAndGhostsJsDelivrUntilTheFileSaysOtherwise(t *testing.T) {
	writeConfig(t, "vault = \"/v\"\n")
	want := []string{"allmymind.org", "cdn.jsdelivr.net/ghost/"}
	if got, err := config.BlogScriptHosts(); err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v %v, want %v", got, err, want)
	}

	writeConfig(t, "vault = \"/v\"\nblog_script_hosts = [\"allmymind.org\", \"platform.twitter.com\"] # embeds\n\n[doctor]\nblog_script_hosts = [\"nope\"]\n")
	want = []string{"allmymind.org", "platform.twitter.com"}
	if got, err := config.BlogScriptHosts(); err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v %v, want %v", got, err, want)
	}
}
