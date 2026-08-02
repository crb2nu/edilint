package edilint

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// SchemaVersion is the version of the --json document shape. It is incremented
// only when an existing field changes meaning or is removed.
const SchemaVersion = 2

// RunSummary aggregates every file linted in one invocation.
type RunSummary struct {
	Files     int  `json:"files"`
	Total     int  `json:"total"`
	Errors    int  `json:"errors"`
	Warnings  int  `json:"warnings"`
	Truncated bool `json:"truncated,omitempty"`
}

// RunReport is the top-level result of one edilint invocation.
type RunReport struct {
	Version int        `json:"version"`
	Files   []*Report  `json:"files"`
	Summary RunSummary `json:"summary"`
}

// NewRunReport returns an empty run report.
func NewRunReport() *RunReport {
	return &RunReport{Version: SchemaVersion}
}

// Add appends a file report and folds it into the run summary.
func (rr *RunReport) Add(r *Report) {
	rr.Files = append(rr.Files, r)
	rr.Summary.Files++
	rr.Summary.Total += r.Summary.Total
	rr.Summary.Errors += r.Summary.Errors
	rr.Summary.Warnings += r.Summary.Warnings
	if r.Summary.Truncated {
		rr.Summary.Truncated = true
	}
}

// OK reports whether every file is clean at or above the given severity.
func (rr *RunReport) OK(failOn Severity) bool {
	for _, r := range rr.Files {
		if !r.OK(failOn) {
			return false
		}
	}
	return true
}

// WriteJSON writes the run report as a single indented JSON document.
func (rr *RunReport) WriteJSON(w io.Writer) error {
	if rr.Files == nil {
		rr.Files = []*Report{}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(rr)
}

// WriteText writes findings one per line in a grep-parseable
// "file:line:col: severity: [rule] message" form. When there are no findings it
// writes nothing unless verbose is set.
func (rr *RunReport) WriteText(w io.Writer, verbose bool) error {
	for _, r := range rr.Files {
		if len(r.Findings) == 0 {
			if verbose {
				if _, err := fmt.Fprintf(w, "%s: ok (%s)\n", r.File, r.Format); err != nil {
					return err
				}
			}
			continue
		}
		for _, f := range r.Findings {
			if _, err := fmt.Fprintln(w, FormatFinding(f, r.Format)); err != nil {
				return err
			}
		}
		if r.Summary.Truncated {
			if _, err := fmt.Fprintf(w, "... and %d more findings (suppressed by --max-findings)\n",
				r.Summary.Total-len(r.Findings)); err != nil {
				return err
			}
		}
	}

	if rr.Summary.Total == 0 {
		if verbose {
			_, err := fmt.Fprintf(w, "%s checked, no findings\n", plural(rr.Summary.Files, "file"))
			return err
		}
		return nil
	}
	_, err := fmt.Fprintf(w, "\n%s checked, %s (%d error, %d warning)\n",
		plural(rr.Summary.Files, "file"), plural(rr.Summary.Total, "finding"),
		rr.Summary.Errors, rr.Summary.Warnings)
	return err
}

// FormatFinding renders one finding as a single diagnostic line in the
// conventional "file:line:col: severity: message" form. The format decides
// whether the record identifier is called a segment or a record type.
func FormatFinding(f Finding, format Format) string {
	var b strings.Builder
	b.WriteString(f.File)
	if f.Line > 0 {
		fmt.Fprintf(&b, ":%d", f.Line)
		if f.Column > 0 {
			fmt.Fprintf(&b, ":%d", f.Column)
		}
	}
	fmt.Fprintf(&b, ": %s: [%s] %s", f.Severity, f.Rule, f.Message)

	var where []string
	if f.RecordNumber > 0 {
		where = append(where, fmt.Sprintf("record %d", f.RecordNumber))
	}
	if f.Record != "" {
		label := "type"
		if format == FormatX12 || format == FormatHL7v2 {
			label = "segment"
		}
		where = append(where, label+" "+f.Record)
	}
	if len(where) > 0 {
		fmt.Fprintf(&b, " (%s)", strings.Join(where, ", "))
	}
	return b.String()
}

func plural(n int, unit string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", unit)
	}
	return fmt.Sprintf("%d %ss", n, unit)
}

// RuleDoc describes a rule for --list-rules and for the documentation table.
type RuleDoc struct {
	Name     string   `json:"name"`
	Class    string   `json:"class"`
	Severity Severity `json:"severity"`
	Formats  string   `json:"formats"`
	Summary  string   `json:"summary"`
}

