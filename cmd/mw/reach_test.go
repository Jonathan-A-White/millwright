package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
)

// boxesOf is a cloud whose book holds the boxes named, each at the address in
// the map: a box with no address in it has none to read, with the reason.
type boxesOf struct {
	boxes []string
	at    map[string]string
}

func (b boxesOf) Boxes(context.Context) ([]string, error) { return b.boxes, nil }

func (b boxesOf) Address(_ context.Context, box string) (application.CloudAddress, error) {
	if host, ok := b.at[box]; ok {
		return application.CloudAddress{User: "mw", Host: host}, nil
	}
	return application.CloudAddress{}, errors.New(box + " is not a peer of the hub: it has no address")
}

type boxesFail struct{ boxesOf }

func (boxesFail) Boxes(context.Context) ([]string, error) { return nil, errors.New("book unreadable") }

func TestReachAddsEachCloudBoxTheHandsHostsLackAtItsAddress(t *testing.T) {
	hands := map[string]string{"desktop": "ssh jwhite@desktop"}
	cloud := boxesOf{boxes: []string{"cloud1", "cloud2"}, at: map[string]string{"cloud1": "10.88.0.21", "cloud2": "10.88.0.22"}}
	reach, gaps, err := reachWithCloud(context.Background(), hands, cloud)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"desktop": "ssh jwhite@desktop",
		"cloud1":  "ssh mw@10.88.0.21",
		"cloud2":  "ssh mw@10.88.0.22",
	}
	if len(reach) != len(want) || len(gaps) != 0 {
		t.Fatalf("reach = %v, gaps = %v, want %v", reach, gaps, want)
	}
	for k, v := range want {
		if reach[k] != v {
			t.Errorf("reach[%s] = %q, want %q", k, reach[k], v)
		}
	}
	if _, ok := hands["cloud1"]; ok {
		t.Error("the [hands_hosts] map was written to")
	}
}

func TestReachKeepsAHandsHostsEntryForACloudBox(t *testing.T) {
	hands := map[string]string{"cloud1": "ssh other@cloud1.example"}
	cloud := boxesOf{boxes: []string{"cloud1"}, at: map[string]string{"cloud1": "10.88.0.21"}}
	reach, _, err := reachWithCloud(context.Background(), hands, cloud)
	if err != nil {
		t.Fatal(err)
	}
	if reach["cloud1"] != "ssh other@cloud1.example" {
		t.Errorf("reach[cloud1] = %q, want the [hands_hosts] entry", reach["cloud1"])
	}
}

func TestReachLeavesOutABoxWhoseAddressCannotBeReadWithTheReason(t *testing.T) {
	hands := map[string]string{"desktop": "ssh desktop"}
	cloud := boxesOf{boxes: []string{"cloud1", "cloud2"}, at: map[string]string{"cloud2": "10.88.0.22"}}
	reach, gaps, err := reachWithCloud(context.Background(), hands, cloud)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reach["cloud1"]; ok {
		t.Errorf("reach = %v, want cloud1 left out", reach)
	}
	if reach["cloud2"] != "ssh mw@10.88.0.22" {
		t.Errorf("reach[cloud2] = %q, want it reached still", reach["cloud2"])
	}
	if !strings.Contains(gaps["cloud1"], "cloud1 is not a peer of the hub") {
		t.Errorf("gaps = %v, want cloud1's reason", gaps)
	}
}

func TestReachWithNoCloudTableIsHandsHostsAlone(t *testing.T) {
	hands := map[string]string{"vps": "ssh vps", "desktop": "ssh desktop"}
	// cloudOfPause with ok false is what pauseCloud gives a home with no [cloud] table.
	reach, gaps, err := reachWithCloud(context.Background(), hands, cloudOfPause{})
	if err != nil {
		t.Fatal(err)
	}
	if len(reach) != 2 || len(gaps) != 0 || reach["vps"] != "ssh vps" || reach["desktop"] != "ssh desktop" {
		t.Errorf("reach = %v, gaps = %v, want [hands_hosts] alone", reach, gaps)
	}
}

func TestReachFailsWhenTheBookCannotBeRead(t *testing.T) {
	if _, _, err := reachWithCloud(context.Background(), nil, boxesFail{}); err == nil {
		t.Fatal("want the book's error")
	}
}
