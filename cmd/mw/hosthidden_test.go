package main

import (
	"fmt"
	"strings"
	"testing"
)

func hostHiddenPaste(t *testing.T) string {
	t.Helper()
	out, err := runMw(t, "host", "hidden")
	if err != nil {
		t.Fatalf("mw host hidden failed: %v\n%s", err, out)
	}
	return out
}

// mw host hidden prints one paste that rewrites the action of every scheduled
// task named millwright*, wherever it came from, to run under conhost.exe
// --headless (mw-gq6.1430).
func TestHostHiddenPasteRewritesEveryMillwrightAction(t *testing.T) {
	out := hostHiddenPaste(t)
	assertElevationFirst(t, "host hidden", out)
	if !strings.Contains(out, windowsElevationCheck) {
		t.Errorf("the paste must use windowsElevationCheck")
	}
	for _, want := range []string{
		"Get-ScheduledTask",
		"-like 'millwright*'",
		"-Execute 'conhost.exe'",
		"--headless $x $($a.Arguments)",
		"-ieq 'conhost.exe'",
		"Set-ScheduledTask",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the paste lacks %q:\n%s", want, out)
		}
	}
	// the old exe and args are printed, with the line that restores them, before the first change
	way := strings.Index(out, "Way back")
	set := strings.Index(out, "Set-ScheduledTask -TaskName $mwhd_t.TaskName -TaskPath $mwhd_t.TaskPath -Action $mwhd_new")
	if set < 0 {
		t.Fatalf("no change statement in:\n%s", out)
	}
	if way < 0 || way > set {
		t.Errorf("the way back (%d) must be printed before the first change (%d)", way, set)
	}
	for _, want := range []string{"$a.Execute", "$a.Arguments", "New-ScheduledTaskAction -Execute", "Set-ScheduledTask -TaskName"} {
		if !strings.Contains(out[:set], want) {
			t.Errorf("the old %s must be printed before any change", want)
		}
	}
	// a task already headless is skipped, so a second run changes nothing
	if !strings.Contains(out, "already headless") {
		t.Errorf("the paste must say it leaves a headless task alone")
	}
	if !strings.Contains(out, "changed:") {
		t.Errorf("the paste must print each task it changed")
	}
}

// Below Windows build 19041 conhost.exe does not take --headless: the paste
// throws before it reads or changes any task.
func TestHostHiddenPasteThrowsOnAnOldWindowsBeforeChangingAnyTask(t *testing.T) {
	out := hostHiddenPaste(t)
	check := fmt.Sprintf("[Environment]::OSVersion.Version.Build -lt %d) { throw", giveWayMinBuild)
	i := strings.Index(out, check)
	if i < 0 {
		t.Fatalf("the paste lacks the build check %q:\n%s", check, out)
	}
	if !strings.Contains(out[i:], "nothing changed") {
		t.Errorf("the build throw must say nothing changed")
	}
	for _, first := range []string{"Get-ScheduledTask", "Set-ScheduledTask"} {
		if j := strings.Index(out, first); j >= 0 && j < i {
			t.Errorf("%s (%d) comes before the build check (%d)", first, j, i)
		}
	}
}
