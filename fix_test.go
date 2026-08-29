package edilint

import (
	"bytes"
	"strings"
	"testing"
)

// TestFixFixturePairs is the acceptance contract for every repair: the before
// fixture carries the target rule's findings and nothing else, Fix produces
// exactly the after fixture, the after fixture is completely clean, and
// running Fix on it again changes nothing.
func TestFixFixturePairs(t *testing.T) {
	tests := []struct {
		rule   string
		before string
		after  string
		unsafe bool
	}{
		{RuleBOM, "fix_bom_before.x12", "fix_bom_after.x12", false},
		{RuleMixedTerminator, "fix_mixed_terminators_before.psv", "fix_mixed_terminators_after.psv", false},
		{RuleMissingFinal, "fix_missing_final_before.psv", "fix_missing_final_after.psv", false},
		{RuleX12Segment, "fix_x12_unterminated_before.x12", "fix_x12_unterminated_after.x12", false},
		{RuleX12Padding, "fix_x12_padding_before.x12", "fix_x12_padding_after.x12", false},
		{RuleSegmentCount, "fix_se01_before.x12", "fix_se01_after.x12", false},
		{RuleGroupCount, "fix_ge01_before.x12", "fix_ge01_after.x12", false},
		{RuleInterchangeCount, "fix_iea01_before.x12", "fix_iea01_after.x12", false},
		{RuleDateTime, "fix_time_before.x12", "fix_time_after.x12", false},
		{RuleBatchMessageCount, "fix_bts_before.hl7", "fix_bts_after.hl7", false},
		{RuleBatchFileCount, "fix_fts_before.hl7", "fix_fts_after.hl7", false},
		{RuleHomoglyph, "fix_homoglyph_before.x12", "fix_homoglyph_after.x12", true},
	}

	for _, tc := range tests {
		t.Run(tc.rule, func(t *testing.T) {
			before := readFixture(t, tc.before)
			after := readFixture(t, tc.after)

			// The before fixture exists to demonstrate this one rule.
			rep := Lint(tc.before, before, Options{})
			if countRule(rep, tc.rule) == 0 {
				t.Fatalf("before fixture lacks a %s finding: %v", tc.rule, ruleNames(rep))
			}
			for _, f := range rep.Findings {
				if f.Rule != tc.rule {
					t.Fatalf("before fixture has an unrelated %s finding; the pair must isolate %s",
						f.Rule, tc.rule)
				}
			}

			opts := FixOptions{Unsafe: tc.unsafe}
			out, repairs := Fix(before, opts)
			if !bytes.Equal(out, after) {
				t.Errorf("Fix output differs from the after fixture:\ngot:\n%q\nwant:\n%q", out, after)
			}
			if len(repairs) == 0 {
				t.Fatal("expected at least one repair")
			}
			for _, r := range repairs {
				if r.Rule != tc.rule {
					t.Errorf("repair clears %s, want only %s", r.Rule, tc.rule)
				}
				if r.ID != RuleID(tc.rule) {
					t.Errorf("repair ID = %q, want %q", r.ID, RuleID(tc.rule))
				}
				if r.Unsafe != tc.unsafe {
					t.Errorf("repair Unsafe = %v, want %v", r.Unsafe, tc.unsafe)
				}
			}

			// The repair clears its rule and introduces nothing.
			requireClean(t, Lint(tc.after, after, Options{}))

			// Fixing a fixed file is a no-op.
			again, more := Fix(after, opts)
			if len(more) != 0 {
				t.Errorf("Fix on the after fixture found %d repair(s), want 0", len(more))
			}
			if !bytes.Equal(again, after) {
				t.Error("Fix on the after fixture changed it")
			}
		})
	}
}

func TestFixRecountEdgeCases(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "a non-numeric declared count is rewritten like a wrong one",
			input: interchange("ST*835*0001~", "BPR*I*100.00*C*ACH*CCP~", "SE*THREE*0001~"),
			want:  interchange("ST*835*0001~", "BPR*I*100.00*C*ACH*CCP~", "SE*3*0001~"),
		},
		{
			name:  "an empty X12 count is mandatory and is filled in",
			input: interchange("ST*835*0001~", "BPR*I*100.00*C*ACH*CCP~", "SE**0001~"),
			want:  interchange("ST*835*0001~", "BPR*I*100.00*C*ACH*CCP~", "SE*3*0001~"),
		},
		{
			name:  "a count that is numerically right but zero-padded is left alone",
			input: interchange("ST*835*0001~", "BPR*I*100.00*C*ACH*CCP~", "SE*03*0001~"),
			want:  interchange("ST*835*0001~", "BPR*I*100.00*C*ACH*CCP~", "SE*03*0001~"),
		},
		{
			name: "an SE without an open ST is not recounted",
			input: isa("000000001", "260115", "1430") + "\n" +
				"GS*HP*A*B*20260115*1430*1*X*005010X221A1~\n" +
				"SE*9*0001~\nGE*0*1~\nIEA*1*000000001~\n",
			want: isa("000000001", "260115", "1430") + "\n" +
				"GS*HP*A*B*20260115*1430*1*X*005010X221A1~\n" +
				"SE*9*0001~\nGE*0*1~\nIEA*1*000000001~\n",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out, _ := Fix([]byte(tc.input), FixOptions{})
			if string(out) != tc.want {
				t.Errorf("Fix output:\n%q\nwant:\n%q", out, tc.want)
			}
		})
	}
}

