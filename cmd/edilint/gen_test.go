package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/crb2nu/edilint"
)

func TestGen(t *testing.T) {
	for _, tc := range []struct {
		args []string
		id   string
		want int
	}{
		{[]string{"837p"}, "CLM", 1},
		{[]string{"835"}, "CLP", 1},
		{[]string{"835", "--claims=10", "--date=2024-02-29", "--control=2"}, "CLP", 10},
		{[]string{"837p", "--claims", "10", "--date=2024-02-29", "--control", "2"}, "CLM", 10},
		{[]string{"--claims=2", "--", "837p"}, "CLM", 2},
		{[]string{"hl7v2", "--messages", "5"}, "MSH", 5},
		{[]string{"edifact"}, "UNH", 1},
		{[]string{"edifact", "--messages=10", "--date=2024-02-29", "--control=42"}, "UNH", 10},
		{[]string{"--control=999999999", "--messages=2", "hl7v2"}, "MSH", 2},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			code, out, diag := exec(append([]string{"gen"}, tc.args...)...)
			if code != exitClean || diag != "" {
				t.Fatalf("exit %d: %s", code, diag)
			}
			rep := edilint.Lint("generated", []byte(out), edilint.Options{})
			if len(rep.Findings) != 0 {
				t.Fatalf("generated findings: %+v", rep.Findings)
			}
			stats, err := edilint.Stats("generated", []byte(out))
			if err != nil || stats.RecordsByID[tc.id] != tc.want {
				t.Fatalf("census=%+v error=%v", stats, err)
			}
		})
	}
}

func TestGenErrors(t *testing.T) {
	for _, args := range [][]string{
		{}, {"999"}, {"837p", "extra"}, {"837p", "--nope"},
		{"837p", "--claims"}, {"837p", "--claims=0"}, {"837p", "--claims=-1"},
		{"837p", "--claims=1000001"}, {"837p", "--claims=9999999999999999999999"},
		{"hl7v2", "--messages=abc"}, {"hl7v2", "--claims=2"}, {"837p", "--messages=2"},
		{"837p", "--claims=2", "--messages=2"},
		{"835", "--messages=2"}, {"835", "--claims=0"},
		{"edifact", "--claims=2"}, {"edifact", "--messages=0"},
		{"edifact", "--messages=1000001"}, {"edifact", "--control=1000000000"},
		{"837p", "--control=0"}, {"hl7v2", "--control=1000000000"},
		{"837p", "--date=2026-02-29"}, {"837p", "--date="}, {"--help=true"},
		{"--", "837p", "--claims=2"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			code, out, diag := exec(append([]string{"gen"}, args...)...)
			if code != exitUsage || out != "" || diag == "" {
				t.Fatalf("exit=%d stdout=%q stderr=%q", code, out, diag)
			}
		})
	}
}

func TestGenHelp(t *testing.T) {
	for _, args := range [][]string{{"gen", "--help"}, {"gen", "hl7v2", "-h"}} {
		code, out, diag := exec(args...)
		if code != exitClean || !strings.Contains(out, "edilint gen 837p") || diag != "" {
			t.Fatalf("exit=%d stdout=%q stderr=%q", code, out, diag)
		}
	}
}

type genErrorWriter struct{}

func (genErrorWriter) Write([]byte) (int, error) { return 0, errors.New("output closed") }

func TestGenOutputFailure(t *testing.T) {
	var diag bytes.Buffer
	if code := run([]string{"gen", "837p"}, genErrorWriter{}, &diag); code != exitUsage || !strings.Contains(diag.String(), "output closed") {
		t.Fatalf("exit=%d stderr=%q", code, diag.String())
	}
}

func TestGenIgnoresLintConfig(t *testing.T) {
	chdir(t, t.TempDir())
	write(t, ".", ".edilint.yml", "not: [valid yaml")
	code, out, diag := exec("gen", "837p")
	if code != exitClean || !strings.HasPrefix(out, "ISA*") || diag != "" {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, out, diag)
	}
}

func TestGenDefects(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want []string
	}{
		{[]string{"837p", "--defect", "EL3005", "--defect=EL3006"}, []string{"EL3005", "EL3006"}},
		{[]string{"835", "--claims=2", "--defect=EL3007", "--defect=EL3008"}, []string{"EL3007", "EL3008"}},
		{[]string{"--defect=el6003", "hl7v2", "--messages=3", "--defect", "EL6004"}, []string{"EL6003", "EL6004"}},
		{[]string{"edifact", "--messages=3", "--defect=el7003", "--defect=EL7005", "--defect=EL7006"}, []string{"EL7003", "EL7005", "EL7006"}},
	} {
		code, out, diag := exec(append([]string{"gen"}, tc.args...)...)
		if code != exitClean || diag != "" {
			t.Fatalf("generation exit=%d stderr=%q", code, diag)
		}
		path := write(t, t.TempDir(), "defects.edi", out)
		code, report, diag := exec("--no-config", path)
		if code != exitFindings || diag != "" {
			t.Fatalf("lint exit=%d stderr=%q", code, diag)
		}
		for _, id := range tc.want {
			if !strings.Contains(report, id) {
				t.Errorf("missing %s in report: %s", id, report)
			}
		}
	}
}

func TestGenDefectErrors(t *testing.T) {
	for _, args := range [][]string{
		{"837p", "--defect"}, {"837p", "--defect="},
		{"837p", "--defect=EL9999"}, {"837p", "--defect=EL6003"},
		{"hl7v2", "--defect=EL3006"},
		{"835", "--defect=EL6003"},
		{"edifact", "--defect=EL7004"}, {"edifact", "--defect=EL3006"},
		{"edifact", "--defect=EL7003", "--defect=el7003"},
		{"837p", "--defect=EL3006", "--defect=el3006"},
	} {
		code, out, diag := exec(append([]string{"gen"}, args...)...)
		if code != exitUsage || out != "" || diag == "" {
			t.Errorf("%v: exit=%d stdout=%q stderr=%q", args, code, out, diag)
		}
	}
}
