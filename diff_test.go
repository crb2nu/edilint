package edilint

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// diffFixtures compares two testdata fixtures and fails the test on a
// comparison error, which none of the committed fixtures should provoke.
func diffFixtures(t *testing.T, a, b string, opts DiffOptions) *DiffReport {
	t.Helper()
	rep, err := DiffX12(a, readFixture(t, a), b, readFixture(t, b), opts)
	if err != nil {
		t.Fatalf("DiffX12(%s, %s): %v", a, b, err)
	}
	return rep
}

// diffStrings compares two in-memory documents under fixed names.
func diffStrings(t *testing.T, a, b string, opts DiffOptions) *DiffReport {
	t.Helper()
	rep, err := DiffX12("a.x12", []byte(a), "b.x12", []byte(b), opts)
	if err != nil {
		t.Fatalf("DiffX12: %v", err)
	}
	return rep
}

// edit applies one string replacement and fails the test if nothing changed,
// so a stale pattern cannot silently turn a test into a no-op.
func edit(t *testing.T, base, from, to string) string {
	t.Helper()
	out := strings.Replace(base, from, to, 1)
	if out == base {
		t.Fatalf("edit %q -> %q did not apply", from, to)
	}
	return out
}

func TestDiffIdenticalFilesReportNothing(t *testing.T) {
	rep := diffFixtures(t, "diff_base_835.x12", "diff_base_835.x12", DiffOptions{})
	if !rep.Identical {
		t.Errorf("identical fixture pair reported %d difference(s): %+v", len(rep.Differences), rep.Differences)
	}
	if rep.Summary != (DiffSummary{}) {
		t.Errorf("summary = %+v, want all zeros", rep.Summary)
	}
	if rep.Differences == nil {
		t.Error("differences must be an empty array, not nil")
	}
}

// TestDiffReportsExactlyTheChangedElement is the acceptance case: a pair
// differing in exactly one element reports exactly that element, with its
// segment path. Each case edits one value of the base fixture.
func TestDiffReportsExactlyTheChangedElement(t *testing.T) {
	base := string(readFixture(t, "diff_base_835.x12"))

	tests := []struct {
		name       string
		from, to   string
		wantPath   DiffPath
		designator string
		wantA      string
		wantB      string
	}{
		{
			name: "claim charge in the first transaction",
			from: "*1200.00*", to: "*1250.00*",
			wantPath: DiffPath{
				Interchange: 1, Group: 1,
				Transaction: 1, TransactionType: "835", TransactionControl: "0001",
				Segment: 8, SegmentID: "CLP", Element: 3,
			},
			designator: "CLP03",
			wantA:      "1200.00", wantB: "1250.00",
		},
		{
			name: "patient name in the first transaction",
			from: "*MORGAN*", to: "*MORGANA*",
			wantPath: DiffPath{
				Interchange: 1, Group: 1,
				Transaction: 1, TransactionType: "835", TransactionControl: "0001",
				Segment: 9, SegmentID: "NM1", Element: 4,
			},
			designator: "NM104",
			wantA:      "MORGAN", wantB: "MORGANA",
		},
		{
			name: "service date in the second transaction",
			from: "*20260209~", to: "*20260210~",
			wantPath: DiffPath{
				Interchange: 1, Group: 1,
				Transaction: 2, TransactionType: "835", TransactionControl: "0002",
				Segment: 11, SegmentID: "DTM", Element: 2,
			},
			designator: "DTM02",
			wantA:      "20260209", wantB: "20260210",
		},
		{
			name: "group date on the GS segment",
			from: "*20260220*0915*201*", to: "*20260221*0915*201*",
			wantPath: DiffPath{
				Interchange: 1, Group: 1,
				Segment: 1, SegmentID: "GS", Element: 4,
			},
			designator: "GS04",
			wantA:      "20260220", wantB: "20260221",
		},
		{
			name: "sender on the ISA segment",
			from: "*BRIDGEPOINTPLAN*ZZ*", to: "*BRIDGEPORTPLAN *ZZ*",
			wantPath: DiffPath{
				Interchange: 1,
				Segment:     1, SegmentID: "ISA", Element: 6,
			},
			designator: "ISA06",
			wantA:      "BRIDGEPOINTPLAN", wantB: "BRIDGEPORTPLAN",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rep := diffStrings(t, base, edit(t, base, tc.from, tc.to), DiffOptions{})
			if len(rep.Differences) != 1 {
				t.Fatalf("got %d difference(s), want exactly 1: %+v", len(rep.Differences), rep.Differences)
			}
			d := rep.Differences[0]
			if d.Kind != DiffElement {
				t.Errorf("kind = %q, want %q", d.Kind, DiffElement)
			}
			if d.Path != tc.wantPath {
				t.Errorf("path = %+v, want %+v", d.Path, tc.wantPath)
			}
			if d.Designator != tc.designator {
				t.Errorf("designator = %q, want %q", d.Designator, tc.designator)
			}
			if d.A != tc.wantA || d.B != tc.wantB {
				t.Errorf("values = %q vs %q, want %q vs %q", d.A, d.B, tc.wantA, tc.wantB)
			}
			if d.Cosmetic {
				t.Error("a value difference must not be marked cosmetic")
			}
			if !strings.Contains(d.Message, tc.designator) {
				t.Errorf("message should carry the designator, got %q", d.Message)
			}
		})
	}
}

