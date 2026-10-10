package application

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// GristExample is one scenario of a grind, `grinds/examples/<kind>/<name>.json`
// in the app's rig: a request to send, the photos beside it to send with it,
// and what the answer must show. A new behaviour of the grind is a new example.
//
//	{
//	  "schemaVersion": "1",
//	  "request": {"...": "..."},
//	  "photos":  ["blank-paper.jpg"],
//	  "expect":  {"price": {"is_null": true}, "confidence": "low"}
//	}
//
// The schemaVersion stands beside the request, as an app sends its v beside
// its input, and is sent as the version of the grist, not copied into the
// request. A request that holds its own schemaVersion does without it; one
// that has both must have them agree.
//
// Each key of expect is a path into the answer, object keys and array
// positions joined by dots ("items.0.name"). Its check is one of: a bare value
// (the field equals it), or an object holding any of equals, is_null, one_of,
// contains, matches (a regular expression), present (true or false) and all,
// every one of which must hold. All is a list of checks of the same kinds,
// each of which must hold too, for a field that must show several things
// (matching two words, which one regular expression cannot say):
//
//	"expect": {"answer": {"all": [{"matches": "gardener"}, {"matches": "library"}]}}
//
// A failure of an all check names the one of its list that did not hold.
type GristExample struct {
	SchemaVersion string                     `json:"schemaVersion"`
	Request       json.RawMessage            `json:"request"`
	Photos        []string                   `json:"photos"`
	Expect        map[string]json.RawMessage `json:"expect"`
}

// ParseGristExample reads one example file, refusing one that has no request
// or a check it does not know, so a misspelt check is not read as no check.
func ParseGristExample(data []byte) (GristExample, error) {
	var example GristExample
	if err := json.Unmarshal(data, &example); err != nil {
		return GristExample{}, fmt.Errorf("it is not an example: %w", err)
	}
	if len(strings.TrimSpace(string(example.Request))) == 0 {
		return GristExample{}, fmt.Errorf("it has no request")
	}
	if example.SchemaVersion != "" {
		var inRequest struct {
			SchemaVersion string `json:"schemaVersion"`
		}
		_ = json.Unmarshal(example.Request, &inRequest)
		if inRequest.SchemaVersion != "" && inRequest.SchemaVersion != example.SchemaVersion {
			return GristExample{}, fmt.Errorf("schemaVersion is %s beside the request but %s in it", example.SchemaVersion, inRequest.SchemaVersion)
		}
	}
	for field, raw := range example.Expect {
		if _, err := parseGristCheck(raw); err != nil {
			return GristExample{}, fmt.Errorf("expect %q: %w", field, err)
		}
	}
	return example, nil
}

// gristCheck is one field's expectation, every part that is set must hold.
type gristCheck struct {
	equals   *any
	isNull   *bool
	oneOf    []any
	contains *any
	matches  *regexp.Regexp
	present  *bool
	all      []gristCheck
}

// gristCheckKeys are the keys a check object may hold.
var gristCheckKeys = []string{"equals", "is_null", "one_of", "contains", "matches", "present", "all"}

