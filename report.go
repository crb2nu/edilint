package edilint

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// SchemaVersion is the version of the --json document shape. It is incremented
// whenever a consumer written against the previous version could meet something
// it has not seen before: a field added, removed, renamed, given a new meaning,
// or given a value outside its documented set.
//
// Version 3 added the "id" field to every finding, added "infos" to every
// summary, and widened "severity" to include "info".
// Version 2 renamed the finding field "segment" to "record" and the former
// "record" ordinal to "record_number".
const SchemaVersion = 3

// RunSummary aggregates every file linted in one invocation.
type RunSummary struct {
	Files     int  `json:"files"`
	Total     int  `json:"total"`
	Errors    int  `json:"errors"`
	Warnings  int  `json:"warnings"`
	Infos     int  `json:"infos"`
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
	rr.Summary.Infos += r.Summary.Infos
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
// "file:line:col: severity: [id rule] message" form. When there are no findings
// it writes nothing unless verbose is set.
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
	// The informational count is only printed when there is one, so that the
	// summary line of an ordinary run is unchanged by a feature it does not use.
	tally := fmt.Sprintf("%d error, %d warning", rr.Summary.Errors, rr.Summary.Warnings)
	if rr.Summary.Infos > 0 {
		tally += fmt.Sprintf(", %d info", rr.Summary.Infos)
	}
	_, err := fmt.Fprintf(w, "\n%s checked, %s (%s)\n",
		plural(rr.Summary.Files, "file"), plural(rr.Summary.Total, "finding"), tally)
	return err
}

// FormatFinding renders one finding as a single diagnostic line in the
// conventional "file:line:col: severity: message" form. The format decides
// whether the record identifier is called a segment or a record type.
//
// The bracketed part carries both the rule identifier and the rule name, so a
// line can be grepped for either. A finding with no identifier — one a caller
// built by hand, or a rule missing from the catalog — prints the name alone.
func FormatFinding(f Finding, format Format) string {
	var b strings.Builder
	b.WriteString(f.File)
	if f.Line > 0 {
		fmt.Fprintf(&b, ":%d", f.Line)
		if f.Column > 0 {
			fmt.Fprintf(&b, ":%d", f.Column)
		}
	}
	rule := f.Rule
	if f.ID != "" {
		rule = f.ID + " " + f.Rule
	}
	fmt.Fprintf(&b, ": %s: [%s] %s", f.Severity, rule, f.Message)

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
