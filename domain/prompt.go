package domain

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// The types an option of a saved prompt may have.
const (
	PromptTypeString = "string"
	PromptTypeInt    = "int"
	PromptTypeBool   = "bool"
)

// promptRequiredSuffix ends an option's spec when the caller must give it.
const promptRequiredSuffix = ":required"

var (
	promptNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)
	promptFlagPattern = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
)

// Prompt is a saved prompt: a name the Mayor runs it by, a line saying what
// it is for, its signature (the options it takes, each as its spec) and its
// body, in which each `<flag>` is replaced by the option's value when it runs.
type Prompt struct {
	Name      string   `json:"name"`
	Summary   string   `json:"summary"`
	Signature []string `json:"signature"`
	Body      string   `json:"body"`
}

// PromptOption is one option of a prompt's signature: `<flag>:<type>=<default>`,
// or `<flag>:<type>:required` when it has no default and must be given.
type PromptOption struct {
	Flag     string
	Type     string
	Default  string
	Required bool
}

// PromptArg is one option as a call gave it.
type PromptArg struct {
	Flag  string
	Value string
}

// ValidPromptName reports whether name can name a prompt: lower-case letters,
// digits, hyphens and underscores, starting with a letter or digit.
func ValidPromptName(name string) bool { return promptNamePattern.MatchString(name) }

// ParsePromptOption reads spec, `<flag>:<type>[=<default>][:required]`. The
// flag is lower-case words joined by hyphens, the type is string, int or
// bool, and a default must be a value of the type. An option is required or
// has a default, never both.
func ParsePromptOption(spec string) (PromptOption, error) {
	rest := spec
	required := strings.HasSuffix(rest, promptRequiredSuffix)
	if required {
		rest = strings.TrimSuffix(rest, promptRequiredSuffix)
	}
	flag, typed, found := strings.Cut(rest, ":")
	if !found {
		return PromptOption{}, fmt.Errorf("option %q is not <flag>:<type>=<default>", spec)
	}
	if !promptFlagPattern.MatchString(flag) {
		return PromptOption{}, fmt.Errorf("option %q: %q is not a flag name (lower-case words joined by hyphens)", spec, flag)
	}
	kind, def, hasDefault := strings.Cut(typed, "=")
	option := PromptOption{Flag: flag, Type: kind, Default: def, Required: required}
	if hasDefault && required {
		return PromptOption{}, fmt.Errorf("option %q is required, so it has no default", spec)
	}
	switch kind {
	case PromptTypeString, PromptTypeInt, PromptTypeBool:
	default:
		return PromptOption{}, fmt.Errorf("option %q: %q is not a type: string, int or bool", spec, kind)
	}
	if def != "" {
		if err := option.check(def); err != nil {
			return PromptOption{}, fmt.Errorf("option %q: %w", spec, err)
		}
	}
	return option, nil
}

// check reports whether value is a value of the option's type.
func (o PromptOption) check(value string) error {
	switch o.Type {
	case PromptTypeInt:
		if _, err := strconv.Atoi(value); err != nil {
			return fmt.Errorf("%q is not an int", value)
		}
	case PromptTypeBool:
		if _, err := strconv.ParseBool(value); err != nil {
			return fmt.Errorf("%q is not a bool", value)
		}
	}
	return nil
}

// String is the option's spec, as ParsePromptOption reads it.
func (o PromptOption) String() string {
	switch {
	case o.Required:
		return o.Flag + ":" + o.Type + promptRequiredSuffix
	case o.Default != "":
		return o.Flag + ":" + o.Type + "=" + o.Default
	}
	return o.Flag + ":" + o.Type
}

// Options reads the prompt's signature.
func (p Prompt) Options() ([]PromptOption, error) {
	options := make([]PromptOption, 0, len(p.Signature))
	seen := map[string]bool{}
	for _, spec := range p.Signature {
		option, err := ParsePromptOption(spec)
		if err != nil {
			return nil, err
		}
		if seen[option.Flag] {
			return nil, fmt.Errorf("option %q is given twice", option.Flag)
		}
		seen[option.Flag] = true
		options = append(options, option)
	}
	return options, nil
}

// SignatureText is the signature as one line: each option as --flag:type
// with its default or :required, or "no options".
func (p Prompt) SignatureText() string {
	if len(p.Signature) == 0 {
		return "no options"
	}
	parts := make([]string, len(p.Signature))
	for i, spec := range p.Signature {
		parts[i] = "--" + spec
	}
	return strings.Join(parts, " ")
}

// Fill checks a call's options against the signature and returns the body with
// every `<flag>` replaced: by the value given, else the option's default, else
// nothing. An option the signature does not name, one given twice, a value that
// is not of its type and a required option left out are each an error that
// names the signature.
func (p Prompt) Fill(args []PromptArg) (string, error) {
	options, err := p.Options()
	if err != nil {
		return "", fmt.Errorf("the saved prompt /%s has a signature that does not read: %w", p.Name, err)
	}
	values := map[string]string{}
	given := map[string]bool{}
	for _, o := range options {
		values[o.Flag] = o.Default
	}
	refuse := func(format string, a ...any) error {
		return fmt.Errorf("/%s: %s; its signature is %s", p.Name, fmt.Sprintf(format, a...), p.SignatureText())
	}
	for _, arg := range args {
		var option *PromptOption
		for i := range options {
			if options[i].Flag == arg.Flag {
				option = &options[i]
			}
		}
		switch {
		case option == nil:
			return "", refuse("--%s is not an option", arg.Flag)
		case given[arg.Flag]:
			return "", refuse("--%s is given twice", arg.Flag)
		}
		if err := option.check(arg.Value); err != nil {
			return "", refuse("--%s: %v", arg.Flag, err)
		}
		given[arg.Flag] = true
		values[arg.Flag] = arg.Value
	}
	pairs := make([]string, 0, 2*len(options))
	for _, o := range options {
		if o.Required && !given[o.Flag] {
			return "", refuse("--%s is required", o.Flag)
		}
		pairs = append(pairs, "<"+o.Flag+">", values[o.Flag])
	}
	return strings.NewReplacer(pairs...).Replace(p.Body), nil
}
