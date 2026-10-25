package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/doctor"
	"github.com/Jonathan-A-White/millwright/infrastructure/vpsnginx"
)

// vpsGuardOff is the environment variable that says this process is not to
// ssh to the VPS at all, as MW_METERED=no says it is not to ask Windows: the
// tests' own, so that none of them reaches the real VPS.
const vpsGuardOff = "MW_VPS_GUARD"

// hostVPSNginx is the guard that the VPS's nginx upstream sends the phone to
// the home first (mw-gq6.274), or nil, with no error, when this process is told
// not to look. withBinary also asks the VPS's mw binary whether it holds the
// commit the [doctor] table names, which mw status says and mw doctor does not
// need.
func hostVPSNginx(files application.HomeFile, withBinary bool) (*application.VPSNginx, error) {
	if os.Getenv(vpsGuardOff) == "off" {
		return nil, nil
	}
	guard, err := config.DoctorVPSGuard()
	if err != nil {
		return nil, err
	}
	rigs, err := config.Rigs()
	if err != nil {
		return nil, err
	}
	reader := &application.VPSNginx{
		Home: files,
		VPS:  vpsnginx.New(guard.SSH, guard.Conf, guard.Mw, rigs[application.FactoryRig]),
		Conf: guard.Conf,
	}
	if b, ok := standbyBackend(); ok {
		reader.StandbyHealth = b.VPSHealth
	}
	if withBinary {
		reader.Commit = guard.Needs
	}
	return reader, nil
}

// hostStandby is the comparison of the VPS standby's backend with the home's, for
// mw status (mw-gq6.189), or nil when no [backend.<rig>] table names a VPS standby
// or this process is told not to look. The home's /healthz is where the home's
// backend answers on the factory's network, the standby's the table's vps_health.
func hostStandby(files application.HomeFile) *application.Standby {
	if os.Getenv(vpsGuardOff) == "off" {
		return nil
	}
	b, ok := standbyBackend()
	if !ok {
		return nil
	}
	return &application.Standby{
		Home:    files,
		Commits: vpsnginx.Healthz{},
		HomeURL: func(host string) string {
			url, err := config.PosternLocalURL(host)
			if err != nil {
				url = fmt.Sprintf("http://%s.mw:%d", host, config.DefaultPosternLocalPort)
			}
			return url + "/healthz"
		},
		StandbyURL: b.VPSHealth,
	}
}

// standbyBackend is the first [backend.<rig>] table, by name, that names a VPS
// standby, or false when none does or the tables cannot be read.
func standbyBackend() (config.BackendSettings, bool) {
	backends, err := config.Backends()
	if err != nil {
		return config.BackendSettings{}, false
	}
	names := make([]string, 0, len(backends))
	for name := range backends {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if b := backends[name]; b.VPSHost != "" {
			return b, true
		}
	}
	return config.BackendSettings{}, false
}

// hostVPSNames is the check that no name pointed at the VPS answers with nginx's
// default page or a certificate error (mw-gq6.765), over the allmymind.org
// hosts of every rig's site_url and healthz_url and the [doctor] table's
// vps_names; nil, with no error, when this process is told not to look or there
// is no name to look at.
func hostVPSNames(ctx context.Context, rules interface {
	EpicRequirements(ctx context.Context, rig string) (domain.EpicRequirements, error)
}) (*doctor.VPSNames, error) {
	if os.Getenv(vpsGuardOff) == "off" {
		return nil, nil
	}
	guard, err := config.DoctorVPSGuard()
	if err != nil {
		return nil, err
	}
	extra, err := config.DoctorVPSNames()
	if err != nil {
		return nil, err
	}
	rigs, err := config.Rigs()
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var names []string
	add := func(name string) {
		if strings.HasSuffix(name, ".allmymind.org") && !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	for _, name := range extra {
		add(name)
	}
	for rig := range rigs {
		requirements, err := rules.EpicRequirements(ctx, rig)
		if err != nil {
			continue
		}
		for _, address := range []string{requirements.SiteURL, requirements.HealthzURL} {
			if u, err := url.Parse(address); err == nil {
				add(u.Hostname())
			}
		}
	}
	if len(names) == 0 {
		return nil, nil
	}
	sort.Strings(names)
	vps := guard.SSH
	if _, after, found := strings.Cut(vps, "@"); found {
		vps = after
	}
	return doctor.NewVPSNames(vps, names), nil
}
