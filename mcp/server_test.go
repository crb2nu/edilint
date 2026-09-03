package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/crb2nu/edilint"
)

// Fixtures are built here so this package stays independent of the engine's
// testdata for content it can express in a line; the fixture files are used
// only where a path is the thing under test.
const (
	cleanISA = "ISA*00*          *00*          *ZZ*NORTHGATEHEALTH*ZZ*VALEMEDGROUP   " +
		"*260115*1430*^*00501*000000001*0*P*:~\n"
	cleanGS  = "GS*HP*NORTHGATEHEALTH*VALEMEDGROUP*20260115*1430*1*X*005010X221A1~\n"
	cleanX12 = cleanISA + cleanGS + "ST*835*0001~\nBPR*I*1440.00*C*ACH*CCP~\nSE*3*0001~\nGE*1*1~\nIEA*1*000000001~\n"

	brokenISA = "ISA*00*          *00*          *ZZ*NORTHGATEHEALTH*ZZ*VALEMEDGROUP   " +
		"*260115*1430*^*00501*000000002*0*P*:~\n"
	// SE01 declares nine segments where the transaction set holds three.
	brokenX12 = brokenISA + cleanGS + "ST*835*0001~\nBPR*I*1440.00*C*ACH*CCP~\nSE*9*0001~\nGE*1*1~\nIEA*1*000000002~\n"

	// The only defect is a missing final terminator, which is a warning.
	warnOnlyPSV = "HDR|NORTHGATE|20260115\nDTL|A|1|X\nDTL|B|2|Y\nDTL|C|3|Z\nTRL|3"

	brokenFixture = "../testdata/835_envelope_broken.x12"
)

// serve feeds lines to a server and returns every message it wrote, checking
// on the way that standard output carried nothing but one message per line.
func serve(t *testing.T, srv *Server, lines ...string) []map[string]any {
	t.Helper()
	var out bytes.Buffer
	in := strings.NewReader(strings.Join(lines, "\n") + "\n")
	if err := srv.Serve(in, &out); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	if out.Len() == 0 {
		return nil
	}
	raw := out.Bytes()
	if raw[len(raw)-1] != '\n' {
		t.Fatalf("output does not end with a newline: %q", raw)
	}
	var msgs []map[string]any
	for _, line := range bytes.Split(bytes.TrimSuffix(raw, []byte("\n")), []byte("\n")) {
		if len(line) == 0 {
			t.Fatalf("blank line on the transport in %q", raw)
		}
		var m map[string]any
		if err := json.Unmarshal(line, &m); err != nil {
			t.Fatalf("output line is not JSON: %v: %q", err, line)
		}
		msgs = append(msgs, m)
	}
	return msgs
}

// one serves lines and requires exactly one reply.
func one(t *testing.T, srv *Server, lines ...string) map[string]any {
	t.Helper()
	msgs := serve(t, srv, lines...)
	if len(msgs) != 1 {
		t.Fatalf("got %d messages, want 1: %v", len(msgs), msgs)
	}
	return msgs[0]
}

func request(id any, method string, params any) string {
	m := map[string]any{"jsonrpc": "2.0", "id": id, "method": method}
	if params != nil {
		m["params"] = params
	}
	data, err := json.Marshal(m)
	if err != nil {
		panic(err)
	}
	return string(data)
}

// modern adds the per-request metadata of the 2026-07-28 revision.
func modern(id any, method string, params map[string]any, version string) string {
	if params == nil {
		params = map[string]any{}
	}
	params["_meta"] = map[string]any{
		metaProtocolVersion:    version,
		metaClientCapabilities: map[string]any{},
	}
	return request(id, method, params)
}

func call(id any, tool string, args any) string {
	return request(id, "tools/call", map[string]any{"name": tool, "arguments": args})
}

func result(t *testing.T, m map[string]any) map[string]any {
	t.Helper()
	r, ok := m["result"].(map[string]any)
	if !ok {
		t.Fatalf("no result in %v", m)
	}
	return r
}

