package edilint

import (
	"strings"
	"testing"
)

func TestParseYAMLAcceptsTheSubset(t *testing.T) {
	doc, err := parseYAML([]byte(`---
# A comment on its own line.
version: 1
charset: extended          # and a trailing one
delimiter: "|"
quoted: 'it''s here'
escaped: "a\tb"
hash-in-value: "#1234"
colon-in-value: TRL:2:DTL
empty:
disable:
  - EL1006
  - charset.nonascii
flow: [a, "b c", 'd']
severity:
  EL2002: info
  terminator.mixed: warning
...
`))
	if err != nil {
		t.Fatalf("parseYAML: %v", err)
	}

	scalars := map[string]string{
		"version":        "1",
		"charset":        "extended",
		"delimiter":      "|",
		"quoted":         "it's here",
		"escaped":        "a\tb",
		"hash-in-value":  "#1234",
		"colon-in-value": "TRL:2:DTL",
		"empty":          "",
	}
	for key, want := range scalars {
		val, ok := doc.values[key]
		if !ok {
			t.Errorf("key %q is missing", key)
			continue
		}
		if val.kind != yamlScalar || val.scalar != want {
			t.Errorf("%q = %q (kind %d), want %q", key, val.scalar, val.kind, want)
		}
	}

	seq := doc.values["disable"]
	if seq.kind != yamlSequence || len(seq.seq) != 2 ||
		seq.seq[0].text != "EL1006" || seq.seq[1].text != "charset.nonascii" {
		t.Errorf("disable = %+v, want a two-entry list", seq)
	}

	flow := doc.values["flow"]
	if flow.kind != yamlSequence || len(flow.seq) != 3 ||
		flow.seq[0].text != "a" || flow.seq[1].text != "b c" || flow.seq[2].text != "d" {
		t.Errorf("flow = %+v, want three entries", flow)
	}

	sev := doc.values["severity"]
	if sev.kind != yamlMapping || len(sev.pairs) != 2 ||
		sev.pairs[0].key != "EL2002" || sev.pairs[0].value != "info" ||
		sev.pairs[1].key != "terminator.mixed" || sev.pairs[1].value != "warning" {
		t.Errorf("severity = %+v, want two entries", sev)
	}

	// Key order is preserved, which is what lets a diagnostic name the offender.
	if len(doc.keys) != len(doc.values) {
		t.Errorf("keys = %v, values = %d", doc.keys, len(doc.values))
	}
}

func TestParseYAMLRecordsLineNumbers(t *testing.T) {
	doc, err := parseYAML([]byte("\n# comment\n\ncharset: basic\ndisable:\n  - EL1006\n"))
	if err != nil {
		t.Fatalf("parseYAML: %v", err)
	}
	if got := doc.values["charset"].line; got != 4 {
		t.Errorf("charset is on line %d, want 4", got)
	}
	if got := doc.values["disable"].seq[0].line; got != 6 {
		t.Errorf("the list entry is on line %d, want 6", got)
	}
}

func TestParseYAMLHandlesCRLF(t *testing.T) {
	doc, err := parseYAML([]byte("charset: basic\r\ndisable:\r\n  - EL1006\r\n"))
	if err != nil {
		t.Fatalf("parseYAML: %v", err)
	}
	if doc.values["charset"].scalar != "basic" {
		t.Errorf("charset = %q, want basic", doc.values["charset"].scalar)
	}
	if len(doc.values["disable"].seq) != 1 || doc.values["disable"].seq[0].text != "EL1006" {
		t.Errorf("disable = %+v", doc.values["disable"].seq)
	}
}

func TestParseYAMLStripsAUTF8ByteOrderMark(t *testing.T) {
	// Some Windows editors prepend one. It changes nothing about what the file
	// says, so it is dropped — and line 1 is still line 1.
	doc, err := parseYAML([]byte("\xef\xbb\xbfcharset: basic\n"))
	if err != nil {
		t.Fatalf("parseYAML: %v", err)
	}
	val := doc.values["charset"]
	if val.scalar != "basic" || val.line != 1 {
		t.Errorf("charset = %q on line %d, want basic on line 1", val.scalar, val.line)
	}
}

func TestParseYAMLTrailingWhitespaceIsInsignificant(t *testing.T) {
	doc, err := parseYAML([]byte("charset: basic   \nquoted: 'a '  \n"))
	if err != nil {
		t.Fatalf("parseYAML: %v", err)
	}
	if got := doc.values["charset"].scalar; got != "basic" {
		t.Errorf("trailing spaces must not reach the value, got %q", got)
	}
	if got := doc.values["quoted"].scalar; got != "a " {
		t.Errorf("a space inside quotes is part of the value, got %q", got)
	}
}

