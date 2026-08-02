package edilint

import "testing"

func TestFieldCountConsistency(t *testing.T) {
	tests := []struct {
		name  string
		input string
		opts  Options
		want  int
	}{
		{
			name: "uniform field counts per record type",
			input: "HDR|A|B\n" +
				"DTL|1|2|3\nDTL|4|5|6\nDTL|7|8|9\n" +
				"TRL|3\n",
		},
		{
			name: "record types may differ from each other",
			input: "HDR|A|B|C|D|E\n" +
				"DTL|1|2\nDTL|3|4\nDTL|5|6\n" +
				"TRL|3\n",
		},
		{
			name: "one short detail record",
			input: "HDR|A|B\n" +
				"DTL|1|2|3\nDTL|4|5\nDTL|7|8|9\n" +
				"TRL|3\n",
			want: 1,
		},
		{
			name: "one long detail record",
			input: "HDR|A|B\n" +
				"DTL|1|2|3\nDTL|4|5|6|7\nDTL|7|8|9\n" +
				"TRL|3\n",
			want: 1,
		},
		{
			name: "two outliers",
			input: "HDR|A|B\n" +
				"DTL|1|2|3\nDTL|4|5\nDTL|7|8|9\nDTL|1\nDTL|2|3|4\n" +
				"TRL|5\n",
			want: 2,
		},
		{
			name:  "fewer than three records of a type is not enough signal",
			input: "DTL|1|2|3\nDTL|4|5\nHDR|A\n",
		},
		{
			name:  "discriminator can be another field",
			input: "1|DTL|a|b\n2|DTL|c|d\n3|DTL|e\n4|HDR|z|z\n",
			opts:  Options{TypeField: 2},
			want:  1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			opts := tc.opts
			opts.Disabled = append(opts.Disabled, ClassCharset, ClassTerminator)
			rep := Lint("test", []byte(tc.input), opts)
			requireRule(t, rep, RuleFieldOutlier, tc.want)
		})
	}
}

func TestFieldOutlierReportsExpectedAndActual(t *testing.T) {
	input := "DTL|1|2|3\nDTL|4|5\nDTL|7|8|9\n"
	rep := Lint("test", []byte(input), Options{Disabled: []string{ClassCharset, ClassTerminator}})

	f := firstOf(rep, RuleFieldOutlier)
	if f == nil {
		t.Fatalf("expected a field-count finding, got %v", ruleNames(rep))
	}
	if f.Expected != "4" || f.Actual != "3" {
		t.Errorf("expected/actual = %q/%q, want 4/3", f.Expected, f.Actual)
	}
	if f.Line != 2 {
		t.Errorf("line = %d, want 2", f.Line)
	}
}

func TestHL7v2FieldOutliersAreWarnings(t *testing.T) {
	// HL7v2 permits omitting trailing fields, so a varying count is only a smell.
	msg := "MSH|^~\\&|A|B|C|D|20260115143000||ADT^A01|M1|P|2.5.1\r" +
		"OBX|1|NM|GLU||99|mg/dL\r" +
		"OBX|2|NM|NA||140|mmol/L\r" +
		"OBX|3|NM|K||4.1\r"
	rep := Lint("test", []byte(msg), Options{})

	f := firstOf(rep, RuleFieldOutlier)
	if f == nil {
		t.Fatalf("expected a field-count finding, got %v", ruleNames(rep))
	}
	if f.Severity != SeverityWarning {
		t.Errorf("severity = %s, want %s", f.Severity, SeverityWarning)
	}
}

func TestX12IsExemptFromFieldCountChecks(t *testing.T) {
	// Omitting trailing elements is routine in X12, so it must not be reported.
	rep := lintFixture(t, "835_clean.x12", Options{})
	requireRule(t, rep, RuleFieldOutlier, 0)
}
