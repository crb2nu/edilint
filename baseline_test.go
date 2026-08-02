package edilint

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// lintRun lints one fixture and returns a run report, the way the CLI does.
func lintRun(t *testing.T, name string, opts Options) *RunReport {
	t.Helper()
	rr := NewRunReport()
	rr.Add(Lint(name, readFixture(t, name), opts))
	return rr
}

func TestBaselineRoundTrip(t *testing.T) {
	// The adoption path in one test: record what a file already reports, run
	// again against the recording and see nothing, then plant a defect and see
	// exactly that.
	const name = "835_envelope_broken.x12"

	first := lintRun(t, name, Options{})
	if first.Summary.Total == 0 {
		t.Fatal("the fixture should report findings")
	}
	baseline := NewBaseline(first)
	if baseline.Total() != first.Summary.Total {
		t.Errorf("baseline holds %d findings, want the %d that were reported",
			baseline.Total(), first.Summary.Total)
	}

	second := lintRun(t, name, Options{Baseline: baseline})
	if second.Summary.Total != 0 {
		t.Fatalf("a baselined run should report nothing, got %v", ruleNames(second.Files[0]))
	}
	if !second.OK(SeverityWarning) {
		t.Error("a fully baselined run must exit clean")
	}

	// A new defect on top of the recorded ones is reported, and only it.
	body := readFixture(t, name)
	planted := bytes.Replace(body, []byte("BPR*I*"), []byte("BPR*IА*"), 1)
	if bytes.Equal(planted, body) {
		t.Fatal("the fixture changed; the planted defect no longer applies")
	}

	baseline.Reset()
	rep := Lint(name, planted, Options{Baseline: baseline})
	if rep.Summary.Total != 1 {
		t.Fatalf("expected exactly one new finding, got %d: %v", rep.Summary.Total, ruleNames(rep))
	}
	if rep.Findings[0].Rule != RuleHomoglyph {
		t.Errorf("new finding = %s, want %s", rep.Findings[0].Rule, RuleHomoglyph)
	}
}

func TestBaselineSurvivesLineNumberDrift(t *testing.T) {
	// The point of a baseline is that it outlives an edit. Inserting a segment
	// above a recorded finding moves its line and its record ordinal, and must
	// not make it look new.
	body := readFixture(t, "835_charset.x12")
	baseline := NewBaseline(lintRun(t, "835_charset.x12", Options{}))
	if baseline.Total() == 0 {
		t.Fatal("the fixture should report findings")
	}

	shifted := bytes.Replace(body, []byte("ST*835*"), []byte("REF*EV*000123~\nST*835*"), 1)
	if bytes.Equal(shifted, body) {
		t.Fatal("the fixture changed; the insertion point no longer applies")
	}

	rep := Lint("835_charset.x12", shifted, Options{Baseline: baseline})
	for _, f := range rep.Findings {
		// The inserted segment breaks SE01's recount, which is genuinely new.
		if f.Rule == RuleSegmentCount {
			continue
		}
		t.Errorf("moving a finding must not make it new: %s", FormatFinding(f, rep.Format))
	}
}

func TestBaselineSurvivesAStatisticChangingInAMessage(t *testing.T) {
	// Several messages quote a count over the whole file. The field-count rule
	// says "9 in 2 of 3 record(s) of this type", which changes as soon as an
	// unrelated record is added. That edit must not resurrect a recorded finding.
	body := readFixture(t, "eligibility_broken.psv")
	baseline := NewBaseline(lintRun(t, "eligibility_broken.psv", Options{}))
	before := firstOf(Lint("eligibility_broken.psv", body, Options{}), RuleFieldOutlier)
	if before == nil {
		t.Fatal("the fixture should report a field-count outlier")
	}

	grown := append(bytes.Clone(body), []byte("DTL|A|B|C|D|E|F|G|H\n")...)
	after := firstOf(Lint("eligibility_broken.psv", grown, Options{}), RuleFieldOutlier)
	if after == nil || after.Message == before.Message {
		t.Fatalf("the appended record should have changed the message, got %+v", after)
	}

	rep := Lint("eligibility_broken.psv", grown, Options{Baseline: baseline})
	if f := firstOf(rep, RuleFieldOutlier); f != nil {
		t.Errorf("a changed statistic must not make a recorded finding new: %s",
			FormatFinding(*f, rep.Format))
	}
}

func TestBaselineMessageKeepsWhatIdentifiesTheDefect(t *testing.T) {
	// Numbers go, names stay. Two defects that differ only in their numbers are
	// the same defect for baseline purposes; two that differ in a field name or
	// a record type are not.
	same := []string{
		`field 2 declares 4 "DTL" record(s) but the file contains 3`,
		`field 2 declares 9 "DTL" record(s) but the file contains 11`,
	}
	if baselineMessage(same[0]) != baselineMessage(same[1]) {
		t.Errorf("%q and %q should share a key", same[0], same[1])
	}

	different := []string{
		`field 2 declares 4 "DTL" record(s) but the file contains 3`,
		`field 2 declares 4 "HDR" record(s) but the file contains 3`,
	}
	if baselineMessage(different[0]) == baselineMessage(different[1]) {
		t.Errorf("%q and %q must not share a key", different[0], different[1])
	}

	if got := baselineMessage("record is 48 character(s) long"); got != "record is # character(s) long" {
		t.Errorf("baselineMessage = %q", got)
	}
	if got := baselineMessage("no numbers here"); got != "no numbers here" {
		t.Errorf("a message with no numbers must be left alone, got %q", got)
	}
}

