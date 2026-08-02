package edilint

import (
	"strings"
	"testing"
)

// isa builds a structurally valid 106-character ISA with the given control
// number, date and time so envelope tests can vary one element at a time.
func isa(control, date, time string) string {
	return "ISA*00*          *00*          *ZZ*NORTHGATEHEALTH*ZZ*VALEMEDGROUP   *" +
		date + "*" + time + "*^*00501*" + control + "*0*P*:~"
}

// interchange wraps segments in a matching ISA/GS/IEA/GE envelope.
func interchange(segments ...string) string {
	head := isa("000000001", "260115", "1430") + "\n" +
		"GS*HP*NORTHGATEHEALTH*VALEMEDGROUP*20260115*1430*1*X*005010X221A1~\n"
	tail := "GE*1*1~\nIEA*1*000000001~\n"
	return head + strings.Join(segments, "\n") + "\n" + tail
}

func TestX12CleanEnvelope(t *testing.T) {
	rep := lintFixture(t, "835_clean.x12", Options{})
	if rep.Format != FormatX12 {
		t.Fatalf("format = %s, want %s", rep.Format, FormatX12)
	}
	requireClean(t, rep)
}

func TestX12EnvelopeIntegrity(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantRule string
		wantMsg  string
	}{
		{
			name:     "SE01 disagrees with the recounted segments",
			input:    interchange("ST*835*0001~", "BPR*I*100.00*C*ACH*CCP~", "SE*9*0001~"),
			wantRule: RuleSegmentCount,
			wantMsg:  "declares 9 segment(s) from ST through SE inclusive but the file contains 3",
		},
		{
			name:     "SE02 does not match ST02",
			input:    interchange("ST*835*0001~", "BPR*I*100.00*C*ACH*CCP~", "SE*3*0002~"),
			wantRule: RuleControlNumber,
			wantMsg:  `ST02 is "0001" but the matching SE02 is "0002"`,
		},
		{
			name: "leading-zero drift is only a warning",
			input: interchange("ST*835*0001~", "BPR*I*100.00*C*ACH*CCP~", "SE*3*1~") +
				"",
			wantRule: RuleControlNumber,
			wantMsg:  "numerically equal but not identical",
		},
		{
			name:     "ST without SE",
			input:    interchange("ST*835*0001~", "BPR*I*100.00*C*ACH*CCP~"),
			wantRule: RuleUnclosed,
			wantMsg:  "ST is never closed by a matching SE",
		},
		{
			name: "SE without ST",
			input: isa("000000001", "260115", "1430") + "\n" +
				"GS*HP*NORTHGATEHEALTH*VALEMEDGROUP*20260115*1430*1*X*005010X221A1~\n" +
				"SE*2*0001~\nGE*0*1~\nIEA*1*000000001~\n",
			wantRule: RuleUnopened,
			wantMsg:  "SE appears without a preceding ST",
		},
		{
			name: "ST outside a functional group",
			input: isa("000000001", "260115", "1430") + "\n" +
				"ST*835*0001~\nSE*2*0001~\nIEA*0*000000001~\n",
			wantRule: RuleEnvelopeNesting,
			wantMsg:  "ST appears outside a GS functional group",
		},
		{
			name: "GE01 disagrees with the transaction sets present",
			input: isa("000000001", "260115", "1430") + "\n" +
				"GS*HP*NORTHGATEHEALTH*VALEMEDGROUP*20260115*1430*1*X*005010X221A1~\n" +
				"ST*835*0001~\nSE*2*0001~\nGE*4*1~\nIEA*1*000000001~\n",
			wantRule: RuleGroupCount,
			wantMsg:  "GE01 declares 4 transaction set(s) (ST) but the file contains 1",
		},
		{
			name: "IEA01 disagrees with the functional groups present",
			input: isa("000000001", "260115", "1430") + "\n" +
				"GS*HP*NORTHGATEHEALTH*VALEMEDGROUP*20260115*1430*1*X*005010X221A1~\n" +
				"ST*835*0001~\nSE*2*0001~\nGE*1*1~\nIEA*3*000000001~\n",
			wantRule: RuleInterchangeCount,
			wantMsg:  "IEA01 declares 3 functional group(s) (GS) but the file contains 1",
		},
		{
			name: "IEA02 does not match ISA13",
			input: isa("000000001", "260115", "1430") + "\n" +
				"GS*HP*NORTHGATEHEALTH*VALEMEDGROUP*20260115*1430*1*X*005010X221A1~\n" +
				"ST*835*0001~\nSE*2*0001~\nGE*1*1~\nIEA*1*000000042~\n",
			wantRule: RuleControlNumber,
			wantMsg:  `ISA13 is "000000001" but the matching IEA02 is "000000042"`,
		},
		{
			name: "segments after the interchange closes",
			input: isa("000000001", "260115", "1430") + "\n" +
				"GS*HP*NORTHGATEHEALTH*VALEMEDGROUP*20260115*1430*1*X*005010X221A1~\n" +
				"ST*835*0001~\nSE*2*0001~\nGE*1*1~\nIEA*1*000000001~\nBPR*I*100.00*C*ACH*CCP~\n",
			wantRule: RuleEnvelopeTrailing,
			wantMsg:  "appears outside any interchange",
		},
		{
			name:     "ISA shorter than 106 characters",
			input:    "ISA*00*  *00*  *ZZ*SENDER*ZZ*RECEIVER*260115*1430*^*00501*000000001*0*P*:~\nIEA*0*000000001~\n",
			wantRule: RuleISALength,
			wantMsg:  "X12 fixes it at 106",
		},
		{
			name:  "non-numeric trailer count",
			input: interchange("ST*835*0001~", "BPR*I*100.00*C*ACH*CCP~", "SE*THREE*0001~"),

			wantRule: RuleSegmentCount,
			wantMsg:  "not a number",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rep := Lint("test", []byte(tc.input), Options{})
			f := firstOf(rep, tc.wantRule)
			if f == nil {
				t.Fatalf("expected a %s finding, got %v", tc.wantRule, ruleNames(rep))
			}
			if !strings.Contains(f.Message, tc.wantMsg) {
				t.Errorf("message = %q\nwant it to contain %q", f.Message, tc.wantMsg)
			}
		})
	}
}

