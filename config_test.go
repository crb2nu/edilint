package edilint

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeConfig puts a configuration file in a temporary directory.
func writeConfig(t *testing.T, name, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

func TestLoadConfigReadsEverySetting(t *testing.T) {
	path := writeConfig(t, ".edilint.yml", `version: 1
format: delimited
delimiter: "|"
charset: basic
type-field: 2
max-findings: 25
allow-warnings: true
disable:
  - EL1006
  - layout
severity:
  EL2002: info
  envelope.segment-count: warning
count-rules:
  - TRL:2:DTL
  - HDR:3:HDR
`)

	conf, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	if conf.Format != FormatDelimited {
		t.Errorf("format = %s, want delimited", conf.Format)
	}
	if conf.Delimiter != "|" {
		t.Errorf("delimiter = %q, want |", conf.Delimiter)
	}
	if conf.Charset != CharsetBasic {
		t.Errorf("charset = %s, want basic", conf.Charset)
	}
	if conf.TypeField != 2 || conf.MaxFindings != 25 || !conf.AllowWarnings {
		t.Errorf("scalars = %d, %d, %v", conf.TypeField, conf.MaxFindings, conf.AllowWarnings)
	}
	if len(conf.Disable) != 2 || conf.Disable[0] != "EL1006" || conf.Disable[1] != "layout" {
		t.Errorf("disable = %v", conf.Disable)
	}
	if len(conf.CountRules) != 2 || conf.CountRules[0].String() != "TRL:2:DTL" {
		t.Errorf("count rules = %v", conf.CountRules)
	}

	// A severity keyed by identifier is stored under the rule's name, so that a
	// lookup during a lint run never has to resolve anything.
	if got := conf.Severity[RuleMissingFinal]; got != SeverityInfo {
		t.Errorf("severity[%s] = %q, want info", RuleMissingFinal, got)
	}
	if got := conf.Severity[RuleSegmentCount]; got != SeverityWarning {
		t.Errorf("severity[%s] = %q, want warning", RuleSegmentCount, got)
	}
}

func TestConfigDistinguishesAbsentFromZero(t *testing.T) {
	// "set to zero" and "not mentioned" must not look alike, or a configuration
	// file could never turn a limit off.
	path := writeConfig(t, ".edilint.yml", "max-findings: 0\n")
	conf, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if !conf.Has("max-findings") {
		t.Error("max-findings was set to 0 and must read as present")
	}
	if conf.Has("charset") {
		t.Error("charset was not mentioned and must read as absent")
	}
}

func TestConfigApply(t *testing.T) {
	path := writeConfig(t, ".edilint.yml", `charset: basic
type-field: 3
disable:
  - EL1006
severity:
  EL2002: info
count-rules:
  - TRL:2:DTL
`)
	conf, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	// Applying onto options that already carry a suppression adds to it rather
	// than replacing it: two sources both asking for quiet should both be heard.
	opts := Options{
		Disabled:   []string{ClassLayout},
		CountRules: []CountRule{{Declaring: "HDR", Field: 2, Counted: "DTL"}},
	}
	conf.Apply(&opts)

	if opts.X12Charset != CharsetBasic || opts.TypeField != 3 {
		t.Errorf("scalars = %s, %d", opts.X12Charset, opts.TypeField)
	}
	if len(opts.Disabled) != 2 {
		t.Errorf("disabled = %v, want both entries", opts.Disabled)
	}
	if len(opts.CountRules) != 2 {
		t.Errorf("count rules = %v, want both entries", opts.CountRules)
	}
	if opts.Severities[RuleMissingFinal] != SeverityInfo {
		t.Errorf("severities = %v", opts.Severities)
	}

	// A nil configuration is the no-file case and must change nothing.
	var none *Config
	before := opts
	none.Apply(&opts)
	if len(opts.Disabled) != len(before.Disabled) {
		t.Error("applying a nil config must be a no-op")
	}
}

func TestConfigLayoutIsRelativeToTheConfigFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".edilint.yml")
	if err := os.WriteFile(path, []byte("layout: layouts/remit.json\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	conf, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	want := filepath.Join(dir, "layouts", "remit.json")
	if conf.Layout != want {
		t.Errorf("layout = %q, want %q", conf.Layout, want)
	}

	absolute := writeConfig(t, ".edilint.yml", "layout: "+filepath.Join(dir, "abs.json")+"\n")
	conf, err = LoadConfig(absolute)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if conf.Layout != filepath.Join(dir, "abs.json") {
		t.Errorf("an absolute layout path must be left alone, got %q", conf.Layout)
	}
}

func TestLoadConfigRejectsBadSettings(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{"unknown setting", "verbosity: high\n", `unknown setting "verbosity"`},
		{"unknown rule in disable", "disable:\n  - EL9999\n", `unknown rule "EL9999"`},
		{"misspelled class", "disable:\n  - charsets\n", `unknown rule "charsets"`},
		{"unknown severity", "severity:\n  EL2002: fatal\n", `unknown severity "fatal"`},
		{"class in severity", "severity:\n  charset: info\n", `unknown rule "charset"`},
		{"bad format", "format: edifact\n", "unknown format"},
		{"bad charset", "charset: strict\n", "unknown charset"},
		{"bad delimiter", "delimiter: \"||\"\n", "delimiter must be a single character"},
		{"bad count rule", "count-rules:\n  - TRL:x:DTL\n", "field index must be a positive integer"},
		{"type-field below one", "type-field: 0\n", `"type-field" must be at least 1`},
		{"negative max-findings", "max-findings: -1\n", `"max-findings" must be at least 0`},
		{"non-numeric max-findings", "max-findings: lots\n", "must be a whole number"},
		{"non-boolean allow-warnings", "allow-warnings: maybe\n", "must be true or false"},
		{"future version", "version: 2\n", "config version 2 is not supported"},
		{"scalar where a list belongs", "disable: EL1006\n", `"disable" takes a list`},
		{"list where a scalar belongs", "charset:\n  - basic\n", `"charset" takes a single value`},
		{"list where a mapping belongs", "severity:\n  - EL2002\n", `"severity" takes indented`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := writeConfig(t, ".edilint.yml", tc.body)
			_, err := LoadConfig(path)
			if err == nil {
				t.Fatalf("expected an error for:\n%s", tc.body)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %v, want it to mention %q", err, tc.want)
			}
			if !strings.Contains(err.Error(), path) {
				t.Errorf("the error should name the file, got %v", err)
			}
		})
	}
}

