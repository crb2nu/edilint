// Package rulesdoc renders the generated rule reference under docs/rules.
//
// A page is a pure function of the rule catalog and the per-rule table in this
// package, so the committed tree can be regenerated and compared byte for byte.
// The example on each page is linted as the page is rendered, which means the
// finding a page shows is the finding the linter actually produces for the
// input above it rather than a transcription of one.
package rulesdoc

import (
	"fmt"
	"sort"
	"strings"

	"github.com/crb2nu/edilint"
)

// regenerate is the command that rewrites the tree, quoted in the drift
// guard's failure message and in the generated index.
const regenerate = "make rulesdoc"

// Render returns the Markdown reference page for rule.
func Render(rule edilint.RuleDoc) string {
	detail, ok := details()[rule.ID]
	if !ok {
		panic("rulesdoc: no detail entry for " + rule.ID)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# %s — %s\n\n", rule.ID, rule.Name)
	fmt.Fprintf(&b, "| Class | Severity | Applies to |\n|---|---|---|\n| %s | %s | %s |\n\n",
		rule.Class, rule.Severity, code(rule.Formats))
	fmt.Fprintf(&b, "%s\n\n", rule.Summary)
	if acks := renderAcks(rule); acks != "" {
		b.WriteString(acks)
	}
	fmt.Fprintf(&b, "## What it catches\n\n%s\n\n", detail.catches)
	b.WriteString(renderExample(rule, detail.example))
	fmt.Fprintf(&b, "## How to fix\n\n%s\n\n", detail.fix)
	fmt.Fprintf(&b, "## When to suppress\n\n%s\n\n", detail.suppress)
	fmt.Fprintf(&b, "Suppress the rule for a run with `edilint --disable %s`, or for a repository by listing it under `disable:` in `.edilint.yml`.\n", rule.ID)
	return b.String()
}

// renderAcks lists the acknowledgment codes a partner returns for the defect,
// for the X12 rules that carry them.
func renderAcks(rule edilint.RuleDoc) string {
	if len(rule.Acks) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Trading partners report this defect with:\n\n")
	b.WriteString("| Acknowledgment | Element | Code | Meaning |\n|---|---|---|---|\n")
	for _, ack := range rule.Acks {
		fmt.Fprintf(&b, "| %s | `%s` | `%s` | %s |\n", ack.Type(), ack.Element, ack.Code, ack.Meaning)
	}
	b.WriteString("\n")
	return b.String()
}

// renderExample renders the "Example" section: the input, the command line and
// the finding the linter returns for that input.
func renderExample(rule edilint.RuleDoc, ex example) string {
	var b strings.Builder
	b.WriteString("## Example\n\n")
	if ex.aux != nil {
		fmt.Fprintf(&b, "`%s`:\n\n%s\n\n", ex.aux.name, fence("json", ex.aux.body))
	}
	fmt.Fprintf(&b, "`%s`:\n\n%s\n\n", ex.file, fence("text", ex.shown()))
	if ex.display != "" {
		b.WriteString("The escape sequences above stand for the bytes the file holds literally.\n\n")
	}
	// A fenced block cannot show whether the file ends with its terminator, and
	// for the rules about a missing one that byte is the whole defect.
	if !strings.HasSuffix(ex.body, "\n") {
		b.WriteString("The file ends immediately after the last record shown, with no terminator after it.\n\n")
	}
	fmt.Fprintf(&b, "%s reports:\n\n%s\n\n", code(ex.command()), fence("text", ex.finding(rule.ID)))
	return b.String()
}

// shown returns the text of the example as the page prints it.
func (e example) shown() string { return strings.TrimRight(e.text(), "\n") }

// text returns the display form of the input, which is the input itself unless
// it carries bytes Markdown cannot show.
func (e example) text() string {
	if e.display != "" {
		return e.display
	}
	return e.body
}

// command returns the command line the page shows.
func (e example) command() string {
	parts := []string{"edilint"}
	if e.args != "" {
		parts = append(parts, e.args)
	}
	return strings.Join(append(parts, e.file), " ")
}

// lint runs the example through the linter.
func (e example) lint() *edilint.Report {
	return edilint.Lint(e.file, []byte(e.body), e.opts)
}

// finding returns the diagnostic line the linter produced for id. It panics
// when the example stopped producing the rule's finding, which keeps a stale
// example out of the generated tree; TestExamplesProduceTheirRule reports the
// same condition as a test failure instead.
func (e example) finding(id string) string {
	rep := e.lint()
	for _, f := range rep.Findings {
		if f.ID == id {
			return edilint.FormatFinding(f, rep.Format)
		}
	}
	panic("rulesdoc: example for " + id + " no longer produces that finding")
}

// Owns reports whether a file in docs/rules belongs to this generator.
//
// docs/rules is shared: cmd/edilint-docs writes the published HTML reference
// into the same directory, so the Markdown tree can only claim, and only
// reconcile, the .md files it writes itself.
func Owns(name string) bool { return strings.HasSuffix(name, ".md") }

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
	fmt.Fprintf(&b, "# Rule reference\n\nOne page per rule in the edilint catalog. The pages are generated from the "+
		"catalog in `rules.go` and the per-rule table in `internal/rulesdoc`; run `%s` after changing either.\n\n", regenerate)
	b.WriteString("| ID | Name | Class | Severity | Detects |\n|---|---|---|---|---|\n")
	for _, rule := range rules {
		fmt.Fprintf(&b, "| [%s](%s.md) | `%s` | %s | %s | %s |\n",
			rule.ID, rule.ID, rule.Name, rule.Class, rule.Severity, rule.Summary)
	}
	return b.String()
}

func code(s string) string { return "`" + strings.ReplaceAll(s, "`", "\\`") + "`" }

// fence wraps body in a fenced block, widening the fence when the body itself
// contains a run of backticks.
func fence(lang, body string) string {
	ticks := "```"
	for strings.Contains(body, ticks) {
		ticks += "`"
	}
	return ticks + lang + "\n" + body + "\n" + ticks
}
