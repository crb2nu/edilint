package edilint

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

// statsFixture builds the census of a testdata fixture.
func statsFixture(t *testing.T, name string) *FileStats {
	t.Helper()
	fs, err := Stats(name, readFixture(t, name))
	if err != nil {
		t.Fatalf("Stats(%s): %v", name, err)
	}
	return fs
}

// TestStatsRemittanceExample is the acceptance case: the census of the
// README's remittance example must match these hand-counted values. The file
// holds one interchange, one HP functional group and one 835 transaction set
// of 27 segments, 31 segments in all.
func TestStatsRemittanceExample(t *testing.T) {
	const path = "examples/remittance.x12"
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	fs, err := Stats(path, data)
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}

	if fs.File != path || fs.Format != FormatX12 {
		t.Errorf("file = %q format = %q, want %q and x12", fs.File, fs.Format, path)
	}
	if fs.Bytes != len(data) {
		t.Errorf("bytes = %d, want %d", fs.Bytes, len(data))
	}
	if fs.Records != 31 {
		t.Errorf("records = %d, want the hand-counted 31", fs.Records)
	}

	wantHistogram := map[string]int{
		"ISA": 1, "GS": 1, "ST": 1, "BPR": 1, "TRN": 1, "REF": 2, "DTM": 3,
		"N1": 2, "N3": 2, "N4": 2, "PER": 1, "LX": 1, "CLP": 2, "NM1": 2,
		"SVC": 2, "CAS": 2, "AMT": 2, "SE": 1, "GE": 1, "IEA": 1,
	}
	if !reflect.DeepEqual(fs.RecordsByID, wantHistogram) {
		t.Errorf("histogram = %v, want the hand-counted %v", fs.RecordsByID, wantHistogram)
	}

	wantSeparators := &SeparatorStats{Element: "*", SubElement: ":", Segment: "~", Repetition: "^"}
	if !reflect.DeepEqual(fs.Separators, wantSeparators) {
		t.Errorf("separators = %+v, want %+v", fs.Separators, wantSeparators)
	}

	// The file is uppercase throughout and every ":" in content is the
	// declared sub-element separator, so the basic set admits everything.
	wantCharset := &CharsetStats{Profile: "basic"}
	if !reflect.DeepEqual(fs.Charset, wantCharset) {
		t.Errorf("charset = %+v, want %+v", fs.Charset, wantCharset)
	}

	wantEnvelope := &EnvelopeStats{
		Interchanges:       1,
		Groups:             1,
		Transactions:       1,
		GroupsByCode:       map[string]int{"HP": 1},
		TransactionsByType: map[string]int{"835": 1},
		ISA13:              &ValueRange{Min: "000000001", Max: "000000001"},
		GS06:               &ValueRange{Min: "1", Max: "1"},
		ST02:               &ValueRange{Min: "0001", Max: "0001"},
		ISADates:           &ValueRange{Min: "260115", Max: "260115"},
		GSDates:            &ValueRange{Min: "20260115", Max: "20260115"},
	}
	if !reflect.DeepEqual(fs.Envelope, wantEnvelope) {
		t.Errorf("envelope = %+v, want %+v", fs.Envelope, wantEnvelope)
	}
}

// TestStatsMultiEnvelope covers ranges that actually range: two interchanges
// with different codes, control numbers and dates.
func TestStatsMultiEnvelope(t *testing.T) {
	fs := statsFixture(t, "stats_multi.x12")

	if fs.Records != 17 {
		t.Errorf("records = %d, want 17", fs.Records)
	}
	want := &EnvelopeStats{
		Interchanges:       2,
		Groups:             2,
		Transactions:       3,
		GroupsByCode:       map[string]int{"HP": 1, "HC": 1},
		TransactionsByType: map[string]int{"835": 2, "837": 1},
		ISA13:              &ValueRange{Min: "000000301", Max: "000000302"},
		GS06:               &ValueRange{Min: "301", Max: "302"},
		ST02:               &ValueRange{Min: "0001", Max: "0005"},
		ISADates:           &ValueRange{Min: "260110", Max: "260112"},
		GSDates:            &ValueRange{Min: "20260110", Max: "20260112"},
	}
	if !reflect.DeepEqual(fs.Envelope, want) {
		t.Errorf("envelope = %+v, want %+v", fs.Envelope, want)
	}
}

