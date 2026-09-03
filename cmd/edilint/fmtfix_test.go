package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crb2nu/edilint"
)

const (
	// The clean interchange squeezed onto one line: same segments, not canonical.
	oneLineX12 = "ISA*00*          *00*          *ZZ*NORTHGATEHEALTH*ZZ*VALEMEDGROUP   " +
		"*260115*1430*^*00501*000000001*0*P*:~" +
		"GS*HP*NORTHGATEHEALTH*VALEMEDGROUP*20260115*1430*1*X*005010X221A1~" +
		"ST*835*0001~BPR*I*1440.00*C*ACH*CCP~SE*3*0001~GE*1*1~IEA*1*000000001~"

	// One Cyrillic А (U+0410) in a payee name, otherwise clean.
	homoglyphX12 = cleanISA + cleanGS +
		"ST*835*0001~\nN1*PE*VАLE MEDICAL GROUP~\nSE*3*0001~\nGE*1*1~\nIEA*1*000000001~\n"
)

func TestFmtPrintsCanonicalFormByDefault(t *testing.T) {
	path := write(t, t.TempDir(), "one-line.x12", oneLineX12)

	code, stdout, stderr := exec("fmt", path)
	if code != exitClean {
		t.Fatalf("exit = %d, want %d (%s)", code, exitClean, stderr)
	}
	if stdout != cleanX12 {
		t.Errorf("canonical output:\n%q\nwant:\n%q", stdout, cleanX12)
	}

	// Printing must not touch the file.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != oneLineX12 {
		t.Error("the default mode modified the input file")
	}
}

func TestFmtCheck(t *testing.T) {
	dir := t.TempDir()
	canonical := write(t, dir, "canonical.x12", cleanX12)
	messy := write(t, dir, "messy.x12", oneLineX12)

	t.Run("a canonical file passes silently", func(t *testing.T) {
		code, stdout, stderr := exec("fmt", "--check", canonical)
		if code != exitClean {
			t.Fatalf("exit = %d, want %d (%s)", code, exitClean, stderr)
		}
		if stdout != "" {
			t.Errorf("nothing should be printed, got %q", stdout)
		}
	})

	t.Run("a non-canonical file is named and fails", func(t *testing.T) {
		code, stdout, _ := exec("fmt", "--check", canonical, messy)
		if code != exitFindings {
			t.Fatalf("exit = %d, want %d", code, exitFindings)
		}
		if stdout != messy+"\n" {
			t.Errorf("stdout = %q, want just the non-canonical path", stdout)
		}
	})
}

func TestFmtWrite(t *testing.T) {
	path := write(t, t.TempDir(), "messy.x12", oneLineX12)

	code, stdout, stderr := exec("fmt", "--write", path)
	if code != exitClean {
		t.Fatalf("exit = %d, want %d (%s)", code, exitClean, stderr)
	}
	if stdout != "" {
		t.Errorf("--write should print nothing, got %q", stdout)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != cleanX12 {
		t.Errorf("file was not rewritten canonically:\n%q", data)
	}

	if code, _, _ := exec("fmt", "--check", path); code != exitClean {
		t.Error("a just-formatted file should pass --check")
	}
}

func TestFmtHL7(t *testing.T) {
	input := "MSH|^~\\&|A|B|C|D|20260115||ADT^A08|1|P|2.5.1\r\n\r\nPID|1"
	want := "MSH|^~\\&|A|B|C|D|20260115||ADT^A08|1|P|2.5.1\nPID|1\n"
	path := write(t, t.TempDir(), "msg.hl7", input)

	code, stdout, stderr := exec("fmt", path)
	if code != exitClean {
		t.Fatalf("exit = %d, want %d (%s)", code, exitClean, stderr)
	}
	if stdout != want {
		t.Errorf("canonical output %q, want %q", stdout, want)
	}
}

func TestFmtErrors(t *testing.T) {
	dir := t.TempDir()
	x12 := write(t, dir, "clean.x12", cleanX12)
	psv := write(t, dir, "extract.psv", cleanPSV)

	tests := []struct {
		name       string
		args       []string
		wantStderr string
	}{
		{"no input files", []string{"fmt"}, "no input files"},
		{"check and write together", []string{"fmt", "--check", "--write", x12}, "cannot be used together"},
		{"write from stdin", []string{"fmt", "--write", "-"}, "standard input"},
		{"unknown flag", []string{"fmt", "--nope", x12}, "unknown flag"},
		{"boolean flag given a value", []string{"fmt", "--check=yes", x12}, "does not take a value"},
		{"unsupported format value", []string{"fmt", "--format", "delimited", x12}, "fmt supports x12 and hl7v2"},
		{"unknown format value", []string{"fmt", "--format", "edi", x12}, "unknown format"},
		{"unsupported input", []string{"fmt", psv}, "fmt supports x12 and hl7v2"},
		{"unreadable file", []string{"fmt", filepath.Join(dir, "absent.x12")}, "absent.x12"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			code, _, stderr := exec(tc.args...)
			if code != exitUsage {
				t.Fatalf("exit = %d, want %d (%s)", code, exitUsage, stderr)
			}
			if !strings.Contains(stderr, tc.wantStderr) {
				t.Errorf("stderr = %q, want it to contain %q", stderr, tc.wantStderr)
			}
		})
	}
}

