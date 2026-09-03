package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/crb2nu/edilint"
)

// defaultMaxFindings caps the findings a lint call retains per file when
// neither the server's configuration nor the call sets max_findings. A
// defect-dense file can produce thousands of findings, and a model reading the
// result gains nothing from the ten-thousandth; the summary counts stay exact
// regardless, and a call can raise the cap or set it to 0 for no cap.
const defaultMaxFindings = 200

// tool is one entry in tools/list plus the function that answers tools/call.
type tool struct {
	name    string
	def     map[string]any
	handler func(s *Server, args json.RawMessage) map[string]any
}

// tools lists every tool in the order tools/list reports them. The order is
// fixed so that a client can cache the list.
var tools = []tool{
	{
		name: "lint_file",
		def: map[string]any{
			"name":  "lint_file",
			"title": "Lint interchange files",
			"description": "Lint one or more healthcare interchange files by path: X12 EDI, HL7v2 " +
				"messages and batches, EDIFACT, delimited and fixed-width records. The format is " +
				"detected unless forced. The result's text is one diagnostic line per finding in " +
				"file:line:col: severity: [id rule] message form; the structured content holds " +
				"exit_status (0 clean, 1 findings, 2 an input could not be read) and the same " +
				"report `edilint --json` writes (schema/report.v3.schema.json). Findings are the " +
				"normal result, not an error. Duplicate interchange control numbers are detected " +
				"across the files of one call.",
			"inputSchema": lintInputSchema(map[string]any{
				"paths": map[string]any{
					"type":        "array",
					"items":       map[string]any{"type": "string"},
					"minItems":    1,
					"description": "Files to lint. Standard input (\"-\") is not accepted; pass content to lint_text instead.",
				},
			}, []string{"paths"}),
			"annotations": readOnlyAnnotations("Lint interchange files"),
		},
		handler: (*Server).lintFile,
	},
	{
		name: "lint_text",
		def: map[string]any{
			"name":  "lint_text",
			"title": "Lint interchange content",
			"description": "Lint interchange content passed directly in the call, for a file that " +
				"exists only in the conversation or that was just generated. Behaves exactly like " +
				"lint_file for one file; see that tool for the result shape.",
			"inputSchema": lintInputSchema(map[string]any{
				"content": map[string]any{
					"type":        "string",
					"description": "The file content to lint, verbatim. Line endings and separators are part of what is checked, so pass them unchanged.",
				},
				"name": map[string]any{
					"type":        "string",
					"description": "A display name for the content in findings. Default \"input\".",
				},
			}, []string{"content"}),
			"annotations": readOnlyAnnotations("Lint interchange content"),
		},
		handler: (*Server).lintText,
	},
	{
		name: "list_rules",
		def: map[string]any{
			"name":  "list_rules",
			"title": "List rules",
			"description": "The catalog of rules edilint checks, each with its stable identifier " +
				"(EL####), dotted name, check class, default severity, the formats it applies to " +
				"and a one-line summary. Optionally filtered to one class.",
			"inputSchema": map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]any{
					"class": map[string]any{
						"type":        "string",
						"description": "Restrict to one check class: " + strings.Join(edilint.RuleClasses(), ", ") + ".",
					},
				},
			},
			"annotations": readOnlyAnnotations("List rules"),
		},
		handler: (*Server).listRules,
	},
	{
		name: "explain_rule",
		def: map[string]any{
			"name":  "explain_rule",
			"title": "Explain a rule",
			"description": "What one rule checks, why it matters, which formats it applies to, its " +
				"default severity, and how to suppress it or baseline existing occurrences. Accepts " +
				"an identifier such as EL3006 or a name such as envelope.segment-count.",
			"inputSchema": map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"required":             []string{"rule"},
				"properties": map[string]any{
					"rule": map[string]any{
						"type":        "string",
						"description": "A rule identifier (EL3006) or rule name (envelope.segment-count).",
					},
				},
			},
			"annotations": readOnlyAnnotations("Explain a rule"),
		},
		handler: (*Server).explainRule,
	},
}

// toolDefinitions returns the tools/list entries.
func toolDefinitions() []map[string]any {
	defs := make([]map[string]any, 0, len(tools))
	for _, t := range tools {
		defs = append(defs, t.def)
	}
	return defs
}

// readOnlyAnnotations describes every tool here: each reads its inputs and
// changes nothing, so a client may run it without confirmation, and repeating
// a call returns the same answer.
func readOnlyAnnotations(title string) map[string]any {
	return map[string]any{
		"title":           title,
		"readOnlyHint":    true,
		"destructiveHint": false,
		"idempotentHint":  true,
		"openWorldHint":   false,
	}
}

