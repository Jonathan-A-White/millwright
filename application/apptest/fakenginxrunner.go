package apptest

import (
	"context"
	"sync"

	"github.com/Jonathan-A-White/millwright/application"
)

// FakeNginxRunner is an in-memory application.PosternNginxRunner: a test
// sets what Test and Reload report, and reads back how many times each ran,
// so a scenario proves the real nginx was never touched.
type FakeNginxRunner struct {
	mu sync.Mutex

	TestOutput string
	TestOK     bool
	TestErr    error

	ReloadOutput string
	ReloadErr    error

	tests   int
	reloads int
}

var _ application.PosternNginxRunner = (*FakeNginxRunner)(nil)

// NewFakeNginxRunner returns a runner whose Test passes, until a test says
// otherwise.
func NewFakeNginxRunner() *FakeNginxRunner {
	return &FakeNginxRunner{TestOK: true, TestOutput: "nginx: configuration file test is successful"}
}

// Test implements application.PosternNginxRunner.
func (f *FakeNginxRunner) Test(context.Context) (string, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tests++
	return f.TestOutput, f.TestOK, f.TestErr
}

// Reload implements application.PosternNginxRunner.
func (f *FakeNginxRunner) Reload(context.Context) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reloads++
	return f.ReloadOutput, f.ReloadErr
}

// Tests reports how many times Test ran.
func (f *FakeNginxRunner) Tests() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.tests
}

// Reloads reports how many times Reload ran.
func (f *FakeNginxRunner) Reloads() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.reloads
}
