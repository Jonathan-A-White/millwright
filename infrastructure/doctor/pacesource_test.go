package doctor

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
)

func TestPaceSourceIsOkWhenTheHostAgreesOrSaysNothing(t *testing.T) {
	verdict, said := NewPaceSource("desktop", nil, nil).Probe(context.Background())
	if verdict != application.DoctorOK || said != "" {
		t.Fatalf("got %v %q, want ok", verdict, said)
	}
}

func TestPaceSourceNamesTheHostTheKeyAndTheCure(t *testing.T) {
	check := NewPaceSource("cloud3", []string{"/home/mw/.config/mw/config.toml says max_at_once = 3 but the vault's pace.toml says 5"}, nil)
	verdict, said := check.Probe(context.Background())
	if verdict != application.DoctorFaulty {
		t.Fatalf("got %v, want faulty", verdict)
	}
	for _, want := range []string{"cloud3", "max_at_once = 3", "says 5", "To cure", "pace.toml"} {
		if !strings.Contains(said, want) {
			t.Errorf("probe said %q, want it to say %q", said, want)
		}
	}
	if check.Cure(context.Background()) == nil {
		t.Error("the check has no cure, so Cure must say so")
	}
}

func TestPaceSourceCannotTellWhenTheTablesDoNotRead(t *testing.T) {
	verdict, _ := NewPaceSource("laptop", nil, errors.New("bad toml")).Probe(context.Background())
	if verdict != application.DoctorCannotTell {
		t.Fatalf("got %v, want cannot-tell", verdict)
	}
}
