package edilint

import (
	"bytes"
	"fmt"
	"strings"
	"unicode/utf8"
)

// A restricted YAML reader.
//
// edilint has no dependencies outside the standard library, and a configuration
// file is not worth changing that: it costs every user of the library a
// transitive dependency to read a file most of them will never have. What the
// configuration format actually needs is a mapping whose values are scalars,
// lists of scalars, or one further mapping of scalars, and that is exactly what
// this reads.
//
// It is deliberately strict. Anything outside the subset — an anchor, a
// multi-line scalar, a list of mappings, a tab in the indentation — is an error
// naming the line, rather than something quietly misread. The same rule covers
// the file's encoding: a UTF-16 file, invalid UTF-8, a carriage return without
// a line feed and a duplicate key at any level are all errors, because each is
// a file that a person and this reader would understand differently. The one
// tolerated irregularity is a UTF-8 byte order mark at the start, which some
// Windows editors prepend and which changes nothing about what the file says.
// A configuration file that is silently misunderstood is worse than one that
// is rejected.
//
// Every error names the line it is about. FuzzParseYAML holds the reader to
// that, and to never panicking, on arbitrary input.

type yamlKind int

const (
	yamlScalar yamlKind = iota
	yamlSequence
	yamlMapping
)

// yamlValue is the value of one top-level key.
type yamlValue struct {
	kind   yamlKind
	line   int
	scalar string
	seq    []yamlItem
	pairs  []yamlPair
}

// yamlItem is one entry of a sequence of scalars.
type yamlItem struct {
	line int
	text string
}

// yamlPair is one entry of a nested mapping of scalars.
type yamlPair struct {
	line       int
	key, value string
}

// yamlDoc is a top-level mapping, with key order preserved.
type yamlDoc struct {
	keys   []string
	values map[string]yamlValue
}

// yamlLine is one significant physical line: blanks, comments and document
// markers are dropped during scanning.
type yamlLine struct {
	num    int
	indent int
	text   string
}

// parseYAML reads the restricted subset described above.
func parseYAML(data []byte) (*yamlDoc, error) {
	lines, err := scanYAML(data)
	if err != nil {
		return nil, err
	}

	doc := &yamlDoc{values: map[string]yamlValue{}}
	for i := 0; i < len(lines); {
		ln := lines[i]
		if ln.indent != 0 {
			return nil, fmt.Errorf("line %d: unexpected indentation; %q is not under any key", ln.num, ln.text)
		}
		if strings.HasPrefix(ln.text, "- ") || ln.text == "-" {
			return nil, fmt.Errorf("line %d: the document must be a mapping of settings, not a list", ln.num)
		}

		key, rest, ok := splitYAMLKey(ln.text)
		if !ok {
			return nil, fmt.Errorf("line %d: expected \"key: value\", got %q", ln.num, ln.text)
		}
		if prev, dup := doc.values[key]; dup {
			return nil, fmt.Errorf("line %d: duplicate key %q (first set on line %d)", ln.num, key, prev.line)
		}
		i++

		val := yamlValue{line: ln.num}
		switch {
		case rest != "":
			if strings.HasPrefix(rest, "[") {
				items, err := parseFlowSequence(rest, ln.num)
				if err != nil {
					return nil, err
				}
				val.kind, val.seq = yamlSequence, items
			} else if strings.HasPrefix(rest, "{") {
				return nil, fmt.Errorf("line %d: inline mappings are not supported; "+
					"write the entries on their own indented lines", ln.num)
			} else {
				scalar, err := unquoteYAML(rest, ln.num)
				if err != nil {
					return nil, err
				}
				val.kind, val.scalar = yamlScalar, scalar
			}
		default:
			var block []yamlLine
			for i < len(lines) && lines[i].indent > 0 {
				block = append(block, lines[i])
				i++
			}
			if len(block) == 0 {
				// "key:" with nothing under it is an explicit empty value.
				val.kind = yamlScalar
				break
			}
			parsed, err := parseYAMLBlock(block)
			if err != nil {
				return nil, err
			}
			parsed.line = ln.num
			val = parsed
		}

		doc.keys = append(doc.keys, key)
		doc.values[key] = val
	}
	return doc, nil
}

