package doctor

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

var _ application.DoctorCheck = (*VPSNames)(nil)

// VPSNamesName is what the check is called: in the log, and on the command
// line as `mw doctor vps-names`.
const VPSNamesName = "vps-names"

// The vps-names check's damper. Its cure is one note to the Mayor and nothing
// on the VPS, so there is no cure to cap: the wait alone says how often the
// Mayor hears of it.
const (
	VPSNamesDamperWait = 6 * time.Hour
	VPSNamesDamperCap  = 0
)

// vpsNamesFetchWait bounds one fetch of a name's front page.
const vpsNamesFetchWait = 15 * time.Second

// nginxDefaultPage is the words of the page nginx serves for a name it has no
// site for.
const nginxDefaultPage = "Welcome to nginx!"

// VPSNames is the check that no name pointed at the VPS answers with nginx's
// default page or a certificate error: a name whose DNS is set before its site
// exists serves the stock page with no certificate (kestrel.allmymind.org,
// mw-gq6.765). It resolves each of Names and VPS, fetches the front page of
// each name that resolves to the VPS and calls faulty one that fails its
// certificate or says "Welcome to nginx!". A name that does not resolve to the
// VPS, or that cannot be fetched for a reason other than its certificate, is
// not the VPS's to answer for. Its cure is one note to the Mayor; it changes
// nothing on the VPS.
type VPSNames struct {
	// VPS is the VPS's own name, resolved to learn the addresses a name must
	// share to be pointed at it.
	VPS string
	// Names are the names to look at.
	Names []string
	// Lookup resolves a name to its addresses. Nil asks the system resolver.
	Lookup func(ctx context.Context, name string) ([]string, error)
	// Client fetches a front page. Nil is a client with a short timeout.
	Client *http.Client
	// URL is the address fetched for a name. Nil is https://<name>/.
	URL func(name string) string
	// Note puts text in the Mayor's way, as RootDiskBudget's does. Nil writes
	// nothing.
	Note func(ctx context.Context, text string) error
}

// NewVPSNames is the check over names, against the addresses of vps.
func NewVPSNames(vps string, names []string) *VPSNames {
	return &VPSNames{VPS: vps, Names: names}
}

// Name implements application.DoctorCheck.
func (v *VPSNames) Name() string { return VPSNamesName }

// Probe implements application.DoctorCheck: ok, n/a, with no names to look at;
// cannot-tell when the VPS's own name does not resolve; faulty, naming each,
// when a name pointed at the VPS answers with nginx's default page or a
// certificate error; otherwise ok.
func (v *VPSNames) Probe(ctx context.Context) (application.Verdict, string) {
	if len(v.Names) == 0 {
		return application.DoctorOK, "n/a: no names to look at"
	}
	vps, err := v.lookup(ctx, v.VPS)
	if err != nil {
		return application.DoctorCannotTell, fmt.Sprintf("resolving the VPS %s: %v", v.VPS, err)
	}
	var faults []string
	for _, name := range v.Names {
		addrs, err := v.lookup(ctx, name)
		if err != nil || !shares(addrs, vps) {
			continue
		}
		if fault := v.fetch(ctx, name); fault != "" {
			faults = append(faults, fault)
		}
	}
	if len(faults) == 0 {
		return application.DoctorOK, ""
	}
	sort.Strings(faults)
	return application.DoctorFaulty, "a name pointed at the VPS has no site: " + strings.Join(faults, "; ")
}

// fetch is what is wrong with name's front page, empty when nothing is.
func (v *VPSNames) fetch(ctx context.Context, name string) string {
	url := "https://" + name + "/"
	if v.URL != nil {
		url = v.URL(name)
	}
	client := v.Client
	if client == nil {
		client = &http.Client{Timeout: vpsNamesFetchWait}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return ""
	}
	resp, err := client.Do(req)
	if err != nil {
		if isCertError(err) {
			return name + " fails its certificate (" + oneLineErr(err) + ")"
		}
		return ""
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if strings.Contains(string(body), nginxDefaultPage) {
		return name + " answers with nginx's default page"
	}
	return ""
}

// Cure implements application.DoctorCheck: one note to the Mayor. A note that
// cannot be written fails the cure, so the damper tries again.
func (v *VPSNames) Cure(ctx context.Context) error {
	_, why := v.Probe(ctx)
	if v.Note == nil {
		return nil
	}
	return v.Note(ctx, "vps-names: "+why+". Either the name's story serves a holding page with a certificate, "+
		"or its DNS is not pointed at the VPS until the site exists.")
}

// Damper implements application.DoctorCheck.
func (v *VPSNames) Damper() (time.Duration, int) { return VPSNamesDamperWait, VPSNamesDamperCap }

// WayBack implements application.DoctorCheck: the cure changes nothing.
func (v *VPSNames) WayBack() string {
	return "none needed: the cure only writes a note to the Mayor"
}

func (v *VPSNames) lookup(ctx context.Context, name string) ([]string, error) {
	if v.Lookup != nil {
		return v.Lookup(ctx, name)
	}
	return net.DefaultResolver.LookupHost(ctx, name)
}

// shares says a and b hold an address in common.
func shares(a, b []string) bool {
	for _, x := range a {
		for _, y := range b {
			if x == y {
				return true
			}
		}
	}
	return false
}

// isCertError says err is a TLS certificate failure, or the handshake refused
// for want of a certificate for the name, not a connection that could not be
// made.
func isCertError(err error) bool {
	text := err.Error()
	for _, sign := range []string{"x509:", "certificate", "tls: unrecognized name", "tls: internal error"} {
		if strings.Contains(text, sign) {
			return true
		}
	}
	return false
}

func oneLineErr(err error) string {
	return strings.Join(strings.Fields(err.Error()), " ")
}
