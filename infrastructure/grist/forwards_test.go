package grist_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

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
