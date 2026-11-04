package grist_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/grist"
)

// A forwarded grist's pictures are kept privately in a directory of their own,
// by full path, in order; a second try writes them again.
func TestForwardedPicturesAreKeptPrivatelyInTheirGristsDirectory(t *testing.T) {
	ctx, dir := context.Background(), t.TempDir()
	forwards := grist.NewForwards(dir)
	pictures := []application.GristRunAttachment{{Ext: ".jpg", Data: []byte("one")}, {Ext: ".png", Data: []byte("two")}}
	for range 2 {
		paths, err := forwards.Keep(ctx, "direct:abc", pictures)
		if err != nil {
			t.Fatal(err)
		}
		want := []string{
			filepath.Join(dir, "forwarded", "direct:abc", "picture-1.jpg"),
			filepath.Join(dir, "forwarded", "direct:abc", "picture-2.png"),
		}
		for i, p := range paths {
			if p != want[i] {
				t.Fatalf("expected %s, got %s", want[i], p)
			}
			info, err := os.Stat(p)
			if err != nil || info.Mode().Perm() != 0o600 {
				t.Fatalf("expected %s kept 0600, got %v %v", p, info, err)
			}
		}
		if len(paths) != 2 {
			t.Fatalf("expected 2 paths, got %v", paths)
		}
	}
	if info, _ := os.Stat(filepath.Join(dir, "forwarded", "direct:abc")); info.Mode().Perm() != 0o700 {
		t.Fatalf("expected the directory 0700, got %v", info.Mode())
	}
}

// A txid that is not fit for a path segment never names a place outside forwarded/.
func TestAForwardedGristCannotNameAPlaceOutsideItsDirectory(t *testing.T) {
	dir := t.TempDir()
	paths, err := grist.NewForwards(dir).Keep(context.Background(), "../../etc", []application.GristRunAttachment{{Ext: ".jpg", Data: []byte("x")}})
	if err != nil {
		t.Fatal(err)
	}
	if rel, err := filepath.Rel(filepath.Join(dir, "forwarded"), paths[0]); err != nil || filepath.IsAbs(rel) || len(rel) > 2 && rel[:2] == ".." {
		t.Fatalf("expected %s inside forwarded/, rel %q err %v", paths[0], rel, err)
	}
}

// mw-gq6.692, on real directories: a forward 30 days old is deleted whole, one
// a second younger is kept whole; the age is that of the last picture kept.
func TestTheThirtyDayBoundaryOnRealForwards(t *testing.T) {
	ctx, dir := context.Background(), t.TempDir()
	forwards := grist.NewForwards(dir)
	now := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	pictures := []application.GristRunAttachment{{Ext: ".jpg", Data: []byte("one")}, {Ext: ".png", Data: []byte("two")}}
	for txid, forwarded := range map[string]time.Time{
		"direct:thirty": now.Add(-application.GristKeepAge),
		"direct:young":  now.Add(-application.GristKeepAge + time.Second),
	} {
		paths, err := forwards.Keep(ctx, txid, pictures)
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range append(paths, filepath.Dir(paths[0])) {
			if err := os.Chtimes(p, forwarded, forwarded); err != nil {
				t.Fatal(err)
			}
		}
	}
	shelved, err := forwards.Shelved(ctx)
	if err != nil || len(shelved) != 2 || shelved[0].Name != "direct:thirty" || shelved[0].Files != 2 || shelved[0].Bytes != 6 {
		t.Fatalf("expected the two forwards, oldest first, got %+v %v", shelved, err)
	}
	report, err := application.GristTidy{Forwards: forwards, Now: func() time.Time { return now }}.Run(ctx)
	if err != nil || len(report.Acts) != 1 || report.Acts[0].Txid != "direct:thirty" {
		t.Fatalf("expected the 30-day forward alone swept, got %+v %v", report, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "forwarded", "direct:thirty")); !os.IsNotExist(err) {
		t.Fatalf("expected the 30-day forward gone, got %v", err)
	}
	for _, name := range []string{"picture-1.jpg", "picture-2.png"} {
		if _, err := os.Stat(filepath.Join(dir, "forwarded", "direct:young", name)); err != nil {
			t.Fatalf("expected the younger forward whole: %v", err)
		}
	}
}

func TestAForwardNameThatClimbsIsRefused(t *testing.T) {
	forwards := grist.NewForwards(t.TempDir())
	for _, name := range []string{"", ".", "..", "../x", "a/b"} {
		if err := forwards.DropShelved(context.Background(), name); err == nil {
			t.Fatalf("expected %q refused", name)
		}
	}
}
