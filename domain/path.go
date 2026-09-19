// Package domain holds the factory's vocabulary as value types: Story, Path
// and the things a Path is made of. It is pure — it imports nothing outside
// the standard library and performs no I/O.
package domain

import (
	"errors"
	"fmt"
	"strings"
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
// The tags are how a Path is written down in the Mayor's plan file; the field
// names are the same as the metadata keys a bead stores it under.
type Path struct {
	Rig     string  `json:"rig,omitempty"`
	Branch  string  `json:"branch,omitempty"`
	Harness Harness `json:"harness,omitempty"`
	Model   Model   `json:"model,omitempty"`
	Effort  Effort  `json:"effort,omitempty"`
	Formula string  `json:"formula,omitempty"`
	Host    string  `json:"host,omitempty"`
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

// UnknownFieldError reports a name that is not one of a path's fields.
type UnknownFieldError struct {
	Field string
}

func (e UnknownFieldError) Error() string {
	return fmt.Sprintf("a path has no %q field", e.Field)
}

// Fields are the names of a path's fields, in the order a path is written out.
// They are also the metadata keys a path is stored under.
var Fields = []string{"rig", "branch", "harness", "model", "effort", "formula", "host"}

// Set sets the field of p named by one of Fields. It is how a path is read out
// of stored metadata: one key, one value, at a time.
func (p *Path) Set(field, value string) error {
	switch field {
	case "rig":
		p.Rig = value
	case "branch":
		p.Branch = value
	case "harness":
		p.Harness = Harness(value)
	case "model":
		p.Model = Model(value)
	case "effort":
		p.Effort = Effort(value)
	case "formula":
		p.Formula = value
	case "host":
		p.Host = value
	default:
		return UnknownFieldError{Field: field}
	}
	return nil
}

// Field reads the field of p named by one of Fields.
func (p Path) Field(field string) (string, error) {
	switch field {
	case "rig":
		return p.Rig, nil
	case "branch":
		return p.Branch, nil
	case "harness":
		return string(p.Harness), nil
	case "model":
		return string(p.Model), nil
	case "effort":
		return string(p.Effort), nil
	case "formula":
		return p.Formula, nil
	case "host":
		return p.Host, nil
	default:
		return "", UnknownFieldError{Field: field}
	}
}

// Metadata is p written out as the metadata keys a bead stores it under. Empty
// fields are left out, so that writing it back does not erase a default.
func (p Path) Metadata() map[string]string {
	m := map[string]string{}
	for _, field := range Fields {
		value, err := p.Field(field)
		if err != nil || value == "" {
			continue
		}
		m[field] = value
	}
	return m
}

// Summary is a path on one line, for a person reading a tree of stories:
// where the work lands, who does it and how. Fields the path does not set are
// left out, so that an incomplete path reads as what it is.
func (p Path) Summary() string {
	var parts []string
	if where := joined("/", p.Rig, p.Branch); where != "" {
		parts = append(parts, where)
	}
	if who := joined("/", string(p.Harness), string(p.Model), string(p.Effort)); who != "" {
		parts = append(parts, who)
	}
	if p.Formula != "" {
		parts = append(parts, p.Formula)
	}
	if p.Host != "" {
		parts = append(parts, p.Host)
	}
	if len(parts) == 0 {
		return "no path"
	}
	return strings.Join(parts, " · ")
}

// joined puts the values that are set together, in order, and drops the rest.
func joined(separator string, values ...string) string {
	set := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" {
			set = append(set, value)
		}
	}
	return strings.Join(set, separator)
}

// PathFromMetadata reads a path out of stored metadata, ignoring every key
// that does not name a path field.
func PathFromMetadata(metadata map[string]string) Path {
	var p Path
	for field, value := range metadata {
		_ = p.Set(field, value)
	}
	return p
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
	case !KnownModel(p.Model):
		return UnknownValueError{Field: "model", Value: string(p.Model)}
	}
	switch {
	case p.Effort == "":
		return ErrEffortRequired
	case !KnownEffort(p.Effort):
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

// KnownModel reports whether a model is one the factory runs on.
func KnownModel(m Model) bool {
	switch m {
	case ModelFable, ModelOpus, ModelSonnet, ModelHaiku:
		return true
	}
	return false
}

// KnownEffort reports whether an effort is one a session can be asked for.
func KnownEffort(e Effort) bool {
	switch e {
	case EffortLow, EffortMedium, EffortHigh, EffortXHigh, EffortMax:
		return true
	}
	return false
}
