package application_test

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
)

// A gate is added to Dispatch.run, to application.DispatchGates and to
// docs/dispatch-gates.md together, or this test says which one was missed.
func TestEveryDispatchGateIsListedInOnePlaceAndInTheDocs(t *testing.T) {
	source, err := os.ReadFile("dispatch.go")
	if err != nil {
		t.Fatal(err)
	}
	var inCode []string
	for _, found := range regexp.MustCompile(`(?m)^\s*// gate: (\S+)$`).FindAllStringSubmatch(string(source), -1) {
		inCode = append(inCode, found[1])
	}
	var listed []string
	for _, gate := range application.DispatchGates {
		listed = append(listed, gate.Name)
	}
	if strings.Join(inCode, " ") != strings.Join(listed, " ") {
		t.Errorf("the gates marked in dispatch.go, in order:\n  %v\nare not application.DispatchGates:\n  %v", inCode, listed)
	}

	docs, err := os.ReadFile("../docs/dispatch-gates.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, gate := range application.DispatchGates {
		if !strings.Contains(string(docs), "`"+gate.Name+"`") {
			t.Errorf("docs/dispatch-gates.md does not name the gate `%s`", gate.Name)
		}
	}
}