// The cosmetic fixture holds the base content re-encoded without newlines
// after segment terminators, with trailing whitespace inside one element, and
// with a trailing empty element on one segment.
func TestDiffCosmeticPair(t *testing.T) {
	t.Run("default comparison ignores it all", func(t *testing.T) {
		rep := diffFixtures(t, "diff_base_835.x12", "diff_cosmetic.x12", DiffOptions{})
		if !rep.Identical {
			t.Errorf("cosmetic-only pair reported %d difference(s): %+v",
				len(rep.Differences), rep.Differences)
		}
	})

	t.Run("strict reports it, and marks it cosmetic", func(t *testing.T) {
		rep := diffFixtures(t, "diff_base_835.x12", "diff_cosmetic.x12", DiffOptions{Strict: true})
		if rep.Identical {
			t.Fatal("strict comparison should report the cosmetic differences")
		}
		kinds := map[string]int{}
		for _, d := range rep.Differences {
			kinds[d.Kind]++
			if !d.Cosmetic {
				t.Errorf("difference is not marked cosmetic: %s", d.Message)
			}
		}
		if kinds[DiffTerminator] == 0 {
			t.Errorf("strict comparison should report terminator style, got kinds %v", kinds)
		}
		if kinds[DiffElement] != 1 {
			t.Errorf("got %d element difference(s), want 1 for the trailing whitespace", kinds[DiffElement])
		}
		if rep.Summary.Cosmetic != rep.Summary.Total {
			t.Errorf("summary = %+v; every difference here is cosmetic", rep.Summary)
		}
	})
}

