package edilint

import (
	"sort"
	"strings"
)

// Severity classifies how serious a finding is.
type Severity string

const (
	// SeverityError marks a defect that will almost certainly break a downstream
	// parser or cause a trading-partner rejection.
	SeverityError Severity = "error"
	// SeverityWarning marks a suspicious pattern that is legal but frequently
	// indicates a generation bug.
	SeverityWarning Severity = "warning"
)

// Rank returns a sortable weight for a severity, lowest value being most severe.
func (s Severity) Rank() int {
	switch s {
	case SeverityError:
		return 0
	case SeverityWarning:
		return 1
	default:
		return 2
	}
}

// Check classes. Every rule name is prefixed with its class, so a class name is
// also a valid value for Options.Disabled.
const (
	ClassCharset    = "charset"
	ClassTerminator = "terminator"
	ClassEnvelope   = "envelope"
	ClassCounts     = "counts"
	ClassFields     = "fields"
	ClassLayout     = "layout"
)

// Rule names. These are part of the tool's public contract: they appear in JSON
// output and may be passed to --disable.
const (
	RuleBOM         = "charset.bom"
	RuleNonPrint    = "charset.nonprintable"
	RuleHomoglyph   = "charset.homoglyph"
	RuleNonASCII    = "charset.nonascii"
	RuleZeroWidth   = "charset.zero-width"
	RuleInvalidUTF8 = "charset.invalid-utf8"
	RuleX12Basic    = "charset.x12-basic"
	RuleX12Extended = "charset.x12-extended"

	RuleMixedTerminator = "terminator.mixed"
	RuleMissingFinal    = "terminator.missing-final"
	RuleX12Segment      = "terminator.x12-segment"
	RuleX12Padding      = "terminator.x12-padding"
	RuleX12Separator    = "terminator.x12-separator"

	RuleISALength         = "envelope.isa-length"
	RuleUnclosed          = "envelope.unclosed"
	RuleUnopened          = "envelope.unopened"
	RuleControlNumber     = "envelope.control-number"
	RuleSegmentCount      = "envelope.segment-count"
	RuleGroupCount        = "envelope.group-count"
	RuleInterchangeCount  = "envelope.interchange-count"
	RuleEnvelopeNesting   = "envelope.nesting"
	RuleEnvelopeTrailing  = "envelope.trailing-data"
	RuleEnvelopeMissingID = "envelope.missing-control-id"
	RuleDupControl        = "envelope.duplicate-control-number"
	RuleDateTime          = "envelope.datetime"

	RuleCountMismatch   = "counts.mismatch"
	RuleCountUnparsable = "counts.unparsable"
	RuleCountNoDeclarer = "counts.no-declaring-record"
	RuleCountShortRec   = "counts.missing-field"

	RuleFieldOutlier = "fields.count-outlier"

	RuleLayoutLength  = "layout.length"
	RuleLayoutPadding = "layout.padding"
)

// Finding is a single defect located in an input file.
type Finding struct {
	// Rule is the stable dotted rule name, e.g. "charset.homoglyph".
	Rule string `json:"rule"`
	// Class is the leading component of Rule, e.g. "charset".
	Class string `json:"class"`
	// Severity is "error" or "warning".
	Severity Severity `json:"severity"`
	// Message is a one-line human-readable description.
	Message string `json:"message"`

	// File is the input path ("-" for stdin).
	File string `json:"file,omitempty"`
	// Line is the 1-based physical line the defect starts on.
	Line int `json:"line,omitempty"`
	// Column is the 1-based rune column within Line.
	Column int `json:"column,omitempty"`
	// Record is the 1-based logical record (or X12 segment) ordinal.
	Record int `json:"record,omitempty"`
	// Segment is the X12/HL7v2 segment identifier or, for line-oriented files,
	// the record type. It is empty when the leading field is data rather than a
	// record-type discriminator.
	Segment string `json:"segment,omitempty"`

	// CodePoint is set for character findings, e.g. "U+0410".
	CodePoint string `json:"code_point,omitempty"`
	// Expected and Actual are set for count, length and padding findings.
	Expected string `json:"expected,omitempty"`
	Actual   string `json:"actual,omitempty"`
}

// Summary aggregates finding counts for a report.
type Summary struct {
	Total    int            `json:"total"`
	Errors   int            `json:"errors"`
	Warnings int            `json:"warnings"`
	ByRule   map[string]int `json:"by_rule,omitempty"`
	// Truncated is true when MaxFindings dropped findings from the report.
	Truncated bool `json:"truncated,omitempty"`
}

// Report is the result of linting one input.
type Report struct {
	File     string    `json:"file"`
	Format   Format    `json:"format"`
	Findings []Finding `json:"findings"`
	Summary  Summary   `json:"summary"`
}

// OK reports whether the file is clean at or above the given severity.
// Passing SeverityWarning means "no findings at all".
func (r *Report) OK(failOn Severity) bool {
	for i := range r.Findings {
		if r.Findings[i].Severity.Rank() <= failOn.Rank() {
			return false
		}
	}
	return true
}

// add appends a finding, filling in Class from the rule name.
func (r *Report) add(f Finding) {
	if i := strings.IndexByte(f.Rule, '.'); i > 0 {
		f.Class = f.Rule[:i]
	}
	if f.Severity == "" {
		f.Severity = SeverityError
	}
	f.File = r.File
	r.Findings = append(r.Findings, f)
}

// finalize filters disabled rules, orders findings deterministically, applies the
// finding cap and computes the summary.
func (r *Report) finalize(disabled []string, maxFindings int) {
	kept := r.Findings[:0]
	for _, f := range r.Findings {
		if ruleDisabled(f.Rule, disabled) {
			continue
		}
		kept = append(kept, f)
	}
	r.Findings = kept

	sort.SliceStable(r.Findings, func(i, j int) bool {
		a, b := r.Findings[i], r.Findings[j]
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		if a.Record != b.Record {
			return a.Record < b.Record
		}
		if a.Column != b.Column {
			return a.Column < b.Column
		}
		if a.Severity.Rank() != b.Severity.Rank() {
			return a.Severity.Rank() < b.Severity.Rank()
		}
		if a.Rule != b.Rule {
			return a.Rule < b.Rule
		}
		return a.Message < b.Message
	})

	r.Summary = Summary{ByRule: map[string]int{}}
	for i := range r.Findings {
		switch r.Findings[i].Severity {
		case SeverityError:
			r.Summary.Errors++
		case SeverityWarning:
			r.Summary.Warnings++
		}
		r.Summary.ByRule[r.Findings[i].Rule]++
	}
	r.Summary.Total = len(r.Findings)

	if maxFindings > 0 && len(r.Findings) > maxFindings {
		r.Findings = r.Findings[:maxFindings]
		r.Summary.Truncated = true
	}
	if len(r.Summary.ByRule) == 0 {
		r.Summary.ByRule = nil
	}
}

// ruleDisabled reports whether rule is suppressed by any entry in disabled.
// An entry matches either the full rule name or a dot-delimited prefix of it,
// so "charset" disables every charset.* rule.
func ruleDisabled(rule string, disabled []string) bool {
	for _, d := range disabled {
		d = strings.TrimSpace(d)
		if d == "" {
			continue
		}
		if d == rule || strings.HasPrefix(rule, d+".") {
			return true
		}
	}
	return false
}
