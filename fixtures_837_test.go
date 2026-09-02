package edilint

import (
	"bytes"
	"testing"
)

func Test837PFixtures(t *testing.T) {
	for _, name := range []string{
		"837p_claims_clean.x12",
		"837p_claims_multi_st.x12",
	} {
		t.Run(name, func(t *testing.T) {
			requireClean(t, lintFixture(t, name, Options{}))
		})
	}

	tests := []struct {
		name      string
		id        string
		record    string
		recordNum int
	}{
		{"837p_claims_se_short.x12", "EL3006", "SE", 90},
		{"837p_claims_dup_st.x12", "EL3009", "ST", 91},
		{"837p_claims_unclosed.x12", "EL3003", "ST", 3},
		{"837p_claims_homoglyph.x12", "EL1005", "NM1", 15},
		{"837p_claims_mixed_term.x12", "EL2004", "SV1", 24},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rep := lintFixture(t, tc.name, Options{})
			if got := len(rep.Findings); got != 1 {
				t.Fatalf("got %d findings, want 1: %v", got, ruleNames(rep))
			}
			f := rep.Findings[0]
			if f.ID != tc.id || f.Record != tc.record || f.RecordNumber != tc.recordNum {
				t.Errorf("finding = (%s, record %d, segment %s), want (%s, record %d, segment %s)",
					f.ID, f.RecordNumber, f.Record, tc.id, tc.recordNum, tc.record)
			}
		})
	}
}

func Test837PFixtureTerminators(t *testing.T) {
	fixtures := []string{
		"837p_claims_clean.x12",
		"837p_claims_multi_st.x12",
		"837p_claims_se_short.x12",
		"837p_claims_dup_st.x12",
		"837p_claims_unclosed.x12",
		"837p_claims_homoglyph.x12",
		"837p_claims_mixed_term.x12",
	}
	for _, name := range fixtures {
		t.Run(name, func(t *testing.T) {
			data := readFixture(t, name)
			wantCRLF := 0
			if name == "837p_claims_mixed_term.x12" {
				wantCRLF = 1
			}
			if got := bytes.Count(data, []byte("\r\n")); got != wantCRLF {
				t.Errorf("CRLF count = %d, want %d", got, wantCRLF)
			}
			withoutCRLF := bytes.ReplaceAll(data, []byte("\r\n"), nil)
			if bytes.ContainsAny(withoutCRLF, "\r\n") {
				t.Error("fixture contains an unexpected line break")
			}
		})
	}
}
