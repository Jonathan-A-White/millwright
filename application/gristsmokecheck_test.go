package application

import (
	"encoding/json"
	"strings"
	"testing"
)

func expectOf(t *testing.T, text string) map[string]json.RawMessage {
	t.Helper()
	example, err := ParseGristExample([]byte(`{"request": {"schemaVersion": "1"}, "expect": ` + text + `}`))
	if err != nil {
		t.Fatalf("parsing the example: %v", err)
	}
	return example.Expect
}

// Every kind of check: a bare value is equals, and each named check holds or
// fails with the field, what was wanted and what came.
func TestExpectChecksTheAnswerFieldByField(t *testing.T) {
	answer := json.RawMessage(`{"price": null, "confidence": "low", "title": "Gold ring", "tags": ["a", "b"], "box": {"count": 3}, "items": [{"name": "ring"}]}`)
	for _, c := range []struct {
		name, expect, failure string
	}{
		{"null", `{"price": {"is_null": true}}`, ""},
		{"bare value", `{"confidence": "low"}`, ""},
		{"bare null", `{"price": null}`, ""},
		{"equals", `{"box.count": {"equals": 3}}`, ""},
		{"one of", `{"confidence": {"one_of": ["low", "medium"]}}`, ""},
		{"contains text", `{"title": {"contains": "ring"}}`, ""},
		{"contains element", `{"tags": {"contains": "b"}}`, ""},
		{"matches", `{"title": {"matches": "^Gold"}}`, ""},
		{"present", `{"price": {"present": true}}`, ""},
		{"absent", `{"colour": {"present": false}}`, ""},
		{"path into an array", `{"items.0.name": "ring"}`, ""},
		{"several parts", `{"title": {"contains": "Gold", "matches": "ring$"}}`, ""},
		{"not null", `{"confidence": {"is_null": true}}`, `confidence: wanted null, got "low"`},
		{"wrong value", `{"confidence": "high"}`, `confidence: wanted equal to "high", got "low"`},
		{"not one of", `{"confidence": {"one_of": ["high", "medium"]}}`, `wanted one of ["high","medium"], got "low"`},
		{"does not contain", `{"title": {"contains": "silver"}}`, `title: wanted containing "silver", got "Gold ring"`},
		{"does not match", `{"title": {"matches": "^Silver"}}`, `title: wanted matching "^Silver", got "Gold ring"`},
		{"missing field", `{"colour": "red"}`, `colour: wanted equal to "red", got (absent)`},
		{"should be absent", `{"price": {"present": false}}`, `price: wanted absent, got null`},
		{"should be present", `{"colour": {"present": true}}`, `colour: wanted present, got (absent)`},
	} {
		failed := CheckExpect(answer, expectOf(t, c.expect))
		switch {
		case c.failure == "" && len(failed) != 0:
			t.Errorf("%s: expected it to hold, got %v", c.name, failed)
		case c.failure != "" && (len(failed) != 1 || !strings.Contains(failed[0].String(), c.failure)):
			t.Errorf("%s: expected a failure saying %q, got %v", c.name, c.failure, failed)
		}
	}
}

func TestAnExampleWithoutARequestOrWithAnUnknownCheckIsRefused(t *testing.T) {
	for _, c := range []struct{ name, text, says string }{
		{"no request", `{"expect": {}}`, "no request"},
		{"unknown check", `{"request": {}, "expect": {"price": {"is_nul": true}}}`, `"is_nul" is not a check`},
		{"bad pattern", `{"request": {}, "expect": {"price": {"matches": "("}}}`, "matches"},
		{"not json", `nonsense`, "not an example"},
	} {
		if _, err := ParseGristExample([]byte(c.text)); err == nil || !strings.Contains(err.Error(), c.says) {
			t.Errorf("%s: expected a refusal saying %q, got %v", c.name, c.says, err)
		}
	}
}

func TestAnswersAreHeldToTheWholeOfTheSchema(t *testing.T) {
	schema := `{"type": "object", "required": ["price", "items"], "additionalProperties": false, "properties": {
	  "price": {"type": ["number", "null"], "minimum": 0},
	  "grade": {"enum": ["a", "b"]},
	  "items": {"type": "array", "minItems": 1, "items": {"$ref": "#/$defs/item"}}},
	  "$defs": {"item": {"type": "object", "required": ["name"], "properties": {"name": {"type": "string", "minLength": 2}}}}}`
	for _, c := range []struct{ name, answer, says string }{
		{"fits", `{"price": null, "grade": "a", "items": [{"name": "ring"}]}`, ""},
		{"a price of a wrong type", `{"price": "12", "items": [{"name": "ring"}]}`, "answer.price is a string"},
		{"a price under the minimum", `{"price": -1, "items": [{"name": "ring"}]}`, "answer.price is -1, at least 0 wanted"},
		{"a field missing", `{"price": 1}`, "has no items"},
		{"a field the schema does not allow", `{"price": 1, "colour": "red", "items": [{"name": "ring"}]}`, "has colour, which the schema does not allow"},
		{"a value not in the enum", `{"price": 1, "grade": "z", "items": [{"name": "ring"}]}`, `answer.grade is "z", not one of`},
		{"no items", `{"price": 1, "items": []}`, "has 0 items, at least 1 wanted"},
		{"an item through a reference", `{"price": 1, "items": [{"name": "x"}]}`, "answer.items.0.name is 1 characters, at least 2 wanted"},
		{"not an object", `[]`, "is an array"},
	} {
		violations := SchemaViolations(json.RawMessage(c.answer), schema)
		joined := strings.Join(violations, "; ")
		switch {
		case c.says == "" && len(violations) != 0:
			t.Errorf("%s: expected it to fit, got %s", c.name, joined)
		case c.says != "" && !strings.Contains(joined, c.says):
			t.Errorf("%s: expected a violation saying %q, got %q", c.name, c.says, joined)
		}
	}
}
