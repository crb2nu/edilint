package edilint

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strings"
	"testing"
)

// The acknowledgment table is documentation with a contract: every entry names
// a real rule, applies only where an X12 receiver would answer, and carries a
// code in the shape its element uses.

func TestAcknowledgmentsNameRealX12Rules(t *testing.T) {
	byID := map[string]RuleDoc{}
	for _, r := range Rules() {
		byID[r.ID] = r
	}
	threeDigits := regexp.MustCompile(`^[0-9]{3}$`)
	short := regexp.MustCompile(`^[1-9][0-9]?$`)

	for id, acks := range ruleAcks {
		doc, ok := byID[id]
		if !ok {
			t.Errorf("ruleAcks has an entry for %q, which is not in the catalog", id)
			continue
		}
		if !strings.Contains(doc.Formats, "x12") {
			t.Errorf("%s applies to %q, not X12, yet has acknowledgments", id, doc.Formats)
		}
		if len(acks) == 0 {
			t.Errorf("%s has an empty acknowledgment list; drop the entry instead", id)
		}
		for _, a := range acks {
			ackType, known := ackElements[a.Element]
			if !known {
				t.Errorf("%s names unknown element %q", id, a.Element)
				continue
			}
			if a.Type() != ackType {
				t.Errorf("%s: %s reports Type %q, want %q", id, a.Element, a.Type(), ackType)
			}
			wantShape := short
			if a.Element == "TA105" {
				wantShape = threeDigits
			}
			if !wantShape.MatchString(a.Code) {
				t.Errorf("%s: code %q is not the shape %s uses", id, a.Code, a.Element)
			}
			if a.Meaning == "" || a.Meaning != strings.TrimSpace(a.Meaning) {
				t.Errorf("%s: %s %s has a blank or untrimmed meaning", id, a.Element, a.Code)
			}
			if s := a.String(); !strings.HasPrefix(s, ackType+" code "+a.Code+" ("+a.Element+"): ") {
				t.Errorf("String() = %q", s)
			}
		}
	}
}

func TestEveryEnvelopeRuleHasAnAcknowledgment(t *testing.T) {
	// The envelope class is exactly the territory TA1 and 999 cover, so a
	// rule added there without a mapping is an omission, not a choice.
	for _, r := range Rules() {
		if r.Class == ClassEnvelope && len(r.Acks) == 0 {
			t.Errorf("%s (%s) has no acknowledgment mapping", r.ID, r.Name)
		}
	}
}

func TestRulesAttachAcknowledgments(t *testing.T) {
	var found bool
	for _, r := range Rules() {
		if r.Name != RuleDupControl {
			continue
		}
		found = true
		if len(r.Acks) != 3 || r.Acks[0].Code != "025" {
			t.Errorf("%s acks = %+v", r.ID, r.Acks)
		}
	}
	if !found {
		t.Fatal("catalog has no duplicate-control-number rule")
	}

	if acks := RuleAcks(RuleSegmentCount); len(acks) != 1 || acks[0].Element != "IK502" || acks[0].Code != "4" {
		t.Errorf("RuleAcks by name = %+v", acks)
	}
	if acks := RuleAcks("el3006"); len(acks) != 1 {
		t.Errorf("RuleAcks by identifier is case-insensitive, got %+v", acks)
	}
	if acks := RuleAcks(RuleBatchUnclosed); len(acks) != 0 {
		t.Errorf("an HL7 rule has no acknowledgments, got %+v", acks)
	}
	if acks := RuleAcks("nope"); acks == nil || len(acks) != 0 {
		t.Errorf("an unknown selector yields an empty, non-nil slice, got %#v", acks)
	}

	// The returned slice is a copy; a caller cannot edit the table.
	acks := RuleAcks(RuleSegmentCount)
	acks[0].Code = "changed"
	if RuleAcks(RuleSegmentCount)[0].Code != "4" {
		t.Error("RuleAcks returned the table itself")
	}
}

func TestRuleHelpNamesAcknowledgmentsAndSuppression(t *testing.T) {
	var dup, batch RuleDoc
	for _, r := range Rules() {
		switch r.Name {
		case RuleDupControl:
			dup = r
		case RuleBatchUnclosed:
			batch = r
		}
	}
	help := RuleHelp(dup)
	for _, want := range []string{"TA1 code 025 (TA105)", "999 code 19 (AK905)", "--disable EL3009", "--write-baseline"} {
		if !strings.Contains(help, want) {
			t.Errorf("help for %s is missing %q:\n%s", dup.ID, want, help)
		}
	}
	help = RuleHelp(batch)
	if !strings.Contains(help, "No X12 acknowledgment") || !strings.Contains(help, "--disable "+batch.ID) {
		t.Errorf("help for %s = %q", batch.ID, help)
	}
}

func TestSARIFRulesCarryHelp(t *testing.T) {
	rr := NewRunReport()
	rr.Add(Lint("dup.x12", []byte(dupControlX12()), Options{}))
	var out bytes.Buffer
	if err := rr.WriteSARIF(&out, "test"); err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Runs []struct {
			Tool struct {
				Driver struct {
					Rules []struct {
						ID   string `json:"id"`
						Help *struct {
							Text string `json:"text"`
						} `json:"help"`
					} `json:"rules"`
				} `json:"driver"`
			} `json:"tool"`
		} `json:"runs"`
	}
	if err := json.Unmarshal(out.Bytes(), &doc); err != nil {
		t.Fatalf("SARIF is not JSON: %v", err)
	}
	var seen bool
	for _, r := range doc.Runs[0].Tool.Driver.Rules {
		if r.ID != "EL3009" {
			continue
		}
		seen = true
		if r.Help == nil || !strings.Contains(r.Help.Text, "TA1 code 025") {
			t.Errorf("EL3009 help = %v", r.Help)
		}
	}
	if !seen {
		t.Fatalf("SARIF has no EL3009 rule; findings were %v", rr.Files[0].Findings)
	}
}

// dupControlX12 is one interchange whose two transaction sets share ST02.
func dupControlX12() string {
	isa := "ISA*00*          *00*          *ZZ*NORTHGATEHEALTH*ZZ*VALEMEDGROUP   " +
		"*260115*1430*^*00501*000000009*0*P*:~\n"
	gs := "GS*HP*NORTHGATEHEALTH*VALEMEDGROUP*20260115*1430*9*X*005010X221A1~\n"
	st := "ST*835*0001~\nBPR*I*1.00*C*ACH*CCP~\nSE*3*0001~\n"
	return isa + gs + st + st + "GE*2*9~\nIEA*1*000000009~\n"
}
