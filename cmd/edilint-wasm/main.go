//go:build js && wasm

// Command edilint-wasm exposes the linter to a browser page as a handful of
// JavaScript globals. It is the third front end after the command line and the
// MCP server, and it shares their contract: bytes in, the same JSON report
// document out, no filesystem, no network. Nothing a page pastes into it
// leaves the page.
//
// Build with:
//
//	GOOS=js GOARCH=wasm go build -o edilint.wasm ./cmd/edilint-wasm
//
// and load it with the wasm_exec.js that ships in the Go distribution. Once
// the program is running the page finds these functions on globalThis:
//
//	edilintVersion()                      -> string
//	edilintRules()                        -> JSON string: {rules:[RuleDoc]}
//	edilintExplain(selector)              -> JSON string: rule doc + acknowledgments, or {error}
//	edilintLint(text, name, optionsJSON)  -> JSON string: {ok, exit_status, text, report}
//	edilintFmt(text, format)              -> JSON string: {output} or {error}
//	edilintFix(text, format, unsafe)      -> JSON string: {output, repairs, diff, changed}
//
// Every function takes and returns strings so the page never has to reach
// into WebAssembly memory. Text goes in as JavaScript strings, which means
// UTF-8 in and out; a page that needs to lint raw bytes should decode them
// itself first, the way a browser does when it reads a file as text.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"runtime/debug"
	"strings"
	"syscall/js"

	"github.com/crb2nu/edilint"
)

// version is set from the module's build information when the binary is
// built without release ldflags, the same way the command line reports it.
var version = "dev"

// defaultMaxFindings mirrors the MCP server: a paste box never needs every
// finding of a pathological file, and the summary counts stay exact.
const defaultMaxFindings = 200

func main() {
	if version == "dev" {
		if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
			version = strings.TrimPrefix(info.Main.Version, "v")
		}
	}

	js.Global().Set("edilintVersion", js.FuncOf(func(js.Value, []js.Value) any { return version }))
	js.Global().Set("edilintRules", js.FuncOf(func(js.Value, []js.Value) any { return rules() }))
	js.Global().Set("edilintExplain", js.FuncOf(func(_ js.Value, args []js.Value) any {
		return explain(argString(args, 0))
	}))
	js.Global().Set("edilintLint", js.FuncOf(func(_ js.Value, args []js.Value) any {
		return lint(argString(args, 0), argString(args, 1), argString(args, 2))
	}))
	js.Global().Set("edilintFmt", js.FuncOf(func(_ js.Value, args []js.Value) any {
		return format(argString(args, 0), argString(args, 1))
	}))
	js.Global().Set("edilintFix", js.FuncOf(func(_ js.Value, args []js.Value) any {
		return fix(argString(args, 0), argString(args, 1), argBool(args, 2))
	}))
	js.Global().Set("edilintReady", js.ValueOf(true))

	// Keep the program alive; the page calls in through the globals above.
	select {}
}

func argString(args []js.Value, i int) string {
	if i >= len(args) || args[i].IsUndefined() || args[i].IsNull() {
		return ""
	}
	return args[i].String()
}

func argBool(args []js.Value, i int) bool {
	if i >= len(args) || args[i].Type() != js.TypeBoolean {
		return false
	}
	return args[i].Bool()
}

// encode renders a result document. Marshalling these maps cannot fail, so the
// fallback exists only to keep the contract of "always a JSON string".
func encode(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf(`{"error":%q}`, err.Error())
	}
	return string(data)
}

func errorDoc(format string, args ...any) string {
	return encode(map[string]any{"error": fmt.Sprintf(format, args...)})
}

// lintOptions is the subset of Options a page can set. Field names match the
// MCP server's lint_text arguments so the three front ends share one vocabulary.
type lintOptions struct {
	Format        string   `json:"format"`
	Delimiter     string   `json:"delimiter"`
	Charset       string   `json:"charset"`
	TypeField     int      `json:"type_field"`
	CountRules    []string `json:"count_rules"`
	Disable       []string `json:"disable"`
	MaxFindings   *int     `json:"max_findings"`
	AllowWarnings bool     `json:"allow_warnings"`
}

