package rig_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/Jonathan-A-White/millwright/infrastructure/rig"
)

func TestLandedBetweenNamesTheStoriesMergedAndTheNewestVersion(t *testing.T) {
	here, _ := aRig(t)
	from := run(t, here, "git", "rev-parse", "HEAD")
	for _, story := range []string{"mw-x.1", "mw-x.2"} {
		run(t, here, "git", "checkout", "-qb", "mw/"+story)
		write(t, here, story+".txt", story)
		run(t, here, "git", "add", "-A")
		run(t, here, "git", "commit", "-qm", "work for "+story+" (not a landing)")
		run(t, here, "git", "checkout", "-q", "main")
		run(t, here, "git", "merge", "-q", "--no-ff", "-m", "Merge branch 'mw/"+story+"' into HEAD", "mw/"+story)
		write(t, here, "v-"+story, story)
		run(t, here, "git", "add", "-A")
		run(t, here, "git", "commit", "-qm", "Version 0.1."+story[len(story)-1:]+"0 ("+story+")")
	}
	run(t, here, "git", "merge", "-q", "--no-ff", "-m", "Merge branch 'main' of origin", "mw/mw-x.1")
	to := run(t, here, "git", "rev-parse", "HEAD")

	got, err := rig.New().LandedBetween(context.Background(), here, from, to)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"mw-x.1", "mw-x.2"}; !reflect.DeepEqual(got.Stories, want) || got.Version != "0.1.20" {
		t.Errorf("LandedBetween = %+v; want stories %v and version 0.1.20", got, want)
	}

	if none, err := rig.New().LandedBetween(context.Background(), here, "", to); err != nil || len(none.Stories) != 0 {
		t.Errorf("LandedBetween with no from = %+v, %v; want nothing", none, err)
	}
	if none, err := rig.New().LandedBetween(context.Background(), here, to, to); err != nil || len(none.Stories) != 0 {
		t.Errorf("LandedBetween of one commit = %+v, %v; want nothing", none, err)
	}
}