func rpcErr(t *testing.T, m map[string]any) map[string]any {
	t.Helper()
	e, ok := m["error"].(map[string]any)
	if !ok {
		t.Fatalf("no error in %v", m)
	}
	return e
}

func obj(t *testing.T, v any) map[string]any {
	t.Helper()
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("want object, got %T: %v", v, v)
	}
	return m
}

func arr(t *testing.T, v any) []any {
	t.Helper()
	a, ok := v.([]any)
	if !ok {
		t.Fatalf("want array, got %T: %v", v, v)
	}
	return a
}

func num(t *testing.T, v any) float64 {
	t.Helper()
	n, ok := v.(float64)
	if !ok {
		t.Fatalf("want number, got %T: %v", v, v)
	}
	return n
}

// toolText returns the text content of a tool result.
func toolText(t *testing.T, r map[string]any) string {
	t.Helper()
	content := arr(t, r["content"])
	if len(content) == 0 {
		t.Fatalf("tool result has no content: %v", r)
	}
	s, ok := obj(t, content[0])["text"].(string)
	if !ok {
		t.Fatalf("first content item has no text: %v", content[0])
	}
	return s
}

func isError(r map[string]any) bool {
	b, _ := r["isError"].(bool)
	return b
}

// lintStatus reads exit_status from a lint tool's structured content.
func lintStatus(t *testing.T, r map[string]any) int {
	t.Helper()
	return int(num(t, obj(t, r["structuredContent"])["exit_status"]))
}

func firstFile(t *testing.T, r map[string]any) map[string]any {
	t.Helper()
	report := obj(t, obj(t, r["structuredContent"])["report"])
	return obj(t, arr(t, report["files"])[0])
}

