package edilint

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
)

func TestEdifactCleanFixtureIsClean(t *testing.T) {
	rep := lintFixture(t, "edifact_clean.edi", Options{})
	if rep.Format != FormatEdifact {
		t.Fatalf("format = %s, want %s", rep.Format, FormatEdifact)
	}
	requireClean(t, rep)
}

func TestEdifactBrokenFixture(t *testing.T) {
	// The fixture plants: a UNT-1 that over-declares, a UNT-2 that does not
	// match its UNH-1, a UNZ whose reference and count are both wrong, and a
	// segment after UNZ.
	rep := lintFixture(t, "edifact_broken.edi", Options{})

	wants := map[string]int{
		RuleEdifactSegmentCount:     1,
		RuleEdifactControlRef:       2,
		RuleEdifactInterchangeCount: 1,
		RuleEdifactTrailing:         1,
	}
	for rule, want := range wants {
		if n := countRule(rep, rule); n != want {
			t.Errorf("%s occurs %d time(s), want %d: %v", rule, n, want, ruleNames(rep))
		}
	}
	if rep.Summary.Total != 5 {
		t.Errorf("total = %d, want the 5 planted defects: %v", rep.Summary.Total, ruleNames(rep))
	}
}

func TestEdifactPairing(t *testing.T) {
	tests := []struct {
		name string
		body string
		rule string
	}{
		{"UNB never closed", "UNB+UNOA:4+A+B+260115:1430+R1'", RuleEdifactUnclosed},
		{"UNH never closed", "UNB+UNOA:4+A+B+260115:1430+R1'UNH+M1+ORDERS:D:96A:UN'UNZ+1+R1'",
			RuleEdifactUnclosed},
		{"UNT without UNH", "UNB+UNOA:4+A+B+260115:1430+R1'UNT+2+M1'UNZ+0+R1'", RuleEdifactUnopened},
		{"UNZ without UNB", "UNB+UNOA:4+A+B+260115:1430+R1'UNZ+0+R1'UNZ+0+R1'", RuleEdifactUnopened},
		{"UNH outside an interchange", "UNB+UNOA:4+A+B+260115:1430+R1'UNZ+0+R1'UNH+M1+ORDERS:D:96A:UN'",
			RuleEdifactNesting},
		{"UNE without UNG", "UNB+UNOA:4+A+B+260115:1430+R1'UNE+1+G1'UNZ+0+R1'", RuleEdifactUnopened},
		{"empty UNB-5", "UNB+UNOA:4+A+B+260115:1430'UNZ+0+'", RuleEdifactControlRef},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rep := Lint("pair.edi", []byte(tc.body), Options{})
			if rep.Format != FormatEdifact {
				t.Fatalf("format = %s, want %s", rep.Format, FormatEdifact)
			}
			if firstOf(rep, tc.rule) == nil {
				t.Errorf("expected %s, got %v", tc.rule, ruleNames(rep))
			}
		})
	}
}

func TestEdifactEmptyGroupAndMessageReference(t *testing.T) {
	type at struct {
		record string
		line   int
	}
	tests := []struct {
		name string
		body string
		want []at
	}{
		{"empty UNG-5 and UNE-2",
			"UNB+UNOA:4+A+B+260115:1430+R1'\nUNG+ORDERS+A+B+260115:1430++UN+D:96A'\n" +
				"UNH+M1+ORDERS:D:96A:UN'\nUNT+2+M1'\nUNE+1+'\nUNZ+1+R1'\n",
			[]at{{"UNG", 2}}},
		{"empty UNH-1 and UNT-2",
			"UNB+UNOA:4+A+B+260115:1430+R1'\nUNH++ORDERS:D:96A:UN'\nUNT+2+'\nUNZ+1+R1'\n",
			[]at{{"UNH", 2}}},
		{"whitespace-only UNG-5 and UNE-2",
			"UNB+UNOA:4+A+B+260115:1430+R1'\nUNG+ORDERS+A+B+260115:1430+  +UN+D:96A'\n" +
				"UNH+M1+ORDERS:D:96A:UN'\nUNT+2+M1'\nUNE+1+  '\nUNZ+1+R1'\n",
			[]at{{"UNG", 2}}},
		{"whitespace-only UNH-1 and UNT-2",
			"UNB+UNOA:4+A+B+260115:1430+R1'\nUNH+ +ORDERS:D:96A:UN'\nUNT+2+ '\nUNZ+1+R1'\n",
			[]at{{"UNH", 2}}},
		{"empty UNH-1 with non-empty UNT-2",
			"UNB+UNOA:4+A+B+260115:1430+R1'\nUNH++ORDERS:D:96A:UN'\nUNT+2+M1'\nUNZ+1+R1'\n",
			[]at{{"UNH", 2}, {"UNT", 3}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			data := []byte(tc.body)
			mem := Lint("stream", data, Options{})
			streamed, err := LintReader("stream", bytes.NewReader(data), Options{})
			if err != nil {
				t.Fatalf("LintReader: %v", err)
			}
			for api, rep := range map[string]*Report{"Lint": mem, "LintReader": streamed} {
				if rep.Format != FormatEdifact {
					t.Fatalf("%s: format = %s, want %s", api, rep.Format, FormatEdifact)
				}
				if rep.Summary.Total != len(tc.want) {
					t.Errorf("%s: total = %d, want %d: %v", api, rep.Summary.Total, len(tc.want), ruleNames(rep))
				}
				var got []at
				for _, f := range rep.Findings {
					if f.Rule != RuleEdifactControlRef || f.Severity != SeverityError {
						continue
					}
					got = append(got, at{f.Record, f.Line})
				}
				if !reflect.DeepEqual(got, tc.want) {
					t.Errorf("%s: %s errors at %v, want %v", api, RuleEdifactControlRef, got, tc.want)
				}
			}
			compareStreamReport(t, data, Options{}, streamed)
		})
	}
}

