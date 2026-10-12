package domain

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// ConfigTOML is the text of ~/.config/mw/config.toml read just far enough to
// find one key and change it, leaving every other byte as it was. It is not a
// general TOML library: it understands tables, `key = value` lines (a value
// may be a string, a number, a boolean, a date, an array or an inline table,
// across lines where TOML allows) and `#` comments, and it refuses a file that
// is not well formed by those rules, which is what the file's own readers need.
type ConfigTOML struct {
	lines []string
	stmts []tomlStatement
}

// tomlStatement is one table header or one `key = value` of the file.
type tomlStatement struct {
	table  string // the table it belongs to, dotted; "" for the root
	key    string // "" for a header
	header bool
	array  bool // a [[table]] header

	first, last int // the lines it spans
	valueCol    int // where the value starts on line first
	endCol      int // where the value ends on line last
}

var (
	tomlBareKey  = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
	tomlInteger  = regexp.MustCompile(`^([+-]?(0|[1-9](_?[0-9])*)|0x[0-9A-Fa-f](_?[0-9A-Fa-f])*|0o[0-7](_?[0-7])*|0b[01](_?[01])*)$`)
	tomlFloat    = regexp.MustCompile(`^[+-]?(0|[1-9](_?[0-9])*)(\.[0-9](_?[0-9])*)?([eE][+-]?[0-9](_?[0-9])*)?$`)
	tomlSpecial  = regexp.MustCompile(`^[+-]?(inf|nan)$`)
	tomlDateTime = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}([Tt][0-9]{2}:[0-9]{2}:[0-9]{2}(\.[0-9]+)?([Zz]|[+-][0-9]{2}:[0-9]{2})?)?$`)
	tomlTime     = regexp.MustCompile(`^[0-9]{2}:[0-9]{2}:[0-9]{2}(\.[0-9]+)?$`)
)

// ParseConfigTOML reads the text of a config file, or says on which line it
// stopped making sense. A table or a key given twice is refused, as TOML does.
func ParseConfigTOML(text string) (*ConfigTOML, error) {
	c := &ConfigTOML{lines: strings.Split(text, "\n")}
	table := ""
	tables := map[string]bool{}
	keys := map[string]bool{}

	for li := 0; li < len(c.lines); li++ {
		s := &tomlScanner{lines: c.lines, li: li}
		s.skipSpace()
		if s.atEnd() || s.peek() == '#' {
			continue
		}
		if s.peek() == '[' {
			s.col++
			array := s.peek() == '['
			if array {
				s.col++
			}
			parts, err := s.key()
			if err != nil {
				return nil, err
			}
			s.skipSpace()
			closing := "]"
			if array {
				closing = "]]"
			}
			if !strings.HasPrefix(s.lines[s.li][s.col:], closing) {
				return nil, s.fail("a table header is not closed with " + closing)
			}
			s.col += len(closing)
			if err := s.endOfLine(); err != nil {
				return nil, err
			}
			for i := range parts {
				parent := strings.Join(parts[:i], ".")
				if keys[parent+"\x00"+parts[i]] {
					return nil, fmt.Errorf("line %d: the table [%s] has the name of a key", li+1, strings.Join(parts, "."))
				}
			}
			table = strings.Join(parts, ".")
			if array {
				table = "[[" + table + "]]"
			} else if tables[table] {
				return nil, fmt.Errorf("line %d: the table [%s] is given twice", li+1, table)
			}
			tables[table] = true
			c.stmts = append(c.stmts, tomlStatement{table: table, header: true, array: array, first: li, last: li})
			continue
		}

		parts, err := s.key()
		if err != nil {
			return nil, err
		}
		s.skipSpace()
		if s.atEnd() || s.peek() != '=' {
			return nil, s.fail("a key is not followed by =")
		}
		s.col++
		s.skipSpace()
		valueCol := s.col
		if err := s.value(); err != nil {
			return nil, err
		}
		endCol, endLine := s.col, s.li
		if err := s.endOfLine(); err != nil {
			return nil, err
		}
		key := strings.Join(parts, ".")
		if !strings.HasPrefix(table, "[[") {
			id := table + "\x00" + key
			if keys[id] || tables[strings.TrimPrefix(table+"."+key, ".")] {
				return nil, fmt.Errorf("line %d: the key %s is given twice", li+1, key)
			}
			keys[id] = true
		}
		c.stmts = append(c.stmts, tomlStatement{
			table: table, key: key, first: li, last: endLine, valueCol: valueCol, endCol: endCol,
		})
		li = endLine
	}
	return c, nil
}

// Lookup is the value of table.key as the file spells it (a string keeps its
// quotes), and whether the file has the key. The root table is "".
func (c *ConfigTOML) Lookup(table, key string) (string, bool) {
	for _, st := range c.stmts {
		if !st.header && st.table == table && st.key == key {
			return c.valueText(st), true
		}
	}
	return "", false
}

func (c *ConfigTOML) valueText(st tomlStatement) string {
	if st.first == st.last {
		return strings.TrimSpace(c.lines[st.first][st.valueCol:st.endCol])
	}
	text := []string{c.lines[st.first][st.valueCol:]}
	text = append(text, c.lines[st.first+1:st.last]...)
	text = append(text, c.lines[st.last][:st.endCol])
	return strings.TrimSpace(strings.Join(text, "\n"))
}

// Set is the file's text with table.key given the value literal, which must be
// TOML text for a value (see TOMLLiteral). An existing key keeps its place and
// whatever comment follows its value; a missing key goes after the last key of
// its table; a missing table is appended after a blank line. Every other byte
// is as it was. Neither table nor key may need quoting.
func (c *ConfigTOML) Set(table, key, literal string) string {
	crlf := len(c.lines) > 1 && strings.HasSuffix(c.lines[0], "\r")
	eol := ""
	if crlf {
		eol = "\r"
	}

	for _, st := range c.stmts {
		if st.header || st.table != table || st.key != key {
			continue
		}
		line := c.lines[st.first][:st.valueCol] + literal + c.lines[st.last][st.endCol:]
		out := append([]string{}, c.lines[:st.first]...)
		out = append(out, line)
		out = append(out, c.lines[st.last+1:]...)
		return strings.Join(out, "\n")
	}

	added := key + " = " + literal + eol
	after := -1
	for _, st := range c.stmts {
		if st.table == table && !st.array && st.last > after {
			after = st.last
		}
	}
	if after >= 0 {
		out := append([]string{}, c.lines[:after+1]...)
		out = append(out, added)
		out = append(out, c.lines[after+1:]...)
		return strings.Join(out, "\n")
	}

	text := strings.Join(c.lines, "\n")
	if text == "" {
		return "[" + table + "]" + eol + "\n" + added + "\n"
	}
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	body := strings.TrimSuffix(text, "\n")
	if strings.TrimSpace(body[strings.LastIndex(body, "\n")+1:]) != "" {
		text += eol + "\n"
	}
	return text + "[" + table + "]" + eol + "\n" + added + "\n"
}

// SplitConfigKey splits "table.key" at its last dot. The table may itself be
// dotted ("cloud.caps"); every part must be a bare TOML key. A key with no
// table is refused: the root table is not edited this way.
func SplitConfigKey(dotted string) (table, key string, err error) {
	cut := strings.LastIndex(dotted, ".")
	if cut < 0 {
		return "", "", fmt.Errorf("%q needs a table: say <table>.<key>, as after_landing.millwright", dotted)
	}
	table, key = dotted[:cut], dotted[cut+1:]
	for _, part := range append(strings.Split(table, "."), key) {
		if !tomlBareKey.MatchString(part) {
			return "", "", fmt.Errorf("%q: %q is not a bare key (letters, digits, - and _ only)", dotted, part)
		}
	}
	return table, key, nil
}

// TOMLLiteral is the TOML text that stores value. A value that is already TOML
// (a quoted string, an array, an inline table, a number, a boolean or a date)
// is written as typed, so a mistake in it is the file's to refuse; anything
// else is a string and is quoted. It is an error for the value to span lines.
func TOMLLiteral(value string) (string, error) {
	if strings.ContainsAny(value, "\n\r") {
		return "", fmt.Errorf("the value must be on one line")
	}
	trimmed := strings.TrimSpace(value)
	if trimmed != "" {
		switch trimmed[0] {
		case '"', '\'', '[', '{':
			return trimmed, nil
		}
		if validBareValue(trimmed) == nil {
			return trimmed, nil
		}
	}
	var quoted strings.Builder
	quoted.WriteByte('"')
	for _, r := range value {
		switch {
		case r == '\\' || r == '"':
			quoted.WriteByte('\\')
			quoted.WriteRune(r)
		case r == '\t':
			quoted.WriteString(`\t`)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&quoted, `\u%04X`, r)
		default:
			quoted.WriteRune(r)
		}
	}
	quoted.WriteByte('"')
	return quoted.String(), nil
}

// TOMLString is the string a single-line TOML string literal holds, and true;
// for any other value it is the literal itself and false.
func TOMLString(literal string) (string, bool) {
	switch {
	case len(literal) >= 6 && strings.HasPrefix(literal, `"""`) && strings.HasSuffix(literal, `"""`),
		len(literal) >= 6 && strings.HasPrefix(literal, `'''`) && strings.HasSuffix(literal, `'''`):
		return strings.TrimPrefix(literal[3:len(literal)-3], "\n"), true
	case len(literal) >= 2 && literal[0] == '\'' && literal[len(literal)-1] == '\'':
		return literal[1 : len(literal)-1], true
	case len(literal) >= 2 && literal[0] == '"' && literal[len(literal)-1] == '"':
		if s, err := strconv.Unquote(literal); err == nil {
			return s, true
		}
		return literal[1 : len(literal)-1], true
	}
	return literal, false
}

// SameTOMLValue reports whether two value literals store the same thing: the
// same string however it is quoted, or the same text for anything else.
func SameTOMLValue(a, b string) bool {
	as, aIs := TOMLString(a)
	bs, bIs := TOMLString(b)
	return aIs == bIs && as == bs
}

func validBareValue(token string) error {
	switch token {
	case "true", "false":
		return nil
	}
	if tomlInteger.MatchString(token) || tomlSpecial.MatchString(token) || tomlDateTime.MatchString(token) || tomlTime.MatchString(token) {
		return nil
	}
	if tomlFloat.MatchString(token) && strings.ContainsAny(token, ".eE") {
		return nil
	}
	return fmt.Errorf("%q is not a TOML value", token)
}

// tomlScanner walks the lines of a file one character at a time.
type tomlScanner struct {
	lines []string
	li    int
	col   int
}

func (s *tomlScanner) fail(what string) error {
	return fmt.Errorf("line %d: %s", s.li+1, what)
}

func (s *tomlScanner) atEnd() bool { return s.col >= len(s.lines[s.li]) }

func (s *tomlScanner) peek() byte {
	if s.atEnd() {
		return 0
	}
	return s.lines[s.li][s.col]
}

func (s *tomlScanner) skipSpace() {
	for !s.atEnd() && strings.IndexByte(" \t\r", s.peek()) >= 0 {
		s.col++
	}
}

// skipBlank moves past spaces, comments and line ends, inside an array.
func (s *tomlScanner) skipBlank() bool {
	for {
		s.skipSpace()
		if !s.atEnd() && s.peek() == '#' {
			s.col = len(s.lines[s.li])
		}
		if !s.atEnd() {
			return true
		}
		if s.li+1 >= len(s.lines) {
			return false
		}
		s.li++
		s.col = 0
	}
}

// endOfLine wants nothing but a comment after a header or a value.
func (s *tomlScanner) endOfLine() error {
	s.skipSpace()
	if !s.atEnd() && s.peek() != '#' {
		return s.fail(fmt.Sprintf("unexpected %q after a value", s.lines[s.li][s.col:]))
	}
	return nil
}

// key reads a dotted key: bare parts, or "quoted" or 'quoted' ones.
func (s *tomlScanner) key() ([]string, error) {
	var parts []string
	for {
		s.skipSpace()
		start := s.col
		switch c := s.peek(); {
		case c == '"' || c == '\'':
			end := strings.IndexByte(s.lines[s.li][s.col+1:], c)
			if end < 0 {
				return nil, s.fail("a quoted key is not closed")
			}
			parts = append(parts, s.lines[s.li][s.col+1:s.col+1+end])
			s.col += end + 2
		default:
			for !s.atEnd() && (s.peek() == '-' || s.peek() == '_' || s.peek() >= '0' && s.peek() <= '9' ||
				s.peek() >= 'A' && s.peek() <= 'Z' || s.peek() >= 'a' && s.peek() <= 'z') {
				s.col++
			}
			if s.col == start {
				return nil, s.fail("expected a key")
			}
			parts = append(parts, s.lines[s.li][start:s.col])
		}
		s.skipSpace()
		if s.peek() != '.' {
			return parts, nil
		}
		s.col++
	}
}

// value reads one value, leaving the scanner just past it.
func (s *tomlScanner) value() error {
	if s.atEnd() {
		return s.fail("a key has no value")
	}
	rest := s.lines[s.li][s.col:]
	switch c := s.peek(); {
	case strings.HasPrefix(rest, `"""`), strings.HasPrefix(rest, `'''`):
		return s.multilineString(rest[:3])
	case c == '"':
		return s.basicString()
	case c == '\'':
		end := strings.IndexByte(rest[1:], '\'')
		if end < 0 {
			return s.fail("a string is not closed")
		}
		s.col += end + 2
		return nil
	case c == '[':
		return s.array()
	case c == '{':
		return s.inlineTable()
	}
	start := s.col
	for !s.atEnd() && strings.IndexByte(" \t\r,]}#", s.peek()) < 0 {
		s.col++
	}
	if s.col == start {
		return s.fail("a key has no value")
	}
	if err := validBareValue(s.lines[s.li][start:s.col]); err != nil {
		return s.fail(err.Error())
	}
	return nil
}

func (s *tomlScanner) basicString() error {
	line := s.lines[s.li]
	for i := s.col + 1; i < len(line); i++ {
		switch line[i] {
		case '\\':
			if i+1 >= len(line) || strings.IndexByte(`btnfre"\uU`, line[i+1]) < 0 {
				s.col = i
				return s.fail("a string has a backslash that is not an escape")
			}
			i++
		case '"':
			s.col = i + 1
			return nil
		}
	}
	return s.fail("a string is not closed")
}

func (s *tomlScanner) multilineString(quote string) error {
	s.col += 3
	for {
		line := s.lines[s.li]
		for ; s.col < len(line); s.col++ {
			if quote == `"""` && line[s.col] == '\\' {
				s.col++
				continue
			}
			if strings.HasPrefix(line[s.col:], quote) {
				s.col += 3
				return nil
			}
		}
		if s.li+1 >= len(s.lines) {
			return s.fail("a multi-line string is not closed")
		}
		s.li++
		s.col = 0
	}
}

func (s *tomlScanner) array() error {
	s.col++
	for {
		if !s.skipBlank() {
			return s.fail("an array is not closed")
		}
		if s.peek() == ']' {
			s.col++
			return nil
		}
		if err := s.value(); err != nil {
			return err
		}
		if !s.skipBlank() {
			return s.fail("an array is not closed")
		}
		switch s.peek() {
		case ',':
			s.col++
		case ']':
			s.col++
			return nil
		default:
			return s.fail("an array has a value not followed by , or ]")
		}
	}
}

func (s *tomlScanner) inlineTable() error {
	s.col++
	for {
		s.skipSpace()
		if s.peek() == '}' {
			s.col++
			return nil
		}
		if _, err := s.key(); err != nil {
			return err
		}
		s.skipSpace()
		if s.peek() != '=' {
			return s.fail("an inline table has a key not followed by =")
		}
		s.col++
		s.skipSpace()
		if err := s.value(); err != nil {
			return err
		}
		s.skipSpace()
		switch s.peek() {
		case ',':
			s.col++
		case '}':
			s.col++
			return nil
		default:
			return s.fail("an inline table is not closed")
		}
	}
}
