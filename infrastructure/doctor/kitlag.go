package doctor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/gitcmd"
)

var _ application.DoctorCheck = (*KitLag)(nil)

// KitLagName is what the check is called: in the log, and on the command line
// as `mw doctor kit-lag`.
const KitLagName = "kit-lag"

// The kit-lag check's damper: it never cures, so one episode spends one.
const (
	KitLagDamperWait = 0 * time.Second
	KitLagDamperCap  = 1
)

const kitLagNoCure = "no cure: a person moves the rig's pin of the kit in its package.json"

// kitPin is a package.json dependency on a kit at a commit, as npm writes it:
// "github:<owner>/<kit>#<sha>".
var kitPin = regexp.MustCompile(`^(?:github:|git\+ssh://git@github\.com/|git\+https://github\.com/)[^/]+/([^/#]+?)(?:\.git)?#([0-9a-f]{7,40})$`)

// KitLag is the check that each rig is not far behind the kits it pins
// (mw-6ww.175.13). A kit is a boundary only if its fixes reach the rigs; a rig
// pins a kit by hand, at a commit, in its package.json, and nothing moved the
// pin when the kit's head did. The check counts, for each rig and each kit the
// rig pins whose checkout is on this host, the commits the kit's head is ahead
// of the pin, and warns (cannot-tell) with one line for each that is behind, as
// "postern: voice-kit 78 behind". It reads checkouts and changes nothing.
type KitLag struct {
	// Rigs maps a rig's name to its checkout's directory.
	Rigs map[string]string
	// KitDir is the checkout of a kit on this host, for the rig that pins it;
	// a kit whose directory is not a repository is left out.
	KitDir func(rig, kit string) string
	// Program is the git command; empty reads "git".
	Program string
}

// SiblingKitDir is the KitDir for kits checked out beside the rigs: the
// directory named for the kit in the rig's parent directory.
func SiblingKitDir(rigs map[string]string) func(rig, kit string) string {
	return func(rig, kit string) string { return filepath.Join(filepath.Dir(rigs[rig]), kit) }
}

// Name implements application.DoctorCheck.
func (c *KitLag) Name() string { return KitLagName }

// Lag lists one line, "<rig>: <kit> <n> behind", for each rig and kit whose pin
// is behind the kit's head, in the order of the rig's and then the kit's name.
// A rig without a package.json, a kit without a checkout, and a pin the checkout
// does not hold are left out: there is nothing to count.
func (c *KitLag) Lag(ctx context.Context) ([]string, error) {
	rigs := make([]string, 0, len(c.Rigs))
	for rig := range c.Rigs {
		rigs = append(rigs, rig)
	}
	sort.Strings(rigs)
	var lines []string
	for _, rig := range rigs {
		pins, err := kitPins(filepath.Join(c.Rigs[rig], "package.json"))
		if err != nil {
			continue
		}
		kits := make([]string, 0, len(pins))
		for kit := range pins {
			kits = append(kits, kit)
		}
		sort.Strings(kits)
		for _, kit := range kits {
			behind, ok := c.behind(ctx, c.KitDir(rig, kit), pins[kit])
			if ok && behind > 0 {
				lines = append(lines, fmt.Sprintf("%s: %s %d behind", rig, kit, behind))
			}
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
	return lines, nil
}

// kitPins reads the kits a package.json pins, by the kit's repository name, from its
// dependencies of every kind.
func kitPins(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var manifest map[string]json.RawMessage
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, err
	}
	pins := map[string]string{}
	for _, section := range []string{"dependencies", "devDependencies", "optionalDependencies"} {
		var deps map[string]string
		if raw, ok := manifest[section]; !ok || json.Unmarshal(raw, &deps) != nil {
			continue
		}
		for _, spec := range deps {
			if m := kitPin.FindStringSubmatch(spec); m != nil && strings.HasSuffix(m[1], "-kit") {
				pins[m[1]] = m[2]
			}
		}
	}
	return pins, nil
}

func (c *KitLag) behind(ctx context.Context, dir, sha string) (int, bool) {
	program := c.Program
	if program == "" {
		program = "git"
	}
	out, err := gitcmd.Command(ctx, program, dir, "rev-list", "--count", sha+"..HEAD").Output()
	if err != nil {
		return 0, false
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(out)))
	return n, err == nil
}

// Probe implements application.DoctorCheck: ok when no pin is behind, cannot-tell
// naming each rig and kit that is, or when the lag could not be read.
func (c *KitLag) Probe(ctx context.Context) (application.Verdict, string) {
	lines, err := c.Lag(ctx)
	if err != nil {
		return application.DoctorCannotTell, fmt.Sprintf("the kits' lag could not be read: %v", err)
	}
	if len(lines) == 0 {
		return application.DoctorOK, ""
	}
	return application.DoctorCannotTell, fmt.Sprintf("%d rig pins of a kit are behind its head, so the kit's fixes have not reached the rig: %s",
		len(lines), strings.Join(lines, "; "))
}

// Cure implements application.DoctorCheck: there is none.
func (c *KitLag) Cure(context.Context) error { return errors.New(kitLagNoCure) }

// Damper implements application.DoctorCheck.
func (c *KitLag) Damper() (time.Duration, int) { return KitLagDamperWait, KitLagDamperCap }

// WayBack implements application.DoctorCheck: nothing ever changes.
func (c *KitLag) WayBack() string { return "none: no cure runs" }