// parseYAMLBlock parses the indented lines under one key: either a sequence of
// scalars or a mapping of scalars, and nothing deeper.
func parseYAMLBlock(block []yamlLine) (yamlValue, error) {
	indent := block[0].indent
	for _, ln := range block {
		if ln.indent != indent {
			return yamlValue{}, fmt.Errorf("line %d: inconsistent indentation; "+
				"every entry under one key must be indented the same amount, and nested "+
				"structures are not supported", ln.num)
		}
	}

	if strings.HasPrefix(block[0].text, "- ") || block[0].text == "-" {
		val := yamlValue{kind: yamlSequence}
		for _, ln := range block {
			if !strings.HasPrefix(ln.text, "- ") && ln.text != "-" {
				return yamlValue{}, fmt.Errorf("line %d: expected a list entry starting with \"- \", got %q",
					ln.num, ln.text)
			}
			text := strings.TrimSpace(strings.TrimPrefix(ln.text, "-"))
			if text == "" {
				return yamlValue{}, fmt.Errorf("line %d: empty list entry", ln.num)
			}
			scalar, err := unquoteYAML(text, ln.num)
			if err != nil {
				return yamlValue{}, err
			}
			val.seq = append(val.seq, yamlItem{line: ln.num, text: scalar})
		}
		return val, nil
	}

	val := yamlValue{kind: yamlMapping}
	seen := map[string]int{}
	for _, ln := range block {
		key, rest, ok := splitYAMLKey(ln.text)
		if !ok {
			return yamlValue{}, fmt.Errorf("line %d: expected \"key: value\", got %q", ln.num, ln.text)
		}
		if first, dup := seen[key]; dup {
			return yamlValue{}, fmt.Errorf("line %d: duplicate key %q (first set on line %d)", ln.num, key, first)
		}
		seen[key] = ln.num
		if rest == "" {
			return yamlValue{}, fmt.Errorf("line %d: %q has no value; nested structures are not supported",
				ln.num, key)
		}
		scalar, err := unquoteYAML(rest, ln.num)
		if err != nil {
			return yamlValue{}, err
		}
		val.pairs = append(val.pairs, yamlPair{line: ln.num, key: key, value: scalar})
	}
	return val, nil
}

// scanYAML turns raw bytes into significant lines. It owns the checks that are
// about the file rather than its structure: the encoding must be UTF-8, line
// endings must be LF or CRLF, and indentation must be spaces.
func scanYAML(data []byte) ([]yamlLine, error) {
	if bytes.HasPrefix(data, []byte{0xFF, 0xFE}) || bytes.HasPrefix(data, []byte{0xFE, 0xFF}) {
		return nil, fmt.Errorf("line 1: the file starts with a UTF-16 byte order mark; edilint reads UTF-8")
	}
	// A UTF-8 byte order mark is what some Windows editors prepend. It means
	// the same document, so it is dropped rather than rejected.
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})

	var out []yamlLine
	for i, raw := range strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n") {
		num := i + 1
		if strings.ContainsRune(raw, '\r') {
			return nil, fmt.Errorf("line %d: carriage return without a line feed; "+
				"the file must use LF or CRLF line endings", num)
		}
		if !utf8.ValidString(raw) {
			return nil, fmt.Errorf("line %d: invalid UTF-8; the file must be UTF-8 text", num)
		}
		raw = strings.TrimRight(raw, " \t")

		indent := 0
		for indent < len(raw) && (raw[indent] == ' ' || raw[indent] == '\t') {
			if raw[indent] == '\t' {
				return nil, fmt.Errorf("line %d: tab in the indentation; YAML requires spaces", num)
			}
			indent++
		}

		text := stripYAMLComment(raw[indent:])
		text = strings.TrimRight(text, " \t")
		switch {
		case text == "", text == "---", text == "...":
			continue
		case strings.HasPrefix(text, "%"):
			return nil, fmt.Errorf("line %d: directives are not supported", num)
		}
		out = append(out, yamlLine{num: num, indent: indent, text: text})
	}
	return out, nil
}

// stripYAMLComment removes a trailing comment. A "#" only starts one at the
// beginning of the content or after a space, and never inside quotes, so a
// value such as "#1234" survives.
func stripYAMLComment(s string) string {
	var quote byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quote == '"' && c == '\\':
			i++
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '\'' || c == '"':
			quote = c
		case c == '#' && (i == 0 || s[i-1] == ' ' || s[i-1] == '\t'):
			return s[:i]
		}
	}
	return s
}

