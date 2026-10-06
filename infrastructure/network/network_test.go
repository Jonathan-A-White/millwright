package network_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/network"
)

func saying(out string, err error) network.Run {
	return func(context.Context, string, ...string) ([]byte, error) { return []byte(out), err }
}

func TestTheWindowsLineIsReadAsProfileAndCost(t *testing.T) {
	for out, want := range map[string]application.NetworkCondition{
		"Whitehouse | cost=Fixed roaming=False overLimit=False approaching=False\r\n": {Profile: "Whitehouse", Cost: "Fixed"},
		"Hotel Wi-Fi | cost=Variable roaming=True overLimit=False approaching=False":  {Profile: "Hotel Wi-Fi", Cost: "Variable"},
		"Home | cost=Unrestricted roaming=False overLimit=False approaching=False\n":  {Profile: "Home", Cost: "Unrestricted"},
		"Home | cost=Unknown roaming=False overLimit=False approaching=False\n":       {Profile: "Home", Cost: "Unknown"},
		"none\r\n": {},
	} {
		got, err := network.Windows{Run: saying(out, nil)}.Probe(context.Background())
		if err != nil || got != want {
			t.Errorf("%q read as %+v, %v; want %+v", out, got, err, want)
		}
	}
}

func TestWhatIsNotTheLineIsAnError(t *testing.T) {
	for _, out := range []string{"", "Something went wrong", "x | cost= roaming=False"} {
		if _, err := (network.Windows{Run: saying(out, nil)}).Probe(context.Background()); err == nil {
			t.Errorf("%q read without an error", out)
		}
	}
}

func TestAFailedCallIsItsError(t *testing.T) {
	boom := errors.New("fork/exec: no such file")
	if _, err := (network.Windows{Run: saying("", boom)}).Probe(context.Background()); !errors.Is(err, boom) {
		t.Fatalf("got %v, want the call's own error", err)
	}
}

func TestACallThatTakesTooLongIsStoppedAndSaysSo(t *testing.T) {
	hang := func(ctx context.Context, _ string, _ ...string) ([]byte, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	_, err := network.Windows{Run: hang, Timeout: 20 * time.Millisecond}.Probe(context.Background())
	if err == nil || !strings.Contains(err.Error(), "took longer") {
		t.Fatalf("got %v, want a timeout saying it took longer", err)
	}
}

func TestTheCallIsMadeToPowerShellByItsFullPath(t *testing.T) {
	var name string
	var args []string
	run := func(_ context.Context, n string, a ...string) ([]byte, error) {
		name, args = n, a
		return []byte("none"), nil
	}
	if _, err := (network.Windows{Run: run}).Probe(context.Background()); err != nil {
		t.Fatal(err)
	}
	if name != network.PowerShell || len(args) != 3 || args[0] != "-NoProfile" || !strings.Contains(args[2], "GetConnectionCost") {
		t.Fatalf("ran %s %q", name, args)
	}
}

func TestTheStoreKeepsTheMemoryBetweenRuns(t *testing.T) {
	store := network.NewStore(t.TempDir() + "/state")
	ctx := context.Background()
	if got, err := store.Load(ctx); err != nil || got.Told || !got.Reading.At.IsZero() {
		t.Fatalf("a fresh store loaded %+v, %v", got, err)
	}
	want := application.NetworkMemory{
		Reading: application.NetworkReading{Metered: true, Source: application.NetworkWindows, Profile: "Whitehouse", Cost: "Fixed", At: time.Date(2026, 10, 6, 0, 20, 0, 0, time.UTC)},
		Told:    true, ToldMetered: true, Alert: 7,
	}
	if err := store.Save(ctx, want); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load(ctx)
	if err != nil || got.Reading.Profile != "Whitehouse" || !got.Reading.At.Equal(want.Reading.At) || !got.Told || !got.ToldMetered || got.Alert != 7 {
		t.Fatalf("loaded %+v, %v; want %+v", got, err, want)
	}
}