// TestStatsNonX12Formats verifies that other formats get the census that
// applies to them — counts and a histogram — and none of the X12 sections.
func TestStatsNonX12Formats(t *testing.T) {
	tests := []struct {
		name          string
		fixture       string
		format        Format
		records       int
		wantHistogram map[string]int
	}{
		{
			name:    "pipe-delimited extract",
			fixture: "eligibility_clean.psv",
			format:  FormatDelimited,
			records: 5,
			wantHistogram: map[string]int{
				"HDR": 1, "DTL": 3, "TRL": 1,
			},
		},
		{
			name:    "HL7v2 message",
			fixture: "hl7v2_clean.hl7",
			format:  FormatHL7v2,
			records: 4,
			wantHistogram: map[string]int{
				"MSH": 1, "EVN": 1, "PID": 1, "PV1": 1,
			},
		},
		{
			// The UNA service string advice is not a segment, so six segments
			// follow it and it does not appear in the histogram.
			name:    "EDIFACT interchange",
			fixture: "edifact_clean.edi",
			format:  FormatEdifact,
			records: 6,
			wantHistogram: map[string]int{
				"UNB": 1, "UNH": 1, "BGM": 1, "DTM": 1, "UNT": 1, "UNZ": 1,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fs := statsFixture(t, tc.fixture)
			if fs.Format != tc.format {
				t.Errorf("format = %q, want %q", fs.Format, tc.format)
			}
			if fs.Records != tc.records {
				t.Errorf("records = %d, want %d", fs.Records, tc.records)
			}
			if !reflect.DeepEqual(fs.RecordsByID, tc.wantHistogram) {
				t.Errorf("histogram = %v, want %v", fs.RecordsByID, tc.wantHistogram)
			}
			if fs.Envelope != nil || fs.Charset != nil || fs.Separators != nil {
				t.Errorf("the X12 sections must be absent for %s input", tc.format)
			}
		})
	}
}

func TestStatsCharsetObserved(t *testing.T) {
	base := string(readFixture(t, "diff_base_835.x12"))

	tests := []struct {
		name string
		body string
		want CharsetStats
	}{
		{
			name: "uppercase content fits the basic set",
			body: base,
			want: CharsetStats{Profile: "basic"},
		},
		{
			name: "lowercase letters need the extended set",
			body: editSegment(t, base, "*HALE*", "*Hale*"),
			want: CharsetStats{Profile: "extended", ExtendedOnly: 3},
		},
		{
			name: "a non-ASCII character is beyond the extended set",
			body: editSegment(t, base, "*MORGAN*", "*MORG\u00c1N*"),
			want: CharsetStats{Profile: "beyond-extended", BeyondExtended: 1},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fs, err := Stats("charset.x12", []byte(tc.body))
			if err != nil {
				t.Fatalf("Stats: %v", err)
			}
			if fs.Charset == nil || *fs.Charset != tc.want {
				t.Errorf("charset = %+v, want %+v", fs.Charset, tc.want)
			}
		})
	}
}

func TestStatsBinaryInputIsAnError(t *testing.T) {
	body := make([]byte, 4096)
	state := uint32(0x2545f491)
	for i := range body {
		state = state*1664525 + 1013904223
		body[i] = byte(state >> 24)
	}
	if _, err := Stats("archive.bin", body); err == nil ||
		!strings.Contains(err.Error(), "does not look like text") {
		t.Errorf("binary input should be refused with an explanation, got %v", err)
	}
}

func TestValueRangeOrdering(t *testing.T) {
	tests := []struct {
		name   string
		values []string
		want   *ValueRange
	}{
		{"no values", nil, nil},
		{"one value", []string{"7"}, &ValueRange{Min: "7", Max: "7"}},
		{"numeric ordering beats lexical", []string{"10", "2"}, &ValueRange{Min: "2", Max: "10"}},
		{"zero-padded numerics keep their text", []string{"0001", "0005"}, &ValueRange{Min: "0001", Max: "0005"}},
		{"non-numeric values order lexically", []string{"A10", "A2"}, &ValueRange{Min: "A10", Max: "A2"}},
		{"mixed values order lexically", []string{"20", "A2"}, &ValueRange{Min: "20", Max: "A2"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := rangeOf(tc.values); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("rangeOf(%v) = %+v, want %+v", tc.values, got, tc.want)
			}
		})
	}
}

