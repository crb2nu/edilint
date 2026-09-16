package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/crb2nu/edilint"
)

func TestReferenceCoversCatalogAndLinks(t *testing.T) {
	files, err := render()
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != len(edilint.Rules())+3 {
		t.Fatalf("generated %d files for %d rules", len(files), len(edilint.Rules()))
	}
	for _, rule := range edilint.Rules() {
		page := string(files["rules/"+rule.ID+".html"])
		for _, want := range []string{rule.ID, rule.Name, edilint.RuleURL(rule.ID), "Failing example", "How to fix it", "False positives and limits"} {
			if !strings.Contains(page, want) {
				t.Errorf("%s page missing %q", rule.ID, want)
			}
		}
	}
	links := regexp.MustCompile(`href="([^"]+)"`)
	for name, body := range files {
		for _, match := range links.FindAllSubmatch(body, -1) {
			link := string(match[1])
			if strings.HasPrefix(link, "https://") {
				continue
			}
			link, _, _ = strings.Cut(link, "#")
			target := path.Join(path.Dir(name), link)
			if _, ok := files[target]; !ok {
				t.Errorf("%s links to missing %s", name, target)
			}
		}
	}
}

func TestReferenceRequiresCompleteGuidance(t *testing.T) {
	rule := edilint.Rules()[0]
	complete := guidance{"example", "fix", "limits"}
	for _, tc := range []struct {
		name  string
		notes map[string]guidance
	}{
		{"missing rule", nil},
		{"missing example", map[string]guidance{rule.ID: {"", "fix", "limits"}}},
		{"missing fix", map[string]guidance{rule.ID: {"example", " ", "limits"}}},
		{"missing limits", map[string]guidance{rule.ID: {"example", "fix", ""}}},
		{"unknown rule", map[string]guidance{rule.ID: complete, "EL9999": complete}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := renderRules([]edilint.RuleDoc{rule}, tc.notes); err == nil {
				t.Fatal("accepted incomplete or unknown guidance")
			}
		})
	}
}

func TestReferenceEscapesContent(t *testing.T) {
	rule := edilint.Rules()[0]
	files, err := renderRules([]edilint.RuleDoc{rule}, map[string]guidance{
		rule.ID: {`<script>alert("example")</script>`, "fix", "limits"},
	})
	if err != nil {
		t.Fatal(err)
	}
	body := string(files["rules/"+rule.ID+".html"])
	if strings.Contains(body, "<script>") || !strings.Contains(body, "&lt;script&gt;") {
		t.Fatal("example was not escaped as HTML text")
	}
}

func TestReferenceGenerationAndDrift(t *testing.T) {
	dir := t.TempDir()
	if err := run(dir, true); err == nil {
		t.Fatal("missing reference passed check")
	}
	if err := run(dir, false); err != nil {
		t.Fatal(err)
	}
	if err := run(dir, true); err != nil {
		t.Fatal(err)
	}
	page := filepath.Join(dir, "rules", "EL1001.html")
	original, err := os.ReadFile(page)
	if err != nil {
		t.Fatal(err)
	}
	if err = run(dir, false); err != nil {
		t.Fatal(err)
	}
	regenerated, err := os.ReadFile(page)
	if err != nil || !bytes.Equal(original, regenerated) {
		t.Fatalf("generation is not deterministic: %v", err)
	}
	if err = os.WriteFile(page, []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err = run(dir, true); err == nil || !strings.Contains(err.Error(), "EL1001.html") {
		t.Fatalf("stale page check: %v", err)
	}
	actual, err := os.ReadFile(page)
	if err != nil || string(actual) != "stale" {
		t.Fatalf("check mode must not repair drift: %v", err)
	}
}

func TestCommittedReferenceIsCurrent(t *testing.T) {
	if err := run(filepath.Join("..", "..", "docs"), true); err != nil {
		t.Fatal(err)
	}
}

func TestGuidanceJSONFields(t *testing.T) {
	body, err := sources.ReadFile("guidance.json")
	if err != nil {
		t.Fatal(err)
	}
	var notes map[string]guidance
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&notes); err != nil {
		t.Fatal(err)
	}
}
