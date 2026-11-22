package diskguard

import (
	"reflect"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
)

func TestLargestKeepsTheNBiggestUnderHomeAndNotHomeItself(t *testing.T) {
	out := "100\t/home/mw/a\n900\t/home/mw/b\n5000\t/home/mw\nnot a line\n300\t/home/mw/c\n"
	got := largest(out, "/home/mw", 2)
	want := []application.DiskHolder{{Path: "/home/mw/b", Bytes: 900}, {Path: "/home/mw/c", Bytes: 300}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestLastLineIsWhatTheStepSaidLast(t *testing.T) {
	if got := lastLine("one\ntwo\n\n"); got != "two" {
		t.Fatalf("got %q", got)
	}
	if got := lastLine(""); got != "" {
		t.Fatalf("got %q", got)
	}
}