func parseGristCheck(raw json.RawMessage) (gristCheck, error) {
	trimmed := strings.TrimSpace(string(raw))
	var check gristCheck
	if !strings.HasPrefix(trimmed, "{") {
		var want any
		if err := json.Unmarshal(raw, &want); err != nil {
			return check, err
		}
		check.equals = &want
		return check, nil
	}
	var parts map[string]json.RawMessage
	if err := json.Unmarshal(raw, &parts); err != nil {
		return check, err
	}
	if len(parts) == 0 {
		return check, fmt.Errorf("a check of none of %s", strings.Join(gristCheckKeys, ", "))
	}
	for key, value := range parts {
		switch key {
		case "equals":
			var want any
			if err := json.Unmarshal(value, &want); err != nil {
				return check, err
			}
			check.equals = &want
		case "is_null", "present":
			var flag bool
			if err := json.Unmarshal(value, &flag); err != nil {
				return check, fmt.Errorf("%s is true or false", key)
			}
			if key == "is_null" {
				check.isNull = &flag
			} else {
				check.present = &flag
			}
		case "one_of":
			if err := json.Unmarshal(value, &check.oneOf); err != nil || len(check.oneOf) == 0 {
				return check, fmt.Errorf("one_of is a list of values")
			}
		case "contains":
			var want any
			if err := json.Unmarshal(value, &want); err != nil {
				return check, err
			}
			check.contains = &want
		case "matches":
			var pattern string
			if err := json.Unmarshal(value, &pattern); err != nil {
				return check, fmt.Errorf("matches is a regular expression in a string")
			}
			re, err := regexp.Compile(pattern)
			if err != nil {
				return check, fmt.Errorf("matches: %w", err)
			}
			check.matches = re
		case "all":
			var each []json.RawMessage
			if err := json.Unmarshal(value, &each); err != nil || len(each) == 0 {
				return check, fmt.Errorf("all is a list of checks")
			}
			for _, raw := range each {
				inner, err := parseGristCheck(raw)
				if err != nil {
					return check, fmt.Errorf("all: %w", err)
				}
				check.all = append(check.all, inner)
			}
		default:
			return check, fmt.Errorf("%q is not a check: use %s", key, strings.Join(gristCheckKeys, ", "))
		}
	}
	return check, nil
}

// ExpectFailure is one thing an example's answer was wanted to show and did
// not: the field, what was wanted and what the answer had.
type ExpectFailure struct {
	Field, Wanted, Got string
}

func (f ExpectFailure) String() string {
	return fmt.Sprintf("%s: wanted %s, got %s", f.Field, f.Wanted, f.Got)
}

// CheckExpect holds answer to every expect of the example, in field order.
func CheckExpect(answer json.RawMessage, expect map[string]json.RawMessage) []ExpectFailure {
	var root any
	if err := json.Unmarshal(answer, &root); err != nil {
		return []ExpectFailure{{Field: "(answer)", Wanted: "a JSON answer", Got: "not JSON"}}
	}
	fields := make([]string, 0, len(expect))
	for field := range expect {
		fields = append(fields, field)
	}
	sort.Strings(fields)
	var failed []ExpectFailure
	for _, field := range fields {
		check, err := parseGristCheck(expect[field])
		if err != nil {
			failed = append(failed, ExpectFailure{Field: field, Wanted: "a check", Got: err.Error()})
			continue
		}
		got, found := gristField(root, field)
		failed = append(failed, check.failures(field, got, found)...)
	}
	return failed
}

// gristField follows a dotted path into a decoded JSON value.
func gristField(root any, path string) (value any, found bool) {
	value = root
	if path == "" {
		return value, true
	}
	for _, step := range strings.Split(path, ".") {
		switch node := value.(type) {
		case map[string]any:
			next, ok := node[step]
			if !ok {
				return nil, false
			}
			value = next
		case []any:
			at, err := strconv.Atoi(step)
			if err != nil || at < 0 || at >= len(node) {
				return nil, false
			}
			value = node[at]
		default:
			return nil, false
		}
	}
	return value, true
}

func (c gristCheck) failures(field string, got any, found bool) []ExpectFailure {
	shown := "(absent)"
	if found {
		shown = gristShow(got)
	}
	var failed []ExpectFailure
	fail := func(wanted string) {
		failed = append(failed, ExpectFailure{Field: field, Wanted: wanted, Got: shown})
	}
	if c.present != nil && found != *c.present {
		if *c.present {
			fail("present")
		} else {
			fail("absent")
		}
	}
	if c.isNull != nil && (found && got == nil) != *c.isNull {
		if *c.isNull {
			fail("null")
		} else {
			fail("not null")
		}
	}
	if c.equals != nil && !(found && reflect.DeepEqual(got, *c.equals)) {
		fail("equal to " + gristShow(*c.equals))
	}
	if len(c.oneOf) > 0 {
		hit := false
		for _, want := range c.oneOf {
			hit = hit || (found && reflect.DeepEqual(got, want))
		}
		if !hit {
			fail("one of " + gristShow(c.oneOf))
		}
	}
	if c.contains != nil && !(found && gristContains(got, *c.contains)) {
		fail("containing " + gristShow(*c.contains))
	}
	if c.matches != nil {
		text, isText := got.(string)
		if !found || !isText || !c.matches.MatchString(text) {
			fail("matching " + strconv.Quote(c.matches.String()))
		}
	}
	for _, inner := range c.all {
		failed = append(failed, inner.failures(field, got, found)...)
	}
	return failed
}

