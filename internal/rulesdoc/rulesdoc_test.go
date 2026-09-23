package rulesdoc

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/crb2nu/edilint"
)

// treeDir is the committed tree the generator writes and the drift guard reads.
const treeDir = "../../docs/rules"

func TestRenderHasEverySection(t *testing.T) {
	for _, rule := range edilint.Rules() {
		page := Render(rule)
		want := []string{
			"# " + rule.ID + " — " + rule.Name,
			"| " + rule.Class + " | " + string(rule.Severity) + " | ",
			rule.Summary,
			"## What it catches",
			"## Example",
			"## How to fix",
			"## When to suppress",
			"edilint --disable " + rule.ID,
		}
		for _, w := range want {
			if !strings.Contains(page, w) {
				t.Errorf("%s page is missing %q", rule.ID, w)
			}
		}
		if !strings.HasSuffix(page, "\n") || strings.Contains(page, "\n\n\n") {
			t.Errorf("%s page has irregular blank lines", rule.ID)
		}
	}
}

func TestEveryRuleHasDetail(t *testing.T) {
	table := details()
	for _, rule := range edilint.Rules() {
		detail, ok := table[rule.ID]
		if !ok {
			t.Errorf("%s (%s) has no entry in details()", rule.ID, rule.Name)
			continue
		}
		for name, text := range map[string]string{"catches": detail.catches, "fix": detail.fix, "suppress": detail.suppress} {
			if strings.TrimSpace(text) == "" {
				t.Errorf("%s has an empty %s", rule.ID, name)
			}
		}
		if detail.example.file == "" || detail.example.body == "" {
			t.Errorf("%s has no example input", rule.ID)
		}
	}
	known := map[string]bool{}
	for _, rule := range edilint.Rules() {
		known[rule.ID] = true
	}
	for id := range table {
		if !known[id] {
			t.Errorf("details() has an entry for %s, which is not in the catalog", id)
		}
	}
}

// TestExamplesProduceTheirRule is the constraint the pages rest on: every
// documented input, linted with the options its command line shows, really does
// report the rule the page is about.
func TestExamplesProduceTheirRule(t *testing.T) {
	for _, rule := range edilint.Rules() {
		detail := details()[rule.ID]
		rep := detail.example.lint()
		var got []string
		found := false
		for _, f := range rep.Findings {
			got = append(got, f.ID)
			if f.ID == rule.ID {
				found = true
			}
		}
		if !found {
			t.Errorf("example for %s (%s) reports %v, not %s", rule.ID, rule.Name, got, rule.ID)
		}
	}
}

// TestExampleDisplayMatchesBody keeps an escaped display honest: unescaping the
// Go escapes it uses must yield the bytes that were linted.
func TestExampleDisplayMatchesBody(t *testing.T) {
	for _, rule := range edilint.Rules() {
		ex := details()[rule.ID].example
		if ex.display == "" {
			continue
		}
		if unescape(ex.display) != ex.body {
			t.Errorf("%s: display does not unescape to the linted body", rule.ID)
		}
	}
}

// TestExampleArgsExpressOptions checks that the command line a page shows
// carries the flags the options stand for.
func TestExampleArgsExpressOptions(t *testing.T) {
	for _, rule := range edilint.Rules() {
		ex := details()[rule.ID].example
		if ex.opts.Format != "" && !strings.Contains(ex.args, "--format "+string(ex.opts.Format)) {
			t.Errorf("%s: options force format %q but the command line is %q", rule.ID, ex.opts.Format, ex.command())
		}
		if ex.opts.Layout != nil && !strings.Contains(ex.args, "--layout ") {
			t.Errorf("%s: options carry a layout but the command line is %q", rule.ID, ex.command())
		}
		if len(ex.opts.CountRules) > 0 && !strings.Contains(ex.args, "--count-rule ") {
			t.Errorf("%s: options carry a count rule but the command line is %q", rule.ID, ex.command())
		}
		if ex.opts.X12Charset != "" && !strings.Contains(ex.args, "--charset "+string(ex.opts.X12Charset)) {
			t.Errorf("%s: options select charset %q but the command line is %q", rule.ID, ex.opts.X12Charset, ex.command())
		}
	}
}

// TestGeneratedTreeIsCurrent is the drift guard: the committed tree must be
// byte for byte what the generator produces now.
func TestGeneratedTreeIsCurrent(t *testing.T) {
	want := Files(edilint.Rules())

	entries, err := os.ReadDir(treeDir)
	if err != nil {
		t.Fatalf("read %s: %v", treeDir, err)
	}
	present := map[string]bool{}
	for _, entry := range entries {
		if !entry.IsDir() && Owns(entry.Name()) {
			present[entry.Name()] = true
		}
	}

	var missing, stale, extra []string
	for name, expected := range want {
		actual, err := os.ReadFile(filepath.Join(treeDir, name))
		switch {
		case os.IsNotExist(err):
			missing = append(missing, name)
		case err != nil:
			t.Fatalf("read %s: %v", name, err)
		case !bytes.Equal(actual, expected):
			stale = append(stale, name)
		}
	}
	for name := range present {
		if _, ok := want[name]; !ok {
			extra = append(extra, name)
		}
	}

	var problems []string
	for label, names := range map[string][]string{"missing": missing, "stale": stale, "extra": extra} {
		if len(names) > 0 {
			sort.Strings(names)
			problems = append(problems, label+": "+strings.Join(names, ", "))
		}
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		t.Fatalf("docs/rules has drifted from the rule catalog:\n  %s\nrun `%s` to regenerate it",
			strings.Join(problems, "\n  "), regenerate)
	}
}

// TestTreeIsOnePagePerRulePlusIndex states the count the spec asks for
// separately from the byte comparison, so a count mistake names itself.
func TestTreeIsOnePagePerRulePlusIndex(t *testing.T) {
	rules := edilint.Rules()
	if got, want := len(Files(rules)), len(rules)+1; got != want {
		t.Fatalf("generator produced %d files for %d rules, want %d", got, len(rules), want)
	}
	entries, err := os.ReadDir(treeDir)
	if err != nil {
		t.Fatal(err)
	}
	pages := 0
	for _, entry := range entries {
		if !entry.IsDir() && Owns(entry.Name()) {
			pages++
		}
	}
	if want := len(rules) + 1; pages != want {
		t.Fatalf("%s holds %d Markdown files for %d rules, want %d", treeDir, pages, len(rules), want)
	}
}

// TestIndexLinksEveryPage checks that each catalog row in the index points at a
// page the generator also writes.
func TestIndexLinksEveryPage(t *testing.T) {
	rules := edilint.Rules()
	index := RenderIndex(rules)
	files := Files(rules)
	for _, rule := range rules {
		link := "[" + rule.ID + "](" + rule.ID + ".md)"
		if !strings.Contains(index, link) {
			t.Errorf("index does not link %s", link)
		}
		if _, ok := files[rule.ID+".md"]; !ok {
			t.Errorf("index links %s but the generator does not write it", rule.ID+".md")
		}
	}
}

// unescape resolves the Go escapes an example's display may use. It handles
// only the forms the table actually contains.
func unescape(s string) string {
	r := strings.NewReplacer(
		`\ufeff`, "\ufeff",
		`\u200b`, "\u200b",
		`\u0410`, "\u0410",
		`\xff`, "\xff",
		`\x07`, "\x07",
		`\r\n`+"\n", "\r\n",
		`\n`+"\n", "\n",
	)
	return r.Replace(s)
}
