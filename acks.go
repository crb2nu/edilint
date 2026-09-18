package edilint

import "fmt"

// Ack cross-references an acknowledgment code related to a defect a rule catches.
// Meaning qualifies the applicable defect; a receiver response is not guaranteed.
//
// Element is the acknowledgment element the code travels in. TA105 is the note
// code of a TA1 interchange acknowledgment. AK905, IK502, IK304 and IK403 are
// the group, transaction set, segment and data element error codes of a 999
// implementation acknowledgment; a 997 functional acknowledgment carries the
// same codes in AK905, AK502, AK304 and AK403.
//
// EDIFACT references name syntax-error element 0085 in CONTRL, qualified by
// reporting segment (UCI, UCF or UCM). HL7v2 references name ERR-3 (Table 0357)
// in ACK messages; they do not prescribe the acknowledgment status in MSA-1.
type Ack struct {
	// Element is the acknowledgment element, e.g. "TA105".
	Element string `json:"element"`
	// Code is the code value as it appears in the element, e.g. "025".
	Code string `json:"code"`
	// Meaning is a short description of the code, in plain words.
	Meaning string `json:"meaning"`
}

// Type names the acknowledgment message: "TA1", "999", "CONTRL" or "ACK".
// Unknown elements retain the historical "999" fallback.
func (a Ack) Type() string {
	if kind, ok := ackElements[a.Element]; ok {
		return kind
	}
	return "999"
}

// String renders the acknowledgment as "TA1 code 025 (TA105): duplicate
// interchange control number".
func (a Ack) String() string {
	return fmt.Sprintf("%s code %s (%s): %s", a.Type(), a.Code, a.Element, a.Meaning)
}

// ackElements are the elements an Ack may name, and the acknowledgment each
// belongs to.
var ackElements = map[string]string{
	"TA105":    "TA1",
	"AK905":    "999",
	"IK502":    "999",
	"IK304":    "999",
	"IK403":    "999",
	"UCI.0085": "CONTRL",
	"UCF.0085": "CONTRL",
	"UCM.0085": "CONTRL",
	"ERR-3":    "ACK",
}

