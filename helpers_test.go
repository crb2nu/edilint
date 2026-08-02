package edilint

import (
	"os"
	"path/filepath"
	"testing"
)

// readFixture loads a synthetic fixture from testdata.
func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return data
}

// lintFixture lints a testdata fixture with the given options.
func lintFixture(t *testing.T, name string, opts Options) *Report {
	t.Helper()
	return Lint(name, readFixture(t, name), opts)
}

// ruleNames lists the rule of every finding, for readable failure messages.
func ruleNames(rep *Report) []string {
	out := make([]string, 0, len(rep.Findings))
	for _, f := range rep.Findings {
		out = append(out, f.Rule)
	}
	return out
}

// countRule returns how many findings carry the given rule.
func countRule(rep *Report, rule string) int {
	n := 0
	for _, f := range rep.Findings {
		if f.Rule == rule {
			n++
		}
	}
	return n
}

// firstOf returns the first finding with the given rule, or nil.
func firstOf(rep *Report, rule string) *Finding {
	for i := range rep.Findings {
		if rep.Findings[i].Rule == rule {
			return &rep.Findings[i]
		}
	}
	return nil
}

// requireClean fails the test unless the report has no findings at all.
func requireClean(t *testing.T, rep *Report) {
	t.Helper()
	if len(rep.Findings) == 0 {
		return
	}
	for _, f := range rep.Findings {
		t.Errorf("unexpected finding: %s", FormatFinding(f, rep.Format))
	}
	t.FailNow()
}

// requireRule fails the test unless exactly want findings carry the given rule.
func requireRule(t *testing.T, rep *Report, rule string, want int) {
	t.Helper()
	if got := countRule(rep, rule); got != want {
		t.Fatalf("%s: got %d finding(s), want %d (all findings: %v)", rule, got, want, ruleNames(rep))
	}
}
