package edilint

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDetectFormat(t *testing.T) {
	tests := []struct {
		name  string
		file  string
		input string
		opts  Options
		want  Format
	}{
		{name: "x12 interchange", file: "835_clean.x12", want: FormatX12},
		{name: "hl7v2 message", file: "hl7v2_clean.hl7", want: FormatHL7v2},
		{name: "pipe delimited", file: "eligibility_clean.psv", want: FormatDelimited},
		{name: "csv", file: "bom_mixed.csv", want: FormatDelimited},
		{
			name:  "hl7v2 batch header",
			input: "FHS|^~\\&|A|B\r\nBHS|^~\\&|A|B\r\n",
			want:  FormatHL7v2,
		},
		{
			name:  "prose has no delimiter",
			input: "the quick brown fox\njumped over the lazy dog\nand kept going\n",
			want:  FormatText,
		},
		{
			name:  "a stray comma does not make a csv",
			input: "AAAA\nBBBB\nCC,CC\nDDDD\nEEEE\nFFFF\nGGGG\nHHHH\nIIII\nJJJJ\n",
			want:  FormatText,
		},
		{
			name:  "a layout implies fixed width",
			input: "AAAA\nBBBB\n",
			opts:  Options{Layout: &Layout{Fields: []LayoutField{{Name: "a", Width: 4}}}},
			want:  FormatFixed,
		},
		{
			name:  "leading whitespace does not hide an ISA",
			input: "\n  ISA*00*x~\n",
			want:  FormatX12,
		},
		{
			name:  "tab delimited",
			input: "HDR\tA\tB\nDTL\tC\tD\nTRL\t1\tX\n",
			want:  FormatDelimited,
		},
		{
			name:  "mixed record types still detect the delimiter",
			input: "HDR|A|B|C|D\nDTL|1|2\nDTL|3|4\nTRL|2\n",
			want:  FormatDelimited,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			body := []byte(tc.input)
			if tc.file != "" {
				body = readFixture(t, tc.file)
			}
			body, _ = splitBOM(body)
			if got := Detect(body, tc.opts); got != tc.want {
				t.Errorf("Detect = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestFormatOverrideBeatsDetection(t *testing.T) {
	rep := Lint("test", readFixture(t, "eligibility_clean.psv"), Options{Format: FormatText})
	if rep.Format != FormatText {
		t.Errorf("format = %s, want %s", rep.Format, FormatText)
	}
}

func TestParseFormat(t *testing.T) {
	for _, name := range []string{"auto", "x12", "hl7v2", "delimited", "fixed", "text"} {
		if _, err := ParseFormat(name); err != nil {
			t.Errorf("ParseFormat(%q): %v", name, err)
		}
	}
	if got, err := ParseFormat(""); err != nil || got != FormatAuto {
		t.Errorf(`ParseFormat("") = %s, %v; want auto, nil`, got, err)
	}
	if _, err := ParseFormat("edifact"); err == nil {
		t.Error("expected an error for an unknown format")
	}
}

func TestParseDelimiter(t *testing.T) {
	tests := []struct {
		in      string
		want    byte
		wantErr bool
	}{
		{in: "|", want: '|'},
		{in: ",", want: ','},
		{in: "\\t", want: '\t'},
		{in: "tab", want: '\t'},
		{in: "\\n", want: '\n'},
		{in: "\\0", want: 0x00},
		{in: "\\x1f", want: 0x1f},
		{in: "", wantErr: true},
		{in: "||", wantErr: true},
		{in: "\\xzz", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			got, err := ParseDelimiter(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected an error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestParseCharsetProfile(t *testing.T) {
	for _, name := range []string{"basic", "extended", "off"} {
		if _, err := ParseCharsetProfile(name); err != nil {
			t.Errorf("ParseCharsetProfile(%q): %v", name, err)
		}
	}
	if got, _ := ParseCharsetProfile(""); got != CharsetExtended {
		t.Errorf(`ParseCharsetProfile("") = %s, want extended`, got)
	}
	if _, err := ParseCharsetProfile("strict"); err == nil {
		t.Error("expected an error for an unknown charset profile")
	}
}

func TestSplitBOM(t *testing.T) {
	tests := []struct {
		name     string
		in       []byte
		wantBody string
		wantBOM  string
	}{
		{name: "none", in: []byte("ABC"), wantBody: "ABC"},
		{name: "utf-8", in: []byte{0xEF, 0xBB, 0xBF, 'A'}, wantBody: "A", wantBOM: "UTF-8"},
		{name: "utf-16le", in: []byte{0xFF, 0xFE, 'A'}, wantBody: "A", wantBOM: "UTF-16LE"},
		{name: "utf-16be", in: []byte{0xFE, 0xFF, 'A'}, wantBody: "A", wantBOM: "UTF-16BE"},
		{name: "empty", in: []byte{}, wantBody: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			body, bom := splitBOM(tc.in)
			if string(body) != tc.wantBody {
				t.Errorf("body = %q, want %q", body, tc.wantBody)
			}
			if bom != tc.wantBOM {
				t.Errorf("bom = %q, want %q", bom, tc.wantBOM)
			}
		})
	}
}

func TestDisableRules(t *testing.T) {
	body := readFixture(t, "835_charset.x12")

	full := Lint("test", body, Options{})
	if len(full.Findings) == 0 {
		t.Fatal("fixture should produce findings")
	}

	t.Run("by rule name", func(t *testing.T) {
		rep := Lint("test", body, Options{Disabled: []string{RuleHomoglyph}})
		requireRule(t, rep, RuleHomoglyph, 0)
		if len(rep.Findings) == 0 {
			t.Error("other rules should still fire")
		}
	})

	t.Run("by class prefix", func(t *testing.T) {
		rep := Lint("test", body, Options{Disabled: []string{ClassCharset}})
		for _, f := range rep.Findings {
			if f.Class == ClassCharset {
				t.Errorf("charset rule %s should have been suppressed", f.Rule)
			}
		}
	})

	t.Run("by identifier", func(t *testing.T) {
		rep := Lint("test", body, Options{Disabled: []string{RuleID(RuleHomoglyph)}})
		requireRule(t, rep, RuleHomoglyph, 0)
		if len(rep.Findings) == 0 {
			t.Error("other rules should still fire")
		}
	})

	t.Run("a class prefix does not match a longer name", func(t *testing.T) {
		// "charset.x12" must not suppress "charset.x12-basic".
		if ruleDisabled(RuleX12Basic, []string{"charset.x12"}) {
			t.Error("prefix matching must respect dot boundaries")
		}
		if !ruleDisabled(RuleX12Basic, []string{"charset"}) {
			t.Error("a class prefix should suppress its rules")
		}
		if !ruleDisabled(RuleX12Basic, []string{RuleX12Basic}) {
			t.Error("an exact name should suppress its rule")
		}
	})

	t.Run("identifiers match whatever their case", func(t *testing.T) {
		for _, selector := range []string{"EL1007", "el1007", "  EL1007  "} {
			if !ruleDisabled(RuleX12Basic, []string{selector}) {
				t.Errorf("%q should suppress %s", selector, RuleX12Basic)
			}
		}
		if ruleDisabled(RuleX12Basic, []string{"EL1"}) {
			t.Error("a partial identifier must not suppress anything; classes exist for that")
		}
	})

	t.Run("an unknown selector suppresses nothing", func(t *testing.T) {
		// The library tolerates a rule it does not know; the command line and the
		// configuration file are where a typo is rejected.
		rep := Lint("test", body, Options{Disabled: []string{"EL9999", "charse"}})
		if len(rep.Findings) != len(full.Findings) {
			t.Errorf("findings = %d, want the unfiltered %d", len(rep.Findings), len(full.Findings))
		}
	})
}

func TestSeverityOverrides(t *testing.T) {
	body := readFixture(t, "835_charset.x12")

	t.Run("downgrading a rule to info keeps it out of the exit status", func(t *testing.T) {
		rep := Lint("test", body, Options{
			Severities: map[string]Severity{RuleHomoglyph: SeverityInfo},
			Disabled:   []string{RuleX12Extended, ClassCounts},
		})
		f := firstOf(rep, RuleHomoglyph)
		if f == nil {
			t.Fatalf("the rule should still fire, got %v", ruleNames(rep))
		}
		if f.Severity != SeverityInfo {
			t.Errorf("severity = %s, want %s", f.Severity, SeverityInfo)
		}
		if rep.Summary.Infos != 1 || rep.Summary.Errors != 0 {
			t.Errorf("summary = %+v, want a single informational finding", rep.Summary)
		}
		if !rep.OK(SeverityWarning) {
			t.Error("an informational finding must not fail the default threshold")
		}
		if rep.OK(SeverityInfo) {
			t.Error("a caller asking to fail on info should fail")
		}
	})

	t.Run("an override is keyed by identifier as well as by name", func(t *testing.T) {
		rep := Lint("test", body, Options{
			Severities: map[string]Severity{"el1005": SeverityWarning},
		})
		f := firstOf(rep, RuleHomoglyph)
		if f == nil {
			t.Fatalf("expected a homoglyph finding, got %v", ruleNames(rep))
		}
		if f.Severity != SeverityWarning {
			t.Errorf("severity = %s, want %s", f.Severity, SeverityWarning)
		}
	})

	t.Run("an override reaches a rule that grades itself by format", func(t *testing.T) {
		// charset.bom is an error for X12 and a warning for delimited; an override
		// replaces whichever the check chose.
		rep := Lint("test", readFixture(t, "bom_mixed.csv"), Options{
			Severities: map[string]Severity{RuleBOM: SeverityError},
		})
		f := firstOf(rep, RuleBOM)
		if f == nil {
			t.Fatalf("expected a byte order mark finding, got %v", ruleNames(rep))
		}
		if f.Severity != SeverityError {
			t.Errorf("severity = %s, want the override %s", f.Severity, SeverityError)
		}
	})
}

func TestMaxFindings(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 40; i++ {
		b.WriteString("DTL|RIVERA\x0bDANA|X\n")
	}
	body := []byte(b.String())

	// The zero value means unlimited.
	for _, limit := range []int{0, -1} {
		full := Lint("test", body, Options{MaxFindings: limit})
		if len(full.Findings) != 40 {
			t.Errorf("MaxFindings %d: findings = %d, want all 40", limit, len(full.Findings))
		}
		if full.Summary.Total != 40 {
			t.Errorf("MaxFindings %d: total = %d, want 40", limit, full.Summary.Total)
		}
		if full.Summary.Truncated {
			t.Errorf("MaxFindings %d: an unlimited run must not be marked truncated", limit)
		}
	}

	capped := Lint("test", body, Options{MaxFindings: 10})
	if len(capped.Findings) != 10 {
		t.Errorf("findings = %d, want 10", len(capped.Findings))
	}
	if capped.Summary.Total != 40 || capped.Summary.Errors != 40 {
		t.Errorf("summary = %+v, want the full count of 40", capped.Summary)
	}
	if !capped.Summary.Truncated {
		t.Error("a capped run must be marked truncated")
	}
	// Truncation must not be able to turn a failing report into a passing one.
	if capped.OK(SeverityWarning) || capped.OK(SeverityError) {
		t.Error("a capped report must still report as failing")
	}
}

func TestOKIgnoresTruncation(t *testing.T) {
	// A file whose only finding is a warning, with output capped to nothing
	// visible, must still fail the default threshold and pass the error one.
	rep := Lint("test", []byte("A|1\nB|2\nC|3"), Options{
		Disabled:    []string{ClassCharset, ClassFields},
		MaxFindings: 1,
	})
	if rep.Summary.Warnings != 1 || rep.Summary.Errors != 0 {
		t.Fatalf("summary = %+v, want exactly one warning", rep.Summary)
	}
	if rep.OK(SeverityWarning) {
		t.Error("a warning should fail the default threshold")
	}
	if !rep.OK(SeverityError) {
		t.Error("a warning should pass the error-only threshold")
	}
}

func TestFindingsAreOrderedByPosition(t *testing.T) {
	rep := lintFixture(t, "835_charset.x12", Options{})
	prev := 0
	for _, f := range rep.Findings {
		if f.Line < prev {
			t.Fatalf("findings are out of order: line %d follows line %d", f.Line, prev)
		}
		prev = f.Line
	}
}

func TestReportOK(t *testing.T) {
	warnOnly := Lint("test", []byte("A|1\nB|2\nC|3"), Options{Disabled: []string{ClassCharset, ClassFields}})
	requireRule(t, warnOnly, RuleMissingFinal, 1)

	if warnOnly.OK(SeverityWarning) {
		t.Error("a warning should fail the default threshold")
	}
	if !warnOnly.OK(SeverityError) {
		t.Error("a warning should pass the error-only threshold")
	}
}

func TestLintFile(t *testing.T) {
	t.Run("reads a file", func(t *testing.T) {
		rep, err := LintFile(filepath.Join("testdata", "835_clean.x12"), Options{})
		if err != nil {
			t.Fatalf("LintFile: %v", err)
		}
		requireClean(t, rep)
	})

	t.Run("reports a missing file", func(t *testing.T) {
		_, err := LintFile(filepath.Join(t.TempDir(), "absent.x12"), Options{})
		if err == nil {
			t.Fatal("expected an error for a missing file")
		}
	})

	t.Run("handles an empty file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "empty.txt")
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
		rep, err := LintFile(path, Options{})
		if err != nil {
			t.Fatalf("LintFile: %v", err)
		}
		requireClean(t, rep)
	})
}

func TestRunReportJSON(t *testing.T) {
	rr := NewRunReport()
	rr.Add(lintFixture(t, "835_clean.x12", Options{}))
	rr.Add(lintFixture(t, "835_envelope_broken.x12", Options{}))

	var buf bytes.Buffer
	if err := rr.WriteJSON(&buf); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}

	var decoded RunReport
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("round trip: %v\n%s", err, buf.String())
	}
	if decoded.Version != SchemaVersion {
		t.Errorf("version = %d, want %d", decoded.Version, SchemaVersion)
	}
	if len(decoded.Files) != 2 {
		t.Fatalf("files = %d, want 2", len(decoded.Files))
	}
	if decoded.Summary.Files != 2 {
		t.Errorf("summary files = %d, want 2", decoded.Summary.Files)
	}
	if decoded.Summary.Errors != 4 {
		t.Errorf("summary errors = %d, want 4", decoded.Summary.Errors)
	}
	if decoded.Files[0].Format != FormatX12 {
		t.Errorf("format = %s, want x12", decoded.Files[0].Format)
	}
	f := decoded.Files[1].Findings[0]
	if f.Rule == "" || f.Class == "" || f.Severity == "" || f.Message == "" {
		t.Errorf("finding is missing required fields: %+v", f)
	}
}

func TestRunReportTextIsQuietWhenClean(t *testing.T) {
	rr := NewRunReport()
	rr.Add(lintFixture(t, "835_clean.x12", Options{}))

	var quiet bytes.Buffer
	if err := rr.WriteText(&quiet, false); err != nil {
		t.Fatalf("WriteText: %v", err)
	}
	if quiet.Len() != 0 {
		t.Errorf("a clean run should print nothing, got %q", quiet.String())
	}

	var verbose bytes.Buffer
	if err := rr.WriteText(&verbose, true); err != nil {
		t.Fatalf("WriteText: %v", err)
	}
	if !strings.Contains(verbose.String(), "ok (x12)") {
		t.Errorf("verbose output should confirm the file, got %q", verbose.String())
	}
}

func TestFormatFindingLine(t *testing.T) {
	f := Finding{
		File: "claims.x12", Line: 12, Column: 5, RecordNumber: 7, Record: "CLP",
		ID: "EL1005", Severity: SeverityError, Rule: RuleHomoglyph, Message: "looks like ASCII",
	}
	got := FormatFinding(f, FormatX12)
	want := "claims.x12:12:5: error: [EL1005 charset.homoglyph] looks like ASCII (record 7, segment CLP)"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}

	// A finding built without an identifier still renders, naming the rule alone.
	noID := f
	noID.ID = ""
	if got := FormatFinding(noID, FormatX12); !strings.Contains(got, "[charset.homoglyph]") {
		t.Errorf("a finding with no identifier should print the rule alone, got %q", got)
	}

	// Line-oriented formats call the identifier a record type, not a segment.
	if got := FormatFinding(f, FormatDelimited); !strings.Contains(got, "type CLP") {
		t.Errorf("delimited rendering should say \"type\", got %q", got)
	}

	// A finding with no position still renders.
	bare := Finding{
		File: "a.txt", ID: "EL4004", Severity: SeverityWarning,
		Rule: RuleCountNoDeclarer, Message: "m",
	}
	if got := FormatFinding(bare, FormatText); got != "a.txt: warning: [EL4004 counts.no-declaring-record] m" {
		t.Errorf("bare rendering = %q", got)
	}
}

func TestRulesCatalogIsComplete(t *testing.T) {
	// Every rule the engine can emit must be documented, because --list-rules and
	// the README table are generated from this catalog.
	documented := map[string]bool{}
	for _, r := range Rules() {
		documented[r.Name] = true
		if r.Summary == "" {
			t.Errorf("rule %s has no summary", r.Name)
		}
		if !strings.HasPrefix(r.Name, r.Class+".") {
			t.Errorf("rule %s is not prefixed with its class %q", r.Name, r.Class)
		}
	}

	emitted := map[string]bool{}
	for _, name := range catalogFixtures {
		rep := lintFixture(t, name, Options{CountRules: []CountRule{{Declaring: "TRL", Field: 2, Counted: "DTL"}}})
		for _, f := range rep.Findings {
			emitted[f.Rule] = true
			// The identifier is what a suppression and a configuration file
			// key on, so no finding may leave the engine without one.
			if f.ID == "" {
				t.Errorf("rule %s emitted a finding with no identifier", f.Rule)
			}
		}
	}
	for name := range emitted {
		if !documented[name] {
			t.Errorf("rule %s is emitted but missing from Rules()", name)
		}
	}
}

// catalogFixtures are the fixtures that between them exercise most of the
// catalog. Several tests walk them.
var catalogFixtures = []string{
	"835_clean.x12", "835_envelope_broken.x12", "835_duplicate_control.x12",
	"835_bad_datetime.x12", "835_charset.x12", "835_padding.x12",
	"hl7v2_clean.hl7", "hl7v2_dirty.hl7", "bom_mixed.csv",
	"eligibility_clean.psv", "eligibility_broken.psv",
}

func TestWriteRules(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteRules(&buf); err != nil {
		t.Fatalf("WriteRules: %v", err)
	}
	out := buf.String()
	for _, want := range []string{RuleHomoglyph, RuleSegmentCount, RuleDupControl, RuleLayoutPadding} {
		if !strings.Contains(out, want) {
			t.Errorf("rule listing is missing %s", want)
		}
	}

	// Every line carries the identifier alongside the name, which is the column
	// --list-rules exists to show.
	for _, r := range Rules() {
		if !strings.Contains(out, r.ID+"  "+r.Name) {
			t.Errorf("rule listing does not pair %s with %s", r.ID, r.Name)
		}
	}
}

func TestREADMEDocumentsEveryRule(t *testing.T) {
	// The rule tables in the README are the user-facing contract, so a new rule
	// must not ship without an entry naming both its identifier and its name.
	body, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatalf("read README: %v", err)
	}
	doc := string(body)
	for _, r := range Rules() {
		if !strings.Contains(doc, "`"+r.Name+"`") {
			t.Errorf("rule %s is not documented in README.md", r.Name)
		}
		if !strings.Contains(doc, "`"+r.ID+"`") {
			t.Errorf("rule identifier %s (%s) is not documented in README.md", r.ID, r.Name)
		}
	}
}

func TestFixtureLineEndingsSurviveCheckout(t *testing.T) {
	// Several fixtures encode their terminators deliberately. If a checkout ever
	// rewrites them — a Windows clone without the repository's .gitattributes,
	// say — the terminator checks would be silently testing nothing.
	t.Run("CR-terminated HL7v2 keeps its CRs", func(t *testing.T) {
		body := readFixture(t, "hl7v2_clean.hl7")
		if !bytes.Contains(body, []byte("\r")) {
			t.Error("fixture should be CR-terminated")
		}
		if bytes.Contains(body, []byte("\n")) {
			t.Error("fixture must contain no LF; line endings were rewritten on checkout")
		}
	})

	t.Run("mixed-ending CSV keeps its BOM and both styles", func(t *testing.T) {
		body := readFixture(t, "bom_mixed.csv")
		if !bytes.HasPrefix(body, []byte{0xEF, 0xBB, 0xBF}) {
			t.Error("fixture should begin with a UTF-8 BOM")
		}
		if !bytes.Contains(body, []byte("\r\n")) {
			t.Error("fixture should contain a CRLF")
		}
		stripped := bytes.ReplaceAll(body, []byte("\r\n"), nil)
		if !bytes.Contains(stripped, []byte("\n")) {
			t.Error("fixture should contain a lone LF; line endings were normalized")
		}
		if bytes.HasSuffix(body, []byte("\n")) {
			t.Error("fixture should end without a terminator")
		}
	})

	t.Run("X12 padding fixture keeps its run-on segment", func(t *testing.T) {
		body := readFixture(t, "835_padding.x12")
		if !bytes.Contains(body, []byte("~BPR")) {
			t.Error("fixture should have one segment running straight into the next")
		}
	})
}

func TestFindingJSONFieldNames(t *testing.T) {
	// The JSON shape is a contract with calling scripts, so the field names are
	// pinned here rather than left to the struct tags.
	rep := lintFixture(t, "eligibility_broken.psv", Options{})
	f := firstOf(rep, RuleFieldOutlier)
	if f == nil {
		t.Fatalf("expected a field-count finding, got %v", ruleNames(rep))
	}

	body, err := json.Marshal(f)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got, ok := decoded["record"].(string); !ok || got != "DTL" {
		t.Errorf(`"record" = %v, want the record type "DTL"`, decoded["record"])
	}
	if got, ok := decoded["record_number"].(float64); !ok || int(got) != 3 {
		t.Errorf(`"record_number" = %v, want the ordinal 3`, decoded["record_number"])
	}
	if _, ok := decoded["segment"]; ok {
		t.Error(`"segment" was renamed to "record" and must no longer appear`)
	}

	// For X12 the same field carries the segment identifier.
	x12 := lintFixture(t, "835_envelope_broken.x12", Options{})
	sf := firstOf(x12, RuleSegmentCount)
	if sf == nil {
		t.Fatalf("expected a segment-count finding, got %v", ruleNames(x12))
	}
	if sf.Record != "SE" {
		t.Errorf("Record = %q, want the segment identifier SE", sf.Record)
	}
}
