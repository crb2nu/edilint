package edilint

import (
	"strings"
	"testing"
)

func TestParseCountRule(t *testing.T) {
	tests := []struct {
		in      string
		want    CountRule
		wantErr string
	}{
		{in: "TRL:2:DTL", want: CountRule{Declaring: "TRL", Field: 2, Counted: "DTL"}},
		{in: "9:1:0", want: CountRule{Declaring: "9", Field: 1, Counted: "0"}},
		{in: "TRL:2", wantErr: "must have the form"},
		{in: "TRL:2:DTL:X", wantErr: "must have the form"},
		{in: "TRL:x:DTL", wantErr: "field index must be a positive integer"},
		{in: "TRL:0:DTL", wantErr: "field index must be a positive integer"},
		{in: "TRL:-1:DTL", wantErr: "field index must be a positive integer"},
		{in: ":2:DTL", wantErr: "empty record prefix"},
		{in: "TRL:2:", wantErr: "empty record prefix"},
	}

	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			got, err := ParseCountRule(tc.in)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("expected an error containing %q", tc.wantErr)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Errorf("error = %q, want it to contain %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %+v, want %+v", got, tc.want)
			}
			if got.String() != tc.in {
				t.Errorf("String() = %q, want %q", got.String(), tc.in)
			}
		})
	}
}

func TestCountRules(t *testing.T) {
	const clean = "HDR|NORTHGATE|20260115\n" +
		"DTL|NGH900000001|RIVERA|1440.00\n" +
		"DTL|NGH900000042|OKONKWO|2810.75\n" +
		"DTL|NGH900000108|BRENNAN|965.00\n" +
		"TRL|3\n"

	tests := []struct {
		name     string
		input    string
		rule     string
		wantRule string
		wantMsg  string
	}{
		{
			name:  "declared count matches",
			input: clean,
			rule:  "TRL:2:DTL",
		},
		{
			name:  "leading zeros are accepted",
			input: strings.Replace(clean, "TRL|3", "TRL|0003", 1),
			rule:  "TRL:2:DTL",
		},
		{
			name:     "declared count is too high",
			input:    strings.Replace(clean, "TRL|3", "TRL|4", 1),
			rule:     "TRL:2:DTL",
			wantRule: RuleCountMismatch,
			wantMsg:  `declares 4 "DTL" record(s) but the file contains 3`,
		},
		{
			name:     "declared count is too low",
			input:    strings.Replace(clean, "TRL|3", "TRL|2", 1),
			rule:     "TRL:2:DTL",
			wantRule: RuleCountMismatch,
			wantMsg:  "but the file contains 3",
		},
		{
			name:     "declared count is not a number",
			input:    strings.Replace(clean, "TRL|3", "TRL|THREE", 1),
			rule:     "TRL:2:DTL",
			wantRule: RuleCountUnparsable,
			wantMsg:  "not an integer",
		},
		{
			name:     "declaring record is too short",
			input:    strings.Replace(clean, "TRL|3", "TRL", 1),
			rule:     "TRL:2:DTL",
			wantRule: RuleCountShortRec,
			wantMsg:  "record has 1 field(s) but the rule reads field 2",
		},
		{
			name:     "no declaring record at all",
			input:    strings.Replace(clean, "TRL|3\n", "", 1),
			rule:     "TRL:2:DTL",
			wantRule: RuleCountNoDeclarer,
			wantMsg:  `no record starting with "TRL" was found`,
		},
		{
			name:     "counted prefix is absent",
			input:    clean,
			rule:     "TRL:2:XYZ",
			wantRule: RuleCountMismatch,
			wantMsg:  `declares 3 "XYZ" record(s) but the file contains 0`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rule, err := ParseCountRule(tc.rule)
			if err != nil {
				t.Fatalf("ParseCountRule: %v", err)
			}
			rep := Lint("test", []byte(tc.input), Options{
				CountRules: []CountRule{rule},
				Disabled:   []string{ClassCharset, ClassTerminator, ClassFields},
			})

			if tc.wantRule == "" {
				requireClean(t, rep)
				return
			}
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

func TestCountRulesOnFixtures(t *testing.T) {
	rule, err := ParseCountRule("TRL:2:DTL")
	if err != nil {
		t.Fatalf("ParseCountRule: %v", err)
	}
	opts := Options{CountRules: []CountRule{rule}}

	t.Run("clean extract", func(t *testing.T) {
		requireClean(t, lintFixture(t, "eligibility_clean.psv", opts))
	})

	t.Run("broken extract", func(t *testing.T) {
		rep := lintFixture(t, "eligibility_broken.psv", opts)
		requireRule(t, rep, RuleCountMismatch, 1)
		requireRule(t, rep, RuleFieldOutlier, 1)
	})
}

func TestCountRuleAppliesToX12Segments(t *testing.T) {
	// A count rule can recount X12 segments too: element 1 of SE declares the
	// segment total, which the envelope check also verifies independently.
	rule, err := ParseCountRule("CLP:1:CLP")
	if err != nil {
		t.Fatalf("ParseCountRule: %v", err)
	}
	rep := lintFixture(t, "835_clean.x12", Options{CountRules: []CountRule{rule}})
	// CLP element 1 is a patient account number, not a count, so this must be
	// reported as unparsable rather than silently passing.
	requireRule(t, rep, RuleCountUnparsable, 2)
}

func TestMultipleCountRules(t *testing.T) {
	input := "HDR|1\nDTL|a\nDTL|b\nSUM|2\nTRL|2\n"
	r1, _ := ParseCountRule("TRL:2:DTL")
	r2, _ := ParseCountRule("SUM:2:DTL")
	rep := Lint("test", []byte(input), Options{
		CountRules: []CountRule{r1, r2},
		Disabled:   []string{ClassCharset, ClassTerminator, ClassFields},
	})
	requireClean(t, rep)
}
