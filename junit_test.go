package edilint

import (
	"bytes"
	"encoding/xml"
	"strings"
	"testing"
)

// decodeJUnit round-trips the writer's output through encoding/xml, so a tag
// mistake that produces well-formed-but-wrong XML fails here rather than in a
// CI panel.
func decodeJUnit(t *testing.T, data []byte) junitTestsuites {
	t.Helper()
	var doc junitTestsuites
	if err := xml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("JUnit output is not valid XML: %v", err)
	}
	return doc
}

func TestJUnitFindingsBecomeFailures(t *testing.T) {
	rep := lintFixture(t, "835_envelope_broken.x12", Options{})
	if len(rep.Findings) == 0 {
		t.Fatal("fixture produced no findings; the test needs a defective input")
	}
	rr := NewRunReport()
	rr.Add(rep)

	var buf bytes.Buffer
	if err := rr.WriteJUnit(&buf); err != nil {
		t.Fatalf("WriteJUnit: %v", err)
	}
	if !strings.HasPrefix(buf.String(), "<?xml") {
		t.Errorf("output lacks the XML declaration:\n%.80s", buf.String())
	}
	doc := decodeJUnit(t, buf.Bytes())

	if len(doc.Suites) != 1 {
		t.Fatalf("testsuites: got %d, want 1 per file", len(doc.Suites))
	}
	suite := doc.Suites[0]
	if suite.Name != "835_envelope_broken.x12" {
		t.Errorf("suite name: got %q, want the file path", suite.Name)
	}
	if suite.Tests != len(rep.Findings) {
		t.Errorf("suite tests: got %d, want %d (one per finding)", suite.Tests, len(rep.Findings))
	}
	if suite.Failures != rep.Summary.Errors+rep.Summary.Warnings {
		t.Errorf("suite failures: got %d, want errors+warnings = %d",
			suite.Failures, rep.Summary.Errors+rep.Summary.Warnings)
	}
	if doc.Tests != suite.Tests || doc.Failures != suite.Failures {
		t.Errorf("document counts (%d/%d) do not match suite counts (%d/%d)",
			doc.Tests, doc.Failures, suite.Tests, suite.Failures)
	}

	for i, c := range suite.Cases {
		if c.Classname != "835_envelope_broken.x12" {
			t.Errorf("case %d: classname %q, want the file path", i, c.Classname)
		}
		if c.Failure == nil {
			t.Errorf("case %d (%s): no failure element", i, c.Name)
			continue
		}
		if c.Failure.Message == "" || c.Failure.Body == "" {
			t.Errorf("case %d (%s): empty failure message or body", i, c.Name)
		}
		if c.Failure.Type != "error" && c.Failure.Type != "warning" {
			t.Errorf("case %d (%s): failure type %q", i, c.Name, c.Failure.Type)
		}
	}

	// Two findings of the same rule must not collapse into one panel entry.
	names := map[string]bool{}
	for _, c := range suite.Cases {
		if names[c.Name] {
			t.Errorf("duplicate testcase name %q; panels collapse those", c.Name)
		}
		names[c.Name] = true
	}
}

func TestJUnitCleanFilePassesVisibly(t *testing.T) {
	rr := NewRunReport()
	rr.Add(lintFixture(t, "835_clean.x12", Options{}))

	var buf bytes.Buffer
	if err := rr.WriteJUnit(&buf); err != nil {
		t.Fatalf("WriteJUnit: %v", err)
	}
	doc := decodeJUnit(t, buf.Bytes())

	if len(doc.Suites) != 1 || len(doc.Suites[0].Cases) != 1 {
		t.Fatalf("clean file: got %+v, want one suite with one passing case", doc)
	}
	c := doc.Suites[0].Cases[0]
	if c.Failure != nil || c.Skipped != nil {
		t.Errorf("clean file's case should pass, got %+v", c)
	}
	if doc.Failures != 0 || doc.Skipped != 0 {
		t.Errorf("clean file: failures=%d skipped=%d, want 0/0", doc.Failures, doc.Skipped)
	}
}

func TestJUnitInfoBecomesSkipped(t *testing.T) {
	rep := lintFixture(t, "835_padding.x12", Options{
		Severities: map[string]Severity{RuleX12Padding: SeverityInfo},
	})
	requireRule(t, rep, RuleX12Padding, 1)
	rr := NewRunReport()
	rr.Add(rep)

	var buf bytes.Buffer
	if err := rr.WriteJUnit(&buf); err != nil {
		t.Fatalf("WriteJUnit: %v", err)
	}
	doc := decodeJUnit(t, buf.Bytes())

	suite := doc.Suites[0]
	if suite.Skipped != 1 {
		t.Fatalf("suite skipped: got %d, want 1 (info findings never fail a run)", suite.Skipped)
	}
	var skipped *junitCase
	for i := range suite.Cases {
		if suite.Cases[i].Skipped != nil {
			skipped = &suite.Cases[i]
		}
	}
	if skipped == nil {
		t.Fatal("no case carries a skipped element")
	}
	if skipped.Failure != nil {
		t.Errorf("a case must not be both skipped and failed: %+v", skipped)
	}
}

func TestJUnitTruncationIsItsOwnFailure(t *testing.T) {
	rep := lintFixture(t, "835_envelope_broken.x12", Options{MaxFindings: 1})
	if !rep.Summary.Truncated {
		t.Fatal("fixture with MaxFindings 1 should truncate; it did not")
	}
	rr := NewRunReport()
	rr.Add(rep)

	var buf bytes.Buffer
	if err := rr.WriteJUnit(&buf); err != nil {
		t.Fatalf("WriteJUnit: %v", err)
	}
	doc := decodeJUnit(t, buf.Bytes())

	suite := doc.Suites[0]
	last := suite.Cases[len(suite.Cases)-1]
	if last.Name != "findings not retained" || last.Failure == nil {
		t.Fatalf("last case: got %+v, want a failing truncation notice", last)
	}
	if last.Failure.Type != "truncated" {
		t.Errorf("truncation failure type: got %q, want truncated", last.Failure.Type)
	}
}