func TestX12EnvelopeFixtures(t *testing.T) {
	rep := lintFixture(t, "835_envelope_broken.x12", Options{})
	requireRule(t, rep, RuleSegmentCount, 1)
	requireRule(t, rep, RuleGroupCount, 1)
	requireRule(t, rep, RuleControlNumber, 2) // SE02 vs ST02 and IEA02 vs ISA13
}

func TestX12DuplicateControlNumbers(t *testing.T) {
	t.Run("within a file", func(t *testing.T) {
		rep := lintFixture(t, "835_duplicate_control.x12", Options{})
		// One repeated ST02 inside a group, one repeated ISA13 in the file.
		requireRule(t, rep, RuleDupControl, 2)
	})

	t.Run("across files in one run", func(t *testing.T) {
		body := readFixture(t, "835_clean.x12")
		seen := map[string]string{}
		opts := Options{SeenISA13: seen}

		first := Lint("monday.x12", body, opts)
		requireClean(t, first)

		second := Lint("tuesday.x12", body, opts)
		f := firstOf(second, RuleDupControl)
		if f == nil {
			t.Fatalf("expected a duplicate ISA13 across files, got %v", ruleNames(second))
		}
		if !strings.Contains(f.Message, "monday.x12") {
			t.Errorf("message should name the earlier file, got %q", f.Message)
		}
	})

	t.Run("duplicate GS06 within an interchange", func(t *testing.T) {
		input := isa("000000001", "260115", "1430") + "\n" +
			"GS*HP*A*B*20260115*1430*7*X*005010X221A1~\nST*835*0001~\nSE*2*0001~\nGE*1*7~\n" +
			"GS*HP*A*B*20260115*1431*7*X*005010X221A1~\nST*835*0002~\nSE*2*0002~\nGE*1*7~\n" +
			"IEA*2*000000001~\n"
		rep := Lint("test", []byte(input), Options{})
		requireRule(t, rep, RuleDupControl, 1)
	})
}

