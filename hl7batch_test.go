package edilint

import (
	"strings"
	"testing"
)

func TestHL7BatchCleanFixtureIsClean(t *testing.T) {
	rep := lintFixture(t, "hl7v2_batch_clean.hl7", Options{})
	if rep.Format != FormatHL7v2 {
		t.Fatalf("format = %s, want %s", rep.Format, FormatHL7v2)
	}
	requireClean(t, rep)
}

func TestHL7BatchBrokenFixture(t *testing.T) {
	// The fixture plants exactly one instance of each defect: a header whose
	// encoding characters disagree, a BTS-1 that over-declares, a message
	// stranded outside any batch, and an FTS-1 that over-declares.
	rep := lintFixture(t, "hl7v2_batch_broken.hl7", Options{})

	for _, rule := range []string{
		RuleBatchSeparator, RuleBatchMessageCount, RuleBatchStrayMessage, RuleBatchFileCount,
	} {
		if n := countRule(rep, rule); n != 1 {
			t.Errorf("%s occurs %d time(s), want 1: %v", rule, n, ruleNames(rep))
		}
	}
	if rep.Summary.Total != 4 {
		t.Errorf("total = %d, want the 4 planted defects: %v", rep.Summary.Total, ruleNames(rep))
	}

	// The count mismatch quotes what it recounted.
	if f := firstOf(rep, RuleBatchMessageCount); f != nil {
		if f.Expected != "3" || f.Actual != "2" {
			t.Errorf("message count expected/actual = %q/%q, want 3/2", f.Expected, f.Actual)
		}
	}
}

func TestHL7BatchPairing(t *testing.T) {
	// Envelope pairing defects, each built in memory from a minimal file.
	msh := "MSH|^~\\&|A|B|C|D|20260115143000||ADT^A08|M1|P|2.5.1\n"
	tests := []struct {
		name string
		body string
		rule string
	}{
		{"BTS without BHS", msh + "BTS|1\n", RuleBatchUnopened},
		{"FTS without FHS", msh + "FTS|0\n", RuleBatchUnopened},
		{"BHS never closed", "BHS|^~\\&|A|B|C|D\n" + msh, RuleBatchUnclosed},
		{"FHS never closed", "FHS|^~\\&|A|B|C|D\nBHS|^~\\&|A|B|C|D\n" + msh + "BTS|1\n", RuleBatchUnclosed},
		{"second BHS while one is open", "BHS|^~\\&|A|B|C|D\n" + msh + "BHS|^~\\&|A|B|C|D\n" + msh +
			"BTS|1\n", RuleBatchUnclosed},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rep := Lint("batch.hl7", []byte(tc.body), Options{})
			if firstOf(rep, tc.rule) == nil {
				t.Errorf("expected %s, got %v", tc.rule, ruleNames(rep))
			}
		})
	}
}

func TestHL7BareMessagesAreNotABatch(t *testing.T) {
	// A stream of plain messages is valid without any batch envelope: no
	// pairing findings, no stray-message findings.
	msh := "MSH|^~\\&|A|B|C|D|20260115143000||ADT^A08|M%d|P|2.5.1\n"
	body := strings.Replace(msh, "M%d", "M1", 1) + strings.Replace(msh, "M%d", "M2", 1)
	rep := Lint("bare.hl7", []byte(body), Options{})
	for _, f := range rep.Findings {
		if strings.HasPrefix(f.Rule, ClassHL7Batch+".") {
			t.Errorf("a bare message stream must produce no batch findings, got %s", f.Rule)
		}
	}

	rep = lintFixture(t, "hl7v2_clean.hl7", Options{})
	for _, f := range rep.Findings {
		if strings.HasPrefix(f.Rule, ClassHL7Batch+".") {
			t.Errorf("the single-message fixture must produce no batch findings, got %s", f.Rule)
		}
	}
}

func TestHL7BatchCountFields(t *testing.T) {
	batch := func(bts string) string {
		return "BHS|^~\\&|A|B|C|D\n" +
			"MSH|^~\\&|A|B|C|D|20260115143000||ADT^A08|M1|P|2.5.1\n" +
			bts + "\n"
	}
	tests := []struct {
		name string
		body string
		rule string
		want bool
	}{
		{"empty BTS-1 is optional and verifies nothing", batch("BTS|"), RuleBatchMessageCount, false},
		{"missing BTS-1 verifies nothing", batch("BTS"), RuleBatchMessageCount, false},
		{"matching BTS-1 is clean", batch("BTS|1"), RuleBatchMessageCount, false},
		{"wrong BTS-1 is reported", batch("BTS|9"), RuleBatchMessageCount, true},
		{"non-numeric BTS-1 is reported", batch("BTS|lots"), RuleBatchMessageCount, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rep := Lint("batch.hl7", []byte(tc.body), Options{})
			if got := firstOf(rep, tc.rule) != nil; got != tc.want {
				t.Errorf("finding %s = %v, want %v: %v", tc.rule, got, tc.want, ruleNames(rep))
			}
		})
	}
}

func TestHL7HeaderSeparatorConsistency(t *testing.T) {
	tests := []struct {
		name string
		body string
		want bool
	}{
		{
			"differing field separator",
			"MSH|^~\\&|A|B|C|D|20260115143000||ADT^A08|M1|P|2.5.1\n" +
				"MSH#^~\\&#A#B#C#D#20260115143100##ADT^A08#M2#P#2.5.1\n",
			true,
		},
		{
			"differing encoding characters",
			"MSH|^~\\&|A|B|C|D|20260115143000||ADT^A08|M1|P|2.5.1\n" +
				"MSH|#~\\&|A|B|C|D|20260115143100||ADT^A08|M2|P|2.5.1\n",
			true,
		},
		{
			"malformed encoding characters: too short",
			"MSH|^~\\|A|B|C|D|20260115143000||ADT^A08|M1|P|2.5.1\n",
			true,
		},
		{
			"malformed encoding characters: duplicate",
			"MSH|^~^&|A|B|C|D|20260115143000||ADT^A08|M1|P|2.5.1\n",
			true,
		},
		{
			"the truncation character of v2.7 is valid",
			"MSH|^~\\&#|A|B|C|D|20260115143000||ADT^A08|M1|P|2.7\n",
			false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rep := Lint("sep.hl7", []byte(tc.body), Options{})
			if got := firstOf(rep, RuleBatchSeparator) != nil; got != tc.want {
				t.Errorf("finding %s = %v, want %v: %v", RuleBatchSeparator, got, tc.want, ruleNames(rep))
			}
		})
	}
}
