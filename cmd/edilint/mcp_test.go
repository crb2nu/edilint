package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// withStdin points the mcp subcommand's input at s for the rest of the test.
func withStdin(t *testing.T, s string) {
	t.Helper()
	saved := stdin
	stdin = strings.NewReader(s)
	t.Cleanup(func() { stdin = saved })
}

const (
	initializeLine = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"t","version":"0"}}}`
	toolsListLine  = `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`
)

func lintTextLine(id int, content string) string {
	params := map[string]any{"name": "lint_text", "arguments": map[string]any{"content": content}}
	data, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": "tools/call", "params": params})
	return string(data)
}

// replies parses one JSON message per line of stdout.
func replies(t *testing.T, stdout string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSuffix(stdout, "\n"), "\n") {
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("stdout line is not JSON: %v: %q", err, line)
		}
		out = append(out, m)
	}
	return out
}

func TestMCPHelp(t *testing.T) {
	code, stdout, stderr := exec("mcp", "--help")
	if code != exitClean || stderr != "" {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	for _, want := range []string{"Usage:", "lint_file", "explain_rule", "--no-config", "--config"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("help is missing %q", want)
		}
	}

	_, help, _ := exec("--help")
	if !strings.Contains(help, "edilint mcp") {
		t.Error("the main help should mention the mcp subcommand")
	}
}

func TestMCPUsageErrors(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"unknown flag", []string{"mcp", "--bogus"}, "unknown flag"},
		{"config without value", []string{"mcp", "--config"}, "requires a value"},
		{"config and no-config", []string{"mcp", "--config=x.yml", "--no-config"}, "cannot be used together"},
		{"value on a boolean", []string{"mcp", "--no-config=1"}, "does not take a value"},
		{"missing config file", []string{"mcp", "--config", "/nonexistent/.edilint.yml"}, "read config"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withStdin(t, "")
			code, stdout, stderr := exec(tt.args...)
			if code != exitUsage {
				t.Errorf("exit = %d, want %d", code, exitUsage)
			}
			if stdout != "" {
				t.Errorf("usage errors must not write to stdout, got %q", stdout)
			}
			if !strings.Contains(stderr, tt.want) {
				t.Errorf("stderr = %q, want it to mention %q", stderr, tt.want)
			}
		})
	}
}

func TestMCPServesOverStandardStreams(t *testing.T) {
	withStdin(t, initializeLine+"\n"+toolsListLine+"\n")
	code, stdout, stderr := exec("mcp", "--no-config")
	if code != exitClean {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	if stderr != "" {
		t.Errorf("nothing should be logged for a clean session, got %q", stderr)
	}
	msgs := replies(t, stdout)
	if len(msgs) != 2 {
		t.Fatalf("got %d replies, want 2: %q", len(msgs), stdout)
	}
	init := msgs[0]["result"].(map[string]any)
	if init["protocolVersion"] != "2025-11-25" {
		t.Errorf("negotiated %v", init["protocolVersion"])
	}
	if init["serverInfo"].(map[string]any)["version"] != version {
		t.Errorf("serverInfo.version = %v, want the build's version %q", init["serverInfo"], version)
	}
	tools := msgs[1]["result"].(map[string]any)["tools"].([]any)
	if len(tools) != 4 {
		t.Errorf("got %d tools, want 4", len(tools))
	}
}

func TestMCPUsesTheConfigurationFile(t *testing.T) {
	dir := t.TempDir()
	conf := write(t, dir, ".edilint.yml", "disable:\n  - envelope\n")

	withStdin(t, lintTextLine(1, brokenX12)+"\n")
	code, stdout, stderr := exec("mcp", "--config", conf, "--verbose")
	if code != exitClean {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	if !strings.Contains(stderr, "using config "+conf) {
		t.Errorf("--verbose should name the configuration file on stderr, got %q", stderr)
	}
	msgs := replies(t, stdout)
	structured := msgs[0]["result"].(map[string]any)["structuredContent"].(map[string]any)
	if structured["exit_status"].(float64) != 0 {
		t.Errorf("the file's suppression should apply to the server: %v", structured)
	}

	// Without the file the same content is a defect.
	withStdin(t, lintTextLine(1, brokenX12)+"\n")
	_, stdout, _ = exec("mcp", "--no-config")
	structured = replies(t, stdout)[0]["result"].(map[string]any)["structuredContent"].(map[string]any)
	if structured["exit_status"].(float64) != 1 {
		t.Errorf("without the file the defect should be reported: %v", structured)
	}
}

func TestMCPConfiguredLayoutIsLoaded(t *testing.T) {
	dir := t.TempDir()
	layout := write(t, dir, "layout.json", `{"name":"t","fields":[{"name":"a","width":3}]}`)
	conf := write(t, dir, ".edilint.yml", "format: fixed\nlayout: "+filepath.Base(layout)+"\n")

	withStdin(t, lintTextLine(1, "abcd\n")+"\n")
	code, stdout, stderr := exec("mcp", "--config", conf)
	if code != exitClean {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	r := replies(t, stdout)[0]["result"].(map[string]any)
	text := r["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "EL5001") {
		t.Errorf("a record longer than the configured layout should be reported, got %q", text)
	}
}

func TestBareMCPArgumentIsTheSubcommandOnly(t *testing.T) {
	// "edilint -- mcp" names a file; no such file exists here, so the run is a
	// usage error about reading it rather than a protocol session.
	withStdin(t, initializeLine+"\n")
	code, stdout, stderr := exec("--", "mcp")
	if code != exitUsage || stdout != "" || !strings.Contains(stderr, "read mcp") {
		t.Errorf("exit = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
	if _, err := os.Stat("mcp"); err == nil {
		t.Fatal("a file named mcp exists in the test directory; the test assumes it does not")
	}
}
