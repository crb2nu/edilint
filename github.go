package edilint

import (
	"fmt"
	"io"
	"strings"
)

// GitHub Actions workflow-command output: one ::error, ::warning or ::notice
// line per finding, which the runner turns into file annotations on the pull
// request. The syntax is fixed by GitHub, escapes included.

// WriteGitHubAnnotations writes one workflow command per finding. A clean run
// writes nothing; the exit status is the gate either way. When --max-findings
// dropped findings, one ::notice per affected file says how many.
func (rr *RunReport) WriteGitHubAnnotations(w io.Writer) error {
	for _, r := range rr.Files {
		for _, f := range r.Findings {
			props := []string{"file=" + escapeGitHubProperty(f.File)}
			if f.Line > 0 {
				props = append(props, fmt.Sprintf("line=%d", f.Line))
				if f.Column > 0 {
					props = append(props, fmt.Sprintf("col=%d", f.Column))
				}
			}
			title := f.Rule
			if f.ID != "" {
				title = f.ID + " " + f.Rule
			}
			props = append(props, "title="+escapeGitHubProperty(title))

			msg := f.Message
			if ctx := findingContext(f, r.Format); ctx != "" {
				msg += " (" + ctx + ")"
			}
			if _, err := fmt.Fprintf(w, "::%s %s::%s\n",
				githubCommand(f.Severity), strings.Join(props, ","), escapeGitHubData(msg)); err != nil {
				return err
			}
		}
		if r.Summary.Truncated {
			dropped := r.Summary.Total - len(r.Findings)
			if _, err := fmt.Fprintf(w, "::notice file=%s::%d more finding(s) were counted but "+
				"not annotated; raise --max-findings to see them\n",
				escapeGitHubProperty(r.File), dropped); err != nil {
				return err
			}
		}
	}
	return nil
}

// githubCommand maps a severity onto the workflow-command vocabulary.
// Informational findings become ::notice, which annotates without failing —
// the same contract info has everywhere else.
func githubCommand(s Severity) string {
	switch s {
	case SeverityError:
		return "error"
	case SeverityWarning:
		return "warning"
	default:
		return "notice"
	}
}

// escapeGitHubData escapes the message part of a workflow command. The percent
// escape must run first, or it would re-escape the others.
func escapeGitHubData(s string) string {
	s = strings.ReplaceAll(s, "%", "%25")
	s = strings.ReplaceAll(s, "\r", "%0D")
	s = strings.ReplaceAll(s, "\n", "%0A")
	return s
}

// escapeGitHubProperty escapes a property value, which forbids two more
// characters than the message does.
func escapeGitHubProperty(s string) string {
	s = escapeGitHubData(s)
	s = strings.ReplaceAll(s, ":", "%3A")
	s = strings.ReplaceAll(s, ",", "%2C")
	return s
}
