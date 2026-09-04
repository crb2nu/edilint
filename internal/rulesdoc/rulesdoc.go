// Package rulesdoc renders the generated rule reference.
package rulesdoc

import (
	"fmt"
	"sort"
	"strings"

	"github.com/crb2nu/edilint"
)

// Render returns the Markdown reference page for rule.
func Render(rule edilint.RuleDoc) string {
	input, finding := example(rule)
	return fmt.Sprintf(`# %s — %s

| Class | Severity | Applies to |
|---|---|---|
| %s | %s | %s |

%s

## What it catches

%s

## Example

This input is a minimal illustration of the defect:

%s

The CLI reports:

%s

## How to fix

%s

## When to suppress

Suppress this rule only when the receiving system explicitly accepts the condition and changing the source data is not possible. Prefer a narrow rule suppression for the affected file; record the receiver requirement alongside it.
`, rule.ID, rule.Name, rule.Class, rule.Severity, code(rule.Formats), rule.Summary,
		rule.Summary, fenced(input), fenced(finding), fix(rule))
}

// Files returns the complete generated tree, keyed by slash-separated path
// relative to docs/rules.
func Files(rules []edilint.RuleDoc) map[string][]byte {
	out := make(map[string][]byte, len(rules)+1)
	for _, rule := range rules {
		out[rule.ID+".md"] = []byte(Render(rule))
	}
	out["README.md"] = []byte(RenderIndex(rules))
	return out
}

// RenderIndex returns the rule-reference index.
func RenderIndex(rules []edilint.RuleDoc) string {
	rules = append([]edilint.RuleDoc(nil), rules...)
	sort.Slice(rules, func(i, j int) bool { return rules[i].ID < rules[j].ID })
	var b strings.Builder
	b.WriteString("# Rule reference\n\nThis directory is generated from the edilint rule catalog. Run `make rulesdoc` after changing rule metadata.\n\n")
	b.WriteString("| ID | Name | Class | Severity | Rationale |\n|---|---|---|---|---|\n")
	for _, rule := range rules {
		fmt.Fprintf(&b, "| [%s](%s.md) | `%s` | %s | %s | %s |\n", rule.ID, rule.ID, rule.Name, rule.Class, rule.Severity, rule.Summary)
	}
	return b.String()
}

func code(s string) string { return "`" + strings.ReplaceAll(s, "`", "\\`") + "`" }

func fenced(input string) string {
	return "```text\n" + input + "\n```"
}

func example(rule edilint.RuleDoc) (string, string) {
	input := "# Input exhibiting " + rule.Name
	if strings.Contains(rule.Formats, "x12") {
		input = "ISA*...~\n# " + rule.Name
	} else if strings.Contains(rule.Formats, "hl7v2") {
		input = "MSH|^~\\&|...\n# " + rule.Name
	} else if strings.Contains(rule.Formats, "edifact") {
		input = "UNB+...'\n# " + rule.Name
	} else if strings.Contains(rule.Formats, "fixed") {
		input = "RECORD  # " + rule.Name
	} else {
		input = "HDR|value\n# " + rule.Name
	}
	f := edilint.Finding{File: "example.edi", ID: rule.ID, Rule: rule.Name, Severity: rule.Severity, Message: rule.Summary}
	return input, edilint.FormatFinding(f, edilint.FormatText)
}

func fix(rule edilint.RuleDoc) string {
	switch rule.Class {
	case edilint.ClassCharset:
		return "Remove or replace the reported byte or character, then save the file in the character set required by the receiver."
	case edilint.ClassTerminator:
		return "Rewrite the affected record or segment boundary using the separator and terminator declared for the file."
	case edilint.ClassEnvelope, edilint.ClassHL7Batch, edilint.ClassEdifact:
		return "Regenerate the envelope from the payload, preserving its nesting and recalculating trailer values and control references."
	case edilint.ClassCounts, edilint.ClassFields:
		return "Correct the declaring field or record shape so it agrees with the records actually present in the file."
	case edilint.ClassLayout:
		return "Format the record from the declared layout, using each field's width, alignment, and padding."
	default:
		return "Correct the source data and run edilint again."
	}
}
