package edilint

import (
	"fmt"
	"strings"
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
// naming the line, rather than something quietly misread. A configuration file
// that is silently misunderstood is worse than one that is rejected.

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
		if _, dup := doc.values[key]; dup {
			return nil, fmt.Errorf("line %d: duplicate key %q", ln.num, key)
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
	for _, ln := range block {
		key, rest, ok := splitYAMLKey(ln.text)
		if !ok {
			return yamlValue{}, fmt.Errorf("line %d: expected \"key: value\", got %q", ln.num, ln.text)
		}
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

// scanYAML turns raw bytes into significant lines, rejecting tab indentation.
func scanYAML(data []byte) ([]yamlLine, error) {
	var out []yamlLine
	for i, raw := range strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n") {
		num := i + 1
		raw = strings.TrimRight(strings.TrimSuffix(raw, "\r"), " \t")

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

// parseFlowSequence reads "[a, b, c]".
func parseFlowSequence(s string, line int) ([]yamlItem, error) {
	if !strings.HasSuffix(s, "]") {
		return nil, fmt.Errorf("line %d: list %q is missing its closing bracket", line, s)
	}
	inner := strings.TrimSpace(s[1 : len(s)-1])
	if inner == "" {
		return nil, nil
	}

	var items []yamlItem
	var field strings.Builder
	var quote byte
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

	for i := 0; i < len(inner); i++ {
		c := inner[i]
		switch {
		case quote == '"' && c == '\\' && i+1 < len(inner):
			field.WriteByte(c)
			i++
			field.WriteByte(inner[i])
		case quote != 0:
			if c == quote {
				quote = 0
			}
			field.WriteByte(c)
		case c == '\'' || c == '"':
			quote = c
			field.WriteByte(c)
		case c == ',':
			if err := flush(); err != nil {
				return nil, err
			}
		case c == '[' || c == ']' || c == '{' || c == '}':
			return nil, fmt.Errorf("line %d: nested lists are not supported", line)
		default:
			field.WriteByte(c)
		}
	}
	if quote != 0 {
		return nil, fmt.Errorf("line %d: unterminated quote in list %q", line, s)
	}
	if err := flush(); err != nil {
		return nil, err
	}
	return items, nil
}

// unquoteYAML removes quoting from a scalar. Double quotes honor the \\, \",
// \n, \r and \t escapes; single quotes are literal except for a doubled quote.
func unquoteYAML(s string, line int) (string, error) {
	if len(s) < 2 {
		return s, nil
	}
	switch s[0] {
	case '\'':
		if s[len(s)-1] != '\'' {
			return "", fmt.Errorf("line %d: unterminated quote in %s", line, s)
		}
		return strings.ReplaceAll(s[1:len(s)-1], "''", "'"), nil
	case '"':
		if s[len(s)-1] != '"' {
			return "", fmt.Errorf("line %d: unterminated quote in %s", line, s)
		}
		var b strings.Builder
		body := s[1 : len(s)-1]
		for i := 0; i < len(body); i++ {
			if body[i] != '\\' {
				b.WriteByte(body[i])
				continue
			}
			i++
			if i >= len(body) {
				return "", fmt.Errorf("line %d: trailing backslash in %s", line, s)
			}
			switch body[i] {
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
				return "", fmt.Errorf("line %d: unsupported escape %q in %s", line, "\\"+string(body[i]), s)
			}
		}
		return b.String(), nil
	default:
		return s, nil
	}
}
