package edilint

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// canonical is a test convenience over Canonical that fails on error.
func canonical(t *testing.T, data []byte) []byte {
	t.Helper()
	out, err := Canonical(data, FormatAuto)
	if err != nil {
		t.Fatalf("Canonical: %v", err)
	}
	return out
}

func TestCanonicalIsIdempotent(t *testing.T) {
	// The round-trip property: formatting a formatted file changes nothing.
	fixtures := []string{
		"835_clean.x12",
		"835_envelope_broken.x12",
		"835_bad_datetime.x12",
		"835_duplicate_control.x12",
		"835_padding.x12",
		"835_charset.x12",
		"hl7v2_clean.hl7",
		"hl7v2_dirty.hl7",
		"hl7v2_batch_clean.hl7",
		"hl7v2_batch_broken.hl7",
	}
	for _, name := range fixtures {
		t.Run(name, func(t *testing.T) {
			once := canonical(t, readFixture(t, name))
			twice := canonical(t, once)
			if !bytes.Equal(once, twice) {
				t.Errorf("Canonical is not idempotent:\nonce:\n%s\ntwice:\n%s", once, twice)
			}
		})
	}

	inline := []struct {
		name  string
		input string
	}{
		{"x12 on one line", isa("000000001", "260115", "1430") +
			"GS*HP*A*B*20260115*1430*1*X*005010X221A1~ST*835*0001~SE*2*0001~GE*1*1~IEA*1*000000001~"},
		{"x12 with CRLF between segments", strings.ReplaceAll(
			interchange("ST*835*0001~", "SE*2*0001~"), "\n", "\r\n")},
		{"x12 with blank lines between segments", strings.ReplaceAll(
			interchange("ST*835*0001~", "SE*2*0001~"), "\n", "\n\n")},
		{"x12 with leading whitespace", "\n  " + interchange("ST*835*0001~", "SE*2*0001~")},
		{"x12 with an unterminated final segment",
			isa("000000001", "260115", "1430") + "\nIEA*0*000000001"},
		{"x12 with a UTF-8 byte order mark", "\xEF\xBB\xBF" + interchange("ST*835*0001~", "SE*2*0001~")},
		{"x12 with a line feed segment terminator",
			strings.Replace(isa("000000001", "260115", "1430"), "~", "\n", 1) +
				"GS*HP*A*B*20260115*1430*1*X*005010X221A1\nGE*1*1\nIEA*1*000000001\n"},
		{"hl7 with CR line endings", "MSH|^~\\&|A|B|C|D|20260115||ADT^A08|1|P|2.5.1\rPID|1\r"},
		{"hl7 with blank lines and no final newline",
			"MSH|^~\\&|A|B|C|D|20260115||ADT^A08|1|P|2.5.1\n\nPID|1"},
	}
	for _, tc := range inline {
		t.Run(tc.name, func(t *testing.T) {
			once := canonical(t, []byte(tc.input))
			twice := canonical(t, once)
			if !bytes.Equal(once, twice) {
				t.Errorf("Canonical is not idempotent:\nonce:\n%q\ntwice:\n%q", once, twice)
			}
		})
	}
}

func TestCanonicalLeavesACanonicalFileUnchanged(t *testing.T) {
	// The committed clean fixtures are already in canonical form, so they are
	// also the fixed point the property tests converge on.
	for _, name := range []string{"835_clean.x12", "hl7v2_batch_clean.hl7"} {
		t.Run(name, func(t *testing.T) {
			data := readFixture(t, name)
			if got := canonical(t, data); !bytes.Equal(got, data) {
				t.Errorf("canonical form differs from the fixture:\n%s", got)
			}
		})
	}
	t.Run("examples/remittance.x12", func(t *testing.T) {
		data, err := os.ReadFile(filepath.Join("examples", "remittance.x12"))
		if err != nil {
			t.Fatalf("read example: %v", err)
		}
		if got := canonical(t, data); !bytes.Equal(got, data) {
			t.Errorf("canonical form differs from the example:\n%s", got)
		}
	})
}

