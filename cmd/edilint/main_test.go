package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The CLI tests build their own fixtures so that this package stays independent
// of the engine's testdata.
const (
	cleanISA = "ISA*00*          *00*          *ZZ*NORTHGATEHEALTH*ZZ*VALEMEDGROUP   " +
		"*260115*1430*^*00501*000000001*0*P*:~\n"
	cleanGS  = "GS*HP*NORTHGATEHEALTH*VALEMEDGROUP*20260115*1430*1*X*005010X221A1~\n"
	cleanX12 = cleanISA + cleanGS + "ST*835*0001~\nBPR*I*1440.00*C*ACH*CCP~\nSE*3*0001~\nGE*1*1~\nIEA*1*000000001~\n"

	// A distinct ISA13 keeps this file independent of cleanX12, so that tests
	// using both do not also trip the duplicate-control-number rule.
	brokenISA = "ISA*00*          *00*          *ZZ*NORTHGATEHEALTH*ZZ*VALEMEDGROUP   " +
		"*260115*1430*^*00501*000000002*0*P*:~\n"
	// SE01 declares nine segments where the transaction set holds three.
	brokenX12 = brokenISA + cleanGS + "ST*835*0001~\nBPR*I*1440.00*C*ACH*CCP~\nSE*9*0001~\nGE*1*1~\nIEA*1*000000002~\n"

	cleanPSV = "HDR|NORTHGATE|20260115\nDTL|A|1|X\nDTL|B|2|Y\nDTL|C|3|Z\nTRL|3\n"
	// The trailer declares four detail records but only three are present.
	badCountPSV = "HDR|NORTHGATE|20260115\nDTL|A|1|X\nDTL|B|2|Y\nDTL|C|3|Z\nTRL|4\n"
	// Only defect is a missing final terminator, which is a warning.
	warnOnlyPSV = "HDR|NORTHGATE|20260115\nDTL|A|1|X\nDTL|B|2|Y\nDTL|C|3|Z\nTRL|3"
)