func TestEdifactMixedGroupsAndMessages(t *testing.T) {
	type at struct {
		record string
		line   int
	}
	const (
		unb   = "UNB+UNOA:4+A+B+260115:1430+R1'\n"
		group = "UNG+ORDERS+A+B+260115:1430+G1+UN+D:96A'\nUNH+M1+ORDERS:D:96A:UN'\nUNT+2+M1'\nUNE+1+G1'\n"
		bare  = "UNH+M2+ORDERS:D:96A:UN'\nUNT+2+M2'\n"
		unz   = "UNZ+1+R1'\n"
	)
	tests := []struct {
		name string
		body string
		want []at
	}{
		{"group then bare message", unb + group + bare + unz, []at{{"UNH", 6}}},
		{"bare message then group", unb + bare + group + unz, []at{{"UNG", 4}}},
		{"two mixed interchanges",
			unb + group + bare + bare + unz + unb + bare + group + group + unz,
			[]at{{"UNH", 6}, {"UNG", 14}}},
		{"grouped only", unb + group + group + "UNZ+2+R1'\n", nil},
		{"ungrouped only", unb + bare + bare + "UNZ+2+R1'\n", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			data := []byte(tc.body)
			mem := Lint("stream", data, Options{})
			streamed, err := LintReader("stream", bytes.NewReader(data), Options{})
			if err != nil {
				t.Fatalf("LintReader: %v", err)
			}
			for api, rep := range map[string]*Report{"Lint": mem, "LintReader": streamed} {
				if rep.Format != FormatEdifact {
					t.Fatalf("%s: format = %s, want %s", api, rep.Format, FormatEdifact)
				}
				var got []at
				for _, f := range rep.Findings {
					if f.Rule != RuleEdifactNesting {
						continue
					}
					if f.Severity != SeverityError || !strings.Contains(f.Message, "not both") {
						t.Errorf("%s: unexpected finding %+v", api, f)
					}
					got = append(got, at{f.Record, f.Line})
				}
				if !reflect.DeepEqual(got, tc.want) {
					t.Errorf("%s: %s errors at %v, want %v", api, RuleEdifactNesting, got, tc.want)
				}
			}
			compareStreamReport(t, data, Options{}, streamed)
		})
	}
}

func TestEdifactFunctionalGroups(t *testing.T) {
	clean := "UNB+UNOA:4+A+B+260115:1430+R1'" +
		"UNG+ORDERS+A+B+260115:1430+G1+UN+D:96A'" +
		"UNH+M1+ORDERS:D:96A:UN'UNT+2+M1'" +
		"UNE+1+G1'UNZ+1+R1'"
	rep := Lint("group.edi", []byte(clean), Options{})
	requireClean(t, rep)

	// With groups in use, UNZ-1 counts groups, not messages; the group trailer
	// recounts its messages and matches its reference.
	broken := strings.NewReplacer("UNE+1+G1", "UNE+2+G9").Replace(clean)
	rep = Lint("group.edi", []byte(broken), Options{})
	if firstOf(rep, RuleEdifactGroupCount) == nil {
		t.Errorf("expected %s, got %v", RuleEdifactGroupCount, ruleNames(rep))
	}
	if firstOf(rep, RuleEdifactControlRef) == nil {
		t.Errorf("expected %s for UNG-5/UNE-2, got %v", RuleEdifactControlRef, ruleNames(rep))
	}
}

