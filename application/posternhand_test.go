package application

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestSetAPIBackendLeavesModifiedNonAPILocationsAlone covers mw-tfne4.13 AC2:
// a location with a modifier (=, ^~, ~, ~*) on a path outside /api is left
// untouched by setAPIBackend.
func TestSetAPIBackendLeavesModifiedNonAPILocationsAlone(t *testing.T) {
	text := "server {\n" +
		"    location ^~ /static/ {\n" +
		"        proxy_pass http://laptop.mw:9000;\n" +
		"    }\n" +
		"    location ~ \\.php$ {\n" +
		"        proxy_pass http://laptop.mw:9001;\n" +
		"    }\n" +
		"    location = /api/healthz {\n" +
		"        proxy_pass http://laptop.mw:8787/healthz;\n" +
		"    }\n" +
		"}"

	got := setAPIBackend(text, "http://desktop.mw:8787", false)

	want := "server {\n" +
		"    location ^~ /static/ {\n" +
		"        proxy_pass http://laptop.mw:9000;\n" +
		"    }\n" +
		"    location ~ \\.php$ {\n" +
		"        proxy_pass http://laptop.mw:9001;\n" +
		"    }\n" +
		"    location = /api/healthz {\n" +
		"        proxy_pass http://desktop.mw:8787/healthz;\n" +
		"    }\n" +
		"}"

	if got != want {
		t.Errorf("setAPIBackend() =\n%s\nwant:\n%s", got, want)
	}
}

func TestUpsertEnvLinesReplacesAnExportedLineAndLeavesAMatchingFileByteForByte(t *testing.T) {
	values := map[string]string{"POSTERN_ADDR": "10.88.0.3:8787"}
	text := "export POSTERN_ADDR=127.0.0.1:8787\nPOSTERN_DATA=/d\n"

	got := upsertEnvLines(text, []string{"POSTERN_ADDR"}, values)
	if want := "POSTERN_ADDR=\"10.88.0.3:8787\"\nPOSTERN_DATA=/d\n"; got != want {
		t.Fatalf("upsertEnvLines() = %q, want %q", got, want)
	}
	if again := upsertEnvLines(got, []string{"POSTERN_ADDR"}, values); again != got {
		t.Fatalf("a second pass changed %q into %q", got, again)
	}
}

func TestUpsertEnvLinesWritesAFreshFile(t *testing.T) {
	got := upsertEnvLines("", []string{"A", "B"}, map[string]string{"A": "1", "B": "two words"})
	if want := "A=\"1\"\nB=\"two words\"\n"; got != want {
		t.Fatalf("upsertEnvLines() = %q, want %q", got, want)
	}
}

func TestTheGeneralAPILocationIsAPrefixOneOnAPI(t *testing.T) {
	for line, want := range map[string]bool{
		"    location /api/ {":          true,
		"    location /api {":           true,
		"    location ^~ /api/ {":       true,
		"    location = /api/ {":        false,
		"    location = /api/healthz {": false,
		"    location ~ ^/api/ {":       false,
		"    location /apiary/ {":       false,
		"    location = /api/events {":  false,
		"    proxy_pass http://x:8787;": false,
		"    location /snapshot {":      false,
	} {
		if got := isGeneralAPILocation(line); got != want {
			t.Errorf("isGeneralAPILocation(%q) = %v, want %v", line, got, want)
		}
	}
}

func TestTheEventsLocationFallsBackToTheAPIEndMarker(t *testing.T) {
	text := "server {\n    location = /api/healthz {\n        proxy_pass http://a:1/healthz;\n    }\n    # mw-api end\n}"

	got, err := ensureEventsLocation(text, "http://b:2")
	if err != nil {
		t.Fatalf("ensureEventsLocation: %v", err)
	}
	if !strings.Contains(got, "    }\n    # mw-api-events\n    location = /api/events {") ||
		!strings.Contains(got, "    }\n    # mw-api end") {
		t.Fatalf("expected the block just ahead of the end marker, got:\n%s", got)
	}
}

func TestTheEventsLocationRefusesASiteThatIsNotThePosterns(t *testing.T) {
	if _, err := ensureEventsLocation("server {\n    location / {\n    }\n}", "http://b:2"); err == nil {
		t.Fatal("expected a site with no /api/ location and no marker to be refused")
	}
}

func TestServeRefusesAnEnvironmentValueThatCannotBeWritten(t *testing.T) {
	req := PosternServeRequest{
		Backend: "http://b:1", SnapshotPath: "/s/snapshot.bin", GovernorKey: "02g",
		EnvFile: "/e/postern.env", ViewPath: "/v/view.b64", Mw: "/bin/mw", MayorKey: "03$(evil)",
	}
	if err := req.validate(); err == nil || !strings.Contains(err.Error(), "POSTERN_MAYOR_KEY") {
		t.Fatalf("expected a $ in the Mayor's key refused, got %v", err)
	}
	req.MayorKey, req.Addr = "03m", "no port"
	if err := req.validate(); err == nil || !strings.Contains(err.Error(), "--addr") {
		t.Fatalf("expected an address with no port refused, got %v", err)
	}
	req.Addr, req.Mw = "", "mw"
	if err := req.validate(); err == nil || !strings.Contains(err.Error(), "--mw") {
		t.Fatalf("expected a relative mw refused, got %v", err)
	}
	req.Mw = "/bin/mw"
	if err := req.validate(); err != nil {
		t.Fatalf("expected an empty --addr to read %s, got %v", DefaultPosternAddr, err)
	}
}

// TestTheDefaultBackupDirIsUnderTheCallersHome covers mw-43v9x.5: the default
// was /root/tidy even when run as another user.
func TestTheDefaultBackupDirIsUnderTheCallersHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	if got, want := DefaultPosternHandBackupDir(), filepath.Join(home, "tidy"); got != want {
		t.Errorf("DefaultPosternHandBackupDir() = %q, want %q", got, want)
	}
	if got, want := (PosternNginx{}).backupDir(), filepath.Join(home, "tidy"); got != want {
		t.Errorf("PosternNginx{}.backupDir() = %q, want %q", got, want)
	}
	if got, want := (PosternServe{}).backupDir(), filepath.Join(home, "tidy"); got != want {
		t.Errorf("PosternServe{}.backupDir() = %q, want %q", got, want)
	}
	if got := (PosternNginx{BackupDir: "/elsewhere"}).backupDir(); got != "/elsewhere" {
		t.Errorf("an explicit BackupDir was not kept: %q", got)
	}
}