// write creates a fixture file in dir and returns its path.
func write(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

// exec runs the CLI and returns its exit code plus captured output.
func exec(args ...string) (code int, stdout, stderr string) {
	var out, errBuf bytes.Buffer
	code = run(args, &out, &errBuf)
	return code, out.String(), errBuf.String()
}

func TestExitCodes(t *testing.T) {
	dir := t.TempDir()
	clean := write(t, dir, "clean.x12", cleanX12)
	broken := write(t, dir, "broken.x12", brokenX12)
	warn := write(t, dir, "warn.psv", warnOnlyPSV)

	tests := []struct {
		name string
		args []string
		want int
	}{
		{"clean file", []string{clean}, exitClean},
		{"file with findings", []string{broken}, exitFindings},
		{"warnings fail by default", []string{warn}, exitFindings},
		{"warnings tolerated with --allow-warnings", []string{"--allow-warnings", warn}, exitClean},
		{"errors still fail with --allow-warnings", []string{"--allow-warnings", broken}, exitFindings},
		{"no input files", nil, exitUsage},
		{"unreadable file", []string{filepath.Join(dir, "absent.x12")}, exitUsage},
		{"unknown flag", []string{"--nope", clean}, exitUsage},
		{"flag missing its value", []string{"--format"}, exitUsage},
		{"invalid format", []string{"--format", "edifact", clean}, exitUsage},
		{"invalid count rule", []string{"--count-rule", "TRL:x:DTL", clean}, exitUsage},
		{"fixed without a layout", []string{"--format", "fixed", clean}, exitUsage},
		{"boolean flag given a value", []string{"--json=yes", clean}, exitUsage},
		{"help", []string{"--help"}, exitClean},
		{"version", []string{"--version"}, exitClean},
		{"list rules", []string{"--list-rules"}, exitClean},
		{"one clean and one broken", []string{clean, broken}, exitFindings},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, _, _ := exec(tc.args...)
			if got != tc.want {
				t.Errorf("exit = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestCleanRunIsSilent(t *testing.T) {
	clean := write(t, t.TempDir(), "clean.x12", cleanX12)

	code, stdout, stderr := exec(clean)
	if code != exitClean {
		t.Fatalf("exit = %d, want %d (stderr: %s)", code, exitClean, stderr)
	}
	if stdout != "" {
		t.Errorf("a clean run should print nothing, got %q", stdout)
	}

	_, verbose, _ := exec("-v", clean)
	if !strings.Contains(verbose, "ok (x12)") {
		t.Errorf("verbose output should confirm the file, got %q", verbose)
	}
}

func TestFindingsGoToStdoutInDiagnosticForm(t *testing.T) {
	broken := write(t, t.TempDir(), "broken.x12", brokenX12)

	code, stdout, _ := exec(broken)
	if code != exitFindings {
		t.Fatalf("exit = %d, want %d", code, exitFindings)
	}
	line := strings.SplitN(stdout, "\n", 2)[0]
	if !strings.HasPrefix(line, broken+":") {
		t.Errorf("finding should start with the file path, got %q", line)
	}
	if !strings.Contains(line, "error: [envelope.segment-count]") {
		t.Errorf("finding should carry severity and rule, got %q", line)
	}
	if !strings.Contains(stdout, "1 file checked, 1 finding") {
		t.Errorf("output should end with a summary, got %q", stdout)
	}
}

func TestJSONOutput(t *testing.T) {
	dir := t.TempDir()
	clean := write(t, dir, "clean.x12", cleanX12)
	broken := write(t, dir, "broken.x12", brokenX12)

	code, stdout, _ := exec("--json", clean, broken)
	if code != exitFindings {
		t.Fatalf("exit = %d, want %d", code, exitFindings)
	}

	var doc struct {
		Version int `json:"version"`
		Files   []struct {
			File     string `json:"file"`
			Format   string `json:"format"`
			Findings []struct {
				Rule     string `json:"rule"`
				Class    string `json:"class"`
				Severity string `json:"severity"`
				Message  string `json:"message"`
				Line     int    `json:"line"`
			} `json:"findings"`
		} `json:"files"`
		Summary struct {
			Files  int `json:"files"`
			Total  int `json:"total"`
			Errors int `json:"errors"`
		} `json:"summary"`
	}
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, stdout)
	}
	if doc.Version != 2 {
		t.Errorf("version = %d, want 2", doc.Version)
	}
	if len(doc.Files) != 2 || doc.Summary.Files != 2 {
		t.Fatalf("expected 2 file reports, got %d", len(doc.Files))
	}
	if doc.Files[0].Format != "x12" {
		t.Errorf("format = %q, want x12", doc.Files[0].Format)
	}
	if len(doc.Files[0].Findings) != 0 {
		t.Errorf("the clean file should have no findings")
	}
	f := doc.Files[1].Findings[0]
	if f.Rule != "envelope.segment-count" || f.Class != "envelope" || f.Severity != "error" {
		t.Errorf("unexpected finding: %+v", f)
	}
}

func TestJSONOutputIsValidWhenClean(t *testing.T) {
	clean := write(t, t.TempDir(), "clean.x12", cleanX12)
	code, stdout, _ := exec("--json", clean)
	if code != exitClean {
		t.Fatalf("exit = %d, want %d", code, exitClean)
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, stdout)
	}
}

func TestCountRuleFlag(t *testing.T) {
	dir := t.TempDir()
	good := write(t, dir, "good.psv", cleanPSV)
	bad := write(t, dir, "bad.psv", badCountPSV)

	if code, _, _ := exec("--count-rule", "TRL:2:DTL", good); code != exitClean {
		t.Errorf("clean extract exit = %d, want %d", code, exitClean)
	}

	code, stdout, _ := exec("--count-rule", "TRL:2:DTL", bad)
	if code != exitFindings {
		t.Fatalf("exit = %d, want %d", code, exitFindings)
	}
	if !strings.Contains(stdout, "counts.mismatch") {
		t.Errorf("expected a count mismatch, got %q", stdout)
	}

	// Without the rule the same file is clean, because nothing declares a count.
	if code, _, _ := exec(bad); code != exitClean {
		t.Errorf("exit without a count rule = %d, want %d", code, exitClean)
	}
}

func TestLayoutFlag(t *testing.T) {
	dir := t.TempDir()
	layout := write(t, dir, "layout.json", `{
  "name": "detail",
  "fields": [
    {"name": "record_type", "width": 3},
    {"name": "member_id", "width": 8, "pad": "right"},
    {"name": "amount", "width": 6, "pad": "left", "padChar": "0"}
  ]
}`)
	good := write(t, dir, "good.txt", "DTLNGH00001001440\nDTLNGH00002000965\n")
	short := write(t, dir, "short.txt", "DTLNGH00001001440\nDTLNGH0000200096\n")

	if code, _, stderr := exec("--format", "fixed", "--layout", layout, good); code != exitClean {
		t.Errorf("clean fixed-width exit = %d, want %d (%s)", code, exitClean, stderr)
	}

	code, stdout, _ := exec("--format", "fixed", "--layout", layout, short)
	if code != exitFindings {
		t.Fatalf("exit = %d, want %d", code, exitFindings)
	}
	if !strings.Contains(stdout, "layout.length") {
		t.Errorf("expected a length finding, got %q", stdout)
	}

	// A malformed layout is a usage error, not a finding.
	badLayout := write(t, dir, "bad.json", `{"fields":[{"name":"a","width":0}]}`)
	if code, _, _ := exec("--format", "fixed", "--layout", badLayout, good); code != exitUsage {
		t.Errorf("bad layout exit = %d, want %d", code, exitUsage)
	}
}

func TestDisableFlag(t *testing.T) {
	broken := write(t, t.TempDir(), "broken.x12", brokenX12)

	if code, _, _ := exec("--disable", "envelope.segment-count", broken); code != exitClean {
		t.Error("disabling the only failing rule should leave the file clean")
	}
	if code, _, _ := exec("--disable", "envelope", broken); code != exitClean {
		t.Error("disabling the class should leave the file clean")
	}
	if code, _, _ := exec("--disable", "charset,envelope", broken); code != exitClean {
		t.Error("a comma-separated list should disable every entry")
	}
	if code, _, _ := exec("--disable", "charset", broken); code != exitFindings {
		t.Error("disabling an unrelated class should not hide the envelope finding")
	}
}

func TestFlagsAreAcceptedInAnyPosition(t *testing.T) {
	broken := write(t, t.TempDir(), "broken.x12", brokenX12)

	variants := [][]string{
		{"--json", broken},
		{broken, "--json"},
		{"--json", "--disable", "charset", broken},
		{broken, "--disable=charset", "--json"},
	}
	for _, args := range variants {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			code, stdout, stderr := exec(args...)
			if code != exitFindings {
				t.Fatalf("exit = %d, want %d (stderr: %s)", code, exitFindings, stderr)
			}
			if !strings.HasPrefix(strings.TrimSpace(stdout), "{") {
				t.Errorf("--json should have been honoured, got %q", stdout)
			}
		})
	}
}