func TestCanonicalX12Layout(t *testing.T) {
	head := isa("000000001", "260115", "1430")
	gs := "GS*HP*A*B*20260115*1430*1*X*005010X221A1~"

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "segments on one line are split onto their own",
			input: head + gs + "GE*1*1~IEA*1*000000001~",
			want:  head + "\n" + gs + "\nGE*1*1~\nIEA*1*000000001~\n",
		},
		{
			name:  "CRLF and indentation between segments are normalized",
			input: head + "\r\n  " + gs + "\r\n  GE*1*1~\r\nIEA*1*000000001~\r\n",
			want:  head + "\n" + gs + "\nGE*1*1~\nIEA*1*000000001~\n",
		},
		{
			name:  "whitespace-only segments are dropped",
			input: head + "~ ~" + gs + "GE*1*1~IEA*1*000000001~",
			want:  head + "\n" + gs + "\nGE*1*1~\nIEA*1*000000001~\n",
		},
		{
			name:  "an unterminated final segment stays unterminated",
			input: head + "IEA*0*000000001",
			want:  head + "\nIEA*0*000000001\n",
		},
		{
			name:  "a byte order mark passes through",
			input: "\xEF\xBB\xBF" + head + "IEA*0*000000001~",
			want:  "\xEF\xBB\xBF" + head + "\nIEA*0*000000001~\n",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := canonical(t, []byte(tc.input))
			if string(got) != tc.want {
				t.Errorf("canonical form:\n%q\nwant:\n%q", got, tc.want)
			}
		})
	}
}

func TestCanonicalHL7Layout(t *testing.T) {
	msh := "MSH|^~\\&|A|B|C|D|20260115||ADT^A08|1|P|2.5.1"
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "CR segment terminators become LF",
			input: msh + "\rPID|1\r",
			want:  msh + "\nPID|1\n",
		},
		{
			name:  "blank lines are dropped and a final LF is added",
			input: msh + "\n\nPID|1",
			want:  msh + "\nPID|1\n",
		},
		{
			name:  "trailing spaces inside a segment are content and stay",
			input: msh + "\nPID|1|RIVERA  \n",
			want:  msh + "\nPID|1|RIVERA  \n",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := canonical(t, []byte(tc.input))
			if string(got) != tc.want {
				t.Errorf("canonical form:\n%q\nwant:\n%q", got, tc.want)
			}
		})
	}
}

// segmentTexts extracts the segment content of an X12 body, layout aside.
func segmentTexts(t *testing.T, data []byte) []string {
	t.Helper()
	s := newSource("test", data, FormatX12, Options{}, &Report{})
	if !s.Delims.Declared {
		t.Fatal("fixture must declare separators")
	}
	var out []string
	for _, r := range s.Records {
		if strings.TrimSpace(r.Text) != "" {
			out = append(out, r.Text)
		}
	}
	return out
}

func TestCanonicalPreservesSegmentContent(t *testing.T) {
	// Formatting is layout only: the segments, byte for byte, survive it.
	input := []byte(strings.ReplaceAll(readFixtureString(t, "835_envelope_broken.x12"), "\n", "\r\n  "))
	before := segmentTexts(t, input)
	after := segmentTexts(t, canonical(t, input))
	if len(before) != len(after) {
		t.Fatalf("segment count changed: %d before, %d after", len(before), len(after))
	}
	for i := range before {
		if before[i] != after[i] {
			t.Errorf("segment %d changed:\nbefore: %q\nafter:  %q", i+1, before[i], after[i])
		}
	}
}

func TestCanonicalErrors(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		format  Format
		wantErr string
	}{
		{"delimited input", "HDR|A|B\nDTL|C|D\n", FormatAuto, "fmt supports x12 and hl7v2"},
		{"plain text input", "hello\nworld\n", FormatAuto, "fmt supports x12 and hl7v2"},
		{"empty input", "", FormatAuto, "fmt supports x12 and hl7v2"},
		{"forced x12 without an ISA", "HDR|A|B\n", FormatX12, "no usable ISA segment"},
		{"forced x12 with a truncated ISA", "ISA*00*x", FormatX12, "no usable ISA segment"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Canonical([]byte(tc.input), tc.format)
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error = %q, want it to contain %q", err, tc.wantErr)
			}
		})
	}
}

func TestCanonicalOutputHasNoLayoutFindings(t *testing.T) {
	// A canonical file can still be defective, but never in its layout: the
	// mixed-terminator, padding and missing-final rules have nothing to say.
	input := []byte(strings.ReplaceAll(readFixtureString(t, "835_envelope_broken.x12"), "\n", "\r\n"))
	rep := Lint("test", canonical(t, input), Options{})
	for _, rule := range []string{RuleX12Padding, RuleMixedTerminator, RuleMissingFinal} {
		requireRule(t, rep, rule, 0)
	}
}

// readFixtureString is readFixture for tests that edit the fixture as text.
func readFixtureString(t *testing.T, name string) string {
	t.Helper()
	return string(readFixture(t, name))
}
