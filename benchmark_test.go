package edilint

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

type lintWorkload struct {
	name      string
	data      []byte
	opts      Options
	maxAllocs float64
}

func lintWorkloads(t testing.TB) []lintWorkload {
	t.Helper()
	cases := []struct {
		name, fixture string
		opts          Options
		maxAllocs     float64
	}{
		{"X12/clean", "835_clean.x12", Options{}, 110},
		{"X12/malformed", "835_envelope_broken.x12", Options{}, 95},
		{"HL7/clean", "hl7v2_batch_clean.hl7", Options{}, 120},
		{"HL7/malformed", "hl7v2_batch_broken.hl7", Options{}, 155},
		{"Edifact/clean", "edifact_clean.edi", Options{}, 90},
		{"Edifact/malformed", "edifact_broken.edi", Options{}, 170},
		{"Delimited/clean", "eligibility_clean.psv", Options{}, 75},
		{"Delimited/malformed", "eligibility_broken.psv", Options{}, 85},
		{"Fixed/clean", "remit_clean.txt", Options{Format: FormatFixed, Layout: remitLayout()}, 20},
		{"Fixed/malformed", "remit_drift.txt", Options{Format: FormatFixed, Layout: remitLayout()}, 60},
	}
	workloads := make([]lintWorkload, 0, len(cases)+3)
	for _, tc := range cases {
		workloads = append(workloads, lintWorkload{tc.name, readFixture(t, tc.fixture), tc.opts, tc.maxAllocs})
	}
	for _, n := range []int{100, 1000, 10000} {
		var b strings.Builder
		b.WriteString(isa("000000001", "260101", "1200"))
		b.WriteString("GS*HP*SENDER*RECEIVER*20260101*1200*1*X*005010X221A1~ST*835*0001~")
		for i := 0; i < n; i++ {
			b.WriteString("NTE*ADD*SYNTHETIC BENCHMARK RECORD~")
		}
		fmt.Fprintf(&b, "SE*%d*0001~GE*1*1~IEA*1*000000001~", n+2)
		workloads = append(workloads, lintWorkload{fmt.Sprintf("X12/segments_%d", n), []byte(b.String()), Options{}, float64(26*n/10 + 50)})
	}
	return workloads
}

// Allocation counts are stable enough to gate on shared runners. Time and
// bytes/op remain benchmark evidence, not noisy wall-clock pass/fail thresholds.
func TestLintAllocationBudget(t *testing.T) {
	for _, tc := range lintWorkloads(t) {
		t.Run(tc.name, func(t *testing.T) {
			if strings.Contains(tc.name, "/clean") || strings.Contains(tc.name, "/segments_") {
				requireClean(t, Lint("benchmark", tc.data, tc.opts))
			} else if Lint("benchmark", tc.data, tc.opts).Summary.Total == 0 {
				t.Fatal("malformed workload no longer exercises findings")
			}
			got := testing.AllocsPerRun(5, func() { Lint("benchmark", tc.data, tc.opts) })
			t.Logf("%.0f allocations/run; budget %.0f", got, tc.maxAllocs)
			if got > tc.maxAllocs {
				t.Errorf("allocation regression: %.0f allocations/run exceeds budget %.0f", got, tc.maxAllocs)
			}
		})
	}
}

func BenchmarkLint(b *testing.B) {
	for _, tc := range lintWorkloads(b) {
		b.Run(tc.name, func(b *testing.B) {
			b.SetBytes(int64(len(tc.data)))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				Lint("benchmark", tc.data, tc.opts)
			}
		})
	}
}

// Reader benchmarks report total allocation traffic, not peak live memory.
// TestStreamMemoryBound separately checks that memory stays bounded with size.
func BenchmarkLintReader(b *testing.B) {
	for _, tc := range lintWorkloads(b) {
		b.Run(tc.name, func(b *testing.B) {
			b.SetBytes(int64(len(tc.data)))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := LintReader("benchmark", bytes.NewReader(tc.data), tc.opts); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
