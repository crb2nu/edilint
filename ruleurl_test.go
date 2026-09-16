package edilint

import (
	"bytes"
	"strings"
	"testing"
)

func TestRuleURL(t *testing.T) {
	for _, rule := range Rules() {
		for _, selector := range []string{rule.ID, strings.ToLower(rule.ID), rule.Name, " " + rule.ID + " "} {
			want := "https://crb2nu.github.io/edilint/rules/" + rule.ID + ".html"
			if got := RuleURL(selector); got != want {
				t.Errorf("RuleURL(%q) = %q, want %q", selector, got, want)
			}
		}
	}
	for _, selector := range []string{"", "EL9999", "charset", "../EL1001", "unknown.rule"} {
		if got := RuleURL(selector); got != "" {
			t.Errorf("unknown selector %q got URL %q", selector, got)
		}
	}
}

func TestReferenceLinksInOutputs(t *testing.T) {
	for _, severity := range []Severity{SeverityError, SeverityWarning, SeverityInfo} {
		t.Run(string(severity), func(t *testing.T) {
			f := Finding{ID: "EL1001", Rule: RuleBOM, Severity: severity, Message: "BOM", File: "sample.x12"}
			rr := NewRunReport()
			rr.Add(&Report{File: f.File, Format: FormatX12, Findings: []Finding{f}})
			want := RuleURL(f.ID)
			if got := FormatFinding(f, FormatX12); !strings.HasSuffix(got, want) {
				t.Errorf("text missing reference: %s", got)
			}
			var annotations, junit bytes.Buffer
			if err := rr.WriteGitHubAnnotations(&annotations); err != nil {
				t.Fatal(err)
			}
			if err := rr.WriteJUnit(&junit); err != nil {
				t.Fatal(err)
			}
			for _, output := range []string{annotations.String(), junit.String()} {
				if !strings.Contains(output, want) {
					t.Errorf("output missing reference: %s", output)
				}
			}
			sarif := sarifRuleFor(f.ID, f)
			if sarif.HelpURI != want || sarif.Help == nil || !strings.Contains(sarif.Help.Text, want) {
				t.Errorf("SARIF missing reference: %+v", sarif)
			}
		})
	}
	unknown := Finding{ID: "EL9999", Rule: "custom.rule", Message: "custom"}
	if strings.Contains(FormatFinding(unknown, FormatText), "https://") || sarifRuleFor(unknown.ID, unknown).HelpURI != "" {
		t.Fatal("unknown rule must not emit a broken reference link")
	}
}
