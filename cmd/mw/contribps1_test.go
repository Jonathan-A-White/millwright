package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// powerShellExe finds Windows PowerShell: on the PATH, or under /mnt/c on a
// WSL host whose PATH leaves Windows out. "" when there is none.
func powerShellExe() string {
	if p, err := exec.LookPath("powershell.exe"); err == nil {
		return p
	}
	const mounted = "/mnt/c/Windows/System32/WindowsPowerShell/v1.0/powershell.exe"
	if _, err := os.Stat(mounted); err == nil {
		return mounted
	}
	return ""
}

// TestContribPowerShellParses parses every .ps1 the factory ships under
// contrib/ with Windows PowerShell's own parser (5.1 on every host): a script
// it refuses is never run, and the scheduled task that runs it reports
// success all the same (mw-6ww.185.18: "$want: begin" is a drive reference).
func TestContribPowerShellParses(t *testing.T) {
	ps := powerShellExe()
	if ps == "" {
		t.Skip("powershell.exe is not on this host (not on the PATH, not under /mnt/c): the parse of contrib/**/*.ps1 is not run here")
	}
	root := filepath.Join("..", "..", "contrib")
	var files []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(path, ".ps1") {
			files = append(files, path)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no .ps1 file under contrib/: the walk is wrong")
	}
	const parse = `$e=$null;$t=$null;[void][System.Management.Automation.Language.Parser]::ParseFile($env:MW_PS1,[ref]$t,[ref]$e);` +
		`foreach($x in $e){ '{0}:{1}: {2}' -f $x.Extent.StartLineNumber,$x.Extent.StartColumnNumber,$x.Message }`
	for _, file := range files {
		abs, err := filepath.Abs(file)
		if err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(ps, "-NoProfile", "-Command", parse)
		// WSLENV's /p turns the Linux path into the Windows one on its way in.
		cmd.Env = append(os.Environ(), "MW_PS1="+abs, "WSLENV=MW_PS1/p:"+os.Getenv("WSLENV"))
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Errorf("%s: powershell.exe failed to run the parse: %v\n%s", file, err, out)
			continue
		}
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			if line = strings.TrimSpace(line); line != "" {
				t.Errorf("%s:%s", file, line)
			}
		}
	}
}
