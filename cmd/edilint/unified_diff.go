// Unified diff rendering for the fix subcommand's dry runs and unsafe repairs.
// Implemented here rather than imported: the standard library has no diff, and
// a linter that runs inside locked-down healthcare shops takes no dependencies
// for presentation.
package main

import (
	"fmt"
	"strings"
)

// diffContext is how many unchanged lines surround each hunk.
const diffContext = 3

// diffMaxCells bounds the line-comparison table. Beyond it — a repair such as
// terminator normalization that touches nearly every line of a large file —
// the changed region is emitted as one replacement hunk instead of a minimal
// one, which is the same information at the same size without the quadratic
// cost of minimizing it.
const diffMaxCells = 1 << 20

// diffOp is one line-level step of an edit script.
type diffOp struct {
	kind byte // ' ' common, '-' only in old, '+' only in new
	text string
}

// diffUnified renders old and new as a unified diff with conventional a/ b/
// headers. It returns "" when the two are byte-identical.
func diffUnified(path string, oldData, newData []byte) string {
	if string(oldData) == string(newData) {
		return ""
	}
	ops := editScript(splitDiffLines(oldData), splitDiffLines(newData))

	var b strings.Builder
	fmt.Fprintf(&b, "--- a/%s\n+++ b/%s\n", path, path)
	writeHunks(&b, ops)
	return b.String()
}

// splitDiffLines splits data after every line feed, keeping the terminator on
// the line. A final line with no terminator stays distinct from the same text
// with one, which is what makes the "no newline at end of file" cases compare
// and render correctly.
func splitDiffLines(data []byte) []string {
	if len(data) == 0 {
		return nil
	}
	var lines []string
	start := 0
	for i := 0; i < len(data); i++ {
		if data[i] == '\n' {
			lines = append(lines, string(data[start:i+1]))
			start = i + 1
		}
	}
	if start < len(data) {
		lines = append(lines, string(data[start:]))
	}
	return lines
}

// editScript computes a line-level edit script from a to b. The common prefix
// and suffix cost nothing; the changed middle is minimized while the
// comparison table stays within diffMaxCells and replaced wholesale beyond it.
func editScript(a, b []string) []diffOp {
	prefix := 0
	for prefix < len(a) && prefix < len(b) && a[prefix] == b[prefix] {
		prefix++
	}
	suffix := 0
	for suffix < len(a)-prefix && suffix < len(b)-prefix &&
		a[len(a)-1-suffix] == b[len(b)-1-suffix] {
		suffix++
	}
	midA := a[prefix : len(a)-suffix]
	midB := b[prefix : len(b)-suffix]

	ops := make([]diffOp, 0, len(a)+len(b))
	for _, line := range a[:prefix] {
		ops = append(ops, diffOp{' ', line})
	}
	if len(midA)*len(midB) > diffMaxCells {
		for _, line := range midA {
			ops = append(ops, diffOp{'-', line})
		}
		for _, line := range midB {
			ops = append(ops, diffOp{'+', line})
		}
	} else {
		ops = append(ops, lcsOps(midA, midB)...)
	}
	for _, line := range a[len(a)-suffix:] {
		ops = append(ops, diffOp{' ', line})
	}
	return ops
}

// lcsOps derives a minimal edit script from a longest-common-subsequence
// table, emitting deletions before insertions within each changed run.
func lcsOps(a, b []string) []diffOp {
	n, m := len(a), len(b)
	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else if dp[i+1][j] >= dp[i][j+1] {
				dp[i][j] = dp[i+1][j]
			} else {
				dp[i][j] = dp[i][j+1]
			}
		}
	}

	var ops []diffOp
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case a[i] == b[j]:
			ops = append(ops, diffOp{' ', a[i]})
			i++
			j++
		case dp[i+1][j] >= dp[i][j+1]:
			ops = append(ops, diffOp{'-', a[i]})
			i++
		default:
			ops = append(ops, diffOp{'+', b[j]})
			j++
		}
	}
	for ; i < n; i++ {
		ops = append(ops, diffOp{'-', a[i]})
	}
	for ; j < m; j++ {
		ops = append(ops, diffOp{'+', b[j]})
	}
	return ops
}

// writeHunks groups the changed runs of an edit script into hunks with
// diffContext lines of context and renders them.
func writeHunks(b *strings.Builder, ops []diffOp) {
	for i := 0; i < len(ops); {
		if ops[i].kind == ' ' {
			i++
			continue
		}

		// A hunk starts diffContext lines before this change and extends until
		// a gap of more than 2*diffContext common lines separates it from the
		// next change.
		start := i - diffContext
		if start < 0 {
			start = 0
		}
		end := i
		for j := i; j < len(ops); j++ {
			if ops[j].kind != ' ' {
				end = j + 1
				continue
			}
			if j-end >= 2*diffContext {
				break
			}
		}
		stop := end + diffContext
		if stop > len(ops) {
			stop = len(ops)
		}

		// Line numbers are derived by counting what precedes the hunk.
		oldStart, newStart := 1, 1
		for _, op := range ops[:start] {
			if op.kind != '+' {
				oldStart++
			}
			if op.kind != '-' {
				newStart++
			}
		}
		oldCount, newCount := 0, 0
		for _, op := range ops[start:stop] {
			if op.kind != '+' {
				oldCount++
			}
			if op.kind != '-' {
				newCount++
			}
		}
		fmt.Fprintf(b, "@@ -%s +%s @@\n", hunkRange(oldStart, oldCount), hunkRange(newStart, newCount))
		for _, op := range ops[start:stop] {
			writeDiffLine(b, op)
		}
		i = stop
	}
}

// hunkRange renders one side of a hunk header. A count of one is conventionally
// implicit, and an empty range names the line before the insertion point.
func hunkRange(start, count int) string {
	switch count {
	case 0:
		return fmt.Sprintf("%d,0", start-1)
	case 1:
		return fmt.Sprintf("%d", start)
	default:
		return fmt.Sprintf("%d,%d", start, count)
	}
}

// writeDiffLine renders one hunk line, flagging a final line that has no
// terminator the way diff does.
func writeDiffLine(b *strings.Builder, op diffOp) {
	b.WriteByte(op.kind)
	if strings.HasSuffix(op.text, "\n") {
		b.WriteString(op.text)
		return
	}
	b.WriteString(op.text)
	b.WriteString("\n\\ No newline at end of file\n")
}
