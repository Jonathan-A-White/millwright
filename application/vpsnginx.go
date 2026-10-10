package application

import (
	"context"
	"fmt"
	"net/url"
	"strings"
)

// DefaultVPSNginxConf is the VPS's nginx site file, whose postern_api upstream
// the guard reads, when nothing says otherwise.
const DefaultVPSNginxConf = "/etc/nginx/sites-enabled/postern.allmymind.org.conf"

// vpsUpstreamName is the upstream the phone's /api requests pass through. It
// is the name mw postern nginx writes (posternUpstreamName).
const vpsUpstreamName = posternUpstreamName

// VPSProbe is what the guard reads on the VPS, over ssh, and never changes.
type VPSProbe interface {
	// NginxSite is the text of the VPS's nginx site file.
	NginxSite(ctx context.Context) (string, error)

	// BinaryLacks says whether the VPS's mw binary was built from a tree that
	// does not hold commit. An error is the binary's age being unknowable: it
	// has no revision stamped, or the commit is not known here.
	BinaryLacks(ctx context.Context, commit string) (bool, error)
}

// VPSNginxReader is what mw status asks for the VPS NGINX line.
type VPSNginxReader interface {
	Read(ctx context.Context) VPSNginxReading
}

// VPSNginxState is what the guard found.
type VPSNginxState int

// The three things the guard may find.
const (
	// VPSNginxOK: the home's server first, every other one `backup`.
	VPSNginxOK VPSNginxState = iota
	// VPSNginxFault: the upstream is not that.
	VPSNginxFault
	// VPSNginxNotChecked: the file could not be read, or the home told.
	VPSNginxNotChecked
)

// VPSNginxReading is what the guard found on one read.
type VPSNginxReading struct {
	State VPSNginxState
	// Backups is how many servers are marked backup, for an ok reading.
	Backups int
	// Why is the fault, in full, or why nothing was checked. Short is the
	// fault in a few words, for the status line.
	Why, Short string
	// Fix is the one command, run on the VPS, that puts a fault right, and
	// WayBack the one that undoes it.
	Fix, WayBack string
	// Lacks is the commit the VPS's mw binary was found to lack, empty when it
	// has it or when that could not be told.
	Lacks string
}

// Line is the reading as mw status prints it.
func (r VPSNginxReading) Line() string {
	var line string
	switch r.State {
	case VPSNginxOK:
		line = fmt.Sprintf("VPS NGINX ok (home first, %d backup)", r.Backups)
	case VPSNginxFault:
		line = "VPS NGINX FAULT: " + r.Short
	default:
		return "VPS NGINX not checked (" + r.Why + ")"
	}
	if r.Lacks != "" {
		line += " · bin/mw lacks " + r.Lacks
	}
	return line
}

// VPSNginx is the guard that the VPS's nginx postern_api upstream sends the
// phone to the home first, and to every other backend only as `backup`. nginx
// takes two plain servers as equal round-robin peers, so a backend that is
// asleep hangs every second request until it times out, and Postern calls the
// line offline (mw-gq6.274). mw postern serve and mw postern nginx write the
// right shape (mw-gq6.267), but the VPS's file is edited by hand and its mw
// is copied there by hand, so the factory looks.
//
// It reads, over the VPS probe, and changes nothing. A file it cannot read, a
// block it cannot find or a home it cannot tell is not checked, never a fault.
type VPSNginx struct {
	Home HomeFile
	VPS  VPSProbe

	// Conf is the site file's path, named in the fix. Empty reads
	// DefaultVPSNginxConf.
	Conf string

	// StandbyHealth, when set, is the standby's /healthz URL (a [backend.<rig>] vps_health):
	// its host:port must be a `backup` server of the upstream too.
	StandbyHealth string

	// Commit, when set, is a commit the VPS's mw binary is also looked at for:
	// a binary built without it is named in the reading. A host with no way to
	// tell leaves the point out.
	Commit string
}

var _ VPSNginxReader = VPSNginx{}

// Read is the guard's reading now.
func (v VPSNginx) Read(ctx context.Context) VPSNginxReading {
	notChecked := func(why string) VPSNginxReading {
		return VPSNginxReading{State: VPSNginxNotChecked, Why: why}
	}
	if v.Home == nil || v.VPS == nil {
		return notChecked("no home file or VPS to read")
	}
	record, err := WhereIsHome(ctx, v.Home)
	if err != nil {
		return notChecked(oneLine(err.Error()))
	}
	text, err := v.VPS.NginxSite(ctx)
	if err != nil {
		return notChecked(oneLine(err.Error()))
	}
	servers, found := ParseUpstreamServers(text, vpsUpstreamName)
	if !found {
		return notChecked("no " + vpsUpstreamName + " upstream in the file")
	}

	reading := v.judge(servers, record.Host)
	if reading.State == VPSNginxOK {
		if standby := standbyAddr(v.StandbyHealth); standby != "" && !hasBackup(servers, standby) {
			reading = v.standbyMissing(standby)
		}
	}
	if v.Commit != "" {
		if lacks, err := v.VPS.BinaryLacks(ctx, v.Commit); err == nil && lacks {
			reading.Lacks = v.Commit
		}
	}
	return reading
}