func TestDoubleDashEndsFlagParsing(t *testing.T) {
	dir := t.TempDir()
	odd := write(t, dir, "--weird.x12", cleanX12)
	code, _, stderr := exec("--", odd)
	if code != exitClean {
		t.Errorf("exit = %d, want %d (stderr: %s)", code, exitClean, stderr)
	}
}

func TestFormatOverride(t *testing.T) {
	// Forcing text disables the envelope checks that would otherwise fire.
	broken := write(t, t.TempDir(), "broken.x12", brokenX12)
	if code, _, _ := exec("--format", "text", broken); code != exitClean {
		t.Error("--format text should skip the X12 envelope checks")
	}
	if code, _, _ := exec("--format", "x12", broken); code != exitFindings {
		t.Error("--format x12 should run the envelope checks")
	}
}

func TestDuplicateControlNumbersAcrossFiles(t *testing.T) {
	dir := t.TempDir()
	a := write(t, dir, "monday.x12", cleanX12)
	b := write(t, dir, "tuesday.x12", cleanX12)

	// Each file is clean on its own.
	if code, _, _ := exec(a); code != exitClean {
		t.Fatal("the fixture should be clean in isolation")
	}

	code, stdout, _ := exec(a, b)
	if code != exitFindings {
		t.Fatalf("exit = %d, want %d", code, exitFindings)
	}
	if !strings.Contains(stdout, "envelope.duplicate-control-number") {
		t.Errorf("expected a duplicate ISA13 across the batch, got %q", stdout)
	}
	if !strings.Contains(stdout, "monday.x12") {
		t.Errorf("the finding should name the earlier file, got %q", stdout)
	}
}