// buildOptions validates page-supplied options the way the MCP server does.
// Layouts and baselines are file-backed and have no browser equivalent.
func buildOptions(a lintOptions) (edilint.Options, error) {
	opts := edilint.Options{
		Format:    edilint.FormatAuto,
		SeenISA13: map[string]string{},
	}
	var err error
	if a.Format != "" {
		if opts.Format, err = edilint.ParseFormat(a.Format); err != nil {
			return opts, fmt.Errorf("format: %w", err)
		}
	}
	if a.Delimiter != "" {
		if _, err = edilint.ParseDelimiter(a.Delimiter); err != nil {
			return opts, fmt.Errorf("delimiter: %w", err)
		}
		opts.Delimiter = a.Delimiter
	}
	if a.Charset != "" {
		if opts.X12Charset, err = edilint.ParseCharsetProfile(a.Charset); err != nil {
			return opts, fmt.Errorf("charset: %w", err)
		}
	}
	if a.TypeField != 0 {
		if a.TypeField < 1 {
			return opts, fmt.Errorf("type_field must be a positive integer (1-based), got %d", a.TypeField)
		}
		opts.TypeField = a.TypeField
	}
	for _, text := range a.CountRules {
		rule, parseErr := edilint.ParseCountRule(text)
		if parseErr != nil {
			return opts, fmt.Errorf("count_rules: %w", parseErr)
		}
		opts.CountRules = append(opts.CountRules, rule)
	}
	if len(a.Disable) > 0 {
		if err = edilint.ValidateSelectors(a.Disable); err != nil {
			return opts, fmt.Errorf("disable: %w", err)
		}
		opts.Disabled = append(opts.Disabled, a.Disable...)
	}
	switch {
	case a.MaxFindings != nil:
		if *a.MaxFindings < 0 {
			return opts, fmt.Errorf("max_findings must be a non-negative integer, got %d", *a.MaxFindings)
		}
		opts.MaxFindings = *a.MaxFindings
	default:
		opts.MaxFindings = defaultMaxFindings
	}
	if opts.Format == edilint.FormatFixed {
		return opts, fmt.Errorf("format fixed requires a layout file, which the browser build does not load")
	}
	return opts, nil
}

func lint(text, name, optionsJSON string) string {
	var a lintOptions
	if strings.TrimSpace(optionsJSON) != "" {
		if err := json.Unmarshal([]byte(optionsJSON), &a); err != nil {
			return errorDoc("options: %v", err)
		}
	}
	opts, err := buildOptions(a)
	if err != nil {
		return errorDoc("%v", err)
	}
	if name == "" {
		name = "input"
	}

	rr := edilint.NewRunReport()
	rr.Add(edilint.Lint(name, []byte(text), opts))
	if rr.Files == nil {
		rr.Files = []*edilint.Report{}
	}
	failOn := edilint.SeverityWarning
	if a.AllowWarnings {
		failOn = edilint.SeverityError
	}
	status := 0
	if !rr.OK(failOn) {
		status = 1
	}

	var out bytes.Buffer
	// The buffer's Write never fails, so the renderer's error is not reachable.
	_ = rr.WriteText(&out, true)
	fmt.Fprintf(&out, "exit status %d\n", status)

	return encode(map[string]any{
		"ok":          status == 0,
		"exit_status": status,
		"text":        out.String(),
		"report":      rr,
	})
}

func rules() string {
	return encode(map[string]any{"rules": edilint.Rules()})
}

func explain(selector string) string {
	doc, ok := findRule(selector)
	if !ok {
		return errorDoc("unknown rule %q; expected an identifier (EL3006) or a rule name (envelope.segment-count)", selector)
	}
	acks := doc.Acks
	if acks == nil {
		// A rule with no acknowledgment must still marshal as an empty array.
		acks = []edilint.Ack{}
	}
	return encode(map[string]any{
		"id":              doc.ID,
		"name":            doc.Name,
		"class":           doc.Class,
		"severity":        doc.Severity,
		"formats":         doc.Formats,
		"summary":         doc.Summary,
		"help":            edilint.RuleHelp(doc),
		"acknowledgments": acks,
		"disable_flag":    "--disable " + doc.ID,
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

// resolveFormat turns a page's format choice into the concrete format fmt and
// fix need: auto-detect with default options when the page did not choose.
func resolveFormat(text, format string) (edilint.Format, error) {
	f := edilint.FormatAuto
	if format != "" {
		var err error
		if f, err = edilint.ParseFormat(format); err != nil {
			return f, fmt.Errorf("format: %w", err)
		}
	}
	if f == edilint.FormatAuto {
		f = edilint.Detect([]byte(text), edilint.Options{Format: edilint.FormatAuto})
	}
	return f, nil
}

func format(text, format string) string {
	f, err := resolveFormat(text, format)
	if err != nil {
		return errorDoc("%v", err)
	}
	out, err := edilint.Canonical([]byte(text), f)
	if err != nil {
		return errorDoc("%v", err)
	}
	return encode(map[string]any{
		"format":  f,
		"output":  string(out),
		"changed": !bytes.Equal(out, []byte(text)),
	})
}

func fix(text, format string, unsafe bool) string {
	f, err := resolveFormat(text, format)
	if err != nil {
		return errorDoc("%v", err)
	}
	out, repairs := edilint.Fix([]byte(text), edilint.FixOptions{Format: f, Unsafe: unsafe})
	if repairs == nil {
		repairs = []edilint.Repair{}
	}
	return encode(map[string]any{
		"format":  f,
		"output":  string(out),
		"repairs": repairs,
		"diff":    edilint.UnifiedDiff("input", []byte(text), out),
		"changed": !bytes.Equal(out, []byte(text)),
		"unsafe":  unsafe,
	})
}