// gristContains reports whether a string holds a substring, or a list holds an
// element equal to want.
func gristContains(got, want any) bool {
	switch node := got.(type) {
	case string:
		sub, ok := want.(string)
		return ok && strings.Contains(node, sub)
	case []any:
		for _, element := range node {
			if reflect.DeepEqual(element, want) {
				return true
			}
		}
	}
	return false
}

func gristShow(v any) string {
	raw, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	if text := string(raw); utf8.RuneCountInString(text) <= 80 {
		return text
	} else {
		return string([]rune(text)[:79]) + "…"
	}
}

// SchemaViolations holds answer to a JSON Schema, the subset the grinds'
// answer schemas use: type, enum, const, required, properties,
// additionalProperties, items, minItems, maxItems, minimum, maximum,
// minLength, maxLength, pattern, anyOf, oneOf, allOf and local $ref into
// $defs or definitions. It reports one line for each place the answer does
// not fit; none when it fits, and a schema it cannot read is one violation.
func SchemaViolations(answer json.RawMessage, schema string) []string {
	var value, root any
	if err := json.Unmarshal(answer, &value); err != nil {
		return []string{"the answer is not JSON: " + err.Error()}
	}
	if err := json.Unmarshal([]byte(schema), &root); err != nil {
		return []string{"the answer schema is not JSON: " + err.Error()}
	}
	v := schemaWalk{root: root}
	v.check(value, root, "answer", 0)
	return v.found
}

type schemaWalk struct {
	root  any
	found []string
}

func (w *schemaWalk) add(path, format string, args ...any) {
	w.found = append(w.found, path+" "+fmt.Sprintf(format, args...))
}

func (w *schemaWalk) check(value, schema any, path string, depth int) {
	node, ok := schema.(map[string]any)
	if !ok || depth > 40 {
		return
	}
	if ref, ok := node["$ref"].(string); ok {
		if target, found := w.resolve(ref); found {
			w.check(value, target, path, depth+1)
		} else {
			w.add(path, "uses the reference %s, which this check cannot follow", ref)
		}
	}
	if want, ok := node["type"]; ok && !typeFits(value, want) {
		w.add(path, "is %s, the schema wants %s", jsonType(value), gristShow(want))
		return
	}
	if enum, ok := node["enum"].([]any); ok && !containsJSON(enum, value) {
		w.add(path, "is %s, not one of %s", gristShow(value), gristShow(enum))
	}
	if want, ok := node["const"]; ok && !reflect.DeepEqual(value, want) {
		w.add(path, "is %s, the schema wants %s", gristShow(value), gristShow(want))
	}
	for _, key := range []string{"allOf", "anyOf", "oneOf"} {
		subs, ok := node[key].([]any)
		if !ok {
			continue
		}
		fitting := 0
		for _, sub := range subs {
			inner := schemaWalk{root: w.root}
			inner.check(value, sub, path, depth+1)
			if len(inner.found) == 0 {
				fitting++
			} else if key == "allOf" {
				w.found = append(w.found, inner.found...)
			}
		}
		switch {
		case key == "anyOf" && fitting == 0:
			w.add(path, "fits none of the schema's anyOf")
		case key == "oneOf" && fitting != 1:
			w.add(path, "fits %d of the schema's oneOf, not exactly one", fitting)
		}
	}
	switch v := value.(type) {
	case map[string]any:
		w.object(v, node, path, depth)
	case []any:
		w.array(v, node, path, depth)
	case string:
		w.text(v, node, path)
	case float64:
		w.number(v, node, path)
	}
}

