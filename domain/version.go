package domain

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var plainVersion = regexp.MustCompile(`^(\d+)\.(\d+)\.(\d+)$`)

// NextPatch raises the patch of a plain X.Y.Z version: 1.2.3 is 1.2.4. A
// version that is not three plain numbers (a pre-release, a leading v) is an
// error, because mw would be guessing what raising it means.
func NextPatch(version string) (string, error) {
	m := plainVersion.FindStringSubmatch(version)
	if m == nil {
		return "", fmt.Errorf("%q is not a plain X.Y.Z version, so its patch cannot be raised", version)
	}
	patch, err := strconv.Atoi(m[3])
	if err != nil {
		return "", fmt.Errorf("the patch of %q is too large: %w", version, err)
	}
	return fmt.Sprintf("%s.%s.%d", m[1], m[2], patch+1), nil
}

// versionPaths are the places an npm package.json or package-lock.json keeps
// the package's own version: the top-level "version", and in a lock file the
// root package's entry, packages[""].version. Every other "version" in either
// file belongs to a dependency and is never touched.
var versionPaths = [][]string{{"version"}, {"packages", "", "version"}}

// VersionOf reads the package's own version out of a package.json or
// package-lock.json: its top-level "version", which must be a string.
func VersionOf(content []byte) (string, error) {
	spans, err := versionSpans(content)
	if err != nil {
		return "", err
	}
	return spans[0].value, nil
}

// SetVersion writes version into every place the file keeps its package's own
// version and changes nothing else: every other byte, the indentation and the
// trailing newline included, comes back as it went in. It is an error when the
// file has no top-level "version" to start from.
func SetVersion(content []byte, version string) ([]byte, error) {
	spans, err := versionSpans(content)
	if err != nil {
		return nil, err
	}
	written, err := json.Marshal(version)
	if err != nil {
		return nil, err
	}
	// File order, whichever of the two comes first.
	if len(spans) == 2 && spans[1].start < spans[0].start {
		spans[0], spans[1] = spans[1], spans[0]
	}
	var out []byte
	at := 0
	for _, span := range spans {
		out = append(append(out, content[at:span.start]...), written...)
		at = span.end
	}
	return append(out, content[at:]...), nil
}

// versionSpan is where one version's string sits in a file, quotes included.
type versionSpan struct {
	start, end int
	value      string
}

// versionSpans finds the version strings at versionPaths, the top-level one
// first. It reads the file with a small scanner of its own rather than
// decoding it, because decoding and encoding again would reorder or reformat
// what it was never asked to change.
func versionSpans(content []byte) ([]versionSpan, error) {
	s := &jsonScan{text: content}
	found := map[string]versionSpan{}
	if err := s.value(nil, func(path []string, span versionSpan) {
		for _, want := range versionPaths {
			if equalPath(path, want) {
				found[strings.Join(path, "\x00")] = span
			}
		}
	}); err != nil {
		return nil, fmt.Errorf("reading the version: %w", err)
	}
	if s.skip(); s.pos != len(s.text) {
		return nil, fmt.Errorf("reading the version: unexpected text after the JSON value at byte %d", s.pos)
	}
	top, ok := found["version"]
	if !ok {
		return nil, fmt.Errorf("the file has no top-level \"version\" string")
	}
	spans := []versionSpan{top}
	if root, ok := found["packages\x00\x00version"]; ok {
		spans = append(spans, root)
	}
	return spans, nil
}

func equalPath(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// jsonScan walks JSON text once, reporting the string values it finds with the
// path of keys that leads to each. Arrays are walked but their elements have
// no path worth reporting.
type jsonScan struct {
	text []byte
	pos  int
}

func (s *jsonScan) skip() {
	for s.pos < len(s.text) && strings.IndexByte(" \t\r\n", s.text[s.pos]) >= 0 {
		s.pos++
	}
}

func (s *jsonScan) value(path []string, found func([]string, versionSpan)) error {
	s.skip()
	if s.pos >= len(s.text) {
		return fmt.Errorf("the JSON ends where a value should be")
	}
	switch s.text[s.pos] {
	case '{':
		return s.object(path, found)
	case '[':
		return s.array(found)
	case '"':
		start := s.pos
		raw, err := s.str()
		if err != nil {
			return err
		}
		var value string
		if err := json.Unmarshal(raw, &value); err != nil {
			return fmt.Errorf("the string at byte %d: %w", start, err)
		}
		found(path, versionSpan{start: start, end: s.pos, value: value})
		return nil
	}
	// A number, true, false or null: it runs to the next delimiter.
	start := s.pos
	for s.pos < len(s.text) && strings.IndexByte(",}] \t\r\n", s.text[s.pos]) < 0 {
		s.pos++
	}
	if s.pos == start {
		return fmt.Errorf("unexpected %q at byte %d", s.text[s.pos], s.pos)
	}
	if !json.Valid(s.text[start:s.pos]) {
		return fmt.Errorf("%q at byte %d is not JSON", s.text[start:s.pos], start)
	}
	return nil
}

// str reads the string that starts at pos and returns its bytes, quotes in.
func (s *jsonScan) str() ([]byte, error) {
	start := s.pos
	for s.pos++; s.pos < len(s.text); s.pos++ {
		switch s.text[s.pos] {
		case '\\':
			s.pos++
		case '"':
			s.pos++
			return s.text[start:s.pos], nil
		}
	}
	return nil, fmt.Errorf("the string at byte %d is never closed", start)
}

func (s *jsonScan) object(path []string, found func([]string, versionSpan)) error {
	s.pos++
	s.skip()
	if s.pos < len(s.text) && s.text[s.pos] == '}' {
		s.pos++
		return nil
	}
	for {
		s.skip()
		if s.pos >= len(s.text) || s.text[s.pos] != '"' {
			return fmt.Errorf("expected a key at byte %d", s.pos)
		}
		raw, err := s.str()
		if err != nil {
			return err
		}
		var key string
		if err := json.Unmarshal(raw, &key); err != nil {
			return err
		}
		if s.skip(); s.pos >= len(s.text) || s.text[s.pos] != ':' {
			return fmt.Errorf("expected : at byte %d", s.pos)
		}
		s.pos++
		if err := s.value(append(append([]string(nil), path...), key), found); err != nil {
			return err
		}
		s.skip()
		if s.pos >= len(s.text) {
			return fmt.Errorf("the object is never closed")
		}
		switch s.text[s.pos] {
		case ',':
			s.pos++
		case '}':
			s.pos++
			return nil
		default:
			return fmt.Errorf("expected , or } at byte %d", s.pos)
		}
	}
}

func (s *jsonScan) array(found func([]string, versionSpan)) error {
	s.pos++
	s.skip()
	if s.pos < len(s.text) && s.text[s.pos] == ']' {
		s.pos++
		return nil
	}
	for {
		// An element has no path of its own: nothing inside it can be a version
		// mw reads or writes.
		if err := s.value([]string{"[]"}, found); err != nil {
			return err
		}
		s.skip()
		if s.pos >= len(s.text) {
			return fmt.Errorf("the array is never closed")
		}
		switch s.text[s.pos] {
		case ',':
			s.pos++
		case ']':
			s.pos++
			return nil
		default:
			return fmt.Errorf("expected , or ] at byte %d", s.pos)
		}
	}
}