func TestDiffAddedAndRemovedTransactions(t *testing.T) {
	t.Run("a transaction only in the second file", func(t *testing.T) {
		rep := diffFixtures(t, "diff_base_835.x12", "diff_extra_txn.x12", DiffOptions{})
		if len(rep.Differences) != 2 {
			t.Fatalf("got %d difference(s), want the added transaction and the GE01 recount: %+v",
				len(rep.Differences), rep.Differences)
		}

		ge := firstDiff(rep, DiffElement)
		if ge == nil || ge.Designator != "GE01" || ge.A != "2" || ge.B != "3" {
			t.Errorf("expected a GE01 difference of 2 vs 3, got %+v", ge)
		}

		added := firstDiff(rep, DiffTransactionAdded)
		if added == nil {
			t.Fatalf("no transaction-added difference in %+v", rep.Differences)
		}
		wantPath := DiffPath{
			Interchange: 1, Group: 1,
			Transaction: 3, TransactionType: "835", TransactionControl: "0003",
		}
		if added.Path != wantPath {
			t.Errorf("path = %+v, want %+v", added.Path, wantPath)
		}
		if added.A != "" || added.B == "" || added.BLine == 0 || added.ALine != 0 {
			t.Errorf("an added transaction should carry only second-file content, got %+v", added)
		}
		if rep.Summary.Added != 1 || rep.Summary.Removed != 0 {
			t.Errorf("summary = %+v", rep.Summary)
		}
	})

	t.Run("the same pair reversed reports a removal", func(t *testing.T) {
		rep := diffFixtures(t, "diff_extra_txn.x12", "diff_base_835.x12", DiffOptions{})
		removed := firstDiff(rep, DiffTransactionRemoved)
		if removed == nil {
			t.Fatalf("no transaction-removed difference in %+v", rep.Differences)
		}
		if removed.B != "" || removed.A == "" {
			t.Errorf("a removed transaction should carry only first-file content, got %+v", removed)
		}
		if rep.Summary.Removed != 1 || rep.Summary.Added != 0 {
			t.Errorf("summary = %+v", rep.Summary)
		}
	})
}

func TestDiffAddedSegmentWithinATransaction(t *testing.T) {
	base := string(readFixture(t, "diff_base_835.x12"))
	// Insert a REF after TRN in the first transaction and correct the SE01
	// recount, which is what a generator emitting one more segment produces.
	with := edit(t, base, "TRN*1*20260220BP0001*1443322110~\n",
		"TRN*1*20260220BP0001*1443322110~\nREF*EV*BP835~\n")
	with = edit(t, with, "SE*13*0001~", "SE*14*0001~")

	rep := diffStrings(t, base, with, DiffOptions{})
	if len(rep.Differences) != 2 {
		t.Fatalf("got %d difference(s), want the added segment and the SE01 recount: %+v",
			len(rep.Differences), rep.Differences)
	}

	added := firstDiff(rep, DiffSegmentAdded)
	if added == nil {
		t.Fatalf("no segment-added difference in %+v", rep.Differences)
	}
	if added.Path.SegmentID != "REF" || added.Path.Segment != 4 || added.Path.Transaction != 1 {
		t.Errorf("path = %+v, want the REF as segment 4 of transaction 1", added.Path)
	}
	if added.B != "REF*EV*BP835" {
		t.Errorf("b = %q, want the segment text", added.B)
	}

	se := firstDiff(rep, DiffElement)
	if se == nil || se.Designator != "SE01" || se.A != "13" || se.B != "14" {
		t.Errorf("expected an SE01 difference of 13 vs 14, got %+v", se)
	}

	// The same pair reversed reports the segment as removed.
	reversed := diffStrings(t, with, base, DiffOptions{})
	if removed := firstDiff(reversed, DiffSegmentRemoved); removed == nil {
		t.Errorf("reversed comparison should report segment-removed, got %+v", reversed.Differences)
	}
}

func TestDiffSeparatorStyleIsNotADifference(t *testing.T) {
	// The same content re-encoded with different separators: pipe for element,
	// bare newline for segment terminator. Alignment is by envelope position
	// and element value, so nothing differs until --strict looks at the
	// terminators themselves.
	base := string(readFixture(t, "diff_base_835.x12"))
	other := strings.ReplaceAll(base, "*", "|")
	other = strings.ReplaceAll(other, "~\n", "\n")

	rep := diffStrings(t, base, other, DiffOptions{})
	if !rep.Identical {
		t.Errorf("re-encoded file reported %d difference(s): %+v", len(rep.Differences), rep.Differences)
	}

	strict := diffStrings(t, base, other, DiffOptions{Strict: true})
	if strict.Identical {
		t.Error("strict comparison should report the terminator style")
	}
}

