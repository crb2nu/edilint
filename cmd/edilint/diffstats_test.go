package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// execSub runs the CLI through run, which dispatches subcommands, the way main does.
func execSub(args ...string) (code int, stdout, stderr string) {
	var out, errBuf bytes.Buffer
	code = run(args, &out, &errBuf)
	return code, out.String(), errBuf.String()
}

// changedX12 is cleanX12 with one BPR02 amount changed, so a diff against
// cleanX12 reports exactly one element.
var changedX12 = strings.Replace(cleanX12, "BPR*I*1440.00*", "BPR*I*1440.50*", 1)

func TestDispatchRoutesSubcommands(t *testing.T) {
	dir := t.TempDir()
	clean := write(t, dir, "clean.x12", cleanX12)

	// A non-subcommand first argument still lints, exit contract unchanged.
	if code, _, _ := execSub(clean); code != exitClean {
		t.Errorf("linting through run: exit = %d, want %d", code, exitClean)
	}
	if code, _, _ := execSub(); code != exitUsage {
		t.Errorf("no arguments: exit = %d, want %d", code, exitUsage)
	}
	if code, _, _ := execSub("--json", clean); code != exitClean {
		t.Errorf("flag-first invocation must stay on the linting path, exit = %d", code)
	}
}

func TestDiffExitCodes(t *testing.T) {
	dir := t.TempDir()
	a := write(t, dir, "a.x12", cleanX12)
	b := write(t, dir, "b.x12", changedX12)
	same := write(t, dir, "same.x12", cleanX12)
	notX12 := write(t, dir, "notes.txt", "these are notes, not an interchange\n")

	tests := []struct {
		name string
		args []string
		want int
	}{
		{"identical files", []string{"diff", a, same}, exitClean},
		{"differing files", []string{"diff", a, b}, exitFindings},
		{"help", []string{"diff", "--help"}, exitClean},
		{"one file only", []string{"diff", a}, exitUsage},
		{"three files", []string{"diff", a, b, same}, exitUsage},
		{"missing file", []string{"diff", a, filepath.Join(dir, "gone.x12")}, exitUsage},
		{"not x12", []string{"diff", a, notX12}, exitUsage},
		{"unknown flag", []string{"diff", "--nope", a, b}, exitUsage},
		{"flag given a value", []string{"diff", "--strict=yes", a, b}, exitUsage},
		{"stdin twice", []string{"diff", "-", "-"}, exitUsage},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, _, stderr := execSub(tc.args...)
			if got != tc.want {
				t.Errorf("exit = %d, want %d (stderr: %s)", got, tc.want, stderr)
			}
		})
	}
}

func TestDiffTextOutput(t *testing.T) {
	dir := t.TempDir()
	a := write(t, dir, "a.x12", cleanX12)
	b := write(t, dir, "b.x12", changedX12)

	code, stdout, _ := execSub("diff", a, b)
	if code != exitFindings {
		t.Fatalf("exit = %d, want %d", code, exitFindings)
	}
	for _, want := range []string{"BPR02", `"1440.00"`, `"1440.50"`, "1 difference"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("output lacks %q:\n%s", want, stdout)
		}
	}

	// Identical files are silent, mirroring a clean lint run.
	code, stdout, stderr := execSub("diff", a, a)
	if code != exitClean || stdout != "" || stderr != "" {
		t.Errorf("identical files: exit = %d stdout = %q stderr = %q, want silence", code, stdout, stderr)
	}
}

func TestDiffJSONOutput(t *testing.T) {
	dir := t.TempDir()
	a := write(t, dir, "a.x12", cleanX12)
	b := write(t, dir, "b.x12", changedX12)

	code, stdout, _ := execSub("diff", "--json", a, b)
	if code != exitFindings {
		t.Fatalf("exit = %d, want %d", code, exitFindings)
	}

	var doc struct {
		Version     int    `json:"version"`
		A           string `json:"a"`
		B           string `json:"b"`
		Identical   bool   `json:"identical"`
		Differences []struct {
			Kind       string `json:"kind"`
			Designator string `json:"designator"`
		} `json:"differences"`
		Summary struct {
			Total int `json:"total"`
		} `json:"summary"`
	}
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, stdout)
	}
	if doc.Version != 1 {
		t.Errorf("version = %d, want 1", doc.Version)
	}
	if doc.A != a || doc.B != b || doc.Identical {
		t.Errorf("document header = %+v", doc)
	}
	if len(doc.Differences) != 1 || doc.Differences[0].Kind != "element" ||
		doc.Differences[0].Designator != "BPR02" {
		t.Errorf("differences = %+v", doc.Differences)
	}
	if doc.Summary.Total != 1 {
		t.Errorf("summary total = %d, want 1", doc.Summary.Total)
	}

	// Identical files still produce a complete document.
	code, stdout, _ = execSub("diff", "--json", a, a)
	if code != exitClean {
		t.Fatalf("exit = %d, want %d", code, exitClean)
	}
	if !strings.Contains(stdout, `"identical": true`) || !strings.Contains(stdout, `"differences": []`) {
		t.Errorf("identical JSON should say so with an empty array:\n%s", stdout)
	}
}