func TestFixHL7EmptyCountStaysEmpty(t *testing.T) {
	// BTS-1 and FTS-1 are optional. An empty declaration declares nothing, so
	// there is no finding and nothing to repair.
	input := "FHS|^~\\&|A|B|C|D|20260115143000\n" +
		"BHS|^~\\&|A|B|C|D|20260115143000\n" +
		"MSH|^~\\&|A|B|C|D|20260115143000||ADT^A08|1|P|2.5.1\n" +
		"BTS||note\nFTS||note\n"
	out, repairs := Fix([]byte(input), FixOptions{})
	if len(repairs) != 0 {
		t.Fatalf("expected no repairs, got %d: %+v", len(repairs), repairs)
	}
	if string(out) != input {
		t.Error("Fix changed a file with nothing to repair")
	}
}

func TestFixTimePadding(t *testing.T) {
	tests := []struct {
		name string
		gs05 string
		want string
	}{
		{"three digits gain the dropped hour zero", "930", "0930"},
		{"five digits gain it ahead of HHMMSS", "14301", "014301"},
		{"seven digits gain it ahead of HHMMSSDD", "1430159", "01430159"},
		{"an invalid padded value is left for a person", "965", "965"},
		{"a value two digits short is ambiguous and left alone", "30", "30"},
		{"a non-digit value is left alone", "93O", "93O"},
		{"a valid time is untouched", "1430", "1430"},
		{"an out-of-range four-digit time cannot be padded", "2560", "2560"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			body := func(gs05 string) string {
				return isa("000000001", "260115", "1430") + "\n" +
					"GS*HP*A*B*20260115*" + gs05 + "*1*X*005010X221A1~\n" +
					"ST*835*0001~\nSE*2*0001~\nGE*1*1~\nIEA*1*000000001~\n"
			}
			out, _ := Fix([]byte(body(tc.gs05)), FixOptions{})
			if string(out) != body(tc.want) {
				t.Errorf("GS05 %q: output GS segment differs:\n%q", tc.gs05, out)
			}
		})
	}
}

func TestFixTerminatorStyles(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "strays are rewritten to the dominant CRLF",
			input: "HDR|A|B\r\nDTL|C|D\nDTL|E|F\r\nTRL|2\r\n",
			want:  "HDR|A|B\r\nDTL|C|D\r\nDTL|E|F\r\nTRL|2\r\n",
		},
		{
			name:  "the appended final terminator matches the file's style",
			input: "HDR|A|B\r\nDTL|C|D\r\nTRL|1",
			want:  "HDR|A|B\r\nDTL|C|D\r\nTRL|1\r\n",
		},
		{
			name:  "a file with no terminator at all gains an LF",
			input: "HDR|A|B",
			want:  "HDR|A|B\n",
		},
		{
			name:  "a terminated consistent file is untouched",
			input: "HDR|A|B\nDTL|C|D\nTRL|1\n",
			want:  "HDR|A|B\nDTL|C|D\nTRL|1\n",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out, _ := Fix([]byte(tc.input), FixOptions{})
			if string(out) != tc.want {
				t.Errorf("Fix output %q, want %q", out, tc.want)
			}
		})
	}
}

func TestFixLeavesUnreadableInputAlone(t *testing.T) {
	binary := make([]byte, 1024)
	state := uint32(0x9e3779b9)
	for i := range binary {
		state = state*1664525 + 1013904223
		binary[i] = byte(state >> 24)
	}

	tests := []struct {
		name  string
		input []byte
	}{
		{"UTF-16LE byte order mark", append([]byte{0xFF, 0xFE}, "H\x00D\x00R\x00"...)},
		{"UTF-16BE byte order mark", append([]byte{0xFE, 0xFF}, "\x00H\x00D\x00R"...)},
		{"binary input", binary},
		{"empty input", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out, repairs := Fix(tc.input, FixOptions{Unsafe: true})
			if len(repairs) != 0 {
				t.Errorf("expected no repairs, got %+v", repairs)
			}
			if !bytes.Equal(out, tc.input) {
				t.Error("Fix changed input it cannot read")
			}
		})
	}
}