func TestDiffTransactionTypeChangeIsAddAndRemove(t *testing.T) {
	// Transaction sets pair by position only when their ST01 types agree;
	// a type change reads as one transaction gone and another arrived.
	base := string(readFixture(t, "diff_base_835.x12"))
	changed := edit(t, base, "ST*835*0002~", "ST*837*0002~")
	rep := diffStrings(t, base, changed, DiffOptions{})
	if firstDiff(rep, DiffTransactionRemoved) == nil || firstDiff(rep, DiffTransactionAdded) == nil {
		t.Errorf("a changed ST01 should read as removed plus added, got %+v", rep.Differences)
	}
}

func TestDiffErrors(t *testing.T) {
	x12 := readFixture(t, "diff_base_835.x12")

	tests := []struct {
		name string
		a, b []byte
		want string
	}{
		{
			name: "first input is not X12",
			a:    []byte("MSH|^~\\&|SENDER|APP|RECEIVER|APP|20260220||ADT^A01|1|P|2.5.1\r"),
			b:    x12,
			want: "not an X12 interchange",
		},
		{
			name: "second input is not X12",
			a:    x12,
			b:    []byte("HDR|X|20260220\nDTL|1\n"),
			want: "not an X12 interchange",
		},
		{
			name: "truncated ISA",
			a:    []byte("ISA*00*truncated"),
			b:    x12,
			want: "missing or truncated",
		},
		{
			name: "binary input",
			a:    []byte{0xFF, 0xFE, 0xFD, 0x00, 0x81, 0x91, 0xA1, 0xB1},
			b:    x12,
			want: "does not look like text",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := DiffX12("a.x12", tc.a, "b.x12", tc.b, DiffOptions{})
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q should contain %q", err, tc.want)
			}
		})
	}
}

func TestDiffWriteText(t *testing.T) {
	rep := diffFixtures(t, "diff_base_835.x12", "diff_one_element.x12", DiffOptions{})
	var buf strings.Builder
	if err := rep.WriteText(&buf); err != nil {
		t.Fatalf("WriteText: %v", err)
	}
	out := buf.String()
	for _, want := range []string{
		"CLP03",
		`"1200.00"`,
		`"1250.00"`,
		"transaction 1 (835, control 0001)",
		"1 difference (1 element, 0 added, 0 removed)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}

	identical := diffFixtures(t, "diff_base_835.x12", "diff_base_835.x12", DiffOptions{})
	buf.Reset()
	if err := identical.WriteText(&buf); err != nil {
		t.Fatalf("WriteText: %v", err)
	}
	if buf.String() != "" {
		t.Errorf("identical files should write nothing, got %q", buf.String())
	}
}

// firstDiff returns the first difference of the given kind, or nil.
func firstDiff(rep *DiffReport, kind string) *Difference {
	for i := range rep.Differences {
		if rep.Differences[i].Kind == kind {
			return &rep.Differences[i]
		}
	}
	return nil
}

// diffSchemaPath is the committed schema for the current DiffSchemaVersion.
const diffSchemaPath = "schema/diff.v1.schema.json"

// diffKinds lists every kind constant the differ can emit, for the enum check.
var diffKinds = []string{
	DiffElement, DiffTerminator,
	DiffSegmentAdded, DiffSegmentRemoved,
	DiffTransactionAdded, DiffTransactionRemoved,
	DiffGroupAdded, DiffGroupRemoved,
	DiffInterchangeAdded, DiffInterchangeRemoved,
}

// requireSchemaMatchesStruct holds one schema object to one struct: the
// property list must equal the marshaled fields, the required list must equal
// the always-present fields, and additionalProperties must be false. It is the
// same contract TestSchemaMatchesStructs enforces for the lint report.
func requireSchemaMatchesStruct(t *testing.T, obj map[string]any, typ reflect.Type) {
	t.Helper()
	all, alwaysPresent := jsonTagSets(t, typ)

	props := map[string]bool{}
	for key := range schemaObject(t, obj, "properties") {
		props[key] = true
	}
	if !reflect.DeepEqual(props, all) {
		t.Errorf("schema properties %v\n!= struct fields %v", sortedKeys(props), sortedKeys(all))
	}

	required := map[string]bool{}
	rawRequired, ok := obj["required"].([]any)
	if !ok {
		t.Fatal("schema object has no required array")
	}
	for _, r := range rawRequired {
		required[r.(string)] = true
	}
	if !reflect.DeepEqual(required, alwaysPresent) {
		t.Errorf("schema required %v\n!= always-present struct fields %v",
			sortedKeys(required), sortedKeys(alwaysPresent))
	}

	if strict, ok := obj["additionalProperties"].(bool); !ok || strict {
		t.Error("additionalProperties must be false, or the version discipline is unenforceable")
	}
}

func TestDiffSchemaMatchesStructs(t *testing.T) {
	doc := loadSchema(t, diffSchemaPath)

	tests := []struct {
		name string
		typ  reflect.Type
		obj  map[string]any
	}{
		{"DiffReport", reflect.TypeOf(DiffReport{}), doc},
		{"Difference", reflect.TypeOf(Difference{}), schemaObject(t, doc, "$defs", "difference")},
		{"DiffPath", reflect.TypeOf(DiffPath{}), schemaObject(t, doc, "$defs", "path")},
		{"DiffSummary", reflect.TypeOf(DiffSummary{}), schemaObject(t, doc, "$defs", "summary")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requireSchemaMatchesStruct(t, tt.obj, tt.typ)
		})
	}
}

