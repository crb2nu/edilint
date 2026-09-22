package rulesdoc

import (
	"strings"

	"github.com/crb2nu/edilint"
)

// ruleDetail is the hand-written half of a rule's reference page: the prose the
// catalog does not carry, plus a synthetic input that exhibits the defect.
//
// The prose is written once here rather than in the generated Markdown, so the
// pages stay a pure function of the catalog and this table.
type ruleDetail struct {
	// catches expands the catalog rationale into the "What it catches" section.
	catches string
	// fix is the "How to fix" section. Render appends the mechanical repair
	// sentence when edilint fix actually repairs the example.
	fix string
	// suppress is the "When to suppress" section: the false-positive story.
	suppress string
	// example is the input shown on the page.
	example example
}

// example is a synthetic input that actually produces its rule's finding.
// Render lints it and prints the finding the linter returned, so a page cannot
// show a diagnostic the example does not produce; TestExamplesProduceTheirRule
// fails when one stops firing.
type example struct {
	// file is the name shown on the command line and in the finding.
	file string
	// body is the input, lint it verbatim.
	body string
	// display replaces body inside the fenced block when body carries bytes a
	// Markdown file cannot show literally. Escapes are Go source syntax.
	display string
	// aux is a companion file the command line needs, such as a layout.
	aux *auxFile
	// args are the flags shown between "edilint" and the file name. They must
	// express opts; TestExampleArgsMatchOptions checks the ones it can.
	args string
	// opts are the lint options the args stand for.
	opts edilint.Options
}

// auxFile is a second file the example's command line refers to.
type auxFile struct {
	name string
	body string
}

// lines joins segments with a line feed and terminates the last one.
func lines(l ...string) string { return strings.Join(l, "\n") + "\n" }

// Shared X12 envelope pieces. The ISA is the fixed 106 characters, so a page
// that mutates it has to keep the length or trip EL3001 instead.
const (
	isa = "ISA*00*          *00*          *ZZ*NORTHGATEHEALTH*ZZ*VALEMEDGROUP   *260115*1430*^*00501*000000001*0*P*:~"
	gs  = "GS*HP*NORTHGATEHEALTH*VALEMEDGROUP*20260115*1430*1*X*005010X221A1~"
	st  = "ST*835*0001~"
	bpr = "BPR*I*4250.75*C*ACH*CCP~"
	se  = "SE*3*0001~"
	ge  = "GE*1*1~"
	iea = "IEA*1*000000001~"
)

// Shared HL7v2 batch pieces.
const (
	fhs = `FHS|^~\&|NORTHGATEHEALTH|NGCLINIC|VALEMEDGROUP|VMOFFICE|20260115143000|||NGFILE0001`
	bhs = `BHS|^~\&|NORTHGATEHEALTH|NGCLINIC|VALEMEDGROUP|VMOFFICE|20260115143000|||NGBATCH0001`
	msh = `MSH|^~\&|NORTHGATEHEALTH|NGCLINIC|VALEMEDGROUP|VMOFFICE|20260115143000||ADT^A08|NGMSG0001|P|2.5.1`
	pid = `PID|1||NGH900000001^^^NORTHGATE^MR||RIVERA^DANA`
)

// Shared EDIFACT pieces.
const (
	una = "UNA:+.? '"
	unb = "UNB+UNOA:4+NORTHGATEHEALTH+VALEMEDGROUP+260115:1430+NG260002'"
	unh = "UNH+NG0001+ORDERS:D:96A:UN'"
	bgm = "BGM+220+NG77001+9'"
	unt = "UNT+3+NG0001'"
	unz = "UNZ+1+NG260002'"
)

// Shared pipe-delimited extract.
const (
	psvHDR  = "HDR|NORTHGATE HEALTH|20260115|ELIG"
	psvDTL1 = "DTL|NGH900000001|RIVERA|DANA|19840322|F|PPO-GOLD|20260101|20261231"
	psvDTL2 = "DTL|NGH900000042|OKONKWO|TERESA|19770915|F|PPO-GOLD|20260101|20261231"
)

// countRule is the TRL:2:DTL rule the delimited examples are linted with.
var countRule = edilint.Options{CountRules: []edilint.CountRule{{Declaring: "TRL", Field: 2, Counted: "DTL"}}}

// basicCharsetOpts selects the X12 basic character-set profile.
var basicCharsetOpts = edilint.Options{X12Charset: edilint.CharsetBasic}

// edifactFormatOpts forces the edifact format for an input with no UNB.
var edifactFormatOpts = edilint.Options{Format: edilint.FormatEdifact}

// remitLayout is the fixed-width layout the EL5xxx examples are linted with.
// Its field widths sum to 49, the length of a well-formed detail record.
var remitLayout = &edilint.Layout{
	Name: "remittance-detail",
	Fields: []edilint.LayoutField{
		{Name: "record_type", Width: 3},
		{Name: "member_id", Width: 12, Pad: "right"},
		{Name: "last_name", Width: 16, Pad: "right"},
		{Name: "paid_amount", Width: 10, Pad: "left", PadChar: "0"},
		{Name: "paid_date", Width: 8},
	},
}

// remitLayoutJSON is remitLayout as the --layout file the command line names.
const remitLayoutJSON = `{
  "name": "remittance-detail",
  "fields": [
    {"name": "record_type", "width": 3},
    {"name": "member_id", "width": 12, "pad": "right"},
    {"name": "last_name", "width": 16, "pad": "right"},
    {"name": "paid_amount", "width": 10, "pad": "left", "padChar": "0"},
    {"name": "paid_date", "width": 8}
  ]
}`

// fixedOpts are the options the fixed-width examples are linted with.
var fixedOpts = edilint.Options{Format: edilint.FormatFixed, Layout: remitLayout}

// fixedExample builds a fixed-width example around one malformed record.
func fixedExample(body string) example {
	return example{
		file: "remit.txt",
		body: body,
		aux:  &auxFile{name: "remit.json", body: remitLayoutJSON},
		args: "--format fixed --layout remit.json",
		opts: fixedOpts,
	}
}

// x12Example builds an X12 example from its segments.
func x12Example(segments ...string) example {
	return example{file: "example.x12", body: lines(segments...)}
}

// psvExample builds a pipe-delimited example carrying the TRL:2:DTL count rule.
func psvExample(records ...string) example {
	return example{
		file: "eligibility.psv",
		body: lines(records...),
		args: "--count-rule TRL:2:DTL",
		opts: countRule,
	}
}

// hl7Example builds an HL7v2 batch example from its segments.
func hl7Example(segments ...string) example {
	return example{file: "batch.hl7", body: lines(segments...)}
}

// edifactExample builds an EDIFACT example from its segments.
func edifactExample(segments ...string) example {
	return example{file: "order.edi", body: lines(segments...)}
}

// details is the per-rule table. Every catalog rule has an entry;
// TestEveryRuleHasDetail fails when the catalog grows past it.
func details() map[string]ruleDetail {
	d := map[string]ruleDetail{}
	for _, group := range []map[string]ruleDetail{charsetDetails(), terminatorDetails(), envelopeDetails(), countDetails(), layoutDetails(), hl7BatchDetails(), edifactDetails()} {
		for id, detail := range group {
			d[id] = detail
		}
	}
	return d
}