func TestLegacyInitializeNegotiation(t *testing.T) {
	tests := []struct {
		requested, want string
	}{
		{"2025-11-25", "2025-11-25"},
		{"2025-06-18", "2025-06-18"},
		{"2024-11-05", "2024-11-05"},
		{"1.0.0", "2025-11-25"},
		{"", "2025-11-25"},
	}
	for _, tt := range tests {
		t.Run(tt.requested, func(t *testing.T) {
			srv := &Server{Version: "1.2.3"}
			params := map[string]any{"capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "t", "version": "0"}}
			if tt.requested != "" {
				params["protocolVersion"] = tt.requested
			}
			r := result(t, one(t, srv, request(1, "initialize", params)))
			if got := r["protocolVersion"]; got != tt.want {
				t.Errorf("protocolVersion = %v, want %s", got, tt.want)
			}
			info := obj(t, r["serverInfo"])
			if info["name"] != ServerName || info["version"] != "1.2.3" {
				t.Errorf("serverInfo = %v", info)
			}
			if _, ok := obj(t, r["capabilities"])["tools"]; !ok {
				t.Errorf("capabilities do not declare tools: %v", r["capabilities"])
			}
			if s, _ := r["instructions"].(string); s == "" {
				t.Error("instructions are empty")
			}
			if _, ok := r["resultType"]; ok {
				t.Error("a handshake-era result must not carry resultType")
			}
		})
	}
}

func TestNotificationsAreSilentAndPingAnswers(t *testing.T) {
	msgs := serve(t, &Server{},
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		request(7, "ping", nil),
		`{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":7}}`,
	)
	if len(msgs) != 1 {
		t.Fatalf("got %d messages, want only the ping reply: %v", len(msgs), msgs)
	}
	if id := msgs[0]["id"]; num(t, id) != 7 {
		t.Errorf("ping reply id = %v, want 7", id)
	}
	if r := result(t, msgs[0]); len(r) != 0 {
		t.Errorf("ping result = %v, want {}", r)
	}
}

func TestModernDiscover(t *testing.T) {
	srv := &Server{Version: "9.9.9"}
	r := result(t, one(t, srv, modern("d1", "server/discover", nil, modernVersion)))
	versions := arr(t, r["supportedVersions"])
	if len(versions) != 1 || versions[0] != modernVersion {
		t.Errorf("supportedVersions = %v", versions)
	}
	if r["resultType"] != "complete" {
		t.Errorf("resultType = %v", r["resultType"])
	}
	info := obj(t, obj(t, r["_meta"])[metaServerInfo])
	if info["name"] != ServerName || info["version"] != "9.9.9" {
		t.Errorf("serverInfo = %v", info)
	}
	if _, ok := obj(t, r["capabilities"])["tools"]; !ok {
		t.Errorf("capabilities do not declare tools: %v", r["capabilities"])
	}
}

func TestModernResultsIdentifyTheServer(t *testing.T) {
	r := result(t, one(t, &Server{}, modern(1, "tools/list", nil, modernVersion)))
	if r["resultType"] != "complete" {
		t.Errorf("resultType = %v", r["resultType"])
	}
	if _, ok := obj(t, r["_meta"])[metaServerInfo]; !ok {
		t.Errorf("result carries no serverInfo: %v", r)
	}
}

func TestModernRejectsUnknownVersion(t *testing.T) {
	e := rpcErr(t, one(t, &Server{}, modern(1, "tools/list", nil, "1900-01-01")))
	if num(t, e["code"]) != codeUnsupportedProtocolVersion {
		t.Fatalf("code = %v, want %d", e["code"], codeUnsupportedProtocolVersion)
	}
	data := obj(t, e["data"])
	if data["requested"] != "1900-01-01" {
		t.Errorf("data.requested = %v", data["requested"])
	}
	if supported := arr(t, data["supported"]); len(supported) == 0 || supported[0] != modernVersion {
		t.Errorf("data.supported = %v", supported)
	}
}

func TestModernRequiresClientCapabilities(t *testing.T) {
	line := request(1, "tools/list", map[string]any{
		"_meta": map[string]any{metaProtocolVersion: modernVersion},
	})
	e := rpcErr(t, one(t, &Server{}, line))
	if num(t, e["code"]) != codeInvalidParams {
		t.Errorf("code = %v, want %d", e["code"], codeInvalidParams)
	}
	if msg, _ := e["message"].(string); !strings.Contains(msg, metaClientCapabilities) {
		t.Errorf("message should name the missing field, got %q", msg)
	}
}

func TestToolsListIsStableAndReadOnly(t *testing.T) {
	srv := &Server{}
	first := one(t, srv, request(1, "tools/list", nil))
	second := one(t, srv, request(1, "tools/list", nil))
	a, _ := json.Marshal(first)
	b, _ := json.Marshal(second)
	if !bytes.Equal(a, b) {
		t.Error("tools/list is not deterministic across calls")
	}

	tools := arr(t, result(t, first)["tools"])
	want := []string{"lint_file", "lint_text", "list_rules", "explain_rule"}
	if len(tools) != len(want) {
		t.Fatalf("got %d tools, want %d", len(tools), len(want))
	}
	for i, v := range tools {
		tool := obj(t, v)
		if tool["name"] != want[i] {
			t.Errorf("tool %d = %v, want %s", i, tool["name"], want[i])
		}
		if s, _ := tool["description"].(string); s == "" {
			t.Errorf("%s has no description", want[i])
		}
		if obj(t, tool["inputSchema"])["type"] != "object" {
			t.Errorf("%s inputSchema is not an object schema", want[i])
		}
		ann := obj(t, tool["annotations"])
		if ann["readOnlyHint"] != true || ann["destructiveHint"] != false {
			t.Errorf("%s annotations = %v", want[i], ann)
		}
	}
}

func TestLintTextReportsFindings(t *testing.T) {
	r := result(t, one(t, &Server{}, call(1, "lint_text", map[string]any{"content": brokenX12, "name": "claims.x12"})))
	if isError(r) {
		t.Fatalf("findings must not be a tool error: %v", r)
	}
	text := toolText(t, r)
	if !strings.Contains(text, "claims.x12:") || !strings.Contains(text, "EL3006") {
		t.Errorf("text should carry a diagnostic line with the rule identifier, got %q", text)
	}
	if !strings.HasSuffix(text, "exit status 1\n") {
		t.Errorf("text should end with the exit status, got %q", text)
	}
	structured := obj(t, r["structuredContent"])
	if structured["ok"] != false || lintStatus(t, r) != 1 {
		t.Errorf("structured = %v", structured)
	}
	report := obj(t, structured["report"])
	if num(t, report["version"]) != float64(edilint.SchemaVersion) {
		t.Errorf("report version = %v, want %d", report["version"], edilint.SchemaVersion)
	}
	if firstFile(t, r)["file"] != "claims.x12" {
		t.Errorf("file = %v", firstFile(t, r)["file"])
	}
}

func TestLintTextCleanAndDefaultName(t *testing.T) {
	r := result(t, one(t, &Server{}, call(1, "lint_text", map[string]any{"content": cleanX12})))
	if lintStatus(t, r) != 0 || obj(t, r["structuredContent"])["ok"] != true {
		t.Errorf("clean input should be ok: %v", r["structuredContent"])
	}
	text := toolText(t, r)
	if !strings.Contains(text, "input: ok (x12)") || !strings.HasSuffix(text, "exit status 0\n") {
		t.Errorf("text = %q", text)
	}
	if findings := arr(t, firstFile(t, r)["findings"]); len(findings) != 0 {
		t.Errorf("findings = %v, want []", findings)
	}
}

func TestAllowWarningsPolicy(t *testing.T) {
	strict := &Server{}
	if got := lintStatus(t, result(t, one(t, strict, call(1, "lint_text", map[string]any{"content": warnOnlyPSV})))); got != 1 {
		t.Errorf("warning-only input: exit_status = %d, want 1", got)
	}
	perCall := call(1, "lint_text", map[string]any{"content": warnOnlyPSV, "allow_warnings": true})
	if got := lintStatus(t, result(t, one(t, strict, perCall))); got != 0 {
		t.Errorf("allow_warnings on the call: exit_status = %d, want 0", got)
	}
	lenient := &Server{AllowWarnings: true}
	if got := lintStatus(t, result(t, one(t, lenient, call(1, "lint_text", map[string]any{"content": warnOnlyPSV})))); got != 0 {
		t.Errorf("AllowWarnings on the server: exit_status = %d, want 0", got)
	}
	override := call(1, "lint_text", map[string]any{"content": warnOnlyPSV, "allow_warnings": false})
	if got := lintStatus(t, result(t, one(t, lenient, override))); got != 1 {
		t.Errorf("a call can turn the policy back off: exit_status = %d, want 1", got)
	}
}

func TestLintArgumentErrors(t *testing.T) {
	tests := []struct {
		name string
		args map[string]any
		want string
	}{
		{"unknown argument", map[string]any{"content": cleanX12, "bogus_option": 1}, "bogus_option"},
		{"missing content", map[string]any{"name": "x"}, "content is required"},
		{"bad format", map[string]any{"content": cleanX12, "format": "xml"}, "unknown format"},
		{"bad charset", map[string]any{"content": cleanX12, "charset": "utf-7"}, "charset"},
		{"bad count rule", map[string]any{"content": cleanX12, "count_rules": []string{"nonsense"}}, "count_rules"},
		{"unknown rule", map[string]any{"content": cleanX12, "disable": []string{"EL9999"}}, "unknown rule"},
		{"negative type_field", map[string]any{"content": cleanX12, "type_field": -1}, "type_field"},
		{"negative max_findings", map[string]any{"content": cleanX12, "max_findings": -1}, "max_findings"},
		{"missing layout file", map[string]any{"content": cleanX12, "layout": "/nonexistent/layout.json"}, "layout"},
		{"fixed without layout", map[string]any{"content": cleanX12, "format": "fixed"}, "requires a layout"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := result(t, one(t, &Server{}, call(1, "lint_text", tt.args)))
			if !isError(r) {
				t.Fatalf("want a tool error, got %v", r)
			}
			if text := toolText(t, r); !strings.Contains(text, tt.want) {
				t.Errorf("error text %q does not mention %q", text, tt.want)
			}
		})
	}
}