func TestDiffStrictFlag(t *testing.T) {
	dir := t.TempDir()
	a := write(t, dir, "a.x12", cleanX12)
	// The same file without newlines after segment terminators.
	b := write(t, dir, "b.x12", strings.ReplaceAll(cleanX12, "~\n", "~"))

	if code, _, _ := execSub("diff", a, b); code != exitClean {
		t.Error("terminator style alone should compare identical by default")
	}
	code, stdout, _ := execSub("diff", "--strict", a, b)
	if code != exitFindings {
		t.Fatalf("--strict exit = %d, want %d", code, exitFindings)
	}
	if !strings.Contains(stdout, "cosmetic") {
		t.Errorf("strict output should count the cosmetic differences, got %q", stdout)
	}
}

func TestStatsExitCodesAndText(t *testing.T) {
	dir := t.TempDir()
	clean := write(t, dir, "clean.x12", cleanX12)
	broken := write(t, dir, "broken.x12", brokenX12)

	// Stats is a census, not a gate: a file with lint findings still exits 0.
	code, stdout, _ := execSub("stats", clean, broken)
	if code != exitClean {
		t.Fatalf("exit = %d, want %d", code, exitClean)
	}
	for _, want := range []string{
		"clean.x12: x12,",
		"7 segments",
		"interchanges: 1, ISA13 000000001",
		"transactions: 1 (835: 1), ST02 0001",
		"broken.x12: x12,",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("output lacks %q:\n%s", want, stdout)
		}
	}

	if code, _, _ := execSub("stats"); code != exitUsage {
		t.Error("stats with no files should be a usage error")
	}
	if code, _, _ := execSub("stats", "--help"); code != exitClean {
		t.Error("stats --help should exit 0")
	}
	if code, _, _ := execSub("stats", "--nope", clean); code != exitUsage {
		t.Error("an unknown stats flag should be a usage error")
	}
}

func TestStatsJSONOutput(t *testing.T) {
	dir := t.TempDir()
	clean := write(t, dir, "clean.x12", cleanX12)

	code, stdout, _ := execSub("stats", "--json", clean)
	if code != exitClean {
		t.Fatalf("exit = %d, want %d", code, exitClean)
	}
	var doc struct {
		Version int `json:"version"`
		Files   []struct {
			File     string `json:"file"`
			Format   string `json:"format"`
			Records  int    `json:"records"`
			Envelope struct {
				Interchanges int `json:"interchanges"`
			} `json:"envelope"`
		} `json:"files"`
	}
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, stdout)
	}
	if doc.Version != 1 || len(doc.Files) != 1 {
		t.Fatalf("document = %+v", doc)
	}
	f := doc.Files[0]
	if f.File != clean || f.Format != "x12" || f.Records != 7 || f.Envelope.Interchanges != 1 {
		t.Errorf("file stats = %+v", f)
	}
}

func TestStatsUnreadableFileStillReportsTheRest(t *testing.T) {
	dir := t.TempDir()
	clean := write(t, dir, "clean.x12", cleanX12)
	missing := filepath.Join(dir, "gone.x12")

	code, stdout, stderr := execSub("stats", clean, missing)
	if code != exitUsage {
		t.Fatalf("exit = %d, want %d", code, exitUsage)
	}
	if !strings.Contains(stdout, "clean.x12: x12,") {
		t.Errorf("the readable file must still be reported, got %q", stdout)
	}
	if !strings.Contains(stderr, "1 of 2 input(s) could not be read") {
		t.Errorf("stderr should summarize the failure, got %q", stderr)
	}
}

func TestSubcommandHelpTexts(t *testing.T) {
	_, diffHelp, _ := execSub("diff", "--help")
	for _, want := range []string{"edilint diff", "--strict", "--json", "Exit status:"} {
		if !strings.Contains(diffHelp, want) {
			t.Errorf("diff help is missing %q", want)
		}
	}
	_, statsHelp, _ := execSub("stats", "--help")
	for _, want := range []string{"edilint stats", "--json", "census"} {
		if !strings.Contains(statsHelp, want) {
			t.Errorf("stats help is missing %q", want)
		}
	}
	// The top-level help mentions both subcommands.
	_, help, _ := execSub("--help")
	for _, want := range []string{"edilint diff", "edilint stats"} {
		if !strings.Contains(help, want) {
			t.Errorf("top-level help is missing %q", want)
		}
	}
}
