package edilint

import (
	"strings"
	"testing"
)

func TestLineEndingConsistency(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantMixed int
		wantFinal int
	}{
		{
			name:  "uniform LF",
			input: "HDR|A|B\nDTL|C|D\nTRL|1|X\n",
		},
		{
			name:  "uniform CRLF",
			input: "HDR|A|B\r\nDTL|C|D\r\nTRL|1|X\r\n",
		},
		{
			name:  "uniform CR",
			input: "HDR|A|B\rDTL|C|D\rTRL|1|X\r",
		},
		{
			name:      "one LF among CRLF",
			input:     "HDR|A|B\r\nDTL|C|D\nTRL|1|X\r\n",
			wantMixed: 1,
		},
		{
			name:      "one CRLF among LF",
			input:     "HDR|A|B\nDTL|C|D\r\nTRL|1|X\n",
			wantMixed: 1,
		},
		{
			name:      "two strays",
			input:     "HDR|A|B\nDTL|C|D\r\nTRL|1|X\rEND|9|Z\n",
			wantMixed: 2,
		},
		{
			name:      "missing terminator on the last record",
			input:     "HDR|A|B\nDTL|C|D\nTRL|1|X",
			wantFinal: 1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rep := Lint("test", []byte(tc.input), Options{Disabled: []string{ClassFields, ClassCharset}})
			requireRule(t, rep, RuleMixedTerminator, tc.wantMixed)
			requireRule(t, rep, RuleMissingFinal, tc.wantFinal)
		})
	}
}

func TestMixedTerminatorNamesTheDominantStyle(t *testing.T) {
	rep := Lint("test", []byte("A|1\r\nB|2\r\nC|3\nD|4\r\n"),
		Options{Disabled: []string{ClassFields, ClassCharset}})

	f := firstOf(rep, RuleMixedTerminator)
	if f == nil {
		t.Fatal("expected a mixed terminator finding")
	}
	if f.Expected != "CRLF" || f.Actual != "LF" {
		t.Errorf("expected/actual = %q/%q, want CRLF/LF", f.Expected, f.Actual)
	}
	if f.Line != 3 {
		t.Errorf("line = %d, want 3", f.Line)
	}
	if !strings.Contains(f.Message, "CRLF x3, LF x1") {
		t.Errorf("message should carry the histogram, got %q", f.Message)
	}
}

func TestX12SeparatorDeclaration(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantMsg string
	}{
		{
			name: "element separator repeated as the segment terminator",
			// Segment terminator at ISA position 105 is "*", the element separator.
			input:   "ISA*00*          *00*          *ZZ*NORTHGATEHEALTH*ZZ*VALEMEDGROUP   *260115*1430*^*00501*000000001*0*P*:*",
			wantMsg: "cannot be tokenized unambiguously",
		},
		{
			name:    "alphanumeric sub-element separator",
			input:   "ISA*00*          *00*          *ZZ*NORTHGATEHEALTH*ZZ*VALEMEDGROUP   *260115*1430*^*00501*000000001*0*P*Z~\nIEA*0*000000001~\n",
			wantMsg: "an alphanumeric character",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rep := Lint("test", []byte(tc.input), Options{})
			f := firstOf(rep, RuleX12Separator)
			if f == nil {
				t.Fatalf("expected a %s finding, got %v", RuleX12Separator, ruleNames(rep))
			}
			if !strings.Contains(f.Message, tc.wantMsg) {
				t.Errorf("message = %q\nwant it to contain %q", f.Message, tc.wantMsg)
			}
		})
	}
}

func TestX12InterSegmentPadding(t *testing.T) {
	t.Run("uniform padding is clean", func(t *testing.T) {
		rep := lintFixture(t, "835_clean.x12", Options{})
		requireRule(t, rep, RuleX12Padding, 0)
	})

	t.Run("one run-on segment", func(t *testing.T) {
		rep := lintFixture(t, "835_padding.x12", Options{})
		requireRule(t, rep, RuleX12Padding, 1)
		f := firstOf(rep, RuleX12Padding)
		if f.Expected != "LF" || f.Actual != "nothing" {
			t.Errorf("expected/actual = %q/%q, want LF/nothing", f.Expected, f.Actual)
		}
		if f.Record != "ST" {
			t.Errorf("segment = %q, want ST", f.Record)
		}
	})
}

func TestX12TruncatedFinalSegment(t *testing.T) {
	// The interchange stops mid-segment with no closing terminator.
	input := "ISA*00*          *00*          *ZZ*NORTHGATEHEALTH*ZZ*VALEMEDGROUP   *260115*1430*^*00501*000000001*0*P*:~\n" +
		"GS*HP*A*B*20260115*1430*1*X*005010X221A1~\nST*835*0001"
	rep := Lint("test", []byte(input), Options{})
	requireRule(t, rep, RuleX12Segment, 1)
}

func TestRenderWS(t *testing.T) {
	cases := map[string]string{
		"":       "nothing",
		"\r\n":   "CRLF",
		"\n":     "LF",
		"\r":     "CR",
		"\n\n":   "LFLF",
		" \t":    "SPTAB",
		"\r\n\n": "CRLFLF",
	}
	for in, want := range cases {
		if got := renderWS(in); got != want {
			t.Errorf("renderWS(%q) = %q, want %q", in, got, want)
		}
	}
}
