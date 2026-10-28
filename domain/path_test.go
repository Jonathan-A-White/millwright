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
		Files:   "src/Reader.tsx,src/data/*.ts",
		// MaxTurns: mw-1ljpo9.6.
		MaxTurns: "160",
		// Size: mw-1ljpo9.3.
		Size: domain.SizeM,
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
		{"unknown harness", func(p *domain.Path) { p.Harness = "gemini" }, "harness", "gemini"},
		{"claude model under codex", func(p *domain.Path) { p.Harness = domain.HarnessCodex; p.Model = "sonnet" }, "model", "sonnet"},
		{"gpt model under claude", func(p *domain.Path) { p.Model = "gpt-6.1-sol" }, "model", "gpt-6.1-sol"},
		{"bare gpt under codex", func(p *domain.Path) { p.Harness = domain.HarnessCodex; p.Model = "gpt" }, "model", "gpt"},
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

func TestValidateAcceptsCodexWithGPTModels(t *testing.T) {
	for _, m := range []domain.Model{"gpt-6-astra", "gpt-6.1-sol", "gpt-6.1-terra", "gpt-6-luna"} {
		p := completePath()
		p.Harness = domain.HarnessCodex
		p.Model = m
		if err := p.Validate(); err != nil {
			t.Errorf("expected codex model %q to be known, got %v", m, err)
		}
		if got := domain.FullModelID(m); got != "" {
			t.Errorf("FullModelID(%q) = %q, want empty: a codex model is passed through", m, got)
		}
	}
}

