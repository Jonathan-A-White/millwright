package steps

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"

	"github.com/cucumber/godog"
)

// registerDoctorLineSteps registers the steps that have another host's doctor
// run and read the line it leaves under that host in the report.
func (c *statusContext) registerDoctorLineSteps(ctx *godog.ScenarioContext) {
	ctx.Given(`^the doctor of "([^"]*)" runs now and finds everything ok$`, func(host string) error {
		return c.theDoctorRan(host, 0)
	})
	ctx.Given(`^the doctor of "([^"]*)" runs now and finds "([^"]*)" faulty: "([^"]*)"(?: and "([^"]*)" faulty: "([^"]*)")?$`,
		func(host, check, reason, check2, reason2 string) error {
			return c.theDoctorRan(host, 0, [2]string{check, reason}, [2]string{check2, reason2})
		})
	ctx.Given(`^the doctor of "([^"]*)" ran (\d+) hours ago and found everything ok$`, func(host, hours string) error {
		return c.theDoctorRanHoursAgo(host, hours)
	})
	ctx.Given(`^the doctor of "([^"]*)" ran (\d+) hours ago and found "([^"]*)" faulty: "([^"]*)"$`, func(host, hours, check, reason string) error {
		return c.theDoctorRanHoursAgo(host, hours, [2]string{check, reason})
	})

	ctx.Then(`^under OTHER HOSTS the host "([^"]*)" shows the doctor line: "([^"]*)"$`, c.otherHostShowsDoctorLine)
	ctx.Then(`^under OTHER HOSTS the host "([^"]*)" shows no doctor line$`, c.otherHostShowsNoDoctorLine)
	ctx.Then(`^the DAMPERS section says the doctor of "([^"]*)" finds "([^"]*)" faulty: "([^"]*)"$`, c.theDampersSayTheDoctorFinds)
}

func (c *statusContext) theDoctorRanHoursAgo(host, hoursText string, faulty ...[2]string) error {
	hours, err := strconv.Atoi(hoursText)
	if err != nil {
		return fmt.Errorf("the hours %q are not a number: %w", hoursText, err)
	}
	return c.theDoctorRan(host, time.Duration(hours)*time.Hour, faulty...)
}

// theDoctorRan runs the whole doctor of host once, ago before the scenario's
// clock, with each faulty check a check that cannot be cured (as boost-config
// cannot) and every other a check that is ok; the note the run leaves in the
// tracker is what the home reads.
func (c *statusContext) theDoctorRan(host string, ago time.Duration, faulty ...[2]string) error {
	checks := application.DoctorChecks{&apptest.FakeDoctorCheck{CheckName: "wifi", Verdict: application.DoctorOK}}
	for _, f := range faulty {
		if f[0] == "" {
			continue
		}
		checks = append(checks, &apptest.FakeDoctorCheck{
			CheckName: f[0], Verdict: application.DoctorFaulty, Reason: f[1],
			CureErr: errors.New("no cure: " + f[1]),
			Cap:     1,
		})
	}
	_, err := application.Doctor{
		Checks:   checks,
		State:    apptest.NewFakeDoctorState(),
		Log:      &apptest.FakeDoctorLog{},
		Verdicts: c.tracker,
		Host:     host,
		Now:      func() time.Time { return c.now.Add(-ago) },
	}.Run(context.Background(), "", false)
	if _, fault := application.DoctorFaults(err); err != nil && !fault {
		return fmt.Errorf("the doctor of %s could not run: %w", host, err)
	}
	return nil
}

func (c *statusContext) hostBlock(host string) ([]string, error) {
	lines, err := c.printedLines()
	if err != nil {
		return nil, err
	}
	block := blockAt(sectionOf(lines, "OTHER HOSTS"), "  "+host+" ")
	if block == nil {
		return nil, fmt.Errorf("expected %s to be listed under OTHER HOSTS, got:\n%s", host, c.report.String())
	}
	return block, nil
}

func (c *statusContext) otherHostShowsDoctorLine(host, words string) error {
	block, err := c.hostBlock(host)
	if err != nil {
		return err
	}
	// A long line wraps to the phone's width: compare the words, not the breaks.
	if !strings.Contains(strings.Join(strings.Fields(strings.Join(block, " ")), " "), words) {
		return fmt.Errorf("expected %s under OTHER HOSTS to show the doctor line %q, got:\n%s", host, words, c.report.String())
	}
	return nil
}

func (c *statusContext) otherHostShowsNoDoctorLine(host string) error {
	block, err := c.hostBlock(host)
	if err != nil {
		return err
	}
	if strings.Contains(strings.Join(block, "\n"), "doctor") {
		return fmt.Errorf("expected %s to show no doctor line, got:\n%s", host, c.report.String())
	}
	return nil
}

func (c *statusContext) theDampersSayTheDoctorFinds(host, check, reason string) error {
	if err := c.readingStatusSucceeds(); err != nil {
		return err
	}
	want := fmt.Sprintf("doctor: %s: %s faulty", host, check)
	text := c.dampersText()
	if !strings.Contains(text, want) || !strings.Contains(text, reason) {
		return fmt.Errorf("expected the DAMPERS section to say %q with %q, got:\n%s", want, reason, c.report.String())
	}
	return nil
}
