package doctor_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/doctor"
)

const (
	vpsAddr   = "203.0.113.7"
	otherAddr = "198.51.100.9"
)

// vpsNamesCheck is the check over names, whose DNS is dns and whose front pages
// are the servers' by name.
func vpsNamesCheck(dns map[string]string, servers map[string]*httptest.Server, names ...string) *doctor.VPSNames {
	check := doctor.NewVPSNames("allmymind.org", names)
	check.Lookup = func(_ context.Context, name string) ([]string, error) {
		addr, ok := dns[name]
		if !ok {
			return nil, fmt.Errorf("no such host %s", name)
		}
		return []string{addr}, nil
	}
	check.URL = func(name string) string { return servers[name].URL }
	return check
}

func tlsServer(t *testing.T, page string) *httptest.Server {
	t.Helper()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, page)
	}))
	t.Cleanup(server.Close)
	return server
}

func TestVPSNamesFlagsADefaultPage(t *testing.T) {
	site := tlsServer(t, "<title>Welcome to nginx!</title>")
	check := vpsNamesCheck(map[string]string{"allmymind.org": vpsAddr, "kestrel.allmymind.org": vpsAddr},
		map[string]*httptest.Server{"kestrel.allmymind.org": site}, "kestrel.allmymind.org")
	check.Client = site.Client()
	verdict, why := check.Probe(context.Background())
	if verdict != application.DoctorFaulty || !strings.Contains(why, "kestrel.allmymind.org answers with nginx's default page") {
		t.Fatalf("got %v %q", verdict, why)
	}
}

func TestVPSNamesFlagsACertificateFailure(t *testing.T) {
	// The server's certificate is not trusted by the default client.
	site := tlsServer(t, "hello")
	check := vpsNamesCheck(map[string]string{"allmymind.org": vpsAddr, "kestrel.allmymind.org": vpsAddr},
		map[string]*httptest.Server{"kestrel.allmymind.org": site}, "kestrel.allmymind.org")
	verdict, why := check.Probe(context.Background())
	if verdict != application.DoctorFaulty || !strings.Contains(why, "kestrel.allmymind.org fails its certificate") {
		t.Fatalf("got %v %q", verdict, why)
	}
}

func TestVPSNamesPassesARealSite(t *testing.T) {
	site := tlsServer(t, "<title>Kestrel</title>")
	check := vpsNamesCheck(map[string]string{"allmymind.org": vpsAddr, "kestrel.allmymind.org": vpsAddr},
		map[string]*httptest.Server{"kestrel.allmymind.org": site}, "kestrel.allmymind.org")
	check.Client = site.Client()
	if verdict, why := check.Probe(context.Background()); verdict != application.DoctorOK {
		t.Fatalf("got %v %q", verdict, why)
	}
}

func TestVPSNamesLeavesANameElsewhereAlone(t *testing.T) {
	site := tlsServer(t, "Welcome to nginx!")
	check := vpsNamesCheck(map[string]string{"allmymind.org": vpsAddr, "elsewhere.allmymind.org": otherAddr},
		map[string]*httptest.Server{"elsewhere.allmymind.org": site}, "elsewhere.allmymind.org", "unresolved.allmymind.org")
	check.Client = site.Client()
	if verdict, why := check.Probe(context.Background()); verdict != application.DoctorOK {
		t.Fatalf("got %v %q", verdict, why)
	}
}

func TestVPSNamesCannotTellWhenTheVPSDoesNotResolve(t *testing.T) {
	check := vpsNamesCheck(map[string]string{}, nil, "kestrel.allmymind.org")
	if verdict, _ := check.Probe(context.Background()); verdict != application.DoctorCannotTell {
		t.Fatalf("got %v", verdict)
	}
}

func TestVPSNamesWithNoNamesIsOK(t *testing.T) {
	check := vpsNamesCheck(nil, nil)
	if verdict, why := check.Probe(context.Background()); verdict != application.DoctorOK || !strings.HasPrefix(why, "n/a") {
		t.Fatalf("got %v %q", verdict, why)
	}
}

func TestVPSNamesCureNotesTheMayor(t *testing.T) {
	site := tlsServer(t, "Welcome to nginx!")
	check := vpsNamesCheck(map[string]string{"allmymind.org": vpsAddr, "kestrel.allmymind.org": vpsAddr},
		map[string]*httptest.Server{"kestrel.allmymind.org": site}, "kestrel.allmymind.org")
	check.Client = site.Client()
	var note string
	check.Note = func(_ context.Context, text string) error { note = text; return nil }
	if err := check.Cure(context.Background()); err != nil || !strings.Contains(note, "kestrel.allmymind.org") {
		t.Fatalf("err %v note %q", err, note)
	}
}
