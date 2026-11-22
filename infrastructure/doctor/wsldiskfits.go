package doctor

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

var (
	_ application.DoctorCheck           = (*WSLDiskFits)(nil)
	_ application.DoctorStandsAfterCure = (*WSLDiskFits)(nil)
)

// WSLDiskFitsName is what the check is called: in the log, and on the command
// line as `mw doctor wsl-disk-fits`.
const WSLDiskFitsName = "wsl-disk-fits"

// WSLDiskFitsMargin is how much smaller than its Windows drive the Linux root
// filesystem must be: 20 GB, so that the vhdx file can never outgrow the drive.
const WSLDiskFitsMargin int64 = 20 << 30

// The wsl-disk-fits check's damper: a note to the Mayor at most every 6 hours,
// and nothing on the host to cap.
const (
	WSLDiskFitsDamperWait = 6 * time.Hour
	WSLDiskFitsDamperCap  = 0
)

// WSLDiskFits is the check that the Linux disk of a WSL host is smaller than its
// Windows drive (mw-6ww.185.5). Linux on the desktop saw a 1,007 GB disk held in
// a file on a 286 GB drive, so nothing inside WSL ever felt the drive fill; the
// file only grows. Faulty, on a WSL host that is not the home, when the root
// filesystem's size is larger than the drive's size less 20 GB (WSLDiskFitsMargin),
// both numbers said and the cure named: `mw host wsl-flag shrink`. The drive's
// size is the vhdx_drive line of windows-state, which the Windows task writes;
// with none, cannot-tell. On the home it is never faulty (its disk is not shrunk
// without the Governor's word): the ok line says the two numbers and that the
// drive is watched by wsl-drive-free. Ok on a host that is not WSL. Its cure is
// one note to the Mayor: the shrink is a Windows task's, behind a drained host.
type WSLDiskFits struct {
	// Path is the windows-state file. Empty reads WSLDriveFreeDir/WindowsStateFile
	// under the home directory.
	Path string
	// DetectVirt says what systemd-detect-virt prints. Nil runs the real one.
	DetectVirt func(ctx context.Context) (string, error)
	// Home says whether this host is the home. Nil reads as not the home.
	Home func(ctx context.Context) (bool, error)
	// RootSize is the size, in bytes, of the root filesystem as df says it. Nil
	// reads the real /.
	RootSize func() (int64, error)
	// Note puts text in the Mayor's way. Nil writes nothing.
	Note func(ctx context.Context, text string) error

	// last is what the latest Probe found faulty, for the note Cure writes.
	last string
}

// NewWSLDiskFits is the check over the real windows-state file and root.
func NewWSLDiskFits() *WSLDiskFits { return &WSLDiskFits{} }

// Name implements application.DoctorCheck.
func (w *WSLDiskFits) Name() string { return WSLDiskFitsName }

// Probe implements application.DoctorCheck.
func (w *WSLDiskFits) Probe(ctx context.Context) (application.Verdict, string) {
	w.last = ""
	virt, err := (&WSLTrim{DetectVirt: w.DetectVirt}).detectVirt(ctx)
	if err != nil {
		return application.DoctorOK, "not WSL: systemd-detect-virt could not be run, so this host is not told to be WSL"
	}
	if virt != "wsl" {
		return application.DoctorOK, "not WSL (systemd-detect-virt says " + virt + ")"
	}
	home := false
	if w.Home != nil {
		if home, err = w.Home(ctx); err != nil {
			return application.DoctorCannotTell, fmt.Sprintf("cannot tell whether this host is the home: %v", err)
		}
	}
	root, err := w.rootSize()
	if err != nil {
		return application.DoctorCannotTell, err.Error()
	}
	drive, driveErr := w.driveSize()
	if home {
		if driveErr != nil {
			return application.DoctorOK, fmt.Sprintf("the Linux disk is %s; the drive's size is not read here (%v); the home's disk is not shrunk without the Governor's word, watched by wsl-drive-free", gbi(root), driveErr)
		}
		return application.DoctorOK, fmt.Sprintf("the Linux disk is %s on a %s drive; the home's disk is not shrunk without the Governor's word, watched by wsl-drive-free", gbi(root), gbi(drive))
	}
	if driveErr != nil {
		return application.DoctorCannotTell, driveErr.Error()
	}
	if root > drive-WSLDiskFitsMargin {
		w.last = fmt.Sprintf("the Linux disk is %s on a %s Windows drive, more than the drive's size less %d GB: its file can outgrow the drive and nothing inside WSL will feel it fill; cure: drain the host (mw config set cap 0), sweep it, then `mw host wsl-flag shrink`",
			gbi(root), gbi(drive), WSLDiskFitsMargin>>30)
		return application.DoctorFaulty, w.last
	}
	return application.DoctorOK, fmt.Sprintf("the Linux disk is %s on a %s drive, under its size less %d GB", gbi(root), gbi(drive), WSLDiskFitsMargin>>30)
}