// lintInputSchema builds the input schema of a lint tool: the tool's own
// properties plus the tuning options every lint call accepts.
func lintInputSchema(own map[string]any, required []string) map[string]any {
	props := map[string]any{
		"format": map[string]any{
			"type":        "string",
			"enum":        []string{"auto", "x12", "hl7v2", "edifact", "delimited", "fixed", "text"},
			"description": "Force an input format. Default auto detects it from the content.",
		},
		"delimiter": map[string]any{
			"type":        "string",
			"description": "Field delimiter for delimited files, one character; the escapes \\t, \\0 and \\xNN are accepted. Default detects it.",
		},
		"charset": map[string]any{
			"type":        "string",
			"enum":        []string{"extended", "basic", "off"},
			"description": "X12 character-set profile to enforce. Default extended.",
		},
		"type_field": map[string]any{
			"type":        "integer",
			"minimum":     1,
			"description": "1-based field used as the record-type discriminator for the field-count check. Default 1.",
		},
		"count_rules": map[string]any{
			"type":        "array",
			"items":       map[string]any{"type": "string"},
			"description": "Declared-count assertions in the form recordPrefix:fieldIndex:countedPrefix, e.g. TRL:2:DTL means field 2 of TRL records declares how many DTL records exist.",
		},
		"disable": map[string]any{
			"type":        "array",
			"items":       map[string]any{"type": "string"},
			"description": "Rules to suppress, by identifier (EL1006), name (charset.nonascii) or class (charset). Added to any the configuration file suppresses.",
		},
		"max_findings": map[string]any{
			"type":        "integer",
			"minimum":     0,
			"description": fmt.Sprintf("Retain at most this many findings per file; 0 means no cap. Default %d unless the configuration file sets one. Summary counts are exact either way.", defaultMaxFindings),
		},
		"layout": map[string]any{
			"type":        "string",
			"description": "Path to a fixed-width layout JSON file. Required when format is fixed.",
		},
		"allow_warnings": map[string]any{
			"type":        "boolean",
			"description": "Report exit_status 0 for a file whose only findings are warnings.",
		},
	}
	for k, v := range own {
		props[k] = v
	}
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             required,
		"properties":           props,
	}
}

// lintArgs are the tuning options shared by lint_file and lint_text.
type lintArgs struct {
	Format        string   `json:"format"`
	Delimiter     string   `json:"delimiter"`
	Charset       string   `json:"charset"`
	TypeField     int      `json:"type_field"`
	CountRules    []string `json:"count_rules"`
	Disable       []string `json:"disable"`
	MaxFindings   *int     `json:"max_findings"`
	Layout        string   `json:"layout"`
	AllowWarnings *bool    `json:"allow_warnings"`
}

// decodeArgs parses tool arguments strictly. An argument the tool does not
// know is reported rather than ignored, because a misspelled option that
// silently changes nothing is the worst outcome a call can have.
func decodeArgs(raw json.RawMessage, into any) error {
	if len(raw) == 0 {
		raw = json.RawMessage("{}")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	return dec.Decode(into)
}

// callTool answers tools/call.
func (s *Server) callTool(params json.RawMessage) (map[string]any, *rpcError) {
	var p struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, errorf(codeInvalidParams, "tools/call params: %v", err)
	}
	for _, t := range tools {
		if t.name == p.Name {
			return t.handler(s, p.Arguments), nil
		}
	}
	return nil, errorf(codeInvalidParams, "Unknown tool: %s", p.Name)
}

// textResult builds a successful tool result carrying text and, optionally,
// structured content.
func textResult(text string, structured any) map[string]any {
	result := map[string]any{
		"content": []map[string]any{{"type": "text", "text": text}},
	}
	if structured != nil {
		result["structuredContent"] = structured
	}
	return result
}

// errorResult builds a tool execution error: something about the call's
// inputs that the caller can correct and retry.
func errorResult(format string, args ...any) map[string]any {
	return map[string]any{
		"content": []map[string]any{{"type": "text", "text": fmt.Sprintf(format, args...)}},
		"isError": true,
	}
}