// Rules returns the catalog of implemented rules, ordered by class then name.
func Rules() []RuleDoc {
	return []RuleDoc{
		{RuleBOM, ClassCharset, SeverityError, "all (warning for delimited)",
			"File starts with a byte order mark. An error for X12, HL7v2 and fixed-width, where a BOM " +
				"before ISA or MSH shifts every fixed position in the file; a warning for delimited, " +
				"because spreadsheet exports emit one routinely and most CSV readers cope."},
		{RuleInvalidUTF8, ClassCharset, SeverityError, "all",
			"Byte sequence is not valid UTF-8."},
		{RuleNonPrint, ClassCharset, SeverityError, "all",
			"Control character in record content that is not a declared separator. Tabs are reported as warnings."},
		{RuleZeroWidth, ClassCharset, SeverityError, "all",
			"Zero-width or bidirectional formatting character that renders as nothing but occupies bytes."},
		{RuleHomoglyph, ClassCharset, SeverityError, "all",
			"Unicode character that is visually identical to an ASCII one, such as Cyrillic А for A."},
		{RuleNonASCII, ClassCharset, SeverityWarning, "all",
			"Non-ASCII character that is not a known lookalike."},
		{RuleX12Basic, ClassCharset, SeverityWarning, "x12 (requires --charset basic)",
			"Character outside the X12 basic character set but inside the extended set. Off by " +
				"default; the default profile is extended."},
		{RuleX12Extended, ClassCharset, SeverityError, "x12",
			"Character outside the X12 extended character set, which in printable ASCII means the " +
				"caret or the backtick. A caret is exempt when ISA11 declares it as the repetition " +
				"separator."},

		{RuleMixedTerminator, ClassTerminator, SeverityError, "hl7v2, delimited, fixed, text",
			"File mixes CRLF, LF and CR line endings."},
		{RuleMissingFinal, ClassTerminator, SeverityWarning, "hl7v2, delimited, fixed, text",
			"Last record has no terminator."},
		{RuleX12Segment, ClassTerminator, SeverityError, "x12",
			"Segment is not closed by the segment terminator the ISA declared."},
		{RuleX12Padding, ClassTerminator, SeverityWarning, "x12",
			"Whitespace between segment terminators is applied inconsistently."},
		{RuleX12Separator, ClassTerminator, SeverityError, "x12",
			"Declared separators collide with each other or are alphanumeric."},

		{RuleISALength, ClassEnvelope, SeverityError, "x12",
			"ISA segment is not the fixed 106 characters, or is absent."},
		{RuleEnvelopeNesting, ClassEnvelope, SeverityError, "x12",
			"GS appears outside an ISA, or ST outside a GS."},
		{RuleUnclosed, ClassEnvelope, SeverityError, "x12",
			"ISA, GS or ST has no matching IEA, GE or SE."},
		{RuleUnopened, ClassEnvelope, SeverityError, "x12",
			"IEA, GE or SE appears without its header."},
		{RuleControlNumber, ClassEnvelope, SeverityError, "x12",
			"Header and trailer control numbers differ (ISA13/IEA02, GS06/GE02, ST02/SE02). " +
				"A leading-zero-only difference is reported as a warning."},
		{RuleSegmentCount, ClassEnvelope, SeverityError, "x12",
			"SE01 does not match the recounted segments from ST through SE inclusive."},
		{RuleGroupCount, ClassEnvelope, SeverityError, "x12",
			"GE01 does not match the recounted transaction sets in the group."},
		{RuleInterchangeCount, ClassEnvelope, SeverityError, "x12",
			"IEA01 does not match the recounted functional groups in the interchange."},
		{RuleDupControl, ClassEnvelope, SeverityError, "x12",
			"Duplicate ISA13 within the file or across the files in one run, duplicate GS06 within an " +
				"interchange, or duplicate ST02 within a functional group."},
		{RuleDateTime, ClassEnvelope, SeverityError, "x12",
			"ISA09/GS04 dates or ISA10/GS05 times are not valid YYMMDD, CCYYMMDD or HHMM[SS[DD]] values."},
		{RuleEnvelopeMissingID, ClassEnvelope, SeverityError, "x12",
			"ISA13 interchange control number is empty."},
		{RuleEnvelopeTrailing, ClassEnvelope, SeverityError, "x12",
			"Segments appear outside any interchange."},

		{RuleCountMismatch, ClassCounts, SeverityError, "all (requires --count-rule)",
			"A declared record count does not match the recounted total."},
		{RuleCountUnparsable, ClassCounts, SeverityError, "all (requires --count-rule)",
			"The field a count rule points at is not an integer."},
		{RuleCountShortRec, ClassCounts, SeverityError, "all (requires --count-rule)",
			"The declaring record has fewer fields than the count rule reads."},
		{RuleCountNoDeclarer, ClassCounts, SeverityWarning, "all (requires --count-rule)",
			"No record matched the count rule's declaring prefix, so nothing was verified."},

		{RuleFieldOutlier, ClassFields, SeverityError, "delimited, hl7v2 (warning)",
			"A record carries a different number of fields from others of the same record type."},

		{RuleLayoutLength, ClassLayout, SeverityError, "fixed (requires --layout)",
			"Record length does not match the sum of the layout's field widths."},
		{RuleLayoutPadding, ClassLayout, SeverityWarning, "fixed (requires --layout)",
			"A field's padding is unambiguously on the side opposite the one the layout declares."},
	}
}

// WriteRules renders the rule catalog as an aligned table.
func WriteRules(w io.Writer) error {
	rules := Rules()
	width := 0
	for _, r := range rules {
		if len(r.Name) > width {
			width = len(r.Name)
		}
	}
	for _, r := range rules {
		if _, err := fmt.Fprintf(w, "%-*s  %-7s  %s\n", width, r.Name, r.Severity, r.Summary); err != nil {
			return err
		}
	}
	return nil
}
