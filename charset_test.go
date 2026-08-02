package edilint

import (
	"strings"
	"testing"
)

func TestCharsetHygiene(t *testing.T) {
	// The inputs contain literal suspicious characters; wantCode pins the exact
	// code point, so the test fails loudly if an editor ever normalizes one away.
	tests := []struct {
		name     string
		input    string
		opts     Options
		wantRule string // "" means the input must be clean
		wantCode string
		wantLine int
		wantCol  int
	}{
		{
			name:  "plain ascii is clean",
			input: "HDR|NORTHGATE|20260115\nDTL|RIVERA|1440.00\nTRL|1\n",
		},
		{
			name:     "utf-8 bom",
			input:    "\ufeffHDR|NORTHGATE|20260115\nDTL|RIVERA|1440.00\nTRL|1\n",
			wantRule: RuleBOM,
			wantCode: "U+FEFF",
			wantLine: 1,
			wantCol:  1,
		},
		{
			name:     "cyrillic capital A masquerading as latin A",
			input:    "HDR|NORTHGATE|20260115\nDTL|RIVERА|1440.00\nTRL|1\n",
			wantRule: RuleHomoglyph,
			wantCode: "U+0410",
			wantLine: 2,
			wantCol:  10,
		},
		{
			name:     "cyrillic lowercase o",
			input:    "HDR|A|B\nDTL|Rоbert|C\nTRL|1\n",
			wantRule: RuleHomoglyph,
			wantCode: "U+043E",
		},
		{
			name:     "greek capital omicron",
			input:    "HDR|A|B\nDTL|RΟBERT|C\nTRL|1\n",
			wantRule: RuleHomoglyph,
			wantCode: "U+039F",
		},
		{
			name:     "fullwidth digit",
			input:    "HDR|A|B\nDTL|RIVERA|14４0.00\nTRL|1\n",
			wantRule: RuleHomoglyph,
			wantCode: "U+FF14",
		},
		{
			name:     "non-breaking space",
			input:    "HDR|A|B\nDTL|RIVERA DANA|C\nTRL|1\n",
			wantRule: RuleHomoglyph,
			wantCode: "U+00A0",
		},
		{
			name:     "en dash for hyphen",
			input:    "HDR|A|B\nDTL|PPO–GOLD|C\nTRL|1\n",
			wantRule: RuleHomoglyph,
			wantCode: "U+2013",
		},
		{
			name:     "curly apostrophe",
			input:    "HDR|A|B\nDTL|O’BRIEN|C\nTRL|1\n",
			wantRule: RuleHomoglyph,
			wantCode: "U+2019",
		},
		{
			name:     "zero width space",
			input:    "HDR|A|B\nDTL|RIVERA\u200bDANA|C\nTRL|1\n",
			wantRule: RuleZeroWidth,
			wantCode: "U+200B",
		},
		{
			name:     "soft hyphen",
			input:    "HDR|A|B\nDTL|RIV\u00adERA|C\nTRL|1\n",
			wantRule: RuleZeroWidth,
			wantCode: "U+00AD",
		},
		{
			name:     "right-to-left override",
			input:    "HDR|A|B\nDTL|\u202eRIVERA|C\nTRL|1\n",
			wantRule: RuleZeroWidth,
			wantCode: "U+202E",
		},
		{
			name:     "vertical tab is a stray control character",
			input:    "HDR|A|B\nDTL|RIVERA\x0bDANA|C\nTRL|1\n",
			wantRule: RuleNonPrint,
			wantCode: "U+000B",
		},
		{
			name:     "null byte",
			input:    "HDR|A|B\nDTL|RIVERA\x00DANA|C\nTRL|1\n",
			wantRule: RuleNonPrint,
			wantCode: "U+0000",
		},
		{
			name:     "tab in a pipe-delimited file",
			input:    "HDR|A|B\nDTL|RIVERA\tDANA|C\nTRL|1\n",
			wantRule: RuleNonPrint,
			wantCode: "U+0009",
		},
		{
			name:     "invalid utf-8 byte",
			input:    "HDR|A|B\nDTL|RIVERA\xffDANA|C\nTRL|1\n",
			wantRule: RuleInvalidUTF8,
			wantCode: "0xFF",
		},
		{
			name:     "genuine accented latin is only a warning",
			input:    "HDR|A|B\nDTL|BJÖRK|C\nTRL|1\n",
			wantRule: RuleNonASCII,
		},
		{
			name:  "tab is fine in a tab-delimited file",
			input: "HDR\tA\tB\nDTL\tRIVERA\tC\nTRL\t1\n",
			opts:  Options{Delimiter: "\\t"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			opts := tc.opts
			// Keep each case focused on the character rules.
			opts.Disabled = append(opts.Disabled, ClassFields, ClassTerminator)
			rep := Lint("test", []byte(tc.input), opts)

			if tc.wantRule == "" {
				if len(rep.Findings) != 0 {
					t.Fatalf("expected no findings, got %v", ruleNames(rep))
				}
				return
			}

			// Exactly one finding, and it is the expected one: a case that also
			// produced a spurious second finding used to pass silently.
			requireRule(t, rep, tc.wantRule, 1)
			if len(rep.Findings) != 1 {
				t.Fatalf("expected exactly 1 finding, got %d: %v", len(rep.Findings), ruleNames(rep))
			}

			f := firstOf(rep, tc.wantRule)
			if tc.wantCode != "" && f.CodePoint != tc.wantCode {
				t.Errorf("code point = %q, want %q", f.CodePoint, tc.wantCode)
			}
			if tc.wantLine != 0 && f.Line != tc.wantLine {
				t.Errorf("line = %d, want %d", f.Line, tc.wantLine)
			}
			if tc.wantCol != 0 && f.Column != tc.wantCol {
				t.Errorf("column = %d, want %d", f.Column, tc.wantCol)
			}
		})
	}
}