func TestKnownModelIsHarnessAware(t *testing.T) {
	tests := []struct {
		h    domain.Harness
		m    domain.Model
		want bool
	}{
		{domain.HarnessClaude, domain.ModelSonnet, true},
		{domain.HarnessClaude, "gpt-6-luna", false},
		{domain.HarnessCodex, "gpt-6-luna", true},
		{domain.HarnessCodex, domain.ModelSonnet, false},
		{domain.HarnessCodex, "", false},
		{"", domain.ModelSonnet, false},
	}
	for _, tt := range tests {
		if got := domain.KnownModel(tt.h, tt.m); got != tt.want {
			t.Errorf("KnownModel(%q, %q) = %v, want %v", tt.h, tt.m, got, tt.want)
		}
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

// The full ids are the current family's: Fable 5.1, Opus 5.5, Sonnet 5.5,
// Haiku 5.5 (mw-2zh33g.10: haiku was still claude-haiku-4-5-20251001).
func TestFullModelIDIsTheCurrentFamilysIdForEveryAlias(t *testing.T) {
	want := map[domain.Model]string{
		domain.ModelFable:  "claude-fable-5-1",
		domain.ModelOpus:   "claude-opus-5-5",
		domain.ModelSonnet: "claude-sonnet-5-5",
		domain.ModelHaiku:  "claude-haiku-5-5",
	}
	for _, m := range []domain.Model{domain.ModelFable, domain.ModelOpus, domain.ModelSonnet, domain.ModelHaiku} {
		if !domain.KnownModel(domain.HarnessClaude, m) {
			t.Fatalf("expected %q to be a known model", m)
		}
		if got := domain.FullModelID(m); got != want[m] {
			t.Errorf("FullModelID(%q) = %q, want %q", m, got, want[m])
		}
	}
	if got := domain.FullModelID("gpt"); got != "" {
		t.Errorf("FullModelID(gpt) = %q, want empty", got)
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
	p.Files = ""
	p.MaxTurns = ""
	p.Size = ""

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

// max_turns (mw-1ljpo9.6) is a story's own turn cap: a whole number, 0 for no
// cap, and empty to leave it to the host's next.max_turns.
func TestMaxTurnsIsAWholeNumberOrEmpty(t *testing.T) {
	for _, ok := range []string{"", "0", "60", "160"} {
		p := completePath()
		p.MaxTurns = ok
		if err := p.Validate(); err != nil {
			t.Errorf("expected max_turns %q to be valid, got %v", ok, err)
		}
	}
	for _, bad := range []string{"-1", "lots", "1.5"} {
		p := completePath()
		p.MaxTurns = bad
		var unknown domain.UnknownValueError
		if err := p.Validate(); !errors.As(err, &unknown) || unknown.Field != "max_turns" {
			t.Errorf("expected max_turns %q to be refused, got %v", bad, err)
		}
	}
}

func TestTurnCapIsThePathsOwnOrTheHostsWhenThePathSaysNothing(t *testing.T) {
	p := completePath()
	p.MaxTurns = ""
	if got := p.TurnCap(120); got != 120 {
		t.Fatalf("expected the host's 120 when the path says nothing, got %d", got)
	}
	p.MaxTurns = "60"
	if got := p.TurnCap(120); got != 60 {
		t.Fatalf("expected the path's 60, got %d", got)
	}
	p.MaxTurns = "0"
	if got := p.TurnCap(120); got != 0 {
		t.Fatalf("expected a path's 0 to set no cap, got %d", got)
	}
}

// size (mw-1ljpo9.3) is how big the Mayor judged a story: XS, S, M, L or XL, or
// empty when nobody has.
func TestSizeIsOneOfTheFiveOrEmpty(t *testing.T) {
	for _, ok := range []domain.Size{"", domain.SizeXS, domain.SizeS, domain.SizeM, domain.SizeL, domain.SizeXL} {
		p := completePath()
		p.Size = ok
		if err := p.Validate(); err != nil {
			t.Errorf("expected size %q to be valid, got %v", ok, err)
		}
	}
	for _, bad := range []domain.Size{"s", "XXL", "big", "2"} {
		p := completePath()
		p.Size = bad
		var unknown domain.UnknownValueError
		if err := p.Validate(); !errors.As(err, &unknown) || unknown.Field != "size" {
			t.Errorf("expected size %q to be refused, got %v", bad, err)
		}
	}
}

func TestSizeIsStoredAndOverlaidLikeAnyPathKey(t *testing.T) {
	var p domain.Path
	if err := p.Set("size", "S"); err != nil {
		t.Fatalf("setting size: %v", err)
	}
	if got, _ := p.Field("size"); got != "S" {
		t.Fatalf("expected size S, got %q", got)
	}
	if got := p.Metadata()["size"]; got != "S" {
		t.Fatalf("expected the metadata key size=S, got %q", got)
	}
	if got := domain.PathFromMetadata(map[string]string{"size": "L"}).Size; got != domain.SizeL {
		t.Fatalf("expected size L read back, got %q", got)
	}
	if got := completePath().Overlay(domain.Path{Size: domain.SizeXL}).Size; got != domain.SizeXL {
		t.Fatalf("expected the override's XL, got %q", got)
	}
	if got := completePath().Overlay(domain.Path{}).Size; got != domain.SizeM {
		t.Fatalf("expected an empty override to keep M, got %q", got)
	}
}

func TestOnlyLAndXLAreBigStories(t *testing.T) {
	for size, big := range map[domain.Size]bool{
		"": false, domain.SizeXS: false, domain.SizeS: false, domain.SizeM: false, domain.SizeL: true, domain.SizeXL: true,
	} {
		if got := size.Big(); got != big {
			t.Errorf("Size(%q).Big() = %v, want %v", size, got, big)
		}
	}
}

func TestBumpIsEmptyOrMinor(t *testing.T) {
	p := completePath()
	p.Bump = domain.BumpMinor
	if err := p.Validate(); err != nil {
		t.Fatalf("expected bump=minor to be valid, got %v", err)
	}
	if got, err := p.Field("bump"); err != nil || got != "minor" {
		t.Fatalf("Field(bump) = %q, %v; want minor", got, err)
	}
	if got := p.Metadata()["bump"]; got != "minor" {
		t.Fatalf("expected the metadata to hold bump=minor, got %q", got)
	}
	var read domain.Path
	if err := read.Set("bump", "minor"); err != nil || read.Bump != domain.BumpMinor {
		t.Fatalf("Set(bump, minor) = %v, %q", err, read.Bump)
	}
	p.Bump = "major"
	var unknown domain.UnknownValueError
	if err := p.Validate(); !errors.As(err, &unknown) || unknown.Field != "bump" {
		t.Fatalf("expected an UnknownValueError for bump=major, got %v", err)
	}
	p.Bump = ""
	if err := p.Validate(); err != nil {
		t.Fatalf("expected no bump to be valid, got %v", err)
	}
}

func TestCallersOKIsAMetadataKeyOutsideTheFields(t *testing.T) {
	path := domain.Path{Rig: "lampas", CallersOK: "additive: new function beside the old"}
	meta := path.Metadata()
	if meta["callers_ok"] != "additive: new function beside the old" {
		t.Fatalf("Metadata() = %v; want callers_ok written", meta)
	}
	if back := domain.PathFromMetadata(meta); back.CallersOK != path.CallersOK {
		t.Errorf("PathFromMetadata lost callers_ok: %q", back.CallersOK)
	}
	for _, field := range domain.Fields {
		if field == "callers_ok" {
			t.Errorf("callers_ok is in Fields, which are the eleven path keys")
		}
	}
	if got := (domain.Path{Rig: "a"}).Overlay(domain.Path{CallersOK: "why"}); got.CallersOK != "why" {
		t.Errorf("Overlay dropped callers_ok: %q", got.CallersOK)
	}
	if _, empty := (domain.Path{Rig: "a"}).Metadata()["callers_ok"]; empty {
		t.Errorf("an unset callers_ok was written")
	}
}
