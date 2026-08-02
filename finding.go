package edilint

import (
	"fmt"
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
	// SeverityInfo marks a finding that is recorded but never fails a run. No
	// rule ships with this severity; it exists so that a configuration file can
	// downgrade a rule it wants to see without gating on.
	SeverityInfo Severity = "info"
)

// Rank returns a sortable weight for a severity, lowest value being most severe.
func (s Severity) Rank() int {
	switch s {
	case SeverityError:
		return 0
	case SeverityWarning:
		return 1
	case SeverityInfo:
		return 2
	default:
		return 3
	}
}

// ParseSeverity converts a configured severity name into a Severity.
func ParseSeverity(s string) (Severity, error) {
	switch Severity(strings.ToLower(strings.TrimSpace(s))) {
	case SeverityError:
		return SeverityError, nil
	case SeverityWarning:
		return SeverityWarning, nil
	case SeverityInfo:
		return SeverityInfo, nil
	default:
		return "", fmt.Errorf("unknown severity %q (want error, warning or info)", s)
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
	// ID is the stable rule identifier, e.g. "EL1005". It is filled in from the
	// rule catalog, so a check only has to set Rule.
	ID string `json:"id"`
	// Rule is the stable dotted rule name, e.g. "charset.homoglyph".
	Rule string `json:"rule"`
	// Class is the leading component of Rule, e.g. "charset".
	Class string `json:"class"`
	// Severity is "error", "warning" or "info".
	Severity Severity `json:"severity"`
	// Message is a one-line human-readable description.
	Message string `json:"message"`

	// File is the input path ("-" for stdin).
	File string `json:"file,omitempty"`
	// Line is the 1-based physical line the defect starts on.
	Line int `json:"line,omitempty"`
	// Column is the 1-based rune column within Line.
	Column int `json:"column,omitempty"`
	// RecordNumber is the 1-based logical record (or X12 segment) ordinal.
	RecordNumber int `json:"record_number,omitempty"`
	// Record identifies the kind of record: the segment identifier for X12 and
	// HL7v2, the record type otherwise. It is empty when the leading field holds
	// data rather than a record-type discriminator.
	Record string `json:"record,omitempty"`

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
	Infos    int            `json:"infos"`
	ByRule   map[string]int `json:"by_rule,omitempty"`
	// Truncated is true when MaxFindings dropped findings from the report.
	Truncated bool `json:"truncated,omitempty"`
}

// Report is the result of linting one input.
//
// Findings holds the findings that were retained for display; Summary always
// describes the complete set, including any that were counted but not kept.
type Report struct {
	File     string    `json:"file"`
	Format   Format    `json:"format"`
	Findings []Finding `json:"findings"`
	Summary  Summary   `json:"summary"`

	// disabled suppresses rules at accumulation time, so a suppressed rule costs
	// nothing to produce.
	disabled []string
	// severities overrides the severity a check assigned, keyed by rule name.
	severities map[string]Severity
	// retain is the hard ceiling on findings kept in memory. Zero means no
	// ceiling, which is only appropriate for a caller that built the report
	// itself. Lint always sets a finite value.
	retain int
}

// MaxRetainedFindings is the number of findings Lint keeps in memory for one
// file when the caller does not ask for more.
//
// A defect-dense input — a compressed archive caught by a shell glob, say —
// can contain one finding per byte. Retaining them all turns a few megabytes of
// input into gigabytes of heap, so accumulation stops here while the summary
// counters keep running. Options.MaxFindings raises the ceiling when it asks
// for more than this many.
const MaxRetainedFindings = 10000

// retentionFor returns the hard accumulation ceiling for a lint run.
func retentionFor(opts Options) int {
	if n := opts.maxFindings(); n > MaxRetainedFindings {
		return n
	}
	return MaxRetainedFindings
}

// OK reports whether the file is clean at or above the given severity.
// Passing SeverityWarning means "no error and no warning".
//
// Informational findings never fail a run through the command line, whose
// strictest threshold is SeverityWarning. They fail only a caller that asks for
// SeverityInfo explicitly.
//
// It reads the summary rather than the findings slice, so that capping output
// with Options.MaxFindings never changes the answer.
func (r *Report) OK(failOn Severity) bool {
	rank := failOn.Rank()
	if r.Summary.Errors > 0 {
		return false
	}
	if rank >= SeverityWarning.Rank() && r.Summary.Warnings > 0 {
		return false
	}
	if rank >= SeverityInfo.Rank() && r.Summary.Infos > 0 {
		return false
	}
	return true
}

// add records a finding: it fills in the derived fields, drops the finding if
// its rule is disabled, counts it, and retains it if there is room.
//
// Counting and retention are separate on purpose. Every accepted finding moves
// the summary, which is what OK and the truncation notice read, but only the
// first r.retain of them are kept, which is what bounds memory on a
// pathological input.
func (r *Report) add(f Finding) {
	if doc, ok := ruleIndex.byName[f.Rule]; ok {
		f.ID = doc.ID
		f.Class = doc.Class
	} else if i := strings.IndexByte(f.Rule, '.'); i > 0 {
		f.Class = f.Rule[:i]
	}
	if f.Severity == "" {
		f.Severity = SeverityError
	}
	if sev, ok := r.severities[f.Rule]; ok {
		f.Severity = sev
	}
	if ruleDisabled(f.Rule, r.disabled) {
		return
	}
	f.File = r.File

	r.Summary.Total++
	switch f.Severity {
	case SeverityError:
		r.Summary.Errors++
	case SeverityWarning:
		r.Summary.Warnings++
	case SeverityInfo:
		r.Summary.Infos++
	}
	if r.Summary.ByRule == nil {
		r.Summary.ByRule = map[string]int{}
	}
	r.Summary.ByRule[f.Rule]++

	if r.retain > 0 && len(r.Findings) >= r.retain {
		return
	}
	r.Findings = append(r.Findings, f)
}

// finalize orders the retained findings, applies the display cap and records
// whether anything was left out.
func (r *Report) finalize(maxFindings int) {
	sort.SliceStable(r.Findings, func(i, j int) bool {
		a, b := r.Findings[i], r.Findings[j]
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		if a.RecordNumber != b.RecordNumber {
			return a.RecordNumber < b.RecordNumber
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

	if maxFindings > 0 && len(r.Findings) > maxFindings {
		r.Findings = r.Findings[:maxFindings]
	}
	r.Summary.Truncated = len(r.Findings) < r.Summary.Total

	// A clean file must still marshal as an empty array. A nil slice becomes
	// JSON null, which the documented jq pipeline cannot iterate.
	if r.Findings == nil {
		r.Findings = []Finding{}
	}
}
