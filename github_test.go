package edilint

import (
	"bytes"
	"regexp"
	"strings"
	"testing"
)

var githubCommandLine = regexp.MustCompile(`^::(error|warning|notice) file=[^:]+(,line=\d+)?(,col=\d+)?,title=[^:]+::.+$`)

func TestGitHubAnnotationsOnePerFinding(t *testing.T) {
	rep := lintFixture(t, "835_envelope_broken.x12", Options{})
	if len(rep.Findings) == 0 {
		t.Fatal("fixture produced no findings; the test needs a defective input")
	}
	rr := NewRunReport()
	rr.Add(rep)

	var buf bytes.Buffer
	if err := rr.WriteGitHubAnnotations(&buf); err != nil {
		t.Fatalf("WriteGitHubAnnotations: %v", err)
	}
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != len(rep.Findings) {
		t.Fatalf("got %d line(s), want %d (one per finding):\n%s", len(lines), len(rep.Findings), buf.String())
	}
	for i, line := range lines {
		if !githubCommandLine.MatchString(line) {
			t.Errorf("line %d is not a workflow command: %s", i, line)
		}
		if !strings.Contains(line, "file=835_envelope_broken.x12") {
			t.Errorf("line %d does not name the file: %s", i, line)
		}
		if !strings.Contains(line, "title="+rep.Findings[i].ID+" ") {
			t.Errorf("line %d title does not carry the rule identifier %s: %s",
				i, rep.Findings[i].ID, line)
		}
	}
}

func TestGitHubAnnotationsCleanRunWritesNothing(t *testing.T) {
	rr := NewRunReport()
	rr.Add(lintFixture(t, "835_clean.x12", Options{}))

	var buf bytes.Buffer
	if err := rr.WriteGitHubAnnotations(&buf); err != nil {
		t.Fatalf("WriteGitHubAnnotations: %v", err)
	}
	if buf.Len() != 0 {
		t.Errorf("clean run wrote output:\n%s", buf.String())
	}
}

func TestGitHubAnnotationsSeverityMapping(t *testing.T) {
	tests := []struct {
		severity Severity
		command  string
	}{
		{SeverityError, "::error "},
		{SeverityWarning, "::warning "},
		{SeverityInfo, "::notice "},
	}
	for _, tt := range tests {
		rr := NewRunReport()
		rr.Add(&Report{
			File:   "a.x12",
			Format: FormatX12,
			Findings: []Finding{{
				ID: "EL3006", Rule: RuleSegmentCount, Severity: tt.severity,
				Message: "m", File: "a.x12", Line: 1,
			}},
		})
		var buf bytes.Buffer
		if err := rr.WriteGitHubAnnotations(&buf); err != nil {
			t.Fatalf("WriteGitHubAnnotations: %v", err)
		}
		if !strings.HasPrefix(buf.String(), tt.command) {
			t.Errorf("severity %s: got %q, want prefix %q", tt.severity, buf.String(), tt.command)
		}
	}
}

// TestGitHubAnnotationsEscaping holds the writer to GitHub's escape table: a
// newline or percent in a message, or a comma or colon in a file name, must
// not break the command apart.
func TestGitHubAnnotationsEscaping(t *testing.T) {
	rr := NewRunReport()
	rr.Add(&Report{
		File:   "dir,x:y.csv",
		Format: FormatDelimited,
		Findings: []Finding{{
			ID: "EL1003", Rule: RuleNonPrint, Severity: SeverityError,
			Message: "50% of\r\nthis message is on another line",
			File:    "dir,x:y.csv", Line: 2,
		}},
	})

	var buf bytes.Buffer
	if err := rr.WriteGitHubAnnotations(&buf); err != nil {
		t.Fatalf("WriteGitHubAnnotations: %v", err)
	}
	out := strings.TrimRight(buf.String(), "\n")
	if strings.Count(out, "\n") != 0 {
		t.Fatalf("message newline leaked into the output:\n%s", out)
	}
	if !strings.Contains(out, "file=dir%2Cx%3Ay.csv") {
		t.Errorf("file property is not escaped: %s", out)
	}
	if !strings.Contains(out, "::50%25 of%0D%0Athis message is on another line") {
		t.Errorf("message data is not escaped: %s", out)
	}
}

func TestGitHubAnnotationsTruncationNotice(t *testing.T) {
	rep := lintFixture(t, "835_envelope_broken.x12", Options{MaxFindings: 1})
	if !rep.Summary.Truncated {
		t.Fatal("fixture with MaxFindings 1 should truncate; it did not")
	}
	rr := NewRunReport()
	rr.Add(rep)

	var buf bytes.Buffer
	if err := rr.WriteGitHubAnnotations(&buf); err != nil {
		t.Fatalf("WriteGitHubAnnotations: %v", err)
	}
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	last := lines[len(lines)-1]
	if !strings.HasPrefix(last, "::notice ") || !strings.Contains(last, "not annotated") {
		t.Errorf("last line should be the truncation notice, got: %s", last)
	}
}
