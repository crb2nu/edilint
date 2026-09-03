package main

import "testing"

func TestDiffUnified(t *testing.T) {
	tests := []struct {
		name string
		old  string
		new  string
		want string
	}{
		{
			name: "identical input renders nothing",
			old:  "a\nb\n",
			new:  "a\nb\n",
			want: "",
		},
		{
			name: "one changed line with context",
			old:  "a\nb\nc\nd\ne\nf\ng\n",
			new:  "a\nb\nc\nX\ne\nf\ng\n",
			want: "--- a/f\n+++ b/f\n@@ -1,7 +1,7 @@\n a\n b\n c\n-d\n+X\n e\n f\n g\n",
		},
		{
			name: "distant changes render as separate hunks",
			old:  "1\n2\n3\n4\n5\n6\n7\n8\n9\n10\n11\n12\n13\n14\n15\n",
			new:  "one\n2\n3\n4\n5\n6\n7\n8\n9\n10\n11\n12\n13\n14\nfifteen\n",
			want: "--- a/f\n+++ b/f\n@@ -1,4 +1,4 @@\n-1\n+one\n 2\n 3\n 4\n" +
				"@@ -12,4 +12,4 @@\n 12\n 13\n 14\n-15\n+fifteen\n",
		},
		{
			name: "a gained final newline is a line change",
			old:  "a\nb",
			new:  "a\nb\n",
			want: "--- a/f\n+++ b/f\n@@ -1,2 +1,2 @@\n a\n-b\n\\ No newline at end of file\n+b\n",
		},
		{
			name: "an appended terminator on an unterminated line",
			old:  "IEA*1*000000104",
			new:  "IEA*1*000000104~",
			want: "--- a/f\n+++ b/f\n@@ -1 +1 @@\n-IEA*1*000000104\n\\ No newline at end of file\n" +
				"+IEA*1*000000104~\n\\ No newline at end of file\n",
		},
		{
			name: "insertion into an empty file",
			old:  "",
			new:  "x\n",
			want: "--- a/f\n+++ b/f\n@@ -0,0 +1 @@\n+x\n",
		},
		{
			name: "deletion of the only line",
			old:  "x\n",
			new:  "",
			want: "--- a/f\n+++ b/f\n@@ -1 +0,0 @@\n-x\n",
		},
		{
			name: "carriage returns stay inside the line",
			old:  "a\r\nb\r\n",
			new:  "a\nb\r\n",
			want: "--- a/f\n+++ b/f\n@@ -1,2 +1,2 @@\n-a\r\n+a\n b\r\n",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := diffUnified("f", []byte(tc.old), []byte(tc.new))
			if got != tc.want {
				t.Errorf("diff:\n%q\nwant:\n%q", got, tc.want)
			}
		})
	}
}

func TestDiffUnifiedLargeChangeFallsBackToOneHunk(t *testing.T) {
	// Beyond the comparison budget the changed region is emitted wholesale.
	// The output must still be a valid diff: every old line deleted, every new
	// line added, headers accounting for all of them.
	var oldB, newB []byte
	for i := 0; i < 2048; i++ {
		oldB = append(oldB, "old line\r\n"...)
		newB = append(newB, "old line\n"...)
	}
	got := diffUnified("f", oldB, newB)
	if got == "" {
		t.Fatal("expected a diff")
	}
	want := "@@ -1,2048 +1,2048 @@\n"
	if len(got) < len(want) || got[:14] != "--- a/f\n+++ b/" {
		t.Fatalf("unexpected diff head: %q", got[:40])
	}
	if !containsLine(got, want) {
		t.Errorf("diff should carry the whole-region hunk header %q", want)
	}
}

func containsLine(s, line string) bool {
	for len(s) > 0 {
		i := 0
		for i < len(s) && s[i] != '\n' {
			i++
		}
		if i < len(s) {
			i++
		}
		if s[:i] == line {
			return true
		}
		s = s[i:]
	}
	return false
}
