package docs_test

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// cloudKeys reads the keys of the [cloud] table out of the comment that heads
// infrastructure/config/config.go, the list the code keeps.
func cloudKeys(t *testing.T) []string {
	t.Helper()
	raw, err := os.ReadFile("../infrastructure/config/config.go")
	if err != nil {
		t.Fatal(err)
	}
	var keys []string
	in := false
	key := regexp.MustCompile(`^//\s+([a-z_]+)\s*=`)
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(line, "//\t[cloud]") {
			in = true
			continue
		}
		if in && strings.HasPrefix(line, "//\t[") {
			break
		}
		if !in {
			continue
		}
		if m := key.FindStringSubmatch(strings.Replace(line, "\t", " ", -1)); m != nil {
			keys = append(keys, m[1])
		}
	}
	if len(keys) == 0 {
		t.Fatal("found no [cloud] keys in the config.go comment")
	}
	return keys
}

func TestTheReadmeCloudExampleListsEveryCloudKeyWithSlowMakeMinutesAtThirty(t *testing.T) {
	raw, err := os.ReadFile("../README.md")
	if err != nil {
		t.Fatal(err)
	}
	readme := string(raw)
	start := strings.Index(readme, "\n[cloud]")
	if start < 0 {
		t.Fatal("README.md has no config.toml example with a [cloud] table")
	}
	example := readme[start:]
	if end := strings.Index(example, "```"); end >= 0 {
		example = example[:end]
	}
	for _, key := range cloudKeys(t) {
		if !regexp.MustCompile(`(?m)^` + key + `\s*=`).MatchString(example) {
			t.Errorf("the README [cloud] example does not list %s", key)
		}
	}
	if !regexp.MustCompile(`(?m)^slow_make_minutes\s*=\s*30\s+#\s*\S`).MatchString(example) {
		t.Error("the README [cloud] example does not give slow_make_minutes = 30 with a one-line meaning")
	}
}