// buildOptions layers a call's arguments over the server's base options. The
// base is copied, never mutated, so calls cannot leak settings into each other.
func (s *Server) buildOptions(a lintArgs) (edilint.Options, bool, error) {
	opts := s.Options
	opts.Disabled = append([]string(nil), s.Options.Disabled...)
	opts.CountRules = append([]edilint.CountRule(nil), s.Options.CountRules...)
	// Cross-file duplicate detection is scoped to one call, and a baseline
	// belongs to a working tree, not to a server that any directory may query.
	opts.SeenISA13 = map[string]string{}
	opts.Baseline = nil
	if opts.Format == "" {
		opts.Format = edilint.FormatAuto
	}

	var err error
	if a.Format != "" {
		if opts.Format, err = edilint.ParseFormat(a.Format); err != nil {
			return opts, false, fmt.Errorf("format: %w", err)
		}
	}
	if a.Delimiter != "" {
		if _, err = edilint.ParseDelimiter(a.Delimiter); err != nil {
			return opts, false, fmt.Errorf("delimiter: %w", err)
		}
		opts.Delimiter = a.Delimiter
	}
	if a.Charset != "" {
		if opts.X12Charset, err = edilint.ParseCharsetProfile(a.Charset); err != nil {
			return opts, false, fmt.Errorf("charset: %w", err)
		}
	}
	if a.TypeField != 0 {
		if a.TypeField < 1 {
			return opts, false, fmt.Errorf("type_field must be a positive integer (1-based), got %d", a.TypeField)
		}
		opts.TypeField = a.TypeField
	}
	for _, text := range a.CountRules {
		rule, parseErr := edilint.ParseCountRule(text)
		if parseErr != nil {
			return opts, false, fmt.Errorf("count_rules: %w", parseErr)
		}
		opts.CountRules = append(opts.CountRules, rule)
	}
	if len(a.Disable) > 0 {
		if err = edilint.ValidateSelectors(a.Disable); err != nil {
			return opts, false, fmt.Errorf("disable: %w", err)
		}
		opts.Disabled = append(opts.Disabled, a.Disable...)
	}
	switch {
	case a.MaxFindings != nil:
		if *a.MaxFindings < 0 {
			return opts, false, fmt.Errorf("max_findings must be a non-negative integer, got %d", *a.MaxFindings)
		}
		opts.MaxFindings = *a.MaxFindings
	case opts.MaxFindings == 0:
		opts.MaxFindings = defaultMaxFindings
	}
	if a.Layout != "" {
		if opts.Layout, err = edilint.LoadLayout(a.Layout); err != nil {
			return opts, false, fmt.Errorf("layout: %w", err)
		}
	}
	if opts.Format == edilint.FormatFixed && opts.Layout == nil {
		return opts, false, fmt.Errorf("format fixed requires a layout")
	}

	allowWarnings := s.AllowWarnings
	if a.AllowWarnings != nil {
		allowWarnings = *a.AllowWarnings
	}
	return opts, allowWarnings, nil
}

// lintFile answers the lint_file tool.
func (s *Server) lintFile(raw json.RawMessage) map[string]any {
	var a struct {
		lintArgs
		Paths []string `json:"paths"`
	}
	if err := decodeArgs(raw, &a); err != nil {
		return errorResult("lint_file arguments: %v", err)
	}
	if len(a.Paths) == 0 {
		return errorResult("lint_file: paths must name at least one file")
	}
	for _, p := range a.Paths {
		if p == "-" {
			return errorResult("lint_file: \"-\" would read standard input, which is the protocol channel; " +
				"pass the content to lint_text instead")
		}
	}
	opts, allowWarnings, err := s.buildOptions(a.lintArgs)
	if err != nil {
		return errorResult("lint_file: %v", err)
	}

	// An unreadable file does not discard the work already done, as on the
	// command line: every readable file is still reported and the status says
	// something was missed.
	rr := edilint.NewRunReport()
	var problems []string
	for _, path := range dedupe(a.Paths) {
		rep, err := edilint.LintFile(path, opts)
		if err != nil {
			problems = append(problems, err.Error())
			continue
		}
		rr.Add(rep)
	}
	if rr.Summary.Files == 0 {
		return errorResult("lint_file: no input could be read: %s", strings.Join(problems, "; "))
	}
	return lintResult(rr, allowWarnings, problems)
}

// lintText answers the lint_text tool.
func (s *Server) lintText(raw json.RawMessage) map[string]any {
	var a struct {
		lintArgs
		Content *string `json:"content"`
		Name    string  `json:"name"`
	}
	if err := decodeArgs(raw, &a); err != nil {
		return errorResult("lint_text arguments: %v", err)
	}
	if a.Content == nil {
		return errorResult("lint_text: content is required")
	}
	opts, allowWarnings, err := s.buildOptions(a.lintArgs)
	if err != nil {
		return errorResult("lint_text: %v", err)
	}
	name := a.Name
	if name == "" {
		name = "input"
	}
	rr := edilint.NewRunReport()
	rr.Add(edilint.Lint(name, []byte(*a.Content), opts))
	return lintResult(rr, allowWarnings, nil)
}

