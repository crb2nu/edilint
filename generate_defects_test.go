package edilint

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"slices"
	"strings"
	"testing"
)

func TestGenerateCleanBytesUnchanged(t *testing.T) {
	// Digests captured before adding new formats or defect support.
	for kind, want := range map[string]string{
		"837p":  "974140c263948d772e60d0bc34aea1d14cecfb3a73dff4cf37a2937c27fe5621",
		"835":   "732199648207db3ba48e0f8a56fa5384ab0ab6e026b1b18cac81defc59580783",
		"hl7v2": "e0fe75f68037400d140822c2cb23b9d5ffa59860127277da39992c1c3d5fa4e2",
	} {
		var b bytes.Buffer
		if err := Generate(&b, GenerateOptions{Kind: kind}); err != nil {
			t.Fatal(err)
		}
		if got := fmt.Sprintf("%x", sha256.Sum256(b.Bytes())); got != want {
			t.Errorf("%s clean bytes changed: digest %s, want %s", kind, got, want)
		}
	}
}

func TestGenerateDefectCombinations(t *testing.T) {
	for kind, ids := range map[string][]string{
		"837p":    {"EL3005", "EL3006", "EL3007", "EL3008"},
		"835":     {"EL3005", "EL3006", "EL3007", "EL3008"},
		"hl7v2":   {"EL6003", "EL6004"},
		"edifact": {"EL7003", "EL7005", "EL7006"},
	} {
		for _, count := range []int{1, 10} {
			for mask := 0; mask < 1<<len(ids); mask++ {
				t.Run(fmt.Sprintf("%s/%d/%d", kind, count, mask), func(t *testing.T) {
					opts := GenerateOptions{Kind: kind, Count: count, ControlNumber: 999999999, Date: "2024-02-29"}
					var clean, defective, reversed bytes.Buffer
					if err := Generate(&clean, opts); err != nil {
						t.Fatal(err)
					}
					for i, id := range ids {
						if mask&(1<<i) != 0 {
							opts.Defects = append(opts.Defects, id)
						}
					}
					if err := Generate(&defective, opts); err != nil {
						t.Fatal(err)
					}
					requireGeneratedFindings(t, Lint("defects", defective.Bytes(), Options{}), opts.Defects)
					rep, err := LintReader("defects", bytes.NewReader(defective.Bytes()), Options{})
					if err != nil {
						t.Fatal(err)
					}
					requireGeneratedFindings(t, rep, opts.Defects)

					fixed, repairs := Fix(defective.Bytes(), FixOptions{})
					var remaining []string
					if slices.Contains(opts.Defects, "EL7006") {
						remaining = []string{"EL7006"}
					} else if slices.Contains(opts.Defects, "EL3005") {
						remaining = []string{"EL3005"}
					} else if !bytes.Equal(fixed, clean.Bytes()) {
						t.Fatal("fix did not restore the original clean fixture")
					}
					requireGeneratedFindings(t, Lint("fixed", fixed, Options{}), remaining)
					if len(repairs) != len(opts.Defects)-len(remaining) {
						t.Fatalf("unexpected repairs: %+v", repairs)
					}

					slices.Reverse(opts.Defects)
					if err := Generate(&reversed, opts); err != nil {
						t.Fatal(err)
					}
					if !bytes.Equal(defective.Bytes(), reversed.Bytes()) {
						t.Fatal("selection order changed fixture bytes")
					}
				})
			}
		}
	}
}

func requireGeneratedFindings(t *testing.T, rep *Report, want []string) {
	t.Helper()
	var got []string
	for _, f := range rep.Findings {
		got = append(got, f.ID)
		if f.Severity != SeverityError {
			t.Errorf("%s severity = %s, want error", f.ID, f.Severity)
		}
	}
	want = slices.Clone(want)
	slices.Sort(want)
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Fatalf("findings=%v, want exactly %v: %+v", got, want, rep.Findings)
	}
}

func TestGenerateDefectValidation(t *testing.T) {
	for _, tc := range []struct {
		kind    string
		defects []string
		want    string
	}{
		{"837p", []string{""}, "unsupported"},
		{"837p", []string{"EL9999"}, "unsupported"},
		{"837p", []string{"EL1001"}, "unsupported"},
		{"837p", []string{"EL3006", "EL6003"}, "only supported for hl7v2"},
		{"hl7v2", []string{"EL6003", "EL3006"}, "only supported for 837p"},
		{"835", []string{"EL3006", "EL6003"}, "only supported for hl7v2"},
		{"edifact", []string{"EL7004"}, "unsupported"},
		{"edifact", []string{"EL7003", "EL3006"}, "only supported for 837p"},
		{"edifact", []string{"EL6003"}, "only supported for hl7v2"},
		{"837p", []string{"EL7003"}, "only supported for edifact"},
		{"hl7v2", []string{"EL7005"}, "only supported for edifact"},
		{"edifact", []string{"EL7006", " el7006 "}, "duplicate"},
		{"837p", []string{"EL3006", " el3006 "}, "duplicate"},
		{"hl7v2", []string{"EL6004", "EL6004"}, "duplicate"},
		{"837p", []string{"EL3006,EL3007"}, "unsupported"},
	} {
		var b bytes.Buffer
		err := Generate(&b, GenerateOptions{Kind: tc.kind, Defects: tc.defects})
		if err == nil || !strings.Contains(err.Error(), tc.want) || b.Len() != 0 {
			t.Errorf("%+v: error=%v, wrote %d bytes", tc, err, b.Len())
		}
	}
	var b bytes.Buffer
	if err := Generate(&b, GenerateOptions{Kind: "837p", Defects: []string{" el3006 "}}); err != nil {
		t.Fatal(err)
	}
	requireGeneratedFindings(t, Lint("normalized", b.Bytes(), Options{}), []string{"EL3006"})
}
