package main

import (
	"bytes"
	"encoding/json"
	"os"
	osexec "os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/crb2nu/edilint"
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
		{"invalid format", []string{"--format", "edi", clean}, exitUsage},
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
	if !strings.Contains(line, "error: [EL3006 envelope.segment-count]") {
		t.Errorf("finding should carry severity, identifier and rule, got %q", line)
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
				ID       string `json:"id"`
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
	if doc.Version != 3 {
		t.Errorf("version = %d, want 3", doc.Version)
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
	if f.ID != "EL3006" {
		t.Errorf("id = %q, want EL3006", f.ID)
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
				t.Errorf("--json should have been honored, got %q", stdout)
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

// jqProgramRE finds a single-quoted jq program in a documented shell pipeline,
// e.g. `edilint --json out/*.x12 | jq -r '.files[].findings[]'`.
var jqProgramRE = regexp.MustCompile(`\| jq (?:-[a-zA-Z]+ )*'([^']*)'`)

// documentedJQPrograms returns every jq program the project documents, both in
// the README and in the tool's own --help output.
func documentedJQPrograms(t *testing.T) map[string]string {
	t.Helper()

	readme, err := os.ReadFile(filepath.Join(repoRoot(t), "README.md"))
	if err != nil {
		t.Fatalf("read README: %v", err)
	}
	_, help, _ := exec("--help")

	found := map[string]string{}
	for source, text := range map[string]string{"README.md": string(readme), "--help": help} {
		for _, m := range jqProgramRE.FindAllStringSubmatch(text, -1) {
			found[m[1]] = source
		}
	}
	if len(found) == 0 {
		t.Fatal("no documented jq pipelines found; the extractor or the docs changed")
	}
	return found
}

func TestDocumentedJQPipelinesWorkOnCleanFiles(t *testing.T) {
	// A Go round-trip cannot catch this: encoding/json unmarshals null into a
	// nil slice of length 0, so `"findings": null` decodes and measures the same
	// as `"findings": []`. jq is the consumer that actually breaks, so jq is
	// what runs here.
	jqPath, err := osexec.LookPath("jq")
	if err != nil {
		t.Skip("jq is not installed")
	}

	clean := write(t, t.TempDir(), "clean.x12", cleanX12)
	code, stdout, stderr := exec("--json", clean)
	if code != exitClean {
		t.Fatalf("fixture should be clean, exit = %d (%s)", code, stderr)
	}

	for program, source := range documentedJQPrograms(t) {
		t.Run(source+": "+program, func(t *testing.T) {
			cmd := osexec.Command(jqPath, "-r", program)
			cmd.Stdin = strings.NewReader(stdout)
			var out, errOut bytes.Buffer
			cmd.Stdout = &out
			cmd.Stderr = &errOut

			if err := cmd.Run(); err != nil {
				t.Fatalf("the %s pipeline fails on a clean file: %v\njq stderr: %s\ninput:\n%s",
					source, err, errOut.String(), stdout)
			}
			if strings.TrimSpace(out.String()) != "" {
				t.Errorf("a clean file should yield no rows, got %q", out.String())
			}
		})
	}
}

func TestDocumentedJQPipelinesWorkOnDirtyFiles(t *testing.T) {
	jqPath, err := osexec.LookPath("jq")
	if err != nil {
		t.Skip("jq is not installed")
	}

	broken := write(t, t.TempDir(), "broken.x12", brokenX12)
	code, stdout, _ := exec("--json", broken)
	if code != exitFindings {
		t.Fatalf("fixture should have findings, exit = %d", code)
	}

	for program, source := range documentedJQPrograms(t) {
		t.Run(source+": "+program, func(t *testing.T) {
			cmd := osexec.Command(jqPath, "-r", program)
			cmd.Stdin = strings.NewReader(stdout)
			var out bytes.Buffer
			cmd.Stdout = &out
			if err := cmd.Run(); err != nil {
				t.Fatalf("the %s pipeline fails on a file with findings: %v", source, err)
			}
			if strings.TrimSpace(out.String()) == "" {
				t.Error("a file with findings should yield at least one row")
			}
		})
	}
}

func TestCleanJSONEmitsAnEmptyFindingsArray(t *testing.T) {
	// The raw-bytes assertion, so the guarantee holds even where jq is absent.
	clean := write(t, t.TempDir(), "clean.x12", cleanX12)
	_, stdout, _ := exec("--json", clean)

	if !strings.Contains(stdout, `"findings": []`) {
		t.Errorf("a clean file must emit an empty findings array, not null:\n%s", stdout)
	}
	if strings.Contains(stdout, `"findings": null`) {
		t.Errorf("findings must never marshal as null:\n%s", stdout)
	}
}

func TestUnreadableFileDoesNotDiscardOtherFindings(t *testing.T) {
	// A glob that races with a file being moved must still report what it found
	// in the files it could read, while exiting 2 so the caller knows the run
	// was incomplete.
	dir := t.TempDir()
	broken := write(t, dir, "broken.x12", brokenX12)
	missing := filepath.Join(dir, "gone.x12")

	code, stdout, stderr := exec(broken, missing)
	if code != exitUsage {
		t.Fatalf("exit = %d, want %d", code, exitUsage)
	}
	if !strings.Contains(stdout, "envelope.segment-count") {
		t.Errorf("findings from the readable file must still be printed, got %q", stdout)
	}
	if !strings.Contains(stderr, "gone.x12") {
		t.Errorf("stderr should name the unreadable file, got %q", stderr)
	}
	if !strings.Contains(stderr, "1 of 2 input(s) could not be read") {
		t.Errorf("stderr should summarize the failures, got %q", stderr)
	}
}

func TestUnreadableFileStillExitsTwoWhenOthersAreClean(t *testing.T) {
	dir := t.TempDir()
	clean := write(t, dir, "clean.x12", cleanX12)
	missing := filepath.Join(dir, "gone.x12")

	if code, _, _ := exec(clean, missing); code != exitUsage {
		t.Errorf("exit = %d, want %d", code, exitUsage)
	}
}

func TestUnknownFlagWithAnInlineValueReportsTheUnknownFlag(t *testing.T) {
	clean := write(t, t.TempDir(), "clean.x12", cleanX12)

	// Both spellings of an unknown flag must blame the flag, not the syntax.
	for _, arg := range []string{"--bogus=1", "--bogus"} {
		code, _, stderr := exec(arg, clean)
		if code != exitUsage {
			t.Errorf("%s: exit = %d, want %d", arg, code, exitUsage)
		}
		if !strings.Contains(stderr, "unknown flag: --bogus") {
			t.Errorf("%s: stderr = %q, want it to name the unknown flag", arg, stderr)
		}
	}

	// A real boolean flag given a value still gets the syntax message.
	code, _, stderr := exec("--json=yes", clean)
	if code != exitUsage {
		t.Errorf("exit = %d, want %d", code, exitUsage)
	}
	if !strings.Contains(stderr, "--json does not take a value") {
		t.Errorf("stderr = %q, want the value-syntax message", stderr)
	}
}

func TestHelpStatesTheRealCharsetDefault(t *testing.T) {
	// The help text drifted from the code once already.
	_, help, _ := exec("--help")
	if !strings.Contains(help, "extended (default)") {
		t.Errorf("--help must state that extended is the default charset:\n%s", help)
	}
	if strings.Contains(help, "basic (default)") {
		t.Error("--help still claims basic is the default charset")
	}
}

func TestBinaryInputIsReportedOnceFromTheCLI(t *testing.T) {
	// The shell-glob-catches-an-archive scenario, end to end.
	body := make([]byte, 1<<20)
	state := uint32(0x9e3779b9)
	for i := range body {
		state = state*1664525 + 1013904223
		body[i] = byte(state >> 24)
	}
	blob := write(t, t.TempDir(), "archive.bin", string(body))

	code, stdout, _ := exec(blob)
	if code != exitFindings {
		t.Fatalf("exit = %d, want %d", code, exitFindings)
	}
	if got := strings.Count(stdout, "charset.invalid-utf8"); got != 1 {
		t.Errorf("binary input produced %d findings, want exactly 1:\n%s", got, stdout)
	}
	if !strings.Contains(stdout, "does not look like text") {
		t.Errorf("the finding should name the real problem, got %q", stdout)
	}
}

// chdir moves the process into dir for the duration of one test. The suite is
// not parallel, so this is safe; t.Chdir would be the modern spelling but it
// needs a newer Go than go.mod's floor.
func chdir(t *testing.T, dir string) {
	t.Helper()
	previous, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir %s: %v", dir, err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(previous); err != nil {
			t.Fatalf("restore working directory: %v", err)
		}
	})
}

func TestListRulesPrintsTheIdentifierColumn(t *testing.T) {
	_, listing, _ := exec("--list-rules")

	for _, want := range []string{"EL1005", "EL3006", "EL5002"} {
		if !strings.Contains(listing, want) {
			t.Errorf("rule listing is missing %s", want)
		}
	}
	// Identifier, name and severity on one line, in that order.
	var found string
	for _, line := range strings.Split(listing, "\n") {
		if strings.HasPrefix(line, "EL3006") {
			found = line
		}
	}
	if found == "" {
		t.Fatalf("no line for EL3006:\n%s", listing)
	}
	fields := strings.Fields(found)
	if len(fields) < 3 || fields[0] != "EL3006" ||
		fields[1] != "envelope.segment-count" || fields[2] != "error" {
		t.Errorf("EL3006 line = %q, want identifier, name then severity", found)
	}
}

func TestDisableAcceptsIdentifiers(t *testing.T) {
	broken := write(t, t.TempDir(), "broken.x12", brokenX12)

	if code, _, _ := exec("--disable", "EL3006", broken); code != exitClean {
		t.Error("disabling the failing rule by identifier should leave the file clean")
	}
	if code, _, _ := exec("--disable", "el3006", broken); code != exitClean {
		t.Error("an identifier should match whatever its case")
	}
	if code, _, _ := exec("--disable", "EL3006,charset", broken); code != exitClean {
		t.Error("identifiers and class names should mix in one list")
	}
}

func TestDisableRejectsAnUnknownRule(t *testing.T) {
	// A misspelled suppression that silently suppresses nothing is the failure
	// mode this rejects.
	clean := write(t, t.TempDir(), "clean.x12", cleanX12)

	code, _, stderr := exec("--disable", "envelop", clean)
	if code != exitUsage {
		t.Fatalf("exit = %d, want %d", code, exitUsage)
	}
	if !strings.Contains(stderr, `unknown rule "envelop"`) {
		t.Errorf("stderr should name the unknown rule, got %q", stderr)
	}
	if !strings.Contains(stderr, "EL3006") {
		t.Errorf("stderr should show the accepted forms, got %q", stderr)
	}
}

func TestBaselineRoundTripFromTheCLI(t *testing.T) {
	// The documented adoption path, end to end.
	dir := t.TempDir()
	target := write(t, dir, "legacy.x12", brokenX12)
	baseline := filepath.Join(dir, "baseline.json")

	// The file fails on its own.
	if code, _, _ := exec(target); code != exitFindings {
		t.Fatal("the fixture should report findings before it is baselined")
	}

	code, stdout, stderr := exec("--write-baseline", baseline, target)
	if code != exitClean {
		t.Fatalf("recording exit = %d, want %d (%s)", code, exitClean, stderr)
	}
	if stdout != "" {
		t.Errorf("recording should not print findings to stdout, got %q", stdout)
	}
	if !strings.Contains(stderr, "recorded 1 finding(s)") {
		t.Errorf("recording should say what it wrote, got %q", stderr)
	}

	// Re-running against the baseline is clean and silent.
	code, stdout, stderr = exec("--baseline", baseline, target)
	if code != exitClean {
		t.Fatalf("baselined exit = %d, want %d (%s)", code, exitClean, stdout)
	}
	if stdout != "" {
		t.Errorf("a fully baselined run should print nothing, got %q", stdout)
	}
	if stderr != "" {
		t.Errorf("a fully baselined run should be silent on stderr, got %q", stderr)
	}

	// A new defect on top of the recorded one is reported, and only it.
	planted := strings.Replace(brokenX12, "BPR*I*", "BPR*I\x0b*", 1)
	if planted == brokenX12 {
		t.Fatal("the planted defect did not apply")
	}
	write(t, dir, "legacy.x12", planted)

	code, stdout, _ = exec("--baseline", baseline, target)
	if code != exitFindings {
		t.Fatalf("exit = %d, want %d", code, exitFindings)
	}
	if !strings.Contains(stdout, "1 finding") {
		t.Errorf("exactly one new finding should be reported, got %q", stdout)
	}
	if !strings.Contains(stdout, "charset.nonprintable") {
		t.Errorf("the new finding should be the planted one, got %q", stdout)
	}
	if strings.Contains(stdout, "envelope.segment-count") {
		t.Errorf("the recorded finding should stay suppressed, got %q", stdout)
	}
}

func TestBaselineIsCommittableJSON(t *testing.T) {
	dir := t.TempDir()
	target := write(t, dir, "legacy.x12", brokenX12)
	baseline := filepath.Join(dir, "baseline.json")

	if code, _, stderr := exec("--write-baseline", baseline, target); code != exitClean {
		t.Fatalf("exit = %d (%s)", code, stderr)
	}
	body, err := os.ReadFile(baseline)
	if err != nil {
		t.Fatalf("read baseline: %v", err)
	}

	var doc struct {
		Version  int `json:"version"`
		Findings []struct {
			File    string `json:"file"`
			ID      string `json:"id"`
			Rule    string `json:"rule"`
			Message string `json:"message"`
			Count   int    `json:"count"`
		} `json:"findings"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatalf("baseline is not valid JSON: %v\n%s", err, body)
	}
	if doc.Version != edilint.BaselineVersion {
		t.Errorf("version = %d, want %d", doc.Version, edilint.BaselineVersion)
	}
	if len(doc.Findings) != 1 {
		t.Fatalf("findings = %d, want 1", len(doc.Findings))
	}
	entry := doc.Findings[0]
	if entry.ID != "EL3006" || entry.Rule != "envelope.segment-count" || entry.Count != 1 {
		t.Errorf("entry = %+v", entry)
	}
	if entry.Message == "" {
		t.Error("the entry should carry its message, so the file can be reviewed")
	}
	// No line or column, which is what lets the entry survive an edit above it.
	if strings.Contains(string(body), `"line"`) {
		t.Errorf("a baseline entry must not record a line number:\n%s", body)
	}
}

func TestBaselineFlagErrors(t *testing.T) {
	dir := t.TempDir()
	clean := write(t, dir, "clean.x12", cleanX12)
	missing := filepath.Join(dir, "absent.json")

	code, _, stderr := exec("--baseline", missing, clean)
	if code != exitUsage {
		t.Errorf("a missing baseline exit = %d, want %d", code, exitUsage)
	}
	if !strings.Contains(stderr, "--write-baseline") {
		t.Errorf("stderr should say how to record one, got %q", stderr)
	}

	code, _, stderr = exec("--baseline", missing, "--write-baseline", missing, clean)
	if code != exitUsage {
		t.Errorf("using both baseline flags exit = %d, want %d", code, exitUsage)
	}
	if !strings.Contains(stderr, "cannot be used together") {
		t.Errorf("stderr should explain the conflict, got %q", stderr)
	}
}

func TestWriteBaselineRefusesAfterAnUnreadableInput(t *testing.T) {
	// Recording from an incomplete run would bake a gap into the baseline.
	dir := t.TempDir()
	target := write(t, dir, "legacy.x12", brokenX12)
	baseline := filepath.Join(dir, "baseline.json")

	code, _, stderr := exec("--write-baseline", baseline, target, filepath.Join(dir, "gone.x12"))
	if code != exitUsage {
		t.Fatalf("exit = %d, want %d", code, exitUsage)
	}
	if !strings.Contains(stderr, "no baseline was written") {
		t.Errorf("stderr should say the file was not written, got %q", stderr)
	}
	if _, err := os.Stat(baseline); err == nil {
		t.Error("no baseline file should have been created")
	}
}

func TestStaleBaselineEntriesAreReportedWhenVerbose(t *testing.T) {
	dir := t.TempDir()
	target := write(t, dir, "legacy.x12", brokenX12)
	baseline := filepath.Join(dir, "baseline.json")

	if code, _, stderr := exec("--write-baseline", baseline, target); code != exitClean {
		t.Fatalf("exit = %d (%s)", code, stderr)
	}
	// Fix the defect the baseline recorded.
	write(t, dir, "legacy.x12", cleanX12)

	code, _, stderr := exec("-v", "--baseline", baseline, target)
	if code != exitClean {
		t.Fatalf("exit = %d, want %d (%s)", code, exitClean, stderr)
	}
	if !strings.Contains(stderr, "no longer occur") {
		t.Errorf("a stale baseline should be mentioned when verbose, got %q", stderr)
	}

	// A stale entry is not an error, and says nothing without --verbose.
	if _, _, quiet := exec("--baseline", baseline, target); quiet != "" {
		t.Errorf("a quiet run should not mention stale entries, got %q", quiet)
	}
}

func TestConfigFileSuppliesOptions(t *testing.T) {
	dir := t.TempDir()
	bad := write(t, dir, "bad.psv", badCountPSV)
	conf := write(t, dir, "edilint.yml", "count-rules:\n  - TRL:2:DTL\n")

	// Without the configuration nothing declares a count, so the file is clean.
	if code, _, _ := exec("--no-config", bad); code != exitClean {
		t.Errorf("exit without a count rule = %d, want %d", code, exitClean)
	}

	code, stdout, stderr := exec("--config", conf, bad)
	if code != exitFindings {
		t.Fatalf("exit = %d, want %d (%s)", code, exitFindings, stderr)
	}
	if !strings.Contains(stdout, "counts.mismatch") {
		t.Errorf("the configured count rule should have run, got %q", stdout)
	}
}

func TestConfigFileIsFoundInTheWorkingDirectory(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, ".edilint.yml", "disable:\n  - envelope\n")
	write(t, dir, "broken.x12", brokenX12)
	chdir(t, dir)

	if code, stdout, _ := exec("broken.x12"); code != exitClean {
		t.Errorf("the discovered config should have suppressed the finding, got %q", stdout)
	}
	if code, _, _ := exec("--no-config", "broken.x12"); code != exitFindings {
		t.Error("--no-config should ignore the file in the working directory")
	}

	// Verbose runs name the file they read, so a surprising suppression is
	// traceable to the configuration that caused it.
	_, _, stderr := exec("-v", "broken.x12")
	if !strings.Contains(stderr, ".edilint.yml") {
		t.Errorf("a verbose run should name the config it used, got %q", stderr)
	}
}

func TestCommandLineBeatsTheConfigFile(t *testing.T) {
	dir := t.TempDir()
	lower := cleanISA + cleanGS + "ST*835*0001~\nN1*PE*Vale Medical Group~\nSE*3*0001~\nGE*1*1~\nIEA*1*000000001~\n"
	path := write(t, dir, "lower.x12", lower)
	conf := write(t, dir, "edilint.yml", "charset: basic\n")

	// The file asks for the strict profile, which flags lowercase.
	if code, stdout, _ := exec("--config", conf, path); code != exitFindings ||
		!strings.Contains(stdout, "charset.x12-basic") {
		t.Errorf("the configured profile should apply, got %q", stdout)
	}
	// The flag overrules it.
	if code, _, _ := exec("--config", conf, "--charset", "extended", path); code != exitClean {
		t.Error("--charset should overrule the configured profile")
	}
}

func TestConfigAndCommandLineSuppressionsAddUp(t *testing.T) {
	// Two sources both asking for quiet must both be heard: a flag adds to what
	// the file suppressed rather than replacing it.
	dir := t.TempDir()
	path := write(t, dir, "warn.psv", warnOnlyPSV)
	conf := write(t, dir, "edilint.yml", "disable:\n  - terminator\n")

	if code, stdout, _ := exec("--config", conf, "--disable", "fields", path); code != exitClean {
		t.Errorf("both suppressions should apply, got %q", stdout)
	}
}

func TestConfigErrorsAreUsageErrors(t *testing.T) {
	dir := t.TempDir()
	clean := write(t, dir, "clean.x12", cleanX12)

	broken := write(t, dir, "broken.yml", "verbosity: high\n")
	code, _, stderr := exec("--config", broken, clean)
	if code != exitUsage {
		t.Errorf("exit = %d, want %d", code, exitUsage)
	}
	if !strings.Contains(stderr, "unknown setting") {
		t.Errorf("stderr should explain the problem, got %q", stderr)
	}

	// An explicitly named file that does not exist is an error; a missing
	// .edilint.yml in the working directory is not.
	if code, _, _ := exec("--config", filepath.Join(dir, "absent.yml"), clean); code != exitUsage {
		t.Errorf("a missing --config exit = %d, want %d", code, exitUsage)
	}
	if code, _, _ := exec("--config", broken, "--no-config", clean); code != exitUsage {
		t.Error("--config and --no-config together should be a usage error")
	}
}

func TestConfigCanRegradeARule(t *testing.T) {
	dir := t.TempDir()
	warn := write(t, dir, "warn.psv", warnOnlyPSV)
	conf := write(t, dir, "edilint.yml", "severity:\n  EL2002: info\n")

	// The only finding is a missing final terminator, normally a warning that
	// fails the run. Downgraded to informational it is printed but does not gate.
	code, stdout, stderr := exec("--config", conf, warn)
	if code != exitClean {
		t.Fatalf("exit = %d, want %d (%s)", code, exitClean, stderr)
	}
	if !strings.Contains(stdout, "info: [EL2002 terminator.missing-final]") {
		t.Errorf("the finding should still be printed as informational, got %q", stdout)
	}
	if !strings.Contains(stdout, "(0 error, 0 warning, 1 info)") {
		t.Errorf("the summary should count it, got %q", stdout)
	}
}

func TestExampleConfigRunsAsDocumented(t *testing.T) {
	root := repoRoot(t)
	conf := filepath.Join(root, "examples", "edilint.yml")
	target := filepath.Join(root, "examples", "eligibility.psv")

	code, stdout, stderr := exec("--config", conf, target)
	if code != exitFindings {
		t.Fatalf("exit = %d, want %d (%s)", code, exitFindings, stderr)
	}
	if !strings.Contains(stdout, "counts.mismatch") {
		t.Errorf("the example config should apply its count rule, got %q", stdout)
	}
}

func TestOutputFlagSelectsTheWriter(t *testing.T) {
	dir := t.TempDir()
	broken := write(t, dir, "broken.x12", brokenX12)

	tests := []struct {
		name   string
		output string
		want   []string
	}{
		{"sarif", "sarif", []string{`"$schema"`, `"2.1.0"`, `"ruleId": "EL3006"`, `"level": "error"`}},
		{"junit", "junit", []string{"<?xml", "<testsuite", `type="error"`, "EL3006 envelope.segment-count"}},
		{"github", "github", []string{"::error file=", "title=EL3006 envelope.segment-count"}},
		{"json", "json", []string{`"version": 3`, `"id": "EL3006"`}},
		{"text", "text", []string{": error: [EL3006 envelope.segment-count]"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			code, stdout, stderr := exec("--output", tc.output, broken)
			if code != exitFindings {
				t.Fatalf("exit = %d, want %d (%s)", code, exitFindings, stderr)
			}
			for _, want := range tc.want {
				if !strings.Contains(stdout, want) {
					t.Errorf("--output %s: output lacks %q:\n%s", tc.output, want, stdout)
				}
			}
		})
	}
}

func TestOutputJSONMatchesTheJSONFlag(t *testing.T) {
	dir := t.TempDir()
	broken := write(t, dir, "broken.x12", brokenX12)

	_, viaFlag, _ := exec("--json", broken)
	_, viaOutput, _ := exec("--output", "json", broken)
	if viaFlag != viaOutput {
		t.Errorf("--json and --output json disagree:\n%s\n---\n%s", viaFlag, viaOutput)
	}
}

func TestOutputFlagValidation(t *testing.T) {
	dir := t.TempDir()
	clean := write(t, dir, "clean.x12", cleanX12)

	if code, _, stderr := exec("--output", "yaml", clean); code != exitUsage ||
		!strings.Contains(stderr, "unknown output format") {
		t.Errorf("--output yaml: exit = %d, stderr = %q, want a usage error naming the format", code, stderr)
	}
	if code, _, stderr := exec("--json", "--output", "sarif", clean); code != exitUsage ||
		!strings.Contains(stderr, "cannot be used together") {
		t.Errorf("--json with --output sarif: exit = %d, stderr = %q, want a conflict error", code, stderr)
	}
	// Agreement is not a conflict.
	if code, _, stderr := exec("--json", "--output", "json", clean); code != exitClean {
		t.Errorf("--json with --output json: exit = %d (%s), want %d", code, stderr, exitClean)
	}
}

func TestOutputFormatsKeepTheExitContract(t *testing.T) {
	dir := t.TempDir()
	clean := write(t, dir, "clean.x12", cleanX12)
	broken := write(t, dir, "broken.x12", brokenX12)

	for _, output := range []string{"sarif", "junit", "github"} {
		if code, _, _ := exec("--output", output, clean); code != exitClean {
			t.Errorf("--output %s on a clean file: exit = %d, want %d", output, code, exitClean)
		}
		if code, _, _ := exec("--output", output, broken); code != exitFindings {
			t.Errorf("--output %s on a broken file: exit = %d, want %d", output, code, exitFindings)
		}
	}
}