func TestMaxFindingsFlag(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 30; i++ {
		b.WriteString("DTL|RIVERA\x0bDANA|X\n")
	}
	noisy := write(t, t.TempDir(), "noisy.psv", b.String())

	t.Run("unlimited by default", func(t *testing.T) {
		_, stdout, _ := exec(noisy)
		if got := strings.Count(stdout, "charset.nonprintable"); got != 30 {
			t.Errorf("printed %d findings, want all 30", got)
		}
		if strings.Contains(stdout, "suppressed by --max-findings") {
			t.Errorf("nothing should be suppressed by default, got %q", stdout)
		}
	})

	t.Run("capped output", func(t *testing.T) {
		code, stdout, _ := exec("--max-findings", "5", noisy)
		if got := strings.Count(stdout, "charset.nonprintable"); got != 5 {
			t.Errorf("printed %d findings, want 5", got)
		}
		if !strings.Contains(stdout, "... and 25 more findings (suppressed by --max-findings)") {
			t.Errorf("truncation should be announced with the remaining count, got %q", stdout)
		}
		// The summary still reports the full set.
		if !strings.Contains(stdout, "30 findings (30 error, 0 warning)") {
			t.Errorf("summary should count every finding, got %q", stdout)
		}
		if code != exitFindings {
			t.Errorf("exit = %d, want %d", code, exitFindings)
		}
	})

	t.Run("zero means unlimited", func(t *testing.T) {
		_, stdout, _ := exec("--max-findings", "0", noisy)
		if got := strings.Count(stdout, "charset.nonprintable"); got != 30 {
			t.Errorf("printed %d findings with --max-findings 0, want 30", got)
		}
	})

	t.Run("a cap never changes the exit status", func(t *testing.T) {
		// Two errors and one warning, with output capped to a single finding, must
		// still fail on the warning as well as the error.
		dir := t.TempDir()
		warn := write(t, dir, "warn.psv", warnOnlyPSV)
		broken := write(t, dir, "broken.x12", brokenX12)

		if code, _, _ := exec("--max-findings", "1", warn); code != exitFindings {
			t.Error("a suppressed warning must still fail the default threshold")
		}
		if code, _, _ := exec("--max-findings", "1", "--allow-warnings", warn); code != exitClean {
			t.Error("a warning-only file should pass with --allow-warnings")
		}
		if code, _, _ := exec("--max-findings", "1", "--allow-warnings", broken); code != exitFindings {
			t.Error("a suppressed error must still fail")
		}
	})

	t.Run("json reports true totals with a truncated array", func(t *testing.T) {
		_, stdout, _ := exec("--json", "--max-findings", "4", noisy)
		var doc struct {
			Files []struct {
				Findings []struct {
					Rule string `json:"rule"`
				} `json:"findings"`
				Summary struct {
					Total     int  `json:"total"`
					Errors    int  `json:"errors"`
					Truncated bool `json:"truncated"`
				} `json:"summary"`
			} `json:"files"`
			Summary struct {
				Total     int  `json:"total"`
				Truncated bool `json:"truncated"`
			} `json:"summary"`
		}
		if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
			t.Fatalf("output is not valid JSON: %v\n%s", err, stdout)
		}
		file := doc.Files[0]
		if len(file.Findings) != 4 {
			t.Errorf("findings array = %d, want 4", len(file.Findings))
		}
		if file.Summary.Total != 30 || file.Summary.Errors != 30 {
			t.Errorf("file summary = %+v, want the full 30", file.Summary)
		}
		if !file.Summary.Truncated || !doc.Summary.Truncated {
			t.Error(`both summaries should carry "truncated": true`)
		}
		if doc.Summary.Total != 30 {
			t.Errorf("run summary total = %d, want 30", doc.Summary.Total)
		}
	})
}

