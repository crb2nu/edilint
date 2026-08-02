package edilint

import (
	"bytes"
	"runtime"
	"strings"
	"testing"
)

// These tests drive the exported API directly with values a caller can
// construct but the CLI never produces. The rest of the suite reaches the
// engine through fixtures or through the command line, both of which validate
// their inputs first, so defects reachable only from the library are invisible
// to it.

func TestLintSurvivesHostileOptions(t *testing.T) {
	body := []byte("DTL0000144000\nDTL0000096500\n")

	tests := []struct {
		name string
		opts Options
	}{
		{
			name: "layout with no fields",
			opts: Options{Format: FormatFixed, Layout: &Layout{Name: "empty"}},
		},
		{
			name: "layout with an explicitly nil field slice",
			opts: Options{Format: FormatFixed, Layout: &Layout{Fields: nil}},
		},
		{
			name: "layout whose widths sum to zero",
			opts: Options{Format: FormatFixed, Layout: &Layout{
				Fields: []LayoutField{{Name: "a", Width: 0}},
			}},
		},
		{
			name: "layout with a negative width",
			opts: Options{Format: FormatFixed, Layout: &Layout{
				Fields: []LayoutField{{Name: "a", Width: -5}},
			}},
		},
		{
			name: "layout field with no name",
			opts: Options{Format: FormatFixed, Layout: &Layout{
				Fields: []LayoutField{{Width: 4}},
			}},
		},
		{
			name: "layout with an unknown pad side",
			opts: Options{Format: FormatFixed, Layout: &Layout{
				Fields: []LayoutField{{Name: "a", Width: 4, Pad: "middle"}},
			}},
		},
		{
			name: "layout supplied without the fixed format",
			opts: Options{Layout: &Layout{Name: "empty"}},
		},
		{
			name: "unparsable delimiter",
			opts: Options{Format: FormatDelimited, Delimiter: "||"},
		},
		{
			name: "unparsable delimiter on x12",
			opts: Options{Format: FormatX12, Delimiter: "not-a-char"},
		},
		{
			name: "negative type field",
			opts: Options{TypeField: -3},
		},
		{
			name: "count rule with a zero field index",
			opts: Options{CountRules: []CountRule{{Declaring: "DTL", Field: 0, Counted: "DTL"}}},
		},
		{
			name: "count rule with a negative field index",
			opts: Options{CountRules: []CountRule{{Declaring: "DTL", Field: -1, Counted: "DTL"}}},
		},
		{
			name: "count rule with empty record types",
			opts: Options{CountRules: []CountRule{{Field: 1}}},
		},
		{
			name: "unknown format string",
			opts: Options{Format: Format("edifact")},
		},
		{
			name: "unknown charset profile",
			opts: Options{X12Charset: CharsetProfile("strict")},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// The contract is that Lint always returns a report. A panic here is
			// the defect, whatever the findings turn out to be.
			rep := Lint("hostile.txt", body, tc.opts)
			if rep == nil {
				t.Fatal("Lint returned nil")
			}
			if rep.Findings == nil {
				t.Error("Findings must never be nil, so it marshals as [] rather than null")
			}
		})
	}
}

func TestLintReportsAnUnusableLayoutRatherThanPanicking(t *testing.T) {
	// This is the exact call in the review: a caller-built Layout with no fields.
	rep := Lint("x.txt", []byte("DTL0001\n"), Options{
		Format: FormatFixed,
		Layout: &Layout{Name: "empty"},
	})

	f := firstOf(rep, RuleLayoutLength)
	if f == nil {
		t.Fatalf("expected the unusable layout to be reported, got %v", ruleNames(rep))
	}
	if !strings.Contains(f.Message, "layout is unusable") {
		t.Errorf("message should explain the layout was rejected, got %q", f.Message)
	}
	if !strings.Contains(f.Message, "no fields") {
		t.Errorf("message should carry the validation error, got %q", f.Message)
	}
}

func TestLintReportsAnUnusableDelimiter(t *testing.T) {
	// Silently substituting different analysis options is the one thing a linter
	// must not do: the caller's "this file is clean" would answer a different
	// question from the one they asked.
	tests := []struct {
		name   string
		format Format
	}{
		{"delimited", FormatDelimited},
		{"x12", FormatX12},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			body := readFixture(t, "eligibility_clean.psv")
			if tc.format == FormatX12 {
				body = readFixture(t, "835_clean.x12")
			}
			rep := Lint("test", body, Options{Format: tc.format, Delimiter: "||"})

			var found *Finding
			for i := range rep.Findings {
				if strings.Contains(rep.Findings[i].Message, "delimiter option") {
					found = &rep.Findings[i]
					break
				}
			}
			if found == nil {
				t.Fatalf("an unusable delimiter must be reported, got %v", ruleNames(rep))
			}
			if found.Severity != SeverityError {
				t.Errorf("severity = %s, want %s", found.Severity, SeverityError)
			}
		})
	}
}