func TestCharsetAggregatesBulkRulesPerRecord(t *testing.T) {
	// Three non-ASCII characters in one record collapse into a single finding so
	// that a mixed-case file does not produce thousands of lines of output.
	input := "HDR|A|B\nDTL|BJÖRKÅÆ|C\nTRL|1\n"
	rep := Lint("test", []byte(input), Options{Disabled: []string{ClassFields, ClassTerminator}})

	if got := countRule(rep, RuleNonASCII); got != 1 {
		t.Fatalf("expected 1 aggregated non-ascii finding, got %d: %v", got, ruleNames(rep))
	}
	f := firstOf(rep, RuleNonASCII)
	if !strings.Contains(f.Message, "3 non-ASCII character(s)") {
		t.Errorf("message should report the total count, got %q", f.Message)
	}
}

func TestX12CharacterSetProfiles(t *testing.T) {
	// 835_charset.x12 carries lowercase text (extended set) and a backtick
	// (outside both sets).
	body := readFixture(t, "835_charset.x12")

	tests := []struct {
		name         string
		charset      CharsetProfile
		wantBasic    int
		wantExtended int
	}{
		{"the default allows lowercase", "", 0, 1},
		{"extended allows lowercase", CharsetExtended, 0, 1},
		{"basic flags lowercase and the backtick", CharsetBasic, 1, 1},
		{"off disables both rules", CharsetOff, 0, 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rep := Lint("test", body, Options{X12Charset: tc.charset})
			if got := countRule(rep, RuleX12Basic); got != tc.wantBasic {
				t.Errorf("%s count = %d, want %d", RuleX12Basic, got, tc.wantBasic)
			}
			if got := countRule(rep, RuleX12Extended); got != tc.wantExtended {
				t.Errorf("%s count = %d, want %d", RuleX12Extended, got, tc.wantExtended)
			}
		})
	}
}

func TestX12CharacterSetMembership(t *testing.T) {
	for _, r := range "ABZ019 !\"&'()*+,-./:;?=" {
		if !inX12Basic(r) {
			t.Errorf("%q should be in the X12 basic set", string(r))
		}
	}
	for _, r := range "abz%~@[]_{}\\|<>#$" {
		if inX12Basic(r) {
			t.Errorf("%q should not be in the X12 basic set", string(r))
		}
		if !inX12Extended(r) {
			t.Errorf("%q should be in the X12 extended set", string(r))
		}
	}
	// The caret and the backtick are the only printable ASCII outside both sets.
	for _, r := range "^`" {
		if inX12Extended(r) {
			t.Errorf("%q should be outside the X12 extended set", string(r))
		}
	}
}

func TestBOMSeverityDependsOnFormat(t *testing.T) {
	const bom = "\ufeff"

	tests := []struct {
		name  string
		input string
		opts  Options
		want  Severity
	}{
		{
			name: "x12",
			input: bom + "ISA*00*          *00*          *ZZ*NORTHGATEHEALTH*ZZ*VALEMEDGROUP   " +
				"*260115*1430*^*00501*000000001*0*P*:~\nIEA*0*000000001~\n",
			want: SeverityError,
		},
		{
			name:  "hl7v2",
			input: bom + "MSH|^~\\&|A|B|C|D|20260115143000||ADT^A01|M1|P|2.5.1\r",
			want:  SeverityError,
		},
		{
			name:  "fixed width",
			input: bom + "DTL0000144000\nDTL0000096500\n",
			opts: Options{Format: FormatFixed, Layout: &Layout{Fields: []LayoutField{
				{Name: "record_type", Width: 3},
				{Name: "amount", Width: 10},
			}}},
			want: SeverityError,
		},
		{
			name:  "delimited",
			input: bom + "member_id,last_name,plan\nNGH900000001,RIVERA,PPO-GOLD\n",
			want:  SeverityWarning,
		},
		{
			name:  "text",
			input: bom + "the quick brown fox\njumped over the lazy dog\n",
			want:  SeverityError,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rep := Lint("test", []byte(tc.input), tc.opts)
			f := firstOf(rep, RuleBOM)
			if f == nil {
				t.Fatalf("expected a BOM finding, got %v", ruleNames(rep))
			}
			if f.Severity != tc.want {
				t.Errorf("severity = %s, want %s (format %s)", f.Severity, tc.want, rep.Format)
			}
			if f.CodePoint != "U+FEFF" || f.Line != 1 || f.Column != 1 {
				t.Errorf("unexpected position: %+v", f)
			}
		})
	}
}

