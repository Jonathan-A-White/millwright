package main

import (
	"context"
	"errors"
	"testing"
)

type boxesOf []string

func (b boxesOf) Boxes(context.Context) ([]string, error) { return b, nil }

func (b boxesOf) Down(context.Context, string) (string, error) { return "", nil }

type boxesFail struct{ boxesOf }

func (boxesFail) Boxes(context.Context) ([]string, error) { return nil, errors.New("book unreadable") }

func TestReachAddsEachCloudBoxTheHandsHostsLack(t *testing.T) {
	hands := map[string]string{"desktop": "ssh jwhite@desktop"}
	reach, err := reachWithCloud(context.Background(), hands, boxesOf{"cloud1", "cloud2"})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"desktop": "ssh jwhite@desktop",
		"cloud1":  "ssh mw@cloud1.mw",
		"cloud2":  "ssh mw@cloud2.mw",
	}
	if len(reach) != len(want) {
		t.Fatalf("reach = %v, want %v", reach, want)
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
	reach, err := reachWithCloud(context.Background(), hands, boxesOf{"cloud1"})
	if err != nil {
		t.Fatal(err)
	}
	if reach["cloud1"] != "ssh other@cloud1.example" {
		t.Errorf("reach[cloud1] = %q, want the [hands_hosts] entry", reach["cloud1"])
	}
}

func TestReachWithNoCloudTableIsHandsHostsAlone(t *testing.T) {
	hands := map[string]string{"vps": "ssh vps", "desktop": "ssh desktop"}
	// cloudOfPause with ok false is what pauseCloud gives a home with no [cloud] table.
	reach, err := reachWithCloud(context.Background(), hands, cloudOfPause{})
	if err != nil {
		t.Fatal(err)
	}
	if len(reach) != 2 || reach["vps"] != "ssh vps" || reach["desktop"] != "ssh desktop" {
		t.Errorf("reach = %v, want [hands_hosts] alone", reach)
	}
}

func TestReachFailsWhenTheBookCannotBeRead(t *testing.T) {
	if _, err := reachWithCloud(context.Background(), nil, boxesFail{}); err == nil {
		t.Fatal("want the book's error")
	}
}