// lintResult renders a run report as a tool result: the text the command line
// would print, then the exit status it would return, with the JSON report as
// structured content.
func lintResult(rr *edilint.RunReport, allowWarnings bool, problems []string) map[string]any {
	if rr.Files == nil {
		rr.Files = []*edilint.Report{}
	}
	failOn := edilint.SeverityWarning
	if allowWarnings {
		failOn = edilint.SeverityError
	}
	status := 0
	switch {
	case len(problems) > 0:
		status = 2
	case !rr.OK(failOn):
		status = 1
	}

	var text bytes.Buffer
	// The buffer's Write never fails, so the renderer's error is not reachable.
	_ = rr.WriteText(&text, true)
	for _, p := range problems {
		fmt.Fprintf(&text, "error: %s\n", p)
	}
	fmt.Fprintf(&text, "exit status %d\n", status)

	structured := map[string]any{
		"ok":          status == 0,
		"exit_status": status,
		"report":      rr,
	}
	if len(problems) > 0 {
		structured["errors"] = problems
	}
	return textResult(text.String(), structured)
}

// listRules answers the list_rules tool.
func (s *Server) listRules(raw json.RawMessage) map[string]any {
	var a struct {
		Class string `json:"class"`
	}
	if err := decodeArgs(raw, &a); err != nil {
		return errorResult("list_rules arguments: %v", err)
	}
	rules := edilint.Rules()
	if a.Class != "" {
		filtered := rules[:0:0]
		for _, r := range rules {
			if r.Class == a.Class {
				filtered = append(filtered, r)
			}
		}
		if len(filtered) == 0 {
			return errorResult("list_rules: unknown class %q; classes are %s",
				a.Class, strings.Join(edilint.RuleClasses(), ", "))
		}
		rules = filtered
	}
	return textResult(rulesTable(rules), map[string]any{"rules": rules})
}

// rulesTable renders rules as the aligned table --list-rules prints.
func rulesTable(rules []edilint.RuleDoc) string {
	idWidth, nameWidth := 0, 0
	for _, r := range rules {
		idWidth = max(idWidth, len(r.ID))
		nameWidth = max(nameWidth, len(r.Name))
	}
	var b strings.Builder
	for _, r := range rules {
		fmt.Fprintf(&b, "%-*s  %-*s  %-7s  %s\n", idWidth, r.ID, nameWidth, r.Name, r.Severity, r.Summary)
	}
	return b.String()
}

// explainRule answers the explain_rule tool.
func (s *Server) explainRule(raw json.RawMessage) map[string]any {
	var a struct {
		Rule string `json:"rule"`
	}
	if err := decodeArgs(raw, &a); err != nil {
		return errorResult("explain_rule arguments: %v", err)
	}
	doc, ok := findRule(a.Rule)
	if !ok {
		return errorResult("explain_rule: unknown rule %q; expected an identifier (EL3006) or a "+
			"rule name (envelope.segment-count). list_rules prints the catalog.", a.Rule)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%s %s\n", doc.ID, doc.Name)
	fmt.Fprintf(&b, "Default severity: %s. A configuration file can re-grade it, and a rule graded info is printed but never fails a run.\n", doc.Severity)
	fmt.Fprintf(&b, "Applies to: %s\n", doc.Formats)
	fmt.Fprintf(&b, "%s\n", doc.Summary)
	fmt.Fprintf(&b, "Suppress it with --disable %s, or list it under \"disable\" in .edilint.yml. "+
		"To accept the occurrences a file already has without suppressing the rule, record them with "+
		"--write-baseline and run with --baseline.\n", doc.ID)

	return textResult(b.String(), map[string]any{
		"id":           doc.ID,
		"name":         doc.Name,
		"class":        doc.Class,
		"severity":     doc.Severity,
		"formats":      doc.Formats,
		"summary":      doc.Summary,
		"disable_flag": "--disable " + doc.ID,
	})
}

// findRule resolves an identifier or a name to its catalog entry.
func findRule(selector string) (edilint.RuleDoc, bool) {
	selector = strings.TrimSpace(selector)
	name := edilint.RuleName(selector)
	if name == "" && edilint.RuleID(selector) != "" {
		name = selector
	}
	if name == "" {
		return edilint.RuleDoc{}, false
	}
	for _, r := range edilint.Rules() {
		if r.Name == name {
			return r, true
		}
	}
	return edilint.RuleDoc{}, false
}

// dedupe drops repeated paths while preserving order, so that a file named
// twice is neither counted twice nor reported as a duplicate of itself.
func dedupe(paths []string) []string {
	seen := make(map[string]bool, len(paths))
	out := paths[:0:0]
	for _, p := range paths {
		if seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return out
}
