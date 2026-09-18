package domain_test

import (
	"errors"
	"testing"

	"github.com/Jonathan-A-White/millwright/domain"
)

func completePath() domain.Path {
	return domain.Path{
		Rig:     "millwright",
		Branch:  "main",
		Harness: domain.HarnessClaude,
		Model:   domain.ModelOpus,
		Effort:  domain.EffortHigh,
		Formula: "tdd-feature",
		Host:    "vps",
	}
}

func TestValidateAcceptsACompletePath(t *testing.T) {
	if err := completePath().Validate(); err != nil {
		t.Fatalf("expected a complete path to be valid, got %v", err)
	}
}

func TestValidateAcceptsAPathWithoutFormulaOrHost(t *testing.T) {
	p := completePath()
	p.Formula = ""
	p.Host = ""
	if err := p.Validate(); err != nil {
		t.Fatalf("expected formula and host to be optional, got %v", err)
	}
}

func TestValidateRejectsMissingRigAndBranch(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*domain.Path)
		want   error
	}{
		{"no rig", func(p *domain.Path) { p.Rig = "" }, domain.ErrRigRequired},
		{"no branch", func(p *domain.Path) { p.Branch = "" }, domain.ErrBranchRequired},
		{"no harness", func(p *domain.Path) { p.Harness = "" }, domain.ErrHarnessRequired},
		{"no model", func(p *domain.Path) { p.Model = "" }, domain.ErrModelRequired},
		{"no effort", func(p *domain.Path) { p.Effort = "" }, domain.ErrEffortRequired},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := completePath()
			tt.mutate(&p)
			err := p.Validate()
			if !errors.Is(err, tt.want) {
				t.Fatalf("expected %v, got %v", tt.want, err)
			}
		})
	}
}

func TestValidateRejectsUnknownValues(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*domain.Path)
		field  string
		value  string
	}{
		{"unknown harness", func(p *domain.Path) { p.Harness = "codex" }, "harness", "codex"},
		{"unknown model", func(p *domain.Path) { p.Model = "gpt" }, "model", "gpt"},
		{"unknown effort", func(p *domain.Path) { p.Effort = "insane" }, "effort", "insane"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := completePath()
			tt.mutate(&p)
			err := p.Validate()
			var unknown domain.UnknownValueError
			if !errors.As(err, &unknown) {
				t.Fatalf("expected an UnknownValueError, got %v", err)
			}
			if unknown.Field != tt.field || unknown.Value != tt.value {
				t.Fatalf("expected unknown %s %q, got unknown %s %q", tt.field, tt.value, unknown.Field, unknown.Value)
			}
		})
	}
}

func TestValidateAcceptsEveryKnownModelAndEffort(t *testing.T) {
	for _, m := range []domain.Model{domain.ModelFable, domain.ModelOpus, domain.ModelSonnet, domain.ModelHaiku} {
		p := completePath()
		p.Model = m
		if err := p.Validate(); err != nil {
			t.Errorf("expected model %q to be known, got %v", m, err)
		}
	}
	for _, e := range []domain.Effort{domain.EffortLow, domain.EffortMedium, domain.EffortHigh, domain.EffortXHigh, domain.EffortMax} {
		p := completePath()
		p.Effort = e
		if err := p.Validate(); err != nil {
			t.Errorf("expected effort %q to be known, got %v", e, err)
		}
	}
}

func TestSetAndFieldRoundTripEveryPathField(t *testing.T) {
	want := completePath()
	var got domain.Path
	for _, field := range domain.Fields {
		value, err := want.Field(field)
		if err != nil {
			t.Fatalf("expected %q to be a path field, got %v", field, err)
		}
		if err := got.Set(field, value); err != nil {
			t.Fatalf("setting %q: %v", field, err)
		}
	}
	if got != want {
		t.Fatalf("expected %+v, got %+v", want, got)
	}
}

func TestSetAndFieldRejectAnUnknownField(t *testing.T) {
	var p domain.Path
	var unknown domain.UnknownFieldError
	if err := p.Set("mode", "serial"); !errors.As(err, &unknown) || unknown.Field != "mode" {
		t.Fatalf("expected an UnknownFieldError for mode, got %v", err)
	}
	if _, err := p.Field("mode"); !errors.As(err, &unknown) {
		t.Fatalf("expected an UnknownFieldError for mode, got %v", err)
	}
}

func TestMetadataLeavesOutEmptyFields(t *testing.T) {
	p := completePath()
	p.Formula = ""
	p.Host = ""

	got := p.Metadata()
	if len(got) != 5 {
		t.Fatalf("expected the five set fields, got %v", got)
	}
	if got["rig"] != "millwright" || got["model"] != "opus" {
		t.Fatalf("expected rig and model to be written out, got %v", got)
	}
	if _, ok := got["formula"]; ok {
		t.Fatalf("expected an empty formula to be left out, got %v", got)
	}
}

func TestPathFromMetadataIgnoresKeysThatAreNotPathFields(t *testing.T) {
	got := domain.PathFromMetadata(map[string]string{
		"rig":    "millwright",
		"model":  "haiku",
		"mode":   "serial",
		"origin": "the Mayor",
	})
	want := domain.Path{Rig: "millwright", Model: domain.ModelHaiku}
	if got != want {
		t.Fatalf("expected %+v, got %+v", want, got)
	}
}

func TestPathFromMetadataAndMetadataRoundTrip(t *testing.T) {
	want := completePath()
	if got := domain.PathFromMetadata(want.Metadata()); got != want {
		t.Fatalf("expected %+v, got %+v", want, got)
	}
}

func TestOverlayReplacesOnlyTheFieldsTheOverrideSets(t *testing.T) {
	defaults := completePath()
	got := defaults.Overlay(domain.Path{
		Branch: "mw/mw-gq6.1",
		Model:  domain.ModelHaiku,
		Host:   "laptop",
	})

	want := completePath()
	want.Branch = "mw/mw-gq6.1"
	want.Model = domain.ModelHaiku
	want.Host = "laptop"

	if got != want {
		t.Fatalf("expected %+v, got %+v", want, got)
	}
	if defaults != completePath() {
		t.Fatalf("expected Overlay to leave the defaults untouched, got %+v", defaults)
	}
}

func TestStoryPathFromOverlaysTheEpicDefaults(t *testing.T) {
	story := domain.Story{
		ID:        "mw-gq6.1",
		Title:     "Go module, layout and the Path domain",
		Overrides: domain.Path{Effort: domain.EffortMax},
	}

	got, err := story.PathFrom(completePath())
	if err != nil {
		t.Fatalf("expected a valid path, got %v", err)
	}
	if got.Effort != domain.EffortMax {
		t.Errorf("expected the story's effort override to win, got %q", got.Effort)
	}
	if got.Model != domain.ModelOpus {
		t.Errorf("expected the epic's default model to survive, got %q", got.Model)
	}
}

func TestStoryPathFromReportsAnInvalidPath(t *testing.T) {
	defaults := completePath()
	defaults.Rig = ""
	story := domain.Story{ID: "mw-gq6.1", Title: "a story"}

	got, err := story.PathFrom(defaults)
	if !errors.Is(err, domain.ErrRigRequired) {
		t.Fatalf("expected ErrRigRequired, got %v", err)
	}
	if got != (domain.Path{}) {
		t.Fatalf("expected no path alongside the error, got %+v", got)
	}
}
