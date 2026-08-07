package edilint

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strings"
	"testing"
)

// decodeSARIF unmarshals a SARIF document generically, so the assertions read
// the JSON a consumer sees rather than the structs that produced it.
func decodeSARIF(t *testing.T, data []byte) map[string]any {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("SARIF output is not valid JSON: %v", err)
	}
	return doc
}

// sarifRun returns runs[0] of a decoded document.
func sarifRunOf(t *testing.T, doc map[string]any) map[string]any {
	t.Helper()
	runs, ok := doc["runs"].([]any)
	if !ok || len(runs) != 1 {
		t.Fatalf("runs: got %v, want exactly one run", doc["runs"])
	}
	run, ok := runs[0].(map[string]any)
	if !ok {
		t.Fatalf("runs[0] is not an object: %v", runs[0])
	}
	return run
}

func TestSARIFEnvelopeAndDriver(t *testing.T) {
	rr := NewRunReport()
	rr.Add(lintFixture(t, "835_envelope_broken.x12", Options{}))

	var buf bytes.Buffer
	if err := rr.WriteSARIF(&buf, "1.2.3"); err != nil {
		t.Fatalf("WriteSARIF: %v", err)
	}
	doc := decodeSARIF(t, buf.Bytes())

	if got := doc["$schema"]; got != sarifSchemaURI {
		t.Errorf("$schema: got %v, want %s", got, sarifSchemaURI)
	}
	if got := doc["version"]; got != "2.1.0" {
		t.Errorf("version: got %v, want 2.1.0", got)
	}

	run := sarifRunOf(t, doc)
	driver := run["tool"].(map[string]any)["driver"].(map[string]any)
	if got := driver["name"]; got != "edilint" {
		t.Errorf("driver.name: got %v, want edilint", got)
	}
	if got := driver["version"]; got != "1.2.3" {
		t.Errorf("driver.version: got %v, want 1.2.3", got)
	}
	if got := driver["informationUri"]; got != sarifInformationURI {
		t.Errorf("driver.informationUri: got %v, want %s", got, sarifInformationURI)
	}
}

func TestSARIFResultsMatchFindings(t *testing.T) {
	rep := lintFixture(t, "835_envelope_broken.x12", Options{})
	if len(rep.Findings) == 0 {
		t.Fatal("fixture produced no findings; the test needs a defective input")
	}
	rr := NewRunReport()
	rr.Add(rep)

	var buf bytes.Buffer
	if err := rr.WriteSARIF(&buf, ""); err != nil {
		t.Fatalf("WriteSARIF: %v", err)
	}
	run := sarifRunOf(t, decodeSARIF(t, buf.Bytes()))

	results := run["results"].([]any)
	if len(results) != len(rep.Findings) {
		t.Fatalf("results: got %d, want %d (one per finding)", len(results), len(rep.Findings))
	}

	rules := run["tool"].(map[string]any)["driver"].(map[string]any)["rules"].([]any)
	idPattern := regexp.MustCompile(`^EL\d{4}$`)
	seen := map[string]bool{}
	for _, r := range rules {
		id := r.(map[string]any)["id"].(string)
		if seen[id] {
			t.Errorf("rule %s appears twice in the rules array", id)
		}
		seen[id] = true
	}

	for i, raw := range results {
		res := raw.(map[string]any)
		id := res["ruleId"].(string)
		if !idPattern.MatchString(id) {
			t.Errorf("result %d: ruleId %q is not an EL#### identifier", i, id)
		}
		if id != rep.Findings[i].ID {
			t.Errorf("result %d: ruleId %q, want %q", i, id, rep.Findings[i].ID)
		}

		// ruleIndex must point at the rules entry carrying the same id, or a
		// viewer resolves the wrong metadata.
		idx := int(res["ruleIndex"].(float64))
		if idx < 0 || idx >= len(rules) {
			t.Fatalf("result %d: ruleIndex %d out of range", i, idx)
		}
		if at := rules[idx].(map[string]any)["id"]; at != id {
			t.Errorf("result %d: ruleIndex %d resolves to %v, want %s", i, idx, at, id)
		}

		switch level := res["level"].(string); level {
		case "error", "warning", "note":
		default:
			t.Errorf("result %d: level %q is outside the SARIF vocabulary", i, level)
		}
		if msg := res["message"].(map[string]any)["text"].(string); msg == "" {
			t.Errorf("result %d: empty message", i)
		}

		loc := res["locations"].([]any)[0].(map[string]any)["physicalLocation"].(map[string]any)
		if uri := loc["artifactLocation"].(map[string]any)["uri"]; uri != "835_envelope_broken.x12" {
			t.Errorf("result %d: uri %v, want the fixture path", i, uri)
		}
		if region, ok := loc["region"].(map[string]any); ok {
			if line := int(region["startLine"].(float64)); line < 1 {
				t.Errorf("result %d: startLine %d, want >= 1", i, line)
			}
		} else if rep.Findings[i].Line > 0 {
			t.Errorf("result %d: finding has line %d but the result has no region",
				i, rep.Findings[i].Line)
		}
	}
}

func TestSARIFCleanRunIsEmptyButValid(t *testing.T) {
	rr := NewRunReport()
	rr.Add(lintFixture(t, "835_clean.x12", Options{}))

	var buf bytes.Buffer
	if err := rr.WriteSARIF(&buf, ""); err != nil {
		t.Fatalf("WriteSARIF: %v", err)
	}
	run := sarifRunOf(t, decodeSARIF(t, buf.Bytes()))

	// A clean run must still carry results and rules as empty arrays, never
	// null: GitHub's upload endpoint validates the document against the schema.
	if results, ok := run["results"].([]any); !ok || len(results) != 0 {
		t.Errorf("results: got %v, want []", run["results"])
	}
	driver := run["tool"].(map[string]any)["driver"].(map[string]any)
	if rules, ok := driver["rules"].([]any); !ok || len(rules) != 0 {
		t.Errorf("rules: got %v, want []", driver["rules"])
	}
	if _, ok := driver["version"]; ok {
		t.Errorf("driver.version present, want omitted when toolVersion is empty")
	}
}

func TestSARIFInfoSeverityBecomesNote(t *testing.T) {
	rep := lintFixture(t, "835_padding.x12", Options{
		Severities: map[string]Severity{RuleX12Padding: SeverityInfo},
	})
	requireRule(t, rep, RuleX12Padding, 1)
	rr := NewRunReport()
	rr.Add(rep)

	var buf bytes.Buffer
	if err := rr.WriteSARIF(&buf, ""); err != nil {
		t.Fatalf("WriteSARIF: %v", err)
	}
	if !strings.Contains(buf.String(), `"level": "note"`) {
		t.Errorf("an info finding should be SARIF level note; output:\n%s", buf.String())
	}
}

func TestFirstSentence(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"One sentence only", "One sentence only"},
		{"First. Second.", "First."},
		{"Trailing period.", "Trailing period."},
	}
	for _, tt := range tests {
		if got := firstSentence(tt.in); got != tt.want {
			t.Errorf("firstSentence(%q): got %q, want %q", tt.in, got, tt.want)
		}
	}
}