func TestLintFileFixtureAndTruncation(t *testing.T) {
	r := result(t, one(t, &Server{}, call(1, "lint_file", map[string]any{"paths": []string{brokenFixture}})))
	if isError(r) || lintStatus(t, r) != 1 {
		t.Fatalf("broken fixture: %v", r)
	}
	summary := obj(t, firstFile(t, r)["summary"])
	total := num(t, summary["total"])
	if total < 2 {
		t.Fatalf("fixture has %v findings; the truncation case needs at least 2", total)
	}

	capped := call(1, "lint_file", map[string]any{"paths": []string{brokenFixture}, "max_findings": 1})
	r = result(t, one(t, &Server{}, capped))
	file := firstFile(t, r)
	if got := len(arr(t, file["findings"])); got != 1 {
		t.Errorf("retained %d findings, want 1", got)
	}
	summary = obj(t, file["summary"])
	if summary["truncated"] != true || num(t, summary["total"]) != total {
		t.Errorf("summary after capping = %v; counts must stay exact", summary)
	}
	if text := toolText(t, r); !strings.Contains(text, "more findings") {
		t.Errorf("text should say findings were left out, got %q", text)
	}
}

func TestLintFileUnreadableInputs(t *testing.T) {
	missing := "/nonexistent/claims.x12"
	r := result(t, one(t, &Server{}, call(1, "lint_file", map[string]any{"paths": []string{missing}})))
	if !isError(r) {
		t.Fatalf("nothing readable should be a tool error, got %v", r)
	}

	mixed := call(1, "lint_file", map[string]any{"paths": []string{missing, brokenFixture}})
	r = result(t, one(t, &Server{}, mixed))
	if isError(r) {
		t.Fatalf("a readable file's findings must survive an unreadable sibling: %v", r)
	}
	if lintStatus(t, r) != 2 {
		t.Errorf("exit_status = %d, want 2", lintStatus(t, r))
	}
	structured := obj(t, r["structuredContent"])
	if errs := arr(t, structured["errors"]); len(errs) != 1 || !strings.Contains(errs[0].(string), missing) {
		t.Errorf("errors = %v", errs)
	}
	if files := num(t, obj(t, obj(t, structured["report"])["summary"])["files"]); files != 1 {
		t.Errorf("files = %v, want 1", files)
	}
	if text := toolText(t, r); !strings.Contains(text, "error: read "+missing) {
		t.Errorf("text should report the unreadable path, got %q", text)
	}
}