func TestCharsetFlag(t *testing.T) {
	lower := cleanISA + cleanGS + "ST*835*0001~\nN1*PE*Vale Medical Group~\nSE*3*0001~\nGE*1*1~\nIEA*1*000000001~\n"
	path := write(t, t.TempDir(), "lower.x12", lower)

	// The default profile is extended, which accepts lowercase.
	if code, stdout, _ := exec(path); code != exitClean {
		t.Errorf("the default profile should accept lowercase, got %q", stdout)
	}
	if code, _, _ := exec("--charset", "extended", path); code != exitClean {
		t.Error("the extended profile should accept lowercase")
	}

	code, stdout, _ := exec("--charset", "basic", path)
	if code != exitFindings || !strings.Contains(stdout, "charset.x12-basic") {
		t.Errorf("the basic profile should flag lowercase, got %q", stdout)
	}

	if code, _, _ := exec("--charset", "off", path); code != exitClean {
		t.Error("--charset off should disable the character-set rules")
	}
	if code, _, _ := exec("--charset", "strict", path); code != exitUsage {
		t.Error("an unknown charset profile should be a usage error")
	}
}

func TestHelpAndRulesOutput(t *testing.T) {
	_, help, _ := exec("--help")
	for _, want := range []string{"Usage:", "--count-rule", "--layout", "Exit status:"} {
		if !strings.Contains(help, want) {
			t.Errorf("help output is missing %q", want)
		}
	}

	_, listing, _ := exec("--list-rules")
	for _, want := range []string{"charset.homoglyph", "envelope.segment-count", "layout.padding"} {
		if !strings.Contains(listing, want) {
			t.Errorf("rule listing is missing %q", want)
		}
	}
}

func TestUsageErrorsGoToStderr(t *testing.T) {
	code, stdout, stderr := exec("--nope")
	if code != exitUsage {
		t.Fatalf("exit = %d, want %d", code, exitUsage)
	}
	if stdout != "" {
		t.Errorf("usage errors must not pollute stdout, got %q", stdout)
	}
	if !strings.Contains(stderr, "unknown flag") {
		t.Errorf("stderr should explain the problem, got %q", stderr)
	}
}

// repoRoot locates the repository root from this package's directory, so that
// the README examples can be exercised at the paths the README actually uses.
func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	return root
}

func TestREADMEExamplesRun(t *testing.T) {
	// Every example command in the README must behave as the README claims.
	root := repoRoot(t)
	example := func(name string) string { return filepath.Join(root, "examples", name) }

	tests := []struct {
		name string
		args []string
		want int
	}{
		{"clean remittance", []string{example("remittance.x12")}, exitClean},
		{
			"eligibility extract",
			[]string{"--count-rule", "TRL:2:DTL", example("eligibility.psv")},
			exitFindings,
		},
		{
			"fixed-width remittance",
			[]string{"--format", "fixed", "--layout", example("remit-layout.json"), example("remit.txt")},
			exitFindings,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			code, _, stderr := exec(tc.args...)
			if code != tc.want {
				t.Errorf("exit = %d, want %d (stderr: %s)", code, tc.want, stderr)
			}
		})
	}
}

func TestRepeatedPathIsCheckedOnce(t *testing.T) {
	// Overlapping globs can name the same file twice; it must not be counted
	// twice, nor flagged as a duplicate of itself.
	clean := write(t, t.TempDir(), "clean.x12", cleanX12)

	code, stdout, _ := exec("-v", clean, clean)
	if code != exitClean {
		t.Fatalf("exit = %d, want %d (%s)", code, exitClean, stdout)
	}
	if !strings.Contains(stdout, "1 file checked") {
		t.Errorf("the repeated path should be collapsed, got %q", stdout)
	}
}

func TestDedupePreservesOrder(t *testing.T) {
	got := dedupe([]string{"b", "a", "b", "c", "a"})
	want := []string{"b", "a", "c"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}