func TestLoadConfigReportsAMissingFile(t *testing.T) {
	_, err := LoadConfig(filepath.Join(t.TempDir(), "absent.yml"))
	if err == nil {
		t.Fatal("expected an error for a missing config")
	}
}

func TestFindConfig(t *testing.T) {
	dir := t.TempDir()
	if got := FindConfig(dir); got != "" {
		t.Errorf("an empty directory has no config, got %q", got)
	}

	// A directory of the right name is not a configuration file.
	if err := os.Mkdir(filepath.Join(dir, ".edilint.yml"), 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if got := FindConfig(dir); got != "" {
		t.Errorf("a directory must not be read as a config, got %q", got)
	}

	yaml := filepath.Join(dir, ".edilint.yaml")
	if err := os.WriteFile(yaml, []byte("charset: basic\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if got := FindConfig(dir); got != yaml {
		t.Errorf("FindConfig = %q, want %q", got, yaml)
	}
}

func TestEmptyConfigIsValid(t *testing.T) {
	// A file holding only comments is a reasonable starting point and must not
	// be an error.
	path := writeConfig(t, ".edilint.yml", "# nothing configured yet\n")
	conf, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	opts := Options{}
	conf.Apply(&opts)
	if len(opts.Disabled) != 0 || opts.X12Charset != "" {
		t.Errorf("an empty config must change nothing, got %+v", opts)
	}
}

func TestExampleConfigIsValidAndDocumented(t *testing.T) {
	// The example is what the README points at, so it has to load and to mean
	// what the README says it means.
	conf, err := LoadConfig(filepath.Join("examples", "edilint.yml"))
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if len(conf.CountRules) != 1 || conf.CountRules[0].String() != "TRL:2:DTL" {
		t.Errorf("count rules = %v", conf.CountRules)
	}
	if conf.Severity[RuleX12Padding] != SeverityInfo {
		t.Errorf("severity = %v", conf.Severity)
	}

	opts := Options{}
	conf.Apply(&opts)
	rep := Lint("eligibility.psv", readFixture(t, "eligibility_broken.psv"), opts)
	if firstOf(rep, RuleCountMismatch) == nil {
		t.Errorf("the example config should apply its count rule, got %v", ruleNames(rep))
	}
}