func TestParseYAMLRejectsWhatItCannotRead(t *testing.T) {
	// A configuration file that is silently misread is worse than one that is
	// rejected, so everything outside the subset has to be an error.
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"tab indentation", "disable:\n\t- EL1006\n", "tab in the indentation"},
		{"nested mapping", "severity:\n  charset:\n    homoglyph: info\n", "inconsistent indentation"},
		{"list of mappings", "rules:\n  - name: a\n    id: b\n", "inconsistent indentation"},
		{"top-level list", "- EL1006\n", "must be a mapping"},
		{"no colon", "charset extended\n", `expected "key: value"`},
		{"duplicate key", "charset: basic\ncharset: off\n", `duplicate key "charset" (first set on line 1)`},
		{"duplicate nested key", "severity:\n  EL2002: info\n  EL2002: warning\n",
			`duplicate key "EL2002" (first set on line 2)`},
		{"stray indentation", "  charset: basic\n", "unexpected indentation"},
		{"inline mapping", "severity: {EL2002: info}\n", "inline mappings are not supported"},
		{"unterminated flow", "disable: [EL1006\n", "missing its closing bracket"},
		{"nested flow", "disable: [[a]]\n", "nested lists are not supported"},
		{"empty flow entry", "disable: [a, , b]\n", "empty entry"},
		{"content after a flow list", "disable: [a] junk\n", "after the closing bracket"},
		{"empty list entry", "disable:\n  -\n  - EL1006\n", "empty list entry"},
		{"unterminated quote", `charset: "basic` + "\n", "unterminated quote"},
		{"lone quote", "charset: \"\n", "unterminated quote"},
		{"escaped closing quote", `charset: "basic\"` + "\n", "unterminated quote"},
		{"single quote closed by its own escape", "charset: '''\n", "unterminated quote"},
		{"content after the closing quote", `charset: "basic" oops` + "\n", "after the closing quote"},
		{"unknown escape", `charset: "a\qb"` + "\n", "unsupported escape"},
		{"directive", "%YAML 1.2\ncharset: basic\n", "directives are not supported"},
		{"key with no value in a block", "severity:\n  EL2002:\n", "nested structures are not supported"},
		{"UTF-16 little-endian BOM", "\xff\xfec\x00h\x00", "UTF-16"},
		{"UTF-16 big-endian BOM", "\xfe\xff\x00c\x00h", "UTF-16"},
		{"invalid UTF-8", "charset: b\xffasic\n", "invalid UTF-8"},
		{"bare carriage return", "a: b\rc: d\n", "carriage return"},
		{"classic Mac line endings", "a: b\rc: d\r", "carriage return"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseYAML([]byte(tc.in))
			if err == nil {
				t.Fatalf("expected an error for:\n%s", tc.in)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %v, want it to mention %q", err, tc.want)
			}
		})
	}
}

func TestStripYAMLComment(t *testing.T) {
	tests := map[string]string{
		"charset: basic # why":     "charset: basic ",
		"charset: basic#not":       "charset: basic#not",
		`charset: "a # b"`:         `charset: "a # b"`,
		`charset: 'a # b' # after`: `charset: 'a # b' `,
		"# whole line":             "",
		`charset: "a\" # b"`:       `charset: "a\" # b"`,
	}
	for in, want := range tests {
		if got := stripYAMLComment(in); got != want {
			t.Errorf("stripYAMLComment(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSplitYAMLKey(t *testing.T) {
	tests := []struct {
		in        string
		key, rest string
		ok        bool
	}{
		{in: "charset: basic", key: "charset", rest: "basic", ok: true},
		{in: "charset:", key: "charset", rest: "", ok: true},
		{in: "count-rules: TRL:2:DTL", key: "count-rules", rest: "TRL:2:DTL", ok: true},
		{in: `"a: b": c`, key: `"a: b"`, rest: "c", ok: true},
		{in: "charset basic"},
		{in: "charset:basic"},
		{in: ": basic"},
	}
	for _, tc := range tests {
		key, rest, ok := splitYAMLKey(tc.in)
		if ok != tc.ok {
			t.Errorf("splitYAMLKey(%q) reported ok = %v, want %v", tc.in, ok, tc.ok)
			continue
		}
		// The key and the remainder only mean anything on a line that split.
		if ok && (key != tc.key || rest != tc.rest) {
			t.Errorf("splitYAMLKey(%q) = %q, %q; want %q, %q", tc.in, key, rest, tc.key, tc.rest)
		}
	}
}