// ruleAcks maps a rule identifier to related acknowledgment codes.
// Rules() attaches them to the catalog.
//
// The code values are the public code lists of X12 data elements I18 (TA105),
// 716 (AK905), 718 (IK502), 720 (IK304) and 723 (IK403). A rule that catches
// more than one shape of defect lists one code per shape, in the order the
// rule's summary names them. EDIFACT and HL7 sources are linked in ackGuidance.
var ruleAcks = map[string][]Ack{
	RuleID(RuleX12Basic): {
		{"IK403", "6", "invalid character in a data element, when the partner accepts only the basic set"},
	},
	RuleID(RuleX12Extended): {
		{"IK403", "6", "invalid character in a data element"},
	},

	RuleID(RuleX12Segment): {
		{"TA105", "004", "the segment terminator is invalid"},
		{"TA105", "023", "premature end of file, when the unterminated segment swallows the trailer"},
	},
	RuleID(RuleX12Padding): {
		{"IK304", "1", "unrecognized segment identifier, because the padding is read as part of the next segment's identifier"},
	},
	RuleID(RuleX12Separator): {
		{"TA105", "026", "invalid data element separator"},
		{"TA105", "027", "invalid component element separator"},
		{"TA105", "032", "invalid repetition separator"},
		{"TA105", "004", "the segment terminator is invalid"},
	},

	RuleID(RuleISALength): {
		{"TA105", "022", "invalid control structure; a receiver that cannot read the ISA often cannot return a TA1 at all and drops the transmission"},
	},
	RuleID(RuleEnvelopeNesting): {
		{"TA105", "024", "invalid interchange content, for a GS outside any ISA"},
		{"IK502", "18", "transaction set not in a functional group, for an ST outside any GS"},
	},
	RuleID(RuleUnclosed): {
		{"TA105", "023", "premature end of file, for an ISA with no IEA"},
		{"AK905", "3", "functional group trailer missing"},
		{"IK502", "2", "transaction set trailer missing"},
	},
	RuleID(RuleUnopened): {
		{"TA105", "022", "invalid control structure"},
		{"IK304", "2", "unexpected segment"},
	},
	RuleID(RuleControlNumber): {
		{"TA105", "001", "interchange control numbers in the header and trailer do not match"},
		{"AK905", "4", "group control numbers in the header and trailer do not agree"},
		{"IK502", "3", "transaction set control numbers in the header and trailer do not match"},
	},
	RuleID(RuleSegmentCount): {
		{"IK502", "4", "number of included segments does not match the actual count"},
	},
	RuleID(RuleGroupCount): {
		{"AK905", "5", "number of included transaction sets does not match the actual count"},
	},
	RuleID(RuleInterchangeCount): {
		{"TA105", "021", "invalid number of included groups value"},
	},
	RuleID(RuleDupControl): {
		{"TA105", "025", "duplicate interchange control number"},
		{"AK905", "19", "functional group control number not unique within the interchange"},
		{"IK502", "23", "transaction set control number not unique within the functional group"},
	},
	RuleID(RuleDateTime): {
		{"TA105", "014", "invalid interchange date value"},
		{"TA105", "015", "invalid interchange time value"},
		{"AK905", "30", "invalid group date"},
		{"AK905", "31", "invalid group time"},
	},
	RuleID(RuleEnvelopeMissingID): {
		{"TA105", "018", "invalid interchange control number value"},
	},
	RuleID(RuleEnvelopeTrailing): {
		{"TA105", "022", "invalid control structure, for segments after the IEA"},
	},

	RuleID(RuleBatchSeparator): {
		{"ERR-3", "101", "a required field is absent, only when this finding reports empty MSH-2; other separator and batch-header defects need partner-specific handling"},
	},
	RuleID(RuleEdifactUnclosed): {
		{"UCI.0085", "13", "UNZ is missing"},
		{"UCF.0085", "13", "UNE is missing"},
		{"UCM.0085", "13", "UNT is missing"},
	},
	RuleID(RuleEdifactSegmentCount): {
		{"UCM.0085", "29", "UNT-1 differs from the received segment count"},
		{"UCM.0085", "13", "UNT-1 is missing"},
		{"UCM.0085", "37", "UNT-1 contains letters where digits are required"},
	},
	RuleID(RuleEdifactGroupCount): {
		{"UCF.0085", "29", "UNE-1 differs from the received message count"},
		{"UCF.0085", "13", "UNE-1 is missing"},
		{"UCF.0085", "37", "UNE-1 contains letters where digits are required"},
	},
	RuleID(RuleEdifactInterchangeCount): {
		{"UCI.0085", "29", "UNZ-1 differs from the received message or group count"},
		{"UCI.0085", "13", "UNZ-1 is missing"},
		{"UCI.0085", "37", "UNZ-1 contains letters where digits are required"},
	},
	RuleID(RuleEdifactControlRef): {
		{"UCI.0085", "28", "UNB-5 and UNZ-2 differ"},
		{"UCF.0085", "28", "UNG-5 and UNE-2 differ"},
		{"UCM.0085", "28", "UNH-1 and UNT-2 differ"},
		{"UCI.0085", "13", "an interchange control reference is missing"},
		{"UCF.0085", "13", "a group control reference is missing"},
		{"UCM.0085", "13", "a message control reference is missing"},
	},
	RuleID(RuleEdifactServiceString): {
		{"UCI.0085", "19", "UNA declares an invalid decimal mark"},
		{"UCI.0085", "20", "UNA declares an unusable service character; this reference does not cover every malformed or misplaced UNA"},
	},
}

// ackGuidance explains the limits of the format-specific cross-references.
// Sources are public service syntax and terminology, not implementation guides.
func ackGuidance(doc RuleDoc) string {
	switch {
	case doc.Formats == "edifact":
		return "CONTRL references use syntax-error element 0085, not action element 0083. " +
			"UCI, UCF and UCM report interchange, group and message service-segment errors respectively. " +
			"A missing or unreadable header can prevent a valid CONTRL response. " +
			"Truncation, orphaned envelopes and data outside an interchange have no single mapping here.\n" +
			"Sources: https://service.gefeg.com/jwg1/Archive/v3/mt/m01.htm " +
			"https://service.gefeg.com/jwg1/Archive/cl/v3/21a/cl16.htm\n"
	case doc.Class == ClassHL7Batch:
		return "HL7 ACKs acknowledge individual messages. Batch/file envelope and count errors have no universal ACK code; " +
			"response batches and error handling depend on the partner agreement. " +
			"ERR-3 uses Table 0357 and does not determine the acknowledgment status in MSA-1.\n" +
			"Sources: https://www.hl7.eu/refactored/ctrl.html (2.10.3.2) " +
			"https://www.hl7.eu/refactored/segERR.html " +
			"https://terminology.hl7.org/5.5.0/CodeSystem-v2-0357.html\n"
	default:
		return ""
	}
}

// RuleAcks returns a copy of the acknowledgment references for a rule identifier
// or name, or an empty, non-nil slice when the rule has none. References apply
// only to the defect described in Meaning, not necessarily every finding.
func RuleAcks(selector string) []Ack {
	id := RuleID(selector)
	if id == "" {
		id = RuleID(RuleName(selector))
	}
	acks := ruleAcks[id]
	out := make([]Ack, len(acks))
	copy(out, acks)
	return out
}