func TestBaselineCountsRepeatedFindings(t *testing.T) {
	// Identical findings collapse into one entry with a count, so that one more
	// of the same defect is still caught.
	body := bytes.Repeat([]byte("DTL|RIVERA\x0bDANA|X\n"), 3)

	rr := NewRunReport()
	rr.Add(Lint("noisy.psv", body, Options{}))
	baseline := NewBaseline(rr)

	if len(baseline.Findings) != 1 {
		t.Fatalf("expected the identical findings to collapse, got %d entries", len(baseline.Findings))
	}
	if baseline.Findings[0].Count != 3 {
		t.Errorf("count = %d, want 3", baseline.Findings[0].Count)
	}

	if rep := Lint("noisy.psv", body, Options{Baseline: baseline}); rep.Summary.Total != 0 {
		t.Errorf("three recorded findings should suppress three, got %d", rep.Summary.Total)
	}

	baseline.Reset()
	fourth := append(bytes.Clone(body), []byte("DTL|RIVERA\x0bDANA|X\n")...)
	rep := Lint("noisy.psv", fourth, Options{Baseline: baseline})
	if rep.Summary.Total != 1 {
		t.Errorf("a fourth occurrence should be reported, got %d findings", rep.Summary.Total)
	}
}

func TestBaselineIsScopedToItsFile(t *testing.T) {
	body := readFixture(t, "eligibility_broken.psv")
	baseline := NewBaseline(lintRun(t, "eligibility_broken.psv", Options{}))
	if baseline.Total() == 0 {
		t.Fatal("the fixture should report findings")
	}

	// The same content under another name is a different file, and its findings
	// are new. A baseline that leaked across files would hide a copy-paste.
	rep := Lint("another.psv", body, Options{Baseline: baseline})
	if rep.Summary.Total == 0 {
		t.Error("findings in a file the baseline does not cover must be reported")
	}
}

func TestBaselineNormalizesPaths(t *testing.T) {
	baseline := NewBaseline(lintRun(t, "eligibility_broken.psv", Options{}))
	body := readFixture(t, "eligibility_broken.psv")

	for _, name := range []string{"./eligibility_broken.psv", "eligibility_broken.psv"} {
		baseline.Reset()
		if rep := Lint(name, body, Options{Baseline: baseline}); rep.Summary.Total != 0 {
			t.Errorf("%s: expected the baseline to match, got %v", name, ruleNames(rep))
		}
	}
}

func TestBaselineUnmatchedEntries(t *testing.T) {
	baseline := NewBaseline(lintRun(t, "835_envelope_broken.x12", Options{}))
	recorded := baseline.Total()

	// Nothing was linted, so every entry is left over.
	stale := baseline.Unmatched()
	left := 0
	for _, e := range stale {
		left += e.Count
	}
	if left != recorded {
		t.Errorf("unmatched = %d, want all %d", left, recorded)
	}

	// Reporting them consumes the tally, so a caller cannot double-count.
	if got := baseline.Unmatched(); got != nil {
		t.Errorf("a second call should report nothing, got %d entries", len(got))
	}

	baseline.Reset()
	Lint("835_envelope_broken.x12", readFixture(t, "835_envelope_broken.x12"), Options{Baseline: baseline})
	if got := baseline.Unmatched(); got != nil {
		t.Errorf("a matched baseline has nothing left over, got %v", got)
	}
}

func TestBaselineJSONRoundTrip(t *testing.T) {
	baseline := NewBaseline(lintRun(t, "835_charset.x12", Options{}))

	var buf bytes.Buffer
	if err := baseline.WriteJSON(&buf); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}
	if !strings.HasSuffix(buf.String(), "\n") {
		t.Error("the document should end with a newline so it can be committed")
	}

	decoded, err := ReadBaseline(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("ReadBaseline: %v", err)
	}
	if decoded.Version != BaselineVersion {
		t.Errorf("version = %d, want %d", decoded.Version, BaselineVersion)
	}
	if decoded.Total() != baseline.Total() {
		t.Errorf("decoded %d findings, want %d", decoded.Total(), baseline.Total())
	}

	// A decoded baseline suppresses exactly what the original did.
	if rep := Lint("835_charset.x12", readFixture(t, "835_charset.x12"),
		Options{Baseline: decoded}); rep.Summary.Total != 0 {
		t.Errorf("the decoded baseline should suppress everything, got %v", ruleNames(rep))
	}
}