func TestX12EnvelopeDateTime(t *testing.T) {
	tests := []struct {
		name    string
		date    string
		time    string
		wantMsg string
	}{
		{"valid", "260115", "1430", ""},
		{"leap day in a leap year", "240229", "0000", ""},
		{"last minute of the day", "260115", "2359", ""},
		{"month 13", "261301", "1430", "month 13 is out of range"},
		{"month 00", "260001", "1430", "month 00 is out of range"},
		{"day 32", "260132", "1430", "day 32 is out of range"},
		{"february 30", "260230", "1430", "day 30 is out of range for month 02"},
		{"non-leap february 29", "260229", "1430", "day 29 is out of range for month 02"},
		{"hour 24", "260115", "2400", "hour 24 is out of range"},
		{"minute 60", "260115", "1460", "minute 60 is out of range"},
		{"letters in the date", "26AB15", "1430", "expected 6 digits"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			input := isa("000000001", tc.date, tc.time) + "\nIEA*0*000000001~\n"
			rep := Lint("test", []byte(input), Options{})

			f := firstOf(rep, RuleDateTime)
			if tc.wantMsg == "" {
				if f != nil {
					t.Fatalf("expected no datetime finding, got %q", f.Message)
				}
				return
			}
			if f == nil {
				t.Fatalf("expected a datetime finding, got %v", ruleNames(rep))
			}
			if !strings.Contains(f.Message, tc.wantMsg) {
				t.Errorf("message = %q\nwant it to contain %q", f.Message, tc.wantMsg)
			}
		})
	}
}

func TestX12EnvelopeDateTimeFixture(t *testing.T) {
	rep := lintFixture(t, "835_bad_datetime.x12", Options{})
	// ISA09, ISA10, GS04 and GS05 are each invalid.
	requireRule(t, rep, RuleDateTime, 4)
}

func TestX12GS05AcceptsSeconds(t *testing.T) {
	input := isa("000000001", "260115", "1430") + "\n" +
		"GS*HP*A*B*20260115*143012*1*X*005010X221A1~\nST*835*0001~\nSE*2*0001~\nGE*1*1~\n" +
		"IEA*1*000000001~\n"
	rep := Lint("test", []byte(input), Options{})
	if f := firstOf(rep, RuleDateTime); f != nil {
		t.Fatalf("HHMMSS should be accepted, got %q", f.Message)
	}
}

func TestDeriveDelimsReadsISAPositions(t *testing.T) {
	body := readFixture(t, "835_clean.x12")
	d, ok := deriveDelims(body, 0)
	if !ok {
		t.Fatal("expected separators to be derivable")
	}
	if d.Element != '*' || d.SubElement != ':' || d.Segment != '~' || d.Repetition != '^' {
		t.Errorf("delims = %q %q %q %q, want * : ~ ^",
			d.Element, d.SubElement, d.Segment, d.Repetition)
	}
	if d.ISALen != 106 {
		t.Errorf("ISA length = %d, want 106", d.ISALen)
	}
	if d.Version != "00501" {
		t.Errorf("version = %q, want 00501", d.Version)
	}
}

func TestControlNumberScopesDoNotCollide(t *testing.T) {
	// Generators routinely reuse the same number for ISA13, GS06 and ST02.
	// Those live in different scopes and must not be reported as duplicates.
	input := "ISA*00*          *00*          *ZZ*NORTHGATEHEALTH*ZZ*VALEMEDGROUP   " +
		"*260115*1430*^*00501*000000001*0*P*:~\n" +
		"GS*HP*A*B*20260115*1430*000000001*X*005010X221A1~\n" +
		"ST*835*000000001~\nBPR*I*100.00*C*ACH*CCP~\nSE*3*000000001~\n" +
		"GE*1*000000001~\nIEA*1*000000001~\n"

	rep := Lint("test", []byte(input), Options{})
	requireRule(t, rep, RuleDupControl, 0)
}

func TestGS06ResetsPerInterchange(t *testing.T) {
	// The same GS06 in two different interchanges is legitimate.
	body := func(isaCtl string) string {
		return "ISA*00*          *00*          *ZZ*NORTHGATEHEALTH*ZZ*VALEMEDGROUP   " +
			"*260115*1430*^*00501*" + isaCtl + "*0*P*:~\n" +
			"GS*HP*A*B*20260115*1430*1*X*005010X221A1~\n" +
			"ST*835*0001~\nSE*2*0001~\nGE*1*1~\nIEA*1*" + isaCtl + "~\n"
	}
	rep := Lint("test", []byte(body("000000001")+body("000000002")), Options{})
	requireRule(t, rep, RuleDupControl, 0)
}
