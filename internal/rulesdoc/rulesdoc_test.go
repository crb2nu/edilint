package rulesdoc

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/crb2nu/edilint"
)

func TestRender(t *testing.T) {
	rule := edilint.Rules()[0]
	page := Render(rule)
	for _, want := range []string{"# " + rule.ID + " — " + rule.Name, "## What it catches", "## Example", "## How to fix", "## When to suppress", rule.Summary} {
		if !strings.Contains(page, want) {
			t.Errorf("page does not contain %q", want)
		}
	}
}

func TestGeneratedTreeIsCurrent(t *testing.T) {
	want := Files(edilint.Rules())
	dir := filepath.Join("..", "..", "docs", "rules")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	gotNames := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".md") {
			gotNames = append(gotNames, entry.Name())
		}
	}
	wantNames := make([]string, 0, len(want))
	for name := range want {
		wantNames = append(wantNames, name)
	}
	sort.Strings(gotNames)
	sort.Strings(wantNames)
	var problems []string
	if !reflect.DeepEqual(gotNames, wantNames) {
		problems = append(problems, "file set differs\n got: "+strings.Join(gotNames, ", ")+"\nwant: "+strings.Join(wantNames, ", "))
	}
	for name, expected := range want {
		actual, err := os.ReadFile(filepath.Join(dir, name))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(actual, expected) {
			problems = append(problems, name+" is stale")
		}
	}
	if len(problems) > 0 {
		t.Fatalf("generated rule documentation has drifted:\n%s\nrun `make rulesdoc`", strings.Join(problems, "\n"))
	}
}

func TestCatalogProduces49Files(t *testing.T) {
	if got := len(edilint.Rules()); got != 48 {
		t.Fatalf("catalog has %d rules, want 48", got)
	}
	if got := len(Files(edilint.Rules())); got != 49 {
		t.Fatalf("generated %d files, want 49", got)
	}
}