func TestLintOnEmptyAndTinyInputs(t *testing.T) {
	inputs := map[string][]byte{
		"nil":               nil,
		"empty":             {},
		"single newline":    []byte("\n"),
		"bare CR":           []byte("\r"),
		"lone BOM":          {0xEF, 0xBB, 0xBF},
		"one byte":          []byte("A"),
		"truncated ISA":     []byte("ISA*"),
		"ISA prefix only":   []byte("ISA"),
		"MSH prefix only":   []byte("MSH"),
		"invalid utf8 only": {0xFF},
		"nul only":          {0x00},
	}

	for name, body := range inputs {
		t.Run(name, func(t *testing.T) {
			for _, format := range []Format{
				FormatAuto, FormatX12, FormatHL7v2, FormatDelimited, FormatText,
			} {
				rep := Lint("tiny", body, Options{Format: format})
				if rep == nil {
					t.Fatalf("format %s: Lint returned nil", format)
				}
				if rep.Findings == nil {
					t.Errorf("format %s: Findings must not be nil", format)
				}
			}
		})
	}
}

func TestBinaryInputIsReportedOnceAndBounded(t *testing.T) {
	// 8 MB of pseudo-random bytes. Before the short-circuit this produced one
	// finding per offending byte and gigabytes of resident memory.
	body := pseudoRandom(8 << 20)

	rep := Lint("binary.dat", body, Options{})

	if got := len(rep.Findings); got != 1 {
		t.Fatalf("binary input produced %d findings, want exactly 1", got)
	}
	f := rep.Findings[0]
	if f.Rule != RuleInvalidUTF8 {
		t.Errorf("rule = %s, want %s", f.Rule, RuleInvalidUTF8)
	}
	if !strings.Contains(f.Message, "does not look like text") {
		t.Errorf("message should name the real problem, got %q", f.Message)
	}
	if rep.Summary.Total != 1 {
		t.Errorf("summary total = %d, want 1", rep.Summary.Total)
	}
}

func TestFindingRetentionIsBoundedOnDefectDenseInput(t *testing.T) {
	// Valid UTF-8, so the binary short-circuit does not apply, but every seventh
	// byte is a control character. This is the path that must be bounded by the
	// retention ceiling rather than by the input size.
	body := bytes.Repeat([]byte("DTL|A\x0b\n"), 200000)

	rep := Lint("dense.psv", body, Options{})

	if len(rep.Findings) > MaxRetainedFindings {
		t.Errorf("retained %d findings, want at most %d", len(rep.Findings), MaxRetainedFindings)
	}
	if rep.Summary.Total <= MaxRetainedFindings {
		t.Fatalf("summary total = %d; the fixture should produce far more than the ceiling",
			rep.Summary.Total)
	}
	if !rep.Summary.Truncated {
		t.Error("a report that dropped findings must be marked truncated")
	}
	// The counters, not the retained slice, are what the exit status reads.
	if rep.OK(SeverityWarning) || rep.OK(SeverityError) {
		t.Error("a report full of errors must not report as OK")
	}
	if rep.Summary.Errors == 0 {
		t.Error("summary must count every error, including the ones not retained")
	}
}

func TestMaxFindingsRaisesTheRetentionCeiling(t *testing.T) {
	body := bytes.Repeat([]byte("DTL|A\x0b\n"), 30000)

	// Asking for more than the default ceiling is honored: the caller opted in.
	rep := Lint("dense.psv", body, Options{MaxFindings: MaxRetainedFindings + 5000})
	if len(rep.Findings) <= MaxRetainedFindings {
		t.Errorf("retained %d findings, want more than the default ceiling of %d",
			len(rep.Findings), MaxRetainedFindings)
	}
}

func TestLintHeapIsProportionateToInput(t *testing.T) {
	// A guard on the blocker itself: 8 MB of hostile input used to peak above
	// 3 GB. The bound is deliberately loose so that allocator behavior does not
	// make this flaky; it is three orders of magnitude below the regression.
	const inputSize = 8 << 20
	const heapBudget = 256 << 20

	body := pseudoRandom(inputSize)

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)

	rep := Lint("binary.dat", body, Options{})

	runtime.ReadMemStats(&after)
	// Keep the report alive across the measurement.
	if rep == nil {
		t.Fatal("Lint returned nil")
	}

	allocated := after.TotalAlloc - before.TotalAlloc
	if allocated > heapBudget {
		t.Errorf("linting %d MB allocated %d MB, want under %d MB",
			inputSize>>20, allocated>>20, heapBudget>>20)
	}
	t.Logf("allocated %d KB for a %d MB hostile input", allocated>>10, inputSize>>20)
}

// pseudoRandom returns n deterministic bytes that are dense in invalid UTF-8.
// A fixed generator keeps the memory tests reproducible.
func pseudoRandom(n int) []byte {
	out := make([]byte, n)
	state := uint32(0x9e3779b9)
	for i := range out {
		state = state*1664525 + 1013904223
		out[i] = byte(state >> 24)
	}
	return out
}
