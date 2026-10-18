package application_test

import (
	"reflect"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
)

func TestReadACNotMet(t *testing.T) {
	cases := []struct {
		name    string
		comment string
		acs     []int
		subject string
	}{
		{"one", "Done.\nAC 3 NOT met: why.", []int{3}, "Landed, AC 3 NOT met"},
		{"done is not met too", "AC 2 NOT done (the harness refused the step)", []int{2}, "Landed, AC 2 NOT met"},
		{"only the one that is not met", "AC 1, 3 and 4 done; AC 2 NOT done", []int{2}, "Landed, AC 2 NOT met"},
		{"two", "AC 2 and 4 NOT met", []int{2, 4}, "Landed, AC 2 and 4 NOT met"},
		{"three", "AC 1, 2 and 5 NOT met.", []int{1, 2, 5}, "Landed, AC 1, 2 and 5 NOT met"},
		{"oxford comma", "AC 1, 2, and 5 NOT met.", []int{1, 2, 5}, "Landed, AC 1, 2 and 5 NOT met"},
		{"on two lines", "AC 4 NOT met.\nAC 2 NOT done.", []int{2, 4}, "Landed, AC 2 and 4 NOT met"},
		{"colon and verb", "AC 3: NOT met\nAC 5 is NOT met", []int{3, 5}, "Landed, AC 3 and 5 NOT met"},
		{"lower case is prose", "AC 3 was not met by my first try.", nil, ""},
		{"no criterion named", "The work is NOT met by the old tests.", nil, ""},
		{"all done", "AC 1, 2 and 3 done.", nil, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := application.ReadACNotMet(c.comment)
			if !reflect.DeepEqual(got.ACs, c.acs) {
				t.Fatalf("ACs = %v, want %v", got.ACs, c.acs)
			}
			if got.Found() != (c.acs != nil) {
				t.Fatalf("Found() = %v for %v", got.Found(), c.acs)
			}
			if c.acs != nil && got.Subject() != c.subject {
				t.Fatalf("Subject() = %q, want %q", got.Subject(), c.subject)
			}
		})
	}
}
