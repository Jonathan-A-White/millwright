package main

import (
	"os"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
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
	if withBinary {
		reader.Commit = guard.Needs
	}
	return reader, nil
}