func TestBOMInDelimitedFileDoesNotFailWithAllowWarnings(t *testing.T) {
	// The point of grading the BOM by format: a spreadsheet-exported CSV should
	// still be shippable when the operator has accepted warnings.
	rep := Lint("test", []byte("\ufeffa,b,c\n1,2,3\n"), Options{})
	if !rep.OK(SeverityError) {
		t.Errorf("a BOM'd CSV should carry no errors, got %v", ruleNames(rep))
	}
}

func TestConfusablesTableIsWellFormed(t *testing.T) {
	// The table promises only code points that are visually indistinguishable
	// from ASCII. A false entry produces a false error in the rule the README
	// leads with, so its shape is checked rather than trusted.
	for r, ascii := range confusables {
		if r < 0x80 {
			t.Errorf("%U maps an ASCII rune; the table is for non-ASCII lookalikes", r)
		}
		if ascii >= 0x80 {
			t.Errorf("%U maps to non-ASCII %U; the target must be ASCII", r, ascii)
		}
		if ascii < 0x20 || ascii == 0x7F {
			t.Errorf("%U maps to control character %U", r, ascii)
		}
		if _, dup := invisible[r]; dup {
			t.Errorf("%U appears in both confusables and invisible", r)
		}
		if !inPlausibleBlock(r) {
			t.Errorf("%U (-> %q) is outside the Unicode blocks this table draws from; "+
				"add the block to inPlausibleBlock if the entry is intended", r, string(ascii))
		}
	}

	for r := range invisible {
		if r < 0x80 {
			t.Errorf("%U is ASCII and cannot be an invisible formatting character", r)
		}
	}
}

// inPlausibleBlock reports whether a confusable comes from a Unicode block that
// actually contains ASCII lookalikes. It is the guard that would have caught
// U+2045 LEFT SQUARE BRACKET WITH QUILL being mapped to "/".
func inPlausibleBlock(r rune) bool {
	switch {
	case r >= 0x00A0 && r <= 0x00FF: // Latin-1 Supplement
		return true
	case r >= 0x0100 && r <= 0x024F: // Latin Extended-A and B
		return true
	case r >= 0x0250 && r <= 0x02FF: // IPA Extensions, Spacing Modifiers
		return true
	case r >= 0x0370 && r <= 0x03FF: // Greek and Coptic
		return true
	case r >= 0x0400 && r <= 0x052F: // Cyrillic and Cyrillic Supplement
		return true
	case r >= 0x2000 && r <= 0x206F: // General Punctuation
		return true
	case r >= 0x2200 && r <= 0x22FF: // Mathematical Operators
		return true
	case r >= 0x3000 && r <= 0x303F: // CJK Symbols and Punctuation
		return true
	case r >= 0xA720 && r <= 0xA7FF: // Latin Extended-D
		return true
	case r >= 0xFE50 && r <= 0xFE6F: // Small Form Variants
		return true
	case r >= 0xFF01 && r <= 0xFF5E: // Halfwidth and Fullwidth Forms
		return true
	}
	return false
}

func TestConfusablesDoNotFlagOrdinaryPunctuation(t *testing.T) {
	// A regression guard for the class of defect S4 was: an entry that maps a
	// character no one would mistake for ASCII.
	notLookalikes := []rune{
		0x2045, // LEFT SQUARE BRACKET WITH QUILL
		0x2046, // RIGHT SQUARE BRACKET WITH QUILL
		0x00A9, // COPYRIGHT SIGN
		0x00AE, // REGISTERED SIGN
		0x20AC, // EURO SIGN
		0x2122, // TRADE MARK SIGN
		0x2026, // HORIZONTAL ELLIPSIS
	}
	for _, r := range notLookalikes {
		if ascii, ok := confusableASCII(r); ok {
			t.Errorf("%U should not be treated as a lookalike for %q", r, string(ascii))
		}
	}
}
