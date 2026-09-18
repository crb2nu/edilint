package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/crb2nu/edilint"
)

func TestStatsStreamFlags(t *testing.T) {
	dir := t.TempDir()
	large := write(t, dir, "large.txt", strings.Repeat("A", 200))
	small := write(t, dir, "small.txt", "hello\n")
	for _, flag := range []string{"--max-record-bytes", "--max-state-entries", "--max-state-bytes"} {
		for _, value := range []string{"-1", "abc", "", "9999999999999999999999999"} {
			if code, _, err := execSub("stats", flag+"="+value, small); code != exitUsage || !strings.Contains(err, "non-negative integer") {
				t.Fatalf("%s=%s: code=%d stderr=%s", flag, value, code, err)
			}
		}
		if code, _, _ := execSub("stats", small, flag); code != exitUsage {
			t.Fatalf("missing %s value accepted", flag)
		}
		for _, args := range [][]string{{"stats", flag, "0", small}, {"stats", flag + "=0", small}} {
			if code, _, err := execSub(args...); code != exitClean {
				t.Fatalf("zero %s: code=%d stderr=%s", flag, code, err)
			}
		}
	}
	code, out, err := execSub("stats", "--json", "--max-record-bytes=100", large, small)
	var doc edilint.StatsReport
	if decodeErr := json.Unmarshal([]byte(out), &doc); decodeErr != nil {
		t.Fatal(decodeErr)
	}
	if code != exitUsage || !strings.Contains(err, "record bytes limit exceeded") || len(doc.Files) != 1 || doc.Files[0].File != small {
		t.Fatalf("partial run: code=%d out=%s stderr=%s", code, out, err)
	}
	if code, _, err := execSub("stats", "--max-record-bytes", "201", large); code != exitClean {
		t.Fatalf("raised limit: code=%d stderr=%s", code, err)
	}
	for _, flag := range []string{"--max-state-entries=1", "--max-state-bytes=64"} {
		x12 := write(t, dir, "clean.x12", cleanX12)
		if code, _, err := execSub("stats", flag, x12); code != exitUsage || !strings.Contains(err, "limit exceeded") {
			t.Fatalf("%s: code=%d stderr=%s", flag, code, err)
		}
	}
	if code, _, _ := execSub("stats", "--json=yes", small); code != exitUsage {
		t.Fatal("boolean flag accepted a value")
	}
}

func TestStatsStreamStdin(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if _, err := w.WriteString(cleanX12); err != nil {
		w.Close()
		t.Fatal(err)
	}
	w.Close()
	old := os.Stdin
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = old })
	code, out, stderr := execSub("stats", "--json", "-", "-")
	var doc edilint.StatsReport
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatal(err)
	}
	if code != exitClean || stderr != "" || len(doc.Files) != 1 || doc.Files[0].File != "-" || doc.Files[0].Records != 7 {
		t.Fatalf("stdin: code=%d out=%s stderr=%s", code, out, stderr)
	}
}
