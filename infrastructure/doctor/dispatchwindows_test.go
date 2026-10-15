package doctor

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
)

func TestDispatchWindowsIsOkWithNoProblem(t *testing.T) {
	verdict, said := NewDispatchWindows(nil).Probe(context.Background())
	if verdict != application.DoctorOK || said != "" {
		t.Fatalf("got %v %q, want ok", verdict, said)
	}
}

func TestDispatchWindowsIsFaultyNamingEachProblem(t *testing.T) {
	check := NewDispatchWindows([]error{errors.New(`to = "6am" is not a time`), errors.New(`tz = "Mars" is not a zone`)})
	verdict, said := check.Probe(context.Background())
	if verdict != application.DoctorFaulty {
		t.Fatalf("got %v, want faulty", verdict)
	}
	for _, want := range []string{`to = "6am"`, `tz = "Mars"`, "root values"} {
		if !strings.Contains(said, want) {
			t.Errorf("probe said %q, want it to say %q", said, want)
		}
	}
	if check.Cure(context.Background()) == nil {
		t.Error("the check has no cure, so Cure must say so")
	}
}