func TestBaselineOutputIsDeterministic(t *testing.T) {
	// A baseline is committed and read in diffs, so recording the same findings
	// twice must produce the same bytes.
	var first, second bytes.Buffer
	for _, buf := range []*bytes.Buffer{&first, &second} {
		rr := NewRunReport()
		for _, name := range []string{"eligibility_broken.psv", "835_charset.x12"} {
			rr.Add(Lint(name, readFixture(t, name), Options{}))
		}
		if err := NewBaseline(rr).WriteJSON(buf); err != nil {
			t.Fatalf("WriteJSON: %v", err)
		}
	}
	if first.String() != second.String() {
		t.Errorf("two recordings differ:\n%s\n---\n%s", first.String(), second.String())
	}

	// Entries are sorted by file, so a multi-file baseline reads in order.
	body := first.String()
	if strings.Index(body, "835_charset.x12") > strings.Index(body, "eligibility_broken.psv") {
		t.Error("entries should be ordered by file name")
	}
}

func TestBaselineRecordsTheRuleIdentifier(t *testing.T) {
	baseline := NewBaseline(lintRun(t, "835_envelope_broken.x12", Options{}))
	for _, e := range baseline.Findings {
		if e.ID == "" {
			t.Errorf("entry %+v has no identifier", e)
		}
		if RuleName(e.ID) != e.Rule {
			t.Errorf("entry %s names rule %q, but %s is %q", e.ID, e.Rule, e.ID, RuleName(e.ID))
		}
	}
}

func TestBaselineWriteAndLoadFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "baseline.json")
	baseline := NewBaseline(lintRun(t, "835_charset.x12", Options{}))
	if err := baseline.WriteFile(path); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	loaded, err := LoadBaseline(path)
	if err != nil {
		t.Fatalf("LoadBaseline: %v", err)
	}
	if loaded.Total() != baseline.Total() {
		t.Errorf("loaded %d findings, want %d", loaded.Total(), baseline.Total())
	}
}

func TestLoadBaselineRejectsBadDocuments(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{"not json", "{", "not a valid edilint baseline"},
		{"unknown field", `{"version":1,"findings":[],"extra":1}`, "not a valid edilint baseline"},
		{"no version", `{"findings":[]}`, "version 0 is not supported"},
		{"future version", `{"version":99,"findings":[]}`, "version 99 is not supported"},
		{
			"entry with no id",
			`{"version":1,"findings":[{"file":"a.x12","message":"m","count":1}]}`,
			"has no rule id",
		},
		{
			"entry with no file",
			`{"version":1,"findings":[{"id":"EL3006","message":"m","count":1}]}`,
			"has no file",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "baseline.json")
			if err := os.WriteFile(path, []byte(tc.body), 0o600); err != nil {
				t.Fatalf("write: %v", err)
			}
			_, err := LoadBaseline(path)
			if err == nil {
				t.Fatalf("expected an error for %s", tc.body)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %v, want it to mention %q", err, tc.want)
			}
		})
	}
}

func TestLoadBaselineSaysHowToCreateOne(t *testing.T) {
	path := filepath.Join(t.TempDir(), "absent.json")
	_, err := LoadBaseline(path)
	if err == nil {
		t.Fatal("expected an error for a missing baseline")
	}
	if !strings.Contains(err.Error(), "--write-baseline") {
		t.Errorf("the error should say how to record one, got %v", err)
	}
}

func TestBaselineHandlesEntriesWithoutACount(t *testing.T) {
	// A hand-edited baseline may leave the count off. One occurrence is the only
	// sensible reading, and it must not suppress an unbounded number.
	path := filepath.Join(t.TempDir(), "baseline.json")
	body := `{"version":1,"findings":[
	  {"file":"a.psv","id":"EL2002","rule":"terminator.missing-final","message":"m"}
	]}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	loaded, err := LoadBaseline(path)
	if err != nil {
		t.Fatalf("LoadBaseline: %v", err)
	}
	if loaded.Total() != 1 {
		t.Errorf("Total = %d, want 1", loaded.Total())
	}
}

func TestNilBaselineAccountsForNothing(t *testing.T) {
	var b *Baseline
	if b.accountsFor(Finding{ID: "EL3006", File: "a.x12", Message: "m"}) {
		t.Error("a nil baseline must suppress nothing")
	}
	if b.Total() != 0 {
		t.Error("a nil baseline holds nothing")
	}
	if b.Unmatched() != nil {
		t.Error("a nil baseline has nothing left over")
	}
	b.Reset()
}

func TestBaselineDoesNotHideDisabledRuleAccounting(t *testing.T) {
	// A rule that is disabled never reaches the baseline, so disabling it does
	// not consume a recorded occurrence and later re-enabling it still works.
	baseline := NewBaseline(lintRun(t, "835_charset.x12", Options{}))
	recorded := baseline.Total()

	Lint("835_charset.x12", readFixture(t, "835_charset.x12"),
		Options{Baseline: baseline, Disabled: []string{ClassCharset, ClassCounts}})

	left := 0
	for _, e := range baseline.Unmatched() {
		left += e.Count
	}
	if left != recorded {
		t.Errorf("%d of %d entries were consumed by a disabled run, want none",
			recorded-left, recorded)
	}
}