func TestStatsWriteText(t *testing.T) {
	sr := NewStatsReport()
	sr.Add(statsFixture(t, "stats_multi.x12"))
	sr.Add(statsFixture(t, "eligibility_clean.psv"))

	var buf strings.Builder
	if err := sr.WriteText(&buf); err != nil {
		t.Fatalf("WriteText: %v", err)
	}
	out := buf.String()
	for _, want := range []string{
		"stats_multi.x12: x12,",
		"17 segments",
		"interchanges: 2, ISA13 000000301..000000302, ISA09 dates 260110..260112",
		"groups: 2 (HC: 1, HP: 1), GS06 301..302, GS04 dates 20260110..20260112",
		"transactions: 3 (835: 2, 837: 1), ST02 0001..0005",
		`separators: element "*", sub-element ":", segment "~", repetition "^"`,
		"eligibility_clean.psv: delimited,",
		"5 records",
		"records by ID: DTL 3, HDR 1, TRL 1",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "\n\n") {
		t.Errorf("per-file sections should be separated by a blank line:\n%s", out)
	}
}

// statsSchemaPath is the committed schema for the current StatsSchemaVersion.
const statsSchemaPath = "schema/stats.v1.schema.json"

func TestStatsSchemaMatchesStructs(t *testing.T) {
	doc := loadSchema(t, statsSchemaPath)

	tests := []struct {
		name string
		typ  reflect.Type
		obj  map[string]any
	}{
		{"StatsReport", reflect.TypeOf(StatsReport{}), doc},
		{"FileStats", reflect.TypeOf(FileStats{}), schemaObject(t, doc, "$defs", "fileStats")},
		{"SeparatorStats", reflect.TypeOf(SeparatorStats{}), schemaObject(t, doc, "$defs", "separators")},
		{"CharsetStats", reflect.TypeOf(CharsetStats{}), schemaObject(t, doc, "$defs", "charset")},
		{"EnvelopeStats", reflect.TypeOf(EnvelopeStats{}), schemaObject(t, doc, "$defs", "envelope")},
		{"ValueRange", reflect.TypeOf(ValueRange{}), schemaObject(t, doc, "$defs", "valueRange")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requireSchemaMatchesStruct(t, tt.obj, tt.typ)
		})
	}
}

func TestStatsSchemaVersionConstMatchesCode(t *testing.T) {
	doc := loadSchema(t, statsSchemaPath)
	got, ok := schemaObject(t, doc, "properties", "version")["const"].(float64)
	if !ok || int(got) != StatsSchemaVersion {
		t.Errorf("schema version const %v, want StatsSchemaVersion %d; a new version needs a new schema file",
			schemaObject(t, doc, "properties", "version")["const"], StatsSchemaVersion)
	}
	if !strings.Contains(statsSchemaPath, "v1") {
		t.Errorf("schema file name %q should carry the version", statsSchemaPath)
	}
}

func TestStatsSchemaEnumsMatchConstants(t *testing.T) {
	doc := loadSchema(t, statsSchemaPath)

	formats := schemaObject(t, doc, "$defs", "fileStats", "properties", "format")["enum"].([]any)
	wantFormats := []string{
		string(FormatX12), string(FormatHL7v2), string(FormatEdifact),
		string(FormatDelimited), string(FormatFixed), string(FormatText),
	}
	if got := toStrings(formats); !sameSet(got, wantFormats) {
		t.Errorf("format enum %v != emitted formats %v", got, wantFormats)
	}

	profiles := schemaObject(t, doc, "$defs", "charset", "properties", "profile")["enum"].([]any)
	wantProfiles := []string{string(CharsetBasic), string(CharsetExtended), "beyond-extended"}
	if got := toStrings(profiles); !sameSet(got, wantProfiles) {
		t.Errorf("profile enum %v != observed profiles %v", got, wantProfiles)
	}
}

// TestStatsSchemaAcceptsRealOutput walks a real multi-format document and
// fails on any emitted key the schema does not declare.
func TestStatsSchemaAcceptsRealOutput(t *testing.T) {
	sr := NewStatsReport()
	sr.Add(statsFixture(t, "stats_multi.x12"))
	sr.Add(statsFixture(t, "eligibility_clean.psv"))
	sr.Add(statsFixture(t, "hl7v2_clean.hl7"))

	var buf strings.Builder
	if err := sr.WriteJSON(&buf); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}
	var emitted map[string]any
	if err := json.Unmarshal([]byte(buf.String()), &emitted); err != nil {
		t.Fatalf("WriteJSON output is not valid JSON: %v", err)
	}

	doc := loadSchema(t, statsSchemaPath)
	requireDeclared(t, "$", emitted, schemaObject(t, doc, "properties"), doc)
}