func TestLintFileRejectsStandardInput(t *testing.T) {
	r := result(t, one(t, &Server{}, call(1, "lint_file", map[string]any{"paths": []string{"-"}})))
	if !isError(r) || !strings.Contains(toolText(t, r), "lint_text") {
		t.Errorf("\"-\" should be refused with a pointer to lint_text, got %v", r)
	}
	r = result(t, one(t, &Server{}, call(1, "lint_file", map[string]any{"paths": []string{}})))
	if !isError(r) {
		t.Errorf("an empty path list should be a tool error, got %v", r)
	}
}

func TestLintFileDedupesPaths(t *testing.T) {
	r := result(t, one(t, &Server{}, call(1, "lint_file", map[string]any{"paths": []string{brokenFixture, brokenFixture}})))
	report := obj(t, obj(t, r["structuredContent"])["report"])
	if files := num(t, obj(t, report["summary"])["files"]); files != 1 {
		t.Errorf("files = %v, want 1", files)
	}
	if strings.Contains(toolText(t, r), "EL3009") {
		t.Error("a file named twice must not be reported as its own duplicate")
	}
}

func TestBaseOptionsAreInheritedNotMutated(t *testing.T) {
	srv := &Server{Options: edilint.Options{Disabled: []string{"envelope"}}}
	r := result(t, one(t, srv, call(1, "lint_file", map[string]any{"paths": []string{brokenFixture}})))
	if lintStatus(t, r) != 0 {
		t.Errorf("the server's suppression should apply: %q", toolText(t, r))
	}

	withMore := call(1, "lint_text", map[string]any{"content": cleanX12, "disable": []string{"charset"}, "count_rules": []string{"TRL:2:DTL"}})
	result(t, one(t, srv, withMore))
	if len(srv.Options.Disabled) != 1 || len(srv.Options.CountRules) != 0 {
		t.Errorf("a call leaked into the base options: %+v", srv.Options)
	}
}