// splitYAMLKey splits "key: value" or a bare "key:". The separator is a colon
// followed by a space or the end of the line, so a value that contains a colon,
// such as a count rule, does not need quoting.
func splitYAMLKey(s string) (key, rest string, ok bool) {
	var quote byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quote == '"' && c == '\\':
			i++
			continue
		case quote != 0:
			if c == quote {
				quote = 0
			}
			continue
		case c == '\'' || c == '"':
			quote = c
			continue
		case c != ':':
			continue
		}
		if i+1 == len(s) {
			key = strings.TrimSpace(s[:i])
			return key, "", key != ""
		}
		if s[i+1] == ' ' || s[i+1] == '\t' {
			key = strings.TrimSpace(s[:i])
			return key, strings.TrimSpace(s[i+1:]), key != ""
		}
	}
	return "", "", false
}

// parseFlowSequence reads "[a, b, c]". The closing bracket must end the value:
// anything after it would be content the reader was about to drop, which is an
// error instead.
func parseFlowSequence(s string, line int) ([]yamlItem, error) {
	var items []yamlItem
	var field strings.Builder
	var quote byte
	sawComma := false

	flush := func() error {
		text := strings.TrimSpace(field.String())
		field.Reset()
		if text == "" {
			return fmt.Errorf("line %d: empty entry in list %q", line, s)
		}
		scalar, err := unquoteYAML(text, line)
		if err != nil {
			return err
		}
		items = append(items, yamlItem{line: line, text: scalar})
		return nil
	}

	for i := 1; i < len(s); i++ {
		c := s[i]
		switch {
		case quote == '"' && c == '\\' && i+1 < len(s):
			field.WriteByte(c)
			i++
			field.WriteByte(s[i])
		case quote != 0:
			if c == quote {
				quote = 0
			}
			field.WriteByte(c)
		case c == '\'' || c == '"':
			quote = c
			field.WriteByte(c)
		case c == ',':
			sawComma = true
			if err := flush(); err != nil {
				return nil, err
			}
		case c == ']':
			if rest := strings.TrimSpace(s[i+1:]); rest != "" {
				return nil, fmt.Errorf("line %d: unexpected %q after the closing bracket of %q",
					line, rest, s[:i+1])
			}
			if sawComma || strings.TrimSpace(field.String()) != "" {
				if err := flush(); err != nil {
					return nil, err
				}
			}
			return items, nil
		case c == '[' || c == '{' || c == '}':
			return nil, fmt.Errorf("line %d: nested lists are not supported", line)
		default:
			field.WriteByte(c)
		}
	}
	if quote != 0 {
		return nil, fmt.Errorf("line %d: unterminated quote in list %q", line, s)
	}
	return nil, fmt.Errorf("line %d: list %q is missing its closing bracket", line, s)
}

// unquoteYAML removes quoting from a scalar. Double quotes honor the \\, \",
// \n, \r and \t escapes; single quotes are literal except for a doubled quote.
//
// It scans for the quote that closes the one that opened the scalar, rather
// than trusting the last character to be it: `"a" b` has a closing quote and
// then content the reader was about to drop, and a lone `"` has none at all.
// Both are errors, not approximations.
func unquoteYAML(s string, line int) (string, error) {
	if s == "" || (s[0] != '\'' && s[0] != '"') {
		return s, nil
	}

	var b strings.Builder
	if s[0] == '\'' {
		for i := 1; i < len(s); i++ {
			if s[i] != '\'' {
				b.WriteByte(s[i])
				continue
			}
			if i+1 < len(s) && s[i+1] == '\'' {
				b.WriteByte('\'')
				i++
				continue
			}
			if i+1 != len(s) {
				return "", fmt.Errorf("line %d: unexpected %q after the closing quote in %s", line, s[i+1:], s)
			}
			return b.String(), nil
		}
		return "", fmt.Errorf("line %d: unterminated quote in %s", line, s)
	}

	for i := 1; i < len(s); i++ {
		switch c := s[i]; c {
		case '"':
			if i+1 != len(s) {
				return "", fmt.Errorf("line %d: unexpected %q after the closing quote in %s", line, s[i+1:], s)
			}
			return b.String(), nil
		case '\\':
			i++
			if i == len(s) {
				return "", fmt.Errorf("line %d: unterminated quote in %s", line, s)
			}
			switch s[i] {
			case '\\':
				b.WriteByte('\\')
			case '"':
				b.WriteByte('"')
			case 'n':
				b.WriteByte('\n')
			case 'r':
				b.WriteByte('\r')
			case 't':
				b.WriteByte('\t')
			case '0':
				b.WriteByte(0)
			default:
				return "", fmt.Errorf("line %d: unsupported escape %q in %s", line, "\\"+string(s[i]), s)
			}
		default:
			b.WriteByte(c)
		}
	}
	return "", fmt.Errorf("line %d: unterminated quote in %s", line, s)
}