func TestDiffSchemaVersionConstMatchesCode(t *testing.T) {
	doc := loadSchema(t, diffSchemaPath)
	got, ok := schemaObject(t, doc, "properties", "version")["const"].(float64)
	if !ok || int(got) != DiffSchemaVersion {
		t.Errorf("schema version const %v, want DiffSchemaVersion %d; a new version needs a new schema file",
			schemaObject(t, doc, "properties", "version")["const"], DiffSchemaVersion)
	}
	if !strings.Contains(diffSchemaPath, "v1") {
		t.Errorf("schema file name %q should carry the version", diffSchemaPath)
	}
}

func TestDiffSchemaKindEnumMatchesConstants(t *testing.T) {
	doc := loadSchema(t, diffSchemaPath)
	kinds := schemaObject(t, doc, "$defs", "difference", "properties", "kind")["enum"].([]any)
	if got := toStrings(kinds); !sameSet(got, diffKinds) {
		t.Errorf("kind enum %v != kind constants %v", got, diffKinds)
	}
}

// TestDiffSchemaAcceptsRealOutput walks real documents — one with element and
// added differences, one strict with cosmetic and terminator differences —
// and fails on any emitted key the schema does not declare.
func TestDiffSchemaAcceptsRealOutput(t *testing.T) {
	doc := loadSchema(t, diffSchemaPath)
	reports := []*DiffReport{
		diffFixtures(t, "diff_base_835.x12", "diff_extra_txn.x12", DiffOptions{}),
		diffFixtures(t, "diff_base_835.x12", "diff_cosmetic.x12", DiffOptions{Strict: true}),
	}
	for _, rep := range reports {
		var buf strings.Builder
		if err := rep.WriteJSON(&buf); err != nil {
			t.Fatalf("WriteJSON: %v", err)
		}
		var emitted map[string]any
		if err := json.Unmarshal([]byte(buf.String()), &emitted); err != nil {
			t.Fatalf("WriteJSON output is not valid JSON: %v", err)
		}
		requireDeclared(t, "$", emitted, schemaObject(t, doc, "properties"), doc)
	}
}