func TestFmtProcessesReadableFilesDespiteAFailure(t *testing.T) {
	// One unreadable input still leaves the readable one checked, mirroring
	// the lint command's behavior; the run exits 2 for the failure.
	dir := t.TempDir()
	messy := write(t, dir, "messy.x12", oneLineX12)

	code, stdout, _ := exec("fmt", "--check", filepath.Join(dir, "gone.x12"), messy)
	if code != exitUsage {
		t.Fatalf("exit = %d, want %d", code, exitUsage)
	}
	if !strings.Contains(stdout, "messy.x12") {
		t.Errorf("the readable file should still be checked, got %q", stdout)
	}
}

func TestFixDryRunIsTheDefault(t *testing.T) {
	dir := t.TempDir()
	path := write(t, dir, "broken.x12", brokenX12)

	code, stdout, stderr := exec("fix", path)
	if code != exitFindings {
		t.Fatalf("exit = %d, want %d (%s)", code, exitFindings, stderr)
	}
	if !strings.HasPrefix(stdout, "--- a/"+path+"\n+++ b/"+path+"\n") {
		t.Errorf("stdout should open with diff headers, got %q", stdout)
	}
	if !strings.Contains(stdout, "-SE*9*0001~\n") || !strings.Contains(stdout, "+SE*3*0001~\n") {
		t.Errorf("the diff should show the recount, got %q", stdout)
	}
	if !strings.Contains(stderr, "[EL3006 envelope.segment-count]") {
		t.Errorf("stderr should describe the repair with its rule, got %q", stderr)
	}
	if !strings.Contains(stderr, "re-run with --write to apply") {
		t.Errorf("stderr should say how to apply, got %q", stderr)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != brokenX12 {
		t.Error("a dry run modified the file")
	}

	// --dry-run spells out the default.
	code2, stdout2, _ := exec("fix", "--dry-run", path)
	if code2 != code || stdout2 != stdout {
		t.Error("--dry-run should behave exactly like the default")
	}
}

func TestFixDryRunMatchesWhatWriteDoes(t *testing.T) {
	dir := t.TempDir()
	path := write(t, dir, "broken.x12", brokenX12)

	_, dryDiff, _ := exec("fix", path)

	code, stdout, stderr := exec("fix", "--write", path)
	if code != exitClean {
		t.Fatalf("exit = %d, want %d (%s)", code, exitClean, stderr)
	}
	if stdout != "" {
		t.Errorf("safe repairs applied with --write should print no diff, got %q", stdout)
	}
	if !strings.Contains(stderr, "applied 1 repair(s) in 1 file(s)") {
		t.Errorf("stderr should summarize what was applied, got %q", stderr)
	}

	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	// The dry run's diff is exactly the change --write then made.
	if got := diffUnified(path, []byte(brokenX12), written); got != dryDiff {
		t.Errorf("dry-run diff:\n%q\nbut --write produced:\n%q", dryDiff, got)
	}
	// And the library agrees byte for byte.
	want, _ := edilint.Fix([]byte(brokenX12), edilint.FixOptions{})
	if string(written) != string(want) {
		t.Error("the written file differs from the library's Fix output")
	}

	// Nothing is left to do.
	code, stdout, stderr = exec("fix", path)
	if code != exitClean || stdout != "" || stderr != "" {
		t.Errorf("a repaired file should be silent: exit %d, stdout %q, stderr %q", code, stdout, stderr)
	}
}

func TestFixCleanFileIsSilent(t *testing.T) {
	path := write(t, t.TempDir(), "clean.x12", cleanX12)

	code, stdout, stderr := exec("fix", path)
	if code != exitClean {
		t.Fatalf("exit = %d, want %d", code, exitClean)
	}
	if stdout != "" || stderr != "" {
		t.Errorf("nothing to repair should print nothing, got %q / %q", stdout, stderr)
	}
}

func TestFixUnsafeTier(t *testing.T) {
	t.Run("homoglyphs are untouched without --unsafe", func(t *testing.T) {
		path := write(t, t.TempDir(), "payee.x12", homoglyphX12)
		code, stdout, _ := exec("fix", path)
		if code != exitClean || stdout != "" {
			t.Errorf("the safe tier should find nothing: exit %d, stdout %q", code, stdout)
		}
	})

	t.Run("a dry run with --unsafe shows the substitution", func(t *testing.T) {
		path := write(t, t.TempDir(), "payee.x12", homoglyphX12)
		code, stdout, stderr := exec("fix", "--unsafe", path)
		if code != exitFindings {
			t.Fatalf("exit = %d, want %d", code, exitFindings)
		}
		if !strings.Contains(stdout, "+N1*PE*VALE MEDICAL GROUP~") {
			t.Errorf("the diff should show the ASCII form, got %q", stdout)
		}
		if !strings.Contains(stderr, "[EL1005 charset.homoglyph]") {
			t.Errorf("stderr should name the rule, got %q", stderr)
		}
	})

	t.Run("--write --unsafe applies and still prints the diff", func(t *testing.T) {
		path := write(t, t.TempDir(), "payee.x12", homoglyphX12)
		code, stdout, stderr := exec("fix", "--write", "--unsafe", path)
		if code != exitClean {
			t.Fatalf("exit = %d, want %d (%s)", code, exitClean, stderr)
		}
		if !strings.Contains(stdout, "+N1*PE*VALE MEDICAL GROUP~") {
			t.Errorf("an applied unsafe repair must still print its diff, got %q", stdout)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "А") {
			t.Error("the homoglyph should have been replaced on disk")
		}
	})
}

func TestFixErrors(t *testing.T) {
	dir := t.TempDir()
	x12 := write(t, dir, "clean.x12", cleanX12)

	tests := []struct {
		name       string
		args       []string
		wantStderr string
	}{
		{"no input files", []string{"fix"}, "no input files"},
		{"dry-run and write together", []string{"fix", "--dry-run", "--write", x12}, "cannot be used together"},
		{"write from stdin", []string{"fix", "--write", "-"}, "standard input"},
		{"unknown flag", []string{"fix", "--nope", x12}, "unknown flag"},
		{"unknown format value", []string{"fix", "--format", "edi", x12}, "unknown format"},
		{"unreadable file", []string{"fix", filepath.Join(dir, "absent.x12")}, "absent.x12"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			code, _, stderr := exec(tc.args...)
			if code != exitUsage {
				t.Fatalf("exit = %d, want %d (%s)", code, exitUsage, stderr)
			}
			if !strings.Contains(stderr, tc.wantStderr) {
				t.Errorf("stderr = %q, want it to contain %q", stderr, tc.wantStderr)
			}
		})
	}
}