// UpstreamServer is one `server` line of an nginx upstream block.
type UpstreamServer struct {
	Addr   string
	Backup bool
}

// Host is the server's address without its port.
func (s UpstreamServer) Host() string {
	host, _, found := strings.Cut(s.Addr, ":")
	if !found {
		return s.Addr
	}
	return host
}

// ParseUpstreamServers reads the servers of the upstream block called name out
// of an nginx file's text, in order, and whether there is such a block.
// Comments and server parameters are read past; only `backup` is kept.
func ParseUpstreamServers(text, name string) ([]UpstreamServer, bool) {
	var servers []UpstreamServer
	inside, found := false, false
	for _, line := range strings.Split(text, "\n") {
		if before, _, cut := strings.Cut(line, "#"); cut {
			line = before
		}
		fields := strings.Fields(strings.TrimSuffix(strings.TrimSpace(line), ";"))
		switch {
		case inside && len(fields) == 1 && fields[0] == "}":
			return servers, found
		case inside && len(fields) >= 2 && fields[0] == "server":
			server := UpstreamServer{Addr: strings.TrimSuffix(fields[1], ";")}
			for _, param := range fields[2:] {
				server.Backup = server.Backup || param == "backup"
			}
			servers = append(servers, server)
		case !inside && len(fields) >= 2 && fields[0] == "upstream" && fields[1] == name:
			inside, found = true, true
		}
	}
	return servers, found
}

// standbyAddr is the host:port of the standby's /healthz URL, empty when there
// is none or it does not parse.
func standbyAddr(health string) string {
	if health == "" {
		return ""
	}
	u, err := url.Parse(health)
	if err != nil {
		return ""
	}
	return u.Host
}

// hasBackup says servers holds addr as a `backup` server.
func hasBackup(servers []UpstreamServer, addr string) bool {
	for _, server := range servers {
		if server.Backup && server.Addr == addr {
			return true
		}
	}
	return false
}

// standbyMissing is the fault for an upstream that is right about the home but
// does not carry the standby's addr (host:port) as a backup, so the front door
// has nowhere to fall back to when the home sleeps.
func (v VPSNginx) standbyMissing(addr string) VPSNginxReading {
	conf := v.Conf
	if conf == "" {
		conf = DefaultVPSNginxConf
	}
	short := "standby " + addr + " not in the upstream"
	return VPSNginxReading{
		State: VPSNginxFault,
		Short: short,
		Why:   fmt.Sprintf("%s: add `server %s backup;` as its last line (mw postern nginx --backend ... --backend http://%s)", short, addr, addr),
		Fix: fmt.Sprintf("cp %s %s.bak && add `server %s backup;` as the last line of the %s block in %s, then nginx -t && systemctl reload nginx",
			conf, conf, addr, vpsUpstreamName, conf),
		WayBack: fmt.Sprintf("cp %s.bak %s && nginx -t && systemctl reload nginx", conf, conf),
	}
}

