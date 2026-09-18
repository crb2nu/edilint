package edilint

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strings"
	"testing"
)

// The acknowledgment table is documentation with a contract: every entry names
// a real rule, matches its format, and carries a code in the shape its element uses.

func TestAcknowledgmentsNameRealRules(t *testing.T) {
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
			format := map[string]string{"TA1": "x12", "999": "x12", "CONTRL": "edifact", "ACK": "hl7v2"}[ackType]
			if !strings.Contains(doc.Formats, format) {
				t.Errorf("%s applies to %q but references %s", id, doc.Formats, ackType)
			}
			wantShape := short
			if a.Element == "TA105" || a.Element == "ERR-3" {
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
		t.Errorf("a batch envelope has no universal ACK code, got %+v", acks)
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
	if !strings.Contains(help, "no universal ACK code") || !strings.Contains(help, "--disable "+batch.ID) {
		t.Errorf("help for %s = %q", batch.ID, help)
	}
}

func TestCrossFormatAcknowledgmentReferences(t *testing.T) {
	for _, tc := range []struct {
		id, element, code, qualification string
	}{
		{"EL7001", "UCI.0085", "13", "UNZ"},
		{"EL7001", "UCF.0085", "13", "UNE"},
		{"EL7001", "UCM.0085", "13", "UNT"},
		{"EL7003", "UCM.0085", "29", "UNT-1"},
		{"EL7004", "UCF.0085", "29", "UNE-1"},
		{"EL7005", "UCI.0085", "29", "UNZ-1"},
		{"EL7003", "UCM.0085", "37", "letters"},
		{"EL7004", "UCF.0085", "13", "missing"},
		{"EL7006", "UCI.0085", "28", "UNB-5"},
		{"EL7006", "UCF.0085", "28", "UNG-5"},
		{"EL7006", "UCM.0085", "28", "UNH-1"},
		{"EL7007", "UCI.0085", "19", "decimal"},
		{"EL7007", "UCI.0085", "20", "does not cover every"},
		{"EL6005", "ERR-3", "101", "only when this finding reports empty MSH-2"},
	} {
		t.Run(tc.id+"/"+tc.element+"/"+tc.code, func(t *testing.T) {
			for _, a := range RuleAcks(tc.id) {
				if a.Element == tc.element && a.Code == tc.code && strings.Contains(a.Meaning, tc.qualification) {
					return
				}
			}
			t.Fatalf("missing qualified reference: %+v", tc)
		})
	}
	for _, id := range []string{"EL6001", "EL6002", "EL6003", "EL6004", "EL6006", "EL2006", "EL7002", "EL7008", "EL7009"} {
		if len(RuleAcks(id)) != 0 {
			t.Errorf("%s must not promise a universal acknowledgment", id)
		}
	}
	for _, id := range []string{"EL6005", "EL7003"} {
		acks := RuleAcks(id)
		original := acks[0]
		acks[0].Code = "changed"
		if RuleAcks(RuleName(id))[0] != original {
			t.Errorf("%s returned shared storage or name lookup differs", id)
		}
	}
	if (Ack{Element: "unknown"}).Type() != "999" {
		t.Error("unknown elements must retain the historical fallback")
	}
}

func TestAcknowledgmentHelpLimitations(t *testing.T) {
	for _, doc := range Rules() {
		help := RuleHelp(doc)
		switch {
		case doc.Class == ClassHL7Batch:
			for _, want := range []string{"individual messages", "no universal ACK code", "Table 0357", "MSA-1", "https://www.hl7.eu/"} {
				if !strings.Contains(help, want) {
					t.Errorf("%s help lacks %q", doc.ID, want)
				}
			}
		case doc.Formats == "edifact":
			for _, want := range []string{"0085, not action element 0083", "prevent a valid CONTRL", "https://service.gefeg.com/"} {
				if !strings.Contains(help, want) {
					t.Errorf("%s help lacks %q", doc.ID, want)
				}
			}
		}
	}
}

func TestSARIFRulesCarryHelp(t *testing.T) {
	rr := NewRunReport()
	rr.Add(Lint("dup.x12", []byte(dupControlX12()), Options{}))
	rr.Add(lintFixture(t, "edifact_broken.edi", Options{}))
	rr.Add(lintFixture(t, "hl7v2_batch_broken.hl7", Options{}))
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
	wants := map[string]string{
		"EL3009": "TA1 code 025", "EL7003": "CONTRL code 29 (UCM.0085)",
		"EL6005": "ACK code 101 (ERR-3)", "EL6003": "no universal ACK code",
	}
	for _, r := range doc.Runs[0].Tool.Driver.Rules {
		if want, ok := wants[r.ID]; ok {
			if r.Help == nil || !strings.Contains(r.Help.Text, want) {
				t.Errorf("%s help = %v, want %q", r.ID, r.Help, want)
			}
			delete(wants, r.ID)
		}
	}
	if len(wants) != 0 {
		t.Fatalf("SARIF missing rules: %v", wants)
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
