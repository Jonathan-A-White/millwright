package application

import "testing"

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

	got := setAPIBackend(text, "http://desktop.mw:8787")

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
