package edilint

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// FuzzParseYAML holds the restricted reader to its contract on arbitrary
// bytes: it never panics, it returns a document or an error and never both,
// every error names a line, and everything in a returned document is
// well-formed UTF-8. The seed corpus is the subset's features and every edge
// the unit tests reject, so a plain `go test` run exercises all of them.
func FuzzParseYAML(f *testing.F) {
	seeds := []string{
		"",
		"---\n...\n",
		"# only a comment\n",
		"version: 1\ncharset: extended # trailing comment\n",
		"delimiter: \"|\"\nquoted: 'it''s here'\nescaped: \"a\\tb\"\n",
		"hash-in-value: \"#1234\"\ncolon-in-value: TRL:2:DTL\nempty:\n",
		"disable:\n  - EL1006\n  - charset.nonascii\n",
		"flow: [a, \"b c\", 'd']\nflow-empty: []\n",
		"severity:\n  EL2002: info\n  terminator.mixed: warning\n",
		"charset: basic\r\ndisable:\r\n  - EL1006\r\n",
		"\xef\xbb\xbfcharset: basic\n",
		"\xff\xfec\x00h\x00",
		"\xfe\xff\x00c\x00h",
		"charset: b\xffasic\n",
		"a: b\rc: d\r",
		"disable:\n\t- EL1006\n",
		"charset: basic\ncharset: off\n",
		"severity:\n  EL2002: info\n  EL2002: warning\n",
		"severity:\n  charset:\n    homoglyph: info\n",
		"severity: {EL2002: info}\n",
		"- EL1006\n",
		"  charset: basic\n",
		"charset extended\n",
		"%YAML 1.2\ncharset: basic\n",
		"disable: [EL1006\n",
		"disable: [[a]]\n",
		"disable: [a, , b]\n",
		"disable: [a] junk\n",
		"disable:\n  -\n  - EL1006\n",
		"charset: \"basic\n",
		"charset: \"a\\qb\"\n",
		"charset: \"basic\" oops\n",
		"charset: \"\n",
		"charset: '''\n",
		"key: \"a\\\"\n",
		"severity:\n  EL2002:\n",
	}
	for _, s := range seeds {
		f.Add([]byte(s))
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		doc, err := parseYAML(data)
		if err != nil {
			if doc != nil {
				t.Error("parseYAML returned a document and an error")
			}
			if !strings.HasPrefix(err.Error(), "line ") {
				t.Errorf("error %q does not name a line", err)
			}
			return
		}
		if doc == nil {
			t.Fatal("parseYAML returned neither a document nor an error")
		}
		if len(doc.keys) != len(doc.values) {
			t.Errorf("%d keys but %d values", len(doc.keys), len(doc.values))
		}
		for _, key := range doc.keys {
			val, ok := doc.values[key]
			if !ok {
				t.Errorf("key %q has no value", key)
				continue
			}
			if !utf8.ValidString(key) || !utf8.ValidString(val.scalar) {
				t.Errorf("key %q holds invalid UTF-8", key)
			}
			for _, item := range val.seq {
				if !utf8.ValidString(item.text) {
					t.Errorf("list entry %q under %q is invalid UTF-8", item.text, key)
				}
			}
			for _, pair := range val.pairs {
				if !utf8.ValidString(pair.key) || !utf8.ValidString(pair.value) {
					t.Errorf("mapping entry %q under %q is invalid UTF-8", pair.key, key)
				}
			}
		}
	})
}
