package edilint

import (
	"bytes"
	"math"
	"testing"
)

// Keep mutation runs bounded; larger records and files have separate benchmarks.
const maxFuzzInput = 64 << 10

func seedParser(f *testing.F, fixtures ...string) {
	f.Helper()
	for _, data := range [][]byte{nil, {}, []byte("\r\n"), {0, 0xff, 0xfe}, []byte("\xef\xbb\xbfA\rB\nC\r\n")} {
		f.Add(data)
	}
	for _, name := range fixtures {
		f.Add(readFixture(f, name))
	}
}

func FuzzX12(f *testing.F) {
	seedParser(f, "835_clean.x12", "835_envelope_broken.x12", "837p_claims_multi_st.x12", "835_padding.x12")
	f.Fuzz(func(t *testing.T, data []byte) {
		checkParserReport(t, data, Options{Format: FormatX12})
	})
}

func FuzzHL7Batch(f *testing.F) {
	seedParser(f, "hl7v2_clean.hl7", "hl7v2_dirty.hl7", "hl7v2_batch_clean.hl7", "hl7v2_batch_broken.hl7")
	f.Fuzz(func(t *testing.T, data []byte) {
		checkParserReport(t, data, Options{Format: FormatHL7v2})
	})
}

func FuzzEdifact(f *testing.F) {
	seedParser(f, "edifact_clean.edi", "edifact_broken.edi")
	f.Add([]byte("UNA:+.? 'UNB+UNOC:3+S+R+260101:1200+1'UNH+1+ORDERS:D:96A:UN'FTX+AAI+++escaped?+plus??question?'quote'UNT+3+1'UNZ+1+1'"))
	f.Fuzz(func(t *testing.T, data []byte) {
		checkParserReport(t, data, Options{Format: FormatEdifact})
	})
}

func FuzzDelimited(f *testing.F) {
	for _, name := range []string{"eligibility_clean.psv", "eligibility_broken.psv", "bom_mixed.csv"} {
		f.Add(readFixture(f, name), "", 1)
	}
	f.Add([]byte("DTL|1\nTRL|1\n"), "|", math.MaxInt)
	f.Add([]byte{0, 0xff}, "\x00", -1)
	f.Fuzz(func(t *testing.T, data []byte, delimiter string, typeField int) {
		if len(delimiter) > 4 {
			t.Skip()
		}
		checkParserReport(t, data, Options{
			Format: FormatDelimited, Delimiter: delimiter, TypeField: typeField,
			CountRules: []CountRule{{Declaring: "TRL", Field: 2, Counted: "DTL"}},
		})
	})
}

func FuzzFixedWidth(f *testing.F) {
	for _, widths := range [][2]int{{3, 5}, {0, 5}, {-1, 5}, {3, math.MaxInt}, {3, math.MaxInt - 3}, {math.MaxInt, 1}} {
		f.Add([]byte("DTL00001\nTRL00001\n"), widths[0], widths[1])
	}
	f.Fuzz(func(t *testing.T, data []byte, firstWidth, secondWidth int) {
		checkParserReport(t, data, Options{
			Format: FormatFixed,
			Layout: &Layout{Fields: []LayoutField{
				{Name: "type", Width: firstWidth},
				{Name: "count", Width: secondWidth, Pad: PadLeft, PadChar: "0"},
			}},
			CountRules: []CountRule{{Declaring: "TRL", Field: 2, Counted: "DTL"}},
		})
	})
}

func FuzzDetectAndLint(f *testing.F) {
	seedParser(f, "835_clean.x12", "hl7v2_batch_clean.hl7", "edifact_clean.edi", "eligibility_clean.psv", "remit_clean.txt")
	f.Fuzz(func(t *testing.T, data []byte) {
		checkParserReport(t, data, Options{})
	})
}

func checkParserReport(t *testing.T, data []byte, opts Options) {
	t.Helper()
	if len(data) > maxFuzzInput {
		t.Skip()
	}
	original := bytes.Clone(data)
	opts.MaxFindings = 32
	rep := Lint("fuzz", data, opts)
	if !bytes.Equal(data, original) {
		t.Fatal("Lint changed its input")
	}
	s := rep.Summary
	if s.Total != s.Errors+s.Warnings+s.Infos || s.Total < len(rep.Findings) {
		t.Fatalf("inconsistent summary: %+v (%d retained)", s, len(rep.Findings))
	}
	total := 0
	for rule, count := range s.ByRule {
		if RuleID(rule) == "" || count < 0 {
			t.Fatalf("invalid rule tally: %q = %d", rule, count)
		}
		total += count
	}
	if total != s.Total || len(rep.Findings) > opts.MaxFindings || s.Truncated != (s.Total > len(rep.Findings)) {
		t.Fatalf("inconsistent retention or rule totals: %+v (%d retained)", s, len(rep.Findings))
	}
	for _, finding := range rep.Findings {
		if finding.ID == "" || finding.ID != RuleID(finding.Rule) || finding.Line < 0 || finding.Column < 0 || finding.RecordNumber < 0 {
			t.Fatalf("invalid finding: %+v", finding)
		}
	}
	if opts.Format == FormatX12 || opts.Format == FormatHL7v2 {
		canonical, err := Canonical(data, opts.Format)
		if err != nil {
			return // Malformed X12 may not declare usable separators.
		}
		again, err := Canonical(canonical, opts.Format)
		if err != nil || !bytes.Equal(canonical, again) {
			t.Fatalf("canonical form is not idempotent: %v", err)
		}
	}
}
