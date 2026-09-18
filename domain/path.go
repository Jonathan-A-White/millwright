// Package domain holds the factory's vocabulary as value types: Story, Path
// and the things a Path is made of. It is pure — it imports nothing outside
// the standard library and performs no I/O.
package domain

import (
	"errors"
	"fmt"
)

// Harness is the agent runtime a session runs in.
type Harness string

// The harnesses the factory can run a session in.
const (
	HarnessClaude Harness = "claude"
)

// Model is the model a session runs on.
type Model string

// The models a session may run on.
const (
	ModelFable  Model = "fable"
	ModelOpus   Model = "opus"
	ModelSonnet Model = "sonnet"
	ModelHaiku  Model = "haiku"
)

// Effort is how hard the session is asked to think.
type Effort string

// The effort levels a session may be run at.
const (
	EffortLow    Effort = "low"
	EffortMedium Effort = "medium"
	EffortHigh   Effort = "high"
	EffortXHigh  Effort = "xhigh"
	EffortMax    Effort = "max"
)

// Path is the plan for how one story gets worked: the rig it is worked in, the
// branch it targets, and the harness, model, effort, formula and host of the
// session that works it. A story without a rig and a target branch has no path.
type Path struct {
	Rig     string
	Branch  string
	Harness Harness
	Model   Model
	Effort  Effort
	Formula string
	Host    string
}

// Story is a bead sized to be worked start to finish by one session in one
// rig. Its Overrides are the fields of the epic's default path this story
// departs from; every field left empty falls through to the epic's default.
type Story struct {
	ID        string
	Title     string
	Overrides Path
}

// Reasons a path is not a path.
var (
	ErrRigRequired     = errors.New("rig is required")
	ErrBranchRequired  = errors.New("branch is required")
	ErrHarnessRequired = errors.New("harness is required")
	ErrModelRequired   = errors.New("model is required")
	ErrEffortRequired  = errors.New("effort is required")
)

// UnknownValueError reports a path field set to a value the factory does not know.
type UnknownValueError struct {
	Field string
	Value string
}

func (e UnknownValueError) Error() string {
	return fmt.Sprintf("unknown %s %q", e.Field, e.Value)
}

// Overlay returns p with every field the override sets replacing p's own.
// An empty field in the override means "keep what the default says".
func (p Path) Overlay(override Path) Path {
	if override.Rig != "" {
		p.Rig = override.Rig
	}
	if override.Branch != "" {
		p.Branch = override.Branch
	}
	if override.Harness != "" {
		p.Harness = override.Harness
	}
	if override.Model != "" {
		p.Model = override.Model
	}
	if override.Effort != "" {
		p.Effort = override.Effort
	}
	if override.Formula != "" {
		p.Formula = override.Formula
	}
	if override.Host != "" {
		p.Host = override.Host
	}
	return p
}

// Validate reports the first reason p cannot be worked. Rig and branch are
// mandatory; harness, model and effort must each name something the factory
// knows. Formula and host are optional here: the dispatcher supplies them.
func (p Path) Validate() error {
	if p.Rig == "" {
		return ErrRigRequired
	}
	if p.Branch == "" {
		return ErrBranchRequired
	}
	switch {
	case p.Harness == "":
		return ErrHarnessRequired
	case !knownHarness(p.Harness):
		return UnknownValueError{Field: "harness", Value: string(p.Harness)}
	}
	switch {
	case p.Model == "":
		return ErrModelRequired
	case !knownModel(p.Model):
		return UnknownValueError{Field: "model", Value: string(p.Model)}
	}
	switch {
	case p.Effort == "":
		return ErrEffortRequired
	case !knownEffort(p.Effort):
		return UnknownValueError{Field: "effort", Value: string(p.Effort)}
	}
	return nil
}

// PathFrom builds the path this story is worked by: the epic's defaults
// overlaid with the story's own overrides. It returns the zero Path and the
// reason if what comes out is not a path.
func (s Story) PathFrom(defaults Path) (Path, error) {
	p := defaults.Overlay(s.Overrides)
	if err := p.Validate(); err != nil {
		return Path{}, err
	}
	return p, nil
}

func knownHarness(h Harness) bool {
	switch h {
	case HarnessClaude:
		return true
	}
	return false
}

func knownModel(m Model) bool {
	switch m {
	case ModelFable, ModelOpus, ModelSonnet, ModelHaiku:
		return true
	}
	return false
}

func knownEffort(e Effort) bool {
	switch e {
	case EffortLow, EffortMedium, EffortHigh, EffortXHigh, EffortMax:
		return true
	}
	return false
}