// Cure implements application.DoctorCheck: one note to the Mayor, nothing on the
// host changed. A note that cannot be written fails the cure, so the damper
// tries again.
func (w *WSLDiskFits) Cure(ctx context.Context) error {
	if w.Note == nil {
		return nil
	}
	return w.Note(ctx, "wsl-disk-fits: "+w.last+". The shrink is the Governor's word: it shuts WSL down, so `mw host wsl-flag shrink --host <host>` is run once the host is drained.")
}

// CureLeavesFault implements application.DoctorStandsAfterCure: the cure is a
// note, and the disk stays as large as the probe found it.
func (w *WSLDiskFits) CureLeavesFault() bool { return true }

// Damper implements application.DoctorCheck.
func (w *WSLDiskFits) Damper() (time.Duration, int) {
	return WSLDiskFitsDamperWait, WSLDiskFitsDamperCap
}

// WayBack implements application.DoctorCheck.
func (w *WSLDiskFits) WayBack() string {
	return "nothing was changed on the host; the note it wrote is cleared by the Mayor"
}

func (w *WSLDiskFits) rootSize() (int64, error) {
	if w.RootSize != nil {
		return w.RootSize()
	}
	return RootSizeBytes()
}

// RootSizeBytes is the size of the root filesystem, as statfs of / reports it:
// blocks x block size (the Size column of df).
func RootSizeBytes() (int64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs("/", &st); err != nil {
		return 0, fmt.Errorf("statfs /: %w", err)
	}
	return int64(st.Blocks) * int64(st.Bsize), nil
}

// driveSize is the size of the vhdx's drive from the vhdx_drive line of
// windows-state, '<letter> <size bytes> <free bytes>'.
func (w *WSLDiskFits) driveSize() (int64, error) {
	path := w.Path
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return 0, fmt.Errorf("there is no home directory to read %s in: %w", WindowsStateFile, err)
		}
		path = filepath.Join(home, WSLDriveFreeDir, WindowsStateFile)
	}
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, fmt.Errorf("there is no %s at %s, so the Windows drive's size is not known: the Windows task writes it; `mw host wsl-flag --install-line` prints the line that installs the task", WindowsStateFile, path)
	}
	if err != nil {
		return 0, fmt.Errorf("%s could not be read: %v", WindowsStateFile, err)
	}
	fields := strings.Fields(parseWindowsState(string(raw)).one("vhdx_drive"))
	if len(fields) < 2 {
		return 0, fmt.Errorf("%s at %s has no 'vhdx_drive <letter> <size> <free>' line, so the Windows drive's size is not known", WindowsStateFile, path)
	}
	size, err := strconv.ParseInt(fields[1], 10, 64)
	if err != nil || size <= 0 {
		return 0, fmt.Errorf("the vhdx_drive line of %s at %s does not hold a size: %q", WindowsStateFile, path, strings.Join(fields, " "))
	}
	return size, nil
}

// gbi is bytes as gigabytes of 2^30, whole: the unit Windows says a drive in.
func gbi(bytes int64) string { return fmt.Sprintf("%d GB", bytes>>30) }