func TestEdifactUNADeclaredSeparators(t *testing.T) {
	// A UNA that moves the segment terminator to "~" governs the whole file.
	body := "UNA:+.? ~UNB+UNOA:4+A+B+260115:1430+R1~UNH+M1+ORDERS:D:96A:UN~UNT+2+M1~UNZ+1+R1~"
	rep := Lint("una.edi", []byte(body), Options{})
	requireClean(t, rep)
}

func TestEdifactReleaseCharacter(t *testing.T) {
	// "?'" inside an element is a released terminator, not the end of the
	// segment, and "?+" is a released element separator, not a split point.
	body := "UNA:+.? '" +
		"UNB+UNOA:4+A+B+260115:1430+R?'1'" +
		"UNH+M1+ORDERS:D:96A:UN'" +
		"FTX+ACB++allowed?+priced'" +
		"UNT+3+M1'" +
		"UNZ+1+R?'1'"
	rep := Lint("release.edi", []byte(body), Options{})
	requireClean(t, rep)
}

func TestEdifactServiceStringDefects(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{"truncated UNA", "UNA:+.?", "fixed at nine"},
		{"colliding separators", "UNA::.? 'UNB+UNOA:4+A+B+260115:1430+R1'UNZ+0+R1'",
			"same character"},
		{"alphanumeric separator", "UNA:a.? 'UNB+UNOA:4+A+B+260115:1430+R1'UNZ+0+R1'",
			"alphanumeric"},
		{"bad decimal mark", "UNA:+;? 'UNB+UNOA:4+A+B+260115:1430+R1'UNZ+0+R1'",
			"decimal mark"},
		{"UNA after the interchange started", "UNB+UNOA:4+A+B+260115:1430+R1'UNA:+.? 'UNZ+0+R1'",
			"very first thing"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rep := Lint("una.edi", []byte(tc.body), Options{})
			f := firstOf(rep, RuleEdifactServiceString)
			if f == nil {
				t.Fatalf("expected %s, got %v", RuleEdifactServiceString, ruleNames(rep))
			}
			if !strings.Contains(f.Message, tc.want) {
				t.Errorf("message %q does not mention %q", f.Message, tc.want)
			}
		})
	}
}

func TestEdifactTruncatedFinalSegment(t *testing.T) {
	body := "UNB+UNOA:4+A+B+260115:1430+R1'UNH+M1+ORDERS:D:96A:UN'UNT+2+M1'UNZ+1+R1"
	rep := Lint("truncated.edi", []byte(body), Options{})
	f := firstOf(rep, RuleEdifactSegment)
	if f == nil {
		t.Fatalf("expected %s for the unterminated UNZ, got %v", RuleEdifactSegment, ruleNames(rep))
	}
	if f.Record != "UNZ" {
		t.Errorf("record = %q, want UNZ", f.Record)
	}
}

func TestEdifactForcedFormatWithoutUNB(t *testing.T) {
	// Format forced to edifact with no envelope in sight degrades to lines, so
	// the character and terminator checks still run, and says why.
	rep := Lint("not.edi", []byte("HDR|A|B\nDTL|1|2\n"), Options{Format: FormatEdifact})
	f := firstOf(rep, RuleEdifactNesting)
	if f == nil {
		t.Fatalf("expected %s, got %v", RuleEdifactNesting, ruleNames(rep))
	}
	if !strings.Contains(f.Message, "no UNB segment") {
		t.Errorf("message %q should say no UNB was found", f.Message)
	}
}

func TestEdifactDetection(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  Format
	}{
		{"UNA service string", "UNA:+.? 'UNB+UNOA:4+A+B+260115:1430+R1'", FormatEdifact},
		{"bare UNB", "UNB+UNOA:4+A+B+260115:1430+R1'", FormatEdifact},
		{"a delimited file whose first field starts with UNA", "UNAVAILABLE|1|2\nUNASSIGNED|3|4\nUNAUDITED|5|6\n", FormatDelimited},
		{"a delimited file whose first field starts with UNB", "UNBALANCED,1,2\nUNBOUNDED,3,4\nUNBRANDED,5,6\n", FormatDelimited},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := Detect([]byte(tc.input), Options{}); got != tc.want {
				t.Errorf("Detect = %s, want %s", got, tc.want)
			}
		})
	}
}