func TestSubcommandHelp(t *testing.T) {
	code, fmtHelp, _ := exec("fmt", "--help")
	if code != exitClean {
		t.Fatalf("fmt --help exit = %d", code)
	}
	for _, want := range []string{"Usage:", "--check", "--write", "canonical"} {
		if !strings.Contains(fmtHelp, want) {
			t.Errorf("fmt help is missing %q", want)
		}
	}

	code, fixHelp, _ := exec("fix", "--help")
	if code != exitClean {
		t.Fatalf("fix --help exit = %d", code)
	}
	for _, want := range []string{"Usage:", "--unsafe", "--dry-run", "EL3006", "EL1005"} {
		if !strings.Contains(fixHelp, want) {
			t.Errorf("fix help is missing %q", want)
		}
	}

	// The top-level help points at both.
	_, help, _ := exec("--help")
	for _, want := range []string{"edilint fmt", "edilint fix"} {
		if !strings.Contains(help, want) {
			t.Errorf("top-level help is missing %q", want)
		}
	}
}

func TestFmtRoundTripsTheExampleFile(t *testing.T) {
	// The committed example is already canonical, so fmt --check passes it —
	// which is also what keeps the README's example output honest.
	example := filepath.Join(repoRoot(t), "examples", "remittance.x12")
	if code, stdout, stderr := exec("fmt", "--check", example); code != exitClean {
		t.Errorf("exit = %d, want %d (stdout %q, stderr %q)", code, exitClean, stdout, stderr)
	}
}