func TestFixEdifactIsOutOfScope(t *testing.T) {
	// EDIFACT repairs are not implemented, so a defective EDIFACT file comes
	// back untouched rather than half-repaired.
	data := readFixture(t, "edifact_broken.edi")
	out, repairs := Fix(data, FixOptions{})
	if len(repairs) != 0 {
		t.Fatalf("expected no repairs, got %+v", repairs)
	}
	if !bytes.Equal(out, data) {
		t.Error("Fix changed an EDIFACT file")
	}
}

func TestFixHomoglyphsRequireUnsafe(t *testing.T) {
	before := readFixture(t, "fix_homoglyph_before.x12")

	out, repairs := Fix(before, FixOptions{})
	if len(repairs) != 0 {
		t.Fatalf("the safe tier must not substitute homoglyphs, got %+v", repairs)
	}
	if !bytes.Equal(out, before) {
		t.Error("the safe tier changed the file")
	}
}

func TestFixHomoglyphSkipsStructuralTargets(t *testing.T) {
	t.Run("a lookalike of the X12 segment terminator", func(t *testing.T) {
		// U+FF5E imitates the tilde. With "~" as the segment terminator,
		// substituting it would split one segment into two.
		input := interchange("ST*835*0001~", "N1*PE*VALE～MEDICAL~", "SE*3*0001~")
		out, repairs := Fix([]byte(input), FixOptions{Unsafe: true})
		if string(out) != input {
			t.Errorf("the substitution must be skipped, got:\n%q", out)
		}
		for _, r := range repairs {
			if r.Rule == RuleHomoglyph {
				t.Errorf("unexpected homoglyph repair: %+v", r)
			}
		}
	})

	t.Run("a lookalike of the HL7 field separator", func(t *testing.T) {
		// U+FF5C imitates the pipe, which separates every HL7 field.
		input := "MSH|^~\\&|A|B|C|D|20260115||ADT^A08|1|P|2.5.1\nPID|1|X｜Y\n"
		out, repairs := Fix([]byte(input), FixOptions{Unsafe: true})
		if string(out) != input {
			t.Errorf("the substitution must be skipped, got:\n%q", out)
		}
		if len(repairs) != 0 {
			t.Errorf("expected no repairs, got %+v", repairs)
		}
	})

	t.Run("a non-structural lookalike is still substituted", func(t *testing.T) {
		input := "HDR|A|B\nDTL|RIVERА|D\nTRL|1\n"
		want := "HDR|A|B\nDTL|RIVERA|D\nTRL|1\n"
		out, repairs := Fix([]byte(input), FixOptions{Unsafe: true})
		if string(out) != want {
			t.Errorf("Fix output %q, want %q", out, want)
		}
		if len(repairs) != 1 || !repairs[0].Unsafe || repairs[0].Rule != RuleHomoglyph {
			t.Errorf("repairs = %+v, want one unsafe homoglyph repair", repairs)
		}
	})
}

func TestFixRepairsCarryPositionsAndMessages(t *testing.T) {
	before := readFixture(t, "fix_se01_before.x12")
	_, repairs := Fix(before, FixOptions{})
	if len(repairs) != 1 {
		t.Fatalf("repairs = %d, want 1", len(repairs))
	}
	r := repairs[0]
	if r.Line != 5 {
		t.Errorf("Line = %d, want 5", r.Line)
	}
	if !strings.Contains(r.Message, `rewrite SE01 from "9" to "3"`) {
		t.Errorf("Message = %q", r.Message)
	}
}

func TestFixIsIdempotentOnDefectiveFixtures(t *testing.T) {
	// Even on files whose defects Fix cannot fully repair, applying Fix twice
	// is the same as applying it once.
	fixtures := []string{
		"835_envelope_broken.x12",
		"835_bad_datetime.x12",
		"835_duplicate_control.x12",
		"835_padding.x12",
		"835_charset.x12",
		"hl7v2_dirty.hl7",
		"hl7v2_batch_broken.hl7",
		"eligibility_broken.psv",
		"bom_mixed.csv",
	}
	for _, name := range fixtures {
		t.Run(name, func(t *testing.T) {
			once, _ := Fix(readFixture(t, name), FixOptions{Unsafe: true})
			twice, repairs := Fix(once, FixOptions{Unsafe: true})
			if !bytes.Equal(once, twice) {
				t.Errorf("Fix is not idempotent:\nonce:\n%q\ntwice:\n%q", once, twice)
			}
			if len(repairs) != 0 {
				t.Errorf("second Fix still found %d repair(s): %+v", len(repairs), repairs)
			}
		})
	}
}