func (w *schemaWalk) object(value map[string]any, node map[string]any, path string, depth int) {
	if required, ok := node["required"].([]any); ok {
		for _, name := range required {
			if field, ok := name.(string); ok {
				if _, there := value[field]; !there {
					w.add(path, "has no %s, which the schema requires", field)
				}
			}
		}
	}
	props, _ := node["properties"].(map[string]any)
	names := make([]string, 0, len(value))
	for name := range value {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if sub, ok := props[name]; ok {
			w.check(value[name], sub, path+"."+name, depth+1)
			continue
		}
		switch extra := node["additionalProperties"].(type) {
		case bool:
			if !extra {
				w.add(path, "has %s, which the schema does not allow", name)
			}
		case map[string]any:
			w.check(value[name], extra, path+"."+name, depth+1)
		}
	}
}

func (w *schemaWalk) array(value []any, node map[string]any, path string, depth int) {
	if least, ok := node["minItems"].(float64); ok && float64(len(value)) < least {
		w.add(path, "has %d items, at least %v wanted", len(value), least)
	}
	if most, ok := node["maxItems"].(float64); ok && float64(len(value)) > most {
		w.add(path, "has %d items, at most %v wanted", len(value), most)
	}
	if items, ok := node["items"]; ok {
		for at, element := range value {
			w.check(element, items, fmt.Sprintf("%s.%d", path, at), depth+1)
		}
	}
}

func (w *schemaWalk) text(value string, node map[string]any, path string) {
	n := utf8.RuneCountInString(value)
	if least, ok := node["minLength"].(float64); ok && float64(n) < least {
		w.add(path, "is %d characters, at least %v wanted", n, least)
	}
	if most, ok := node["maxLength"].(float64); ok && float64(n) > most {
		w.add(path, "is %d characters, at most %v wanted", n, most)
	}
	if pattern, ok := node["pattern"].(string); ok {
		if re, err := regexp.Compile(pattern); err == nil && !re.MatchString(value) {
			w.add(path, "does not match the schema's pattern %q", pattern)
		}
	}
}

func (w *schemaWalk) number(value float64, node map[string]any, path string) {
	if least, ok := node["minimum"].(float64); ok && value < least {
		w.add(path, "is %v, at least %v wanted", value, least)
	}
	if most, ok := node["maximum"].(float64); ok && value > most {
		w.add(path, "is %v, at most %v wanted", value, most)
	}
}

// resolve follows a local reference, "#/$defs/name" or "#/definitions/name".
func (w *schemaWalk) resolve(ref string) (any, bool) {
	rest, ok := strings.CutPrefix(ref, "#/")
	if !ok {
		return nil, false
	}
	node := w.root
	for _, step := range strings.Split(rest, "/") {
		m, ok := node.(map[string]any)
		if !ok {
			return nil, false
		}
		if node, ok = m[step]; !ok {
			return nil, false
		}
	}
	return node, true
}

func jsonType(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case bool:
		return "a boolean"
	case float64:
		return "a number"
	case string:
		return "a string"
	case []any:
		return "an array"
	default:
		return "an object"
	}
}

func typeFits(value, want any) bool {
	switch t := want.(type) {
	case string:
		return oneTypeFits(value, t)
	case []any:
		for _, each := range t {
			if name, ok := each.(string); ok && oneTypeFits(value, name) {
				return true
			}
		}
		return false
	}
	return true
}

func oneTypeFits(value any, name string) bool {
	switch name {
	case "null":
		return value == nil
	case "boolean":
		_, ok := value.(bool)
		return ok
	case "string":
		_, ok := value.(string)
		return ok
	case "array":
		_, ok := value.([]any)
		return ok
	case "object":
		_, ok := value.(map[string]any)
		return ok
	case "number":
		_, ok := value.(float64)
		return ok
	case "integer":
		n, ok := value.(float64)
		return ok && n == math.Trunc(n)
	}
	return true
}

func containsJSON(list []any, value any) bool {
	for _, each := range list {
		if reflect.DeepEqual(each, value) {
			return true
		}
	}
	return false
}
