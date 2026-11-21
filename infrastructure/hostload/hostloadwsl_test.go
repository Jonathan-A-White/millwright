package hostload_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/hostload"
)

// mw-gq6.1527: a reading knows a host is WSL from its kernel's version line, so
// that the desktop, sealed from Windows and with no drive report, has no room
// rather than none of the drive guard.
func TestProcKnowsAWSLHostFromItsKernelAndHoldsItWithoutADriveReport(t *testing.T) {
	dir := t.TempDir()
	load := filepath.Join(dir, "loadavg")
	version := filepath.Join(dir, "version")
	if err := os.WriteFile(load, []byte("0.10 0.10 0.10 1/1 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1763245200, 0)
	proc := hostload.Proc{File: load, Cores: 4, DriveFile: filepath.Join(dir, "windows-drive-free"),
		VersionFile: version, Now: func() time.Time { return now }}
	for _, tt := range []struct {
		kernel string
		wsl    bool
	}{
		{"Linux version 6.6.87.2-microsoft-standard-WSL2 (root@439a258ad544) (gcc (GCC) 11.2.0) #1 SMP", true},
		{"Linux version 5.15.0-1-Microsoft (Microsoft@Microsoft.com) (gcc version 5.4.0) #1-Microsoft", true},
		{"Linux version 6.8.0-149-generic (buildd@lcy02-amd64-001) (x86_64-linux-gnu-gcc-13) #169-Ubuntu SMP", false},
	} {
		if err := os.WriteFile(version, []byte(tt.kernel+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		got, err := proc.Load(context.Background())
		if err != nil || got.WSL != tt.wsl {
			t.Errorf("%q: WSL = %v, %v; want %v", tt.kernel, got.WSL, err, tt.wsl)
			continue
		}
		why := got.NoRoom(application.RoomLimits{})
		if blind := strings.Contains(why, "cannot see its Windows drive"); blind != tt.wsl {
			t.Errorf("%q with no drive report: NoRoom = %q", tt.kernel, why)
		}
	}
	if err := os.Remove(version); err != nil {
		t.Fatal(err)
	}
	if got, err := proc.Load(context.Background()); err != nil || got.WSL {
		t.Errorf("no version file: WSL = %v, %v; want false and no error", got.WSL, err)
	}
}