// judge is the reading for servers when the home is home.
func (v VPSNginx) judge(servers []UpstreamServer, home string) VPSNginxReading {
	conf := v.Conf
	if conf == "" {
		conf = DefaultVPSNginxConf
	}
	homeHost := home + ".mw"

	var plain []UpstreamServer
	for _, server := range servers {
		if !server.Backup {
			plain = append(plain, server)
		}
	}
	right := len(plain) == 1 && plain[0].Host() == homeHost
	if right {
		return VPSNginxReading{State: VPSNginxOK, Backups: len(servers) - 1}
	}

	var held []string
	for _, server := range servers {
		if server.Backup {
			held = append(held, server.Addr+" backup")
		} else {
			held = append(held, server.Addr)
		}
	}
	fault := VPSNginxReading{
		State:   VPSNginxFault,
		WayBack: fmt.Sprintf("cp %s.bak %s && nginx -t && systemctl reload nginx", conf, conf),
	}
	switch {
	case len(plain) == 0:
		fault.Short = "no server takes traffic"
		fault.Why = "every server is backup"
	case len(plain) > 1:
		fault.Short = fmt.Sprintf("%d servers take traffic", len(plain))
		fault.Why = fmt.Sprintf("%d servers take traffic, so nginx round-robins between them and a sleeping one hangs every other request", len(plain))
	default:
		fault.Short = plain[0].Addr + " first, home is " + home
		fault.Why = fmt.Sprintf("%s takes the traffic, but home is %s", plain[0].Addr, home)
	}
	fault.Why += " (the block holds: " + strings.Join(held, ", ") + ")"

	var edits []string
	atHome := false
	for _, server := range servers {
		switch {
		case server.Host() == homeHost && server.Backup:
			edits = append(edits, fmt.Sprintf("s/server %s backup;/server %s;/", server.Addr, server.Addr))
			atHome = true
		case server.Host() == homeHost:
			atHome = true
		case !server.Backup:
			edits = append(edits, fmt.Sprintf("s/server %s;/server %s backup;/", server.Addr, server.Addr))
		}
	}
	if !atHome {
		fault.Fix = fmt.Sprintf("add `server %s:8787;` as the first server of the %s block in %s, then nginx -t && systemctl reload nginx (copy %s to %s.bak first)",
			homeHost, vpsUpstreamName, conf, conf, conf)
		return fault
	}
	sed := ""
	for _, edit := range edits {
		sed += " -e '" + edit + "'"
	}
	fault.Fix = fmt.Sprintf("cp %s %s.bak && sed -i%s %s && nginx -t && systemctl reload nginx", conf, conf, sed, conf)
	return fault
}

// CommitSource is what a backend's /healthz is asked its commit through.
type CommitSource interface {
	// Commit is the commit the backend answering at url says it was built from,
	// empty when it answers with none, as a build older than its healthz's commit
	// field does. An error is a backend that did not answer.
	Commit(ctx context.Context, url string) (string, error)
}

// StandbyReader is what mw status asks for the standby line.
type StandbyReader interface {
	Read(ctx context.Context) StandbyReading
}

// StandbyReading is what one comparison of the standby's backend with the home's
// found.
type StandbyReading struct {
	// Standby and Home are the two commits, Standby empty when it names none. Why is
	// set instead when the comparison could not be made.
	Standby, Home string
	Why           string
}

// Behind says the standby runs a build other than the home's.
func (r StandbyReading) Behind() bool {
	return r.Why == "" && !sameCommit(r.Standby, r.Home)
}

// Line is the reading as mw status prints it.
func (r StandbyReading) Line() string {
	switch {
	case r.Why != "":
		return "standby not checked (" + r.Why + ")"
	case r.Behind():
		standby := r.Standby
		if standby == "" {
			standby = "none"
		}
		return "standby behind: " + standby + " vs " + r.Home
	}
	return "standby level at " + r.Home
}

// sameCommit says a and b name one commit, one of them being the other cut short.
func sameCommit(a, b string) bool {
	return a != "" && b != "" && (strings.HasPrefix(a, b) || strings.HasPrefix(b, a))
}

// Standby compares the backend the VPS standby runs with the home's, from the
// commit each says at its /healthz (mw-gq6.189): the front door serves the
// standby whenever the home is asleep, and a landing that staged only the home's
// backend leaves the standby on whatever it was built from. It reads and changes
// nothing; a backend that does not answer, or a home with no commit to compare,
// is not checked, never behind.
type Standby struct {
	Home    HomeFile
	Commits CommitSource

	// HomeURL is the /healthz of the home's backend, given the home's name, and
	// StandbyURL the standby's.
	HomeURL    func(host string) string
	StandbyURL string
}

var _ StandbyReader = Standby{}

// Read is the comparison now.
func (s Standby) Read(ctx context.Context) StandbyReading {
	notChecked := func(why string) StandbyReading { return StandbyReading{Why: why} }
	if s.Home == nil || s.Commits == nil || s.HomeURL == nil || s.StandbyURL == "" {
		return notChecked("no home file or backend to read")
	}
	record, err := WhereIsHome(ctx, s.Home)
	if err != nil {
		return notChecked(oneLine(err.Error()))
	}
	home, err := s.Commits.Commit(ctx, s.HomeURL(record.Host))
	if err != nil {
		return notChecked("the home's backend: " + oneLine(err.Error()))
	}
	if home == "" || home == "dev" {
		return notChecked("the home's backend names no commit")
	}
	standby, err := s.Commits.Commit(ctx, s.StandbyURL)
	if err != nil {
		return notChecked("the standby: " + oneLine(err.Error()))
	}
	return StandbyReading{Standby: standby, Home: home}
}