func TestBuildOptionsDefaults(t *testing.T) {
	var srv Server
	opts, allow, err := srv.buildOptions(lintArgs{})
	if err != nil {
		t.Fatal(err)
	}
	if opts.MaxFindings != defaultMaxFindings {
		t.Errorf("MaxFindings = %d, want %d", opts.MaxFindings, defaultMaxFindings)
	}
	if opts.Format != edilint.FormatAuto || allow {
		t.Errorf("opts = %+v, allow = %v", opts, allow)
	}
	if opts.SeenISA13 == nil || opts.Baseline != nil {
		t.Error("each call needs a fresh duplicate map and no baseline")
	}

	srv.Options.MaxFindings = 5
	if opts, _, _ = srv.buildOptions(lintArgs{}); opts.MaxFindings != 5 {
		t.Errorf("a configured cap should win over the default, got %d", opts.MaxFindings)
	}
	zero := 0
	if opts, _, _ = srv.buildOptions(lintArgs{MaxFindings: &zero}); opts.MaxFindings != 0 {
		t.Errorf("a call can lift the cap, got %d", opts.MaxFindings)
	}
}

func TestExplainRule(t *testing.T) {
	for _, sel := range []string{"EL3006", "el3006", "envelope.segment-count", " EL3006 "} {
		t.Run(sel, func(t *testing.T) {
			r := result(t, one(t, &Server{}, call(1, "explain_rule", map[string]any{"rule": sel})))
			if isError(r) {
				t.Fatalf("%v", r)
			}
			structured := obj(t, r["structuredContent"])
			if structured["id"] != "EL3006" || structured["name"] != edilint.RuleSegmentCount {
				t.Errorf("structured = %v", structured)
			}
			text := toolText(t, r)
			for _, want := range []string{"EL3006 envelope.segment-count", "--disable EL3006", "x12",
				"Default severity: error", "999 code 4 (IK502)"} {
				if !strings.Contains(text, want) {
					t.Errorf("text is missing %q: %q", want, text)
				}
			}
			acks := arr(t, structured["acknowledgments"])
			if len(acks) != 1 || obj(t, acks[0])["code"] != "4" {
				t.Errorf("acknowledgments = %v", acks)
			}
		})
	}

	// A rule outside X12 has no acknowledgment, and says so rather than
	// omitting the field.
	r := result(t, one(t, &Server{}, call(1, "explain_rule", map[string]any{"rule": "EL6001"})))
	if acks := arr(t, obj(t, r["structuredContent"])["acknowledgments"]); len(acks) != 0 {
		t.Errorf("EL6001 acknowledgments = %v, want []", acks)
	}
	if !strings.Contains(toolText(t, r), "No X12 acknowledgment") {
		t.Errorf("EL6001 text should say there is no acknowledgment: %q", toolText(t, r))
	}
	r = result(t, one(t, &Server{}, call(1, "explain_rule", map[string]any{"rule": "EL9999"})))
	if !isError(r) || !strings.Contains(toolText(t, r), "list_rules") {
		t.Errorf("unknown rule should be an error pointing at list_rules, got %v", r)
	}
	r = result(t, one(t, &Server{}, call(1, "explain_rule", map[string]any{})))
	if !isError(r) {
		t.Errorf("a missing rule argument should be an error, got %v", r)
	}
}

func TestListRules(t *testing.T) {
	r := result(t, one(t, &Server{}, call(1, "list_rules", map[string]any{})))
	rules := arr(t, obj(t, r["structuredContent"])["rules"])
	if len(rules) != len(edilint.Rules()) {
		t.Errorf("listed %d rules, catalog has %d", len(rules), len(edilint.Rules()))
	}
	if text := toolText(t, r); !strings.Contains(text, "EL1005  charset.homoglyph") {
		t.Errorf("text table = %q", text)
	}

	r = result(t, one(t, &Server{}, call(1, "list_rules", map[string]any{"class": "charset"})))
	for _, v := range arr(t, obj(t, r["structuredContent"])["rules"]) {
		if obj(t, v)["class"] != "charset" {
			t.Errorf("filtered listing contains %v", v)
		}
	}
	r = result(t, one(t, &Server{}, call(1, "list_rules", map[string]any{"class": "bogus"})))
	if !isError(r) || !strings.Contains(toolText(t, r), "charset") {
		t.Errorf("unknown class should be an error naming the classes, got %v", r)
	}
}

