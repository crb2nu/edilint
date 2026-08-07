package edilint

import (
	"encoding/xml"
	"fmt"
	"io"
	"strings"
)

// JUnit XML output, the format CI test panels ingest. There is no JUnit
// standard, only the shape the common consumers agree on: a testsuite per
// input file, a testcase per finding, counts in the attributes.

type junitTestsuites struct {
	XMLName  xml.Name     `xml:"testsuites"`
	Tests    int          `xml:"tests,attr"`
	Failures int          `xml:"failures,attr"`
	Skipped  int          `xml:"skipped,attr"`
	Suites   []junitSuite `xml:"testsuite"`
}

type junitSuite struct {
	Name     string      `xml:"name,attr"`
	Tests    int         `xml:"tests,attr"`
	Failures int         `xml:"failures,attr"`
	Skipped  int         `xml:"skipped,attr"`
	Time     string      `xml:"time,attr"`
	Cases    []junitCase `xml:"testcase"`
}

type junitCase struct {
	Name      string        `xml:"name,attr"`
	Classname string        `xml:"classname,attr"`
	Time      string        `xml:"time,attr"`
	Failure   *junitFailure `xml:"failure"`
	Skipped   *junitSkipped `xml:"skipped"`
}

type junitFailure struct {
	Message string `xml:"message,attr"`
	Type    string `xml:"type,attr"`
	Body    string `xml:",chardata"`
}

type junitSkipped struct {
	Message string `xml:"message,attr"`
}

// WriteJUnit writes the run report as JUnit XML.
//
// A clean file renders as one passing testcase, so a green run shows every
// file it checked rather than an empty panel. Errors and warnings render as
// failures, matching the default exit-status contract; informational findings
// render as skipped, because info never fails a run. When --max-findings
// dropped findings, one more failing testcase says how many, so a truncated
// panel cannot read as a complete one.
func (rr *RunReport) WriteJUnit(w io.Writer) error {
	doc := junitTestsuites{Suites: []junitSuite{}}

	for _, r := range rr.Files {
		suite := junitSuite{Name: r.File, Time: "0"}

		if len(r.Findings) == 0 {
			suite.Cases = append(suite.Cases, junitCase{
				Name:      "edilint",
				Classname: r.File,
				Time:      "0",
			})
		}
		for _, f := range r.Findings {
			c := junitCase{
				Name:      junitCaseName(f),
				Classname: r.File,
				Time:      "0",
			}
			msg := f.Message
			if ctx := findingContext(f, r.Format); ctx != "" {
				msg += " (" + ctx + ")"
			}
			if f.Severity == SeverityInfo {
				c.Skipped = &junitSkipped{Message: msg}
			} else {
				c.Failure = &junitFailure{
					Message: msg,
					Type:    string(f.Severity),
					Body:    FormatFinding(f, r.Format),
				}
				suite.Failures++
			}
			suite.Cases = append(suite.Cases, c)
		}

		if r.Summary.Truncated {
			dropped := r.Summary.Total - len(r.Findings)
			suite.Cases = append(suite.Cases, junitCase{
				Name:      "findings not retained",
				Classname: r.File,
				Time:      "0",
				Failure: &junitFailure{
					Message: fmt.Sprintf("%d more finding(s) were counted but not retained; "+
						"raise --max-findings to see them", dropped),
					Type: "truncated",
				},
			})
			suite.Failures++
		}

		suite.Tests = len(suite.Cases)
		for _, c := range suite.Cases {
			if c.Skipped != nil {
				suite.Skipped++
			}
		}

		doc.Tests += suite.Tests
		doc.Failures += suite.Failures
		doc.Skipped += suite.Skipped
		doc.Suites = append(doc.Suites, suite)
	}

	if _, err := io.WriteString(w, xml.Header); err != nil {
		return err
	}
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return err
	}
	_, err := io.WriteString(w, "\n")
	return err
}

// junitCaseName names a testcase after its rule and its location. The location
// is part of the name because test panels collapse cases whose classname and
// name both match, and two findings of one rule in one file are two defects.
func junitCaseName(f Finding) string {
	name := f.Rule
	if f.ID != "" {
		name = f.ID + " " + f.Rule
	}
	var loc []string
	if f.Line > 0 {
		loc = append(loc, fmt.Sprintf("line %d", f.Line))
		if f.Column > 0 {
			loc = append(loc, fmt.Sprintf("col %d", f.Column))
		}
	} else if f.RecordNumber > 0 {
		loc = append(loc, fmt.Sprintf("record %d", f.RecordNumber))
	}
	if len(loc) > 0 {
		name += " (" + strings.Join(loc, ", ") + ")"
	}
	return name
}
