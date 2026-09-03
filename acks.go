package edilint

import "fmt"

// Ack names the acknowledgment code a trading partner's front end returns for
// the defect a rule catches, so that a finding can be read as the rejection it
// prevents.
//
// Element is the acknowledgment element the code travels in. TA105 is the note
// code of a TA1 interchange acknowledgment. AK905, IK502, IK304 and IK403 are
// the group, transaction set, segment and data element error codes of a 999
// implementation acknowledgment; a 997 functional acknowledgment carries the
// same codes in AK905, AK502, AK304 and AK403.
//
// Only X12 rules have acknowledgments. HL7v2 and EDIFACT receivers answer with
// ACK and CONTRL messages whose codes are not mapped here.
type Ack struct {
	// Element is the acknowledgment element, e.g. "TA105".
	Element string `json:"element"`
	// Code is the code value as it appears in the element, e.g. "025".
	Code string `json:"code"`
	// Meaning is a short description of the code, in plain words.
	Meaning string `json:"meaning"`
}

// Type names the acknowledgment transaction the code arrives in: "TA1" or "999".
func (a Ack) Type() string {
	if a.Element == "TA105" {
		return "TA1"
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
	"TA105": "TA1",
	"AK905": "999",
	"IK502": "999",
	"IK304": "999",
	"IK403": "999",
}

// ruleAcks maps a rule identifier to the acknowledgments a receiver returns
// for the defect it catches. Rules() attaches them to the catalog.
//
// The code values are the public code lists of X12 data elements I18 (TA105),
// 716 (AK905), 718 (IK502), 720 (IK304) and 723 (IK403). A rule that catches
// more than one shape of defect lists one code per shape, in the order the
// rule's summary names them.
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
}

// RuleAcks returns the acknowledgments for a rule identifier or name, or nil
// when the rule has none.
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