func TestUnknownToolAndMethod(t *testing.T) {
	e := rpcErr(t, one(t, &Server{}, call(1, "nope", map[string]any{})))
	if num(t, e["code"]) != codeInvalidParams || !strings.Contains(e["message"].(string), "nope") {
		t.Errorf("unknown tool: %v", e)
	}
	e = rpcErr(t, one(t, &Server{}, request(2, "resources/list", nil)))
	if num(t, e["code"]) != codeMethodNotFound {
		t.Errorf("unknown method: %v", e)
	}
}

func TestMalformedInput(t *testing.T) {
	tests := []struct {
		name string
		line string
		code int
		id   any
	}{
		{"parse error", `{"jsonrpc":"2.0","id":1,`, codeParseError, nil},
		{"batch", `[{"jsonrpc":"2.0","id":1,"method":"ping"}]`, codeInvalidRequest, nil},
		{"wrong version", `{"jsonrpc":"1.0","id":3,"method":"ping"}`, codeInvalidRequest, float64(3)},
		{"null id", `{"jsonrpc":"2.0","id":null,"method":"ping"}`, codeInvalidRequest, nil},
		{"no method", `{"jsonrpc":"2.0","id":4}`, codeInvalidRequest, float64(4)},
		{"bad tools/call params", `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":[]}`, codeInvalidParams, float64(5)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := one(t, &Server{}, tt.line)
			e := rpcErr(t, m)
			if num(t, e["code"]) != float64(tt.code) {
				t.Errorf("code = %v, want %d (%v)", e["code"], tt.code, e)
			}
			if m["id"] != tt.id {
				t.Errorf("id = %v, want %v", m["id"], tt.id)
			}
		})
	}
}

func TestRequestsAreAnsweredInOrderAndInEra(t *testing.T) {
	msgs := serve(t, &Server{},
		request(1, "initialize", map[string]any{"protocolVersion": "2025-06-18"}),
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		modern("two", "tools/list", nil, modernVersion),
		request(3, "tools/list", nil),
	)
	if len(msgs) != 3 {
		t.Fatalf("got %d messages, want 3", len(msgs))
	}
	if num(t, msgs[0]["id"]) != 1 || msgs[1]["id"] != "two" || num(t, msgs[2]["id"]) != 3 {
		t.Errorf("ids out of order: %v", msgs)
	}
	if _, ok := result(t, msgs[1])["resultType"]; !ok {
		t.Error("the modern request should get a modern result")
	}
	if _, ok := result(t, msgs[2])["resultType"]; ok {
		t.Error("the handshake-era request should not get resultType")
	}
}

func TestLongLinesAreRead(t *testing.T) {
	// Beyond bufio.Scanner's default token limit, which the transport must not
	// inherit: a lint_text call carries whole files.
	var content strings.Builder
	content.WriteString("HDR|NORTHGATE|20260115\n")
	for i := 0; content.Len() < 256<<10; i++ {
		fmt.Fprintf(&content, "DTL|%d|X|Y\n", i)
	}
	r := result(t, one(t, &Server{}, call(1, "lint_text", map[string]any{"content": content.String()})))
	if isError(r) || lintStatus(t, r) != 0 {
		t.Errorf("long clean input: %v", r["structuredContent"])
	}
}

func TestLogGoesToTheLogStreamOnly(t *testing.T) {
	var log bytes.Buffer
	srv := &Server{Log: &log}
	msgs := serve(t, srv, request(1, "initialize", map[string]any{"protocolVersion": "0.1"}))
	if len(msgs) != 1 {
		t.Fatalf("got %d messages", len(msgs))
	}
	if !strings.Contains(log.String(), "0.1") {
		t.Errorf("the version fallback should be logged, got %q", log.String())
	}
}
