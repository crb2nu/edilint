package edilint

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
)

var ruleIDForm = regexp.MustCompile(`^EL[1-9][0-9]{3}$`)

func TestRuleIdentifiersAreWellFormed(t *testing.T) {
	// Identifiers are a permanent public contract: they appear in findings, in
	// suppressions, in baselines and in configuration files. Every property that
	// makes them usable is pinned here rather than left to review.
	seenID := map[string]string{}
	seenName := map[string]bool{}
	previous := ""

	for _, r := range Rules() {
		if !ruleIDForm.MatchString(r.ID) {
			t.Errorf("rule %s has identifier %q, want the EL#### form", r.Name, r.ID)
			continue
		}
		if other, dup := seenID[r.ID]; dup {
			t.Errorf("identifier %s is used by both %s and %s", r.ID, other, r.Name)
		}
		seenID[r.ID] = r.Name

		if seenName[r.Name] {
			t.Errorf("rule %s appears in the catalog twice", r.Name)
		}
		seenName[r.Name] = true

		want, ok := classBlocks[r.Class]
		if !ok {
			t.Errorf("class %q has no identifier block", r.Class)
			continue
		}
		if !strings.HasPrefix(r.ID, want) {
			t.Errorf("rule %s is in class %s, so its identifier should start with %s, got %s",
				r.Name, r.Class, want, r.ID)
		}
		if block, reserved := reservedBlocks[r.ID[:3]]; reserved {
			t.Errorf("rule %s uses %s, which is reserved for %s", r.Name, r.ID, block)
		}

		// Ascending order makes the catalog, --list-rules and the README table
		// all read the same way, and makes a gap deliberate rather than accidental.
		if previous != "" && r.ID <= previous {
			t.Errorf("catalog is out of order: %s follows %s", r.ID, previous)
		}
		previous = r.ID
	}

	if len(seenID) == 0 {
		t.Fatal("the catalog is empty")
	}
}

func TestRuleIdentifierBlocksAreNumeric(t *testing.T) {
	// A block prefix such as "EL3" must be a real thousands block, so that
	// reading the first digit of an identifier tells you the class.
	for class, prefix := range classBlocks {
		digit := strings.TrimPrefix(prefix, "EL")
		if _, err := strconv.Atoi(digit); err != nil || len(digit) != 1 {
			t.Errorf("class %s has block %q, want EL followed by one digit", class, prefix)
		}
	}
}

func TestRuleLookups(t *testing.T) {
	if got := RuleID(RuleSegmentCount); got != "EL3006" {
		t.Errorf("RuleID(%s) = %q, want EL3006", RuleSegmentCount, got)
	}
	if got := RuleName("EL3006"); got != RuleSegmentCount {
		t.Errorf("RuleName(EL3006) = %q, want %s", got, RuleSegmentCount)
	}
	if got := RuleName("el3006"); got != RuleSegmentCount {
		t.Errorf("identifier lookup should be case-insensitive, got %q", got)
	}
	if got := RuleID("charset.not-a-rule"); got != "" {
		t.Errorf("an unknown name should have no identifier, got %q", got)
	}
	if got := RuleName("EL9999"); got != "" {
		t.Errorf("an unknown identifier should have no name, got %q", got)
	}

	classes := RuleClasses()
	want := []string{"charset", "counts", "edifact", "envelope", "fields", "hl7batch", "layout", "terminator"}
	if len(classes) != len(want) {
		t.Fatalf("RuleClasses() = %v, want %v", classes, want)
	}
	for i := range want {
		if classes[i] != want[i] {
			t.Fatalf("RuleClasses() = %v, want %v", classes, want)
		}
	}
}

func TestValidateSelectors(t *testing.T) {
	valid := []string{"EL1005", "el1005", "charset.homoglyph", "charset", "layout", ""}
	if err := ValidateSelectors(valid); err != nil {
		t.Errorf("ValidateSelectors(%v): %v", valid, err)
	}

	for _, bad := range []string{"EL9999", "charse", "charset.homoglyphs", "EL1", "envelope."} {
		err := ValidateSelectors([]string{bad})
		if err == nil {
			t.Errorf("%q should be rejected", bad)
			continue
		}
		if !strings.Contains(err.Error(), bad) {
			t.Errorf("the error for %q should quote it, got %v", bad, err)
		}
	}
}

func TestParseSeverity(t *testing.T) {
	for name, want := range map[string]Severity{
		"error": SeverityError, "warning": SeverityWarning, "info": SeverityInfo,
		"ERROR": SeverityError, " info ": SeverityInfo,
	} {
		got, err := ParseSeverity(name)
		if err != nil {
			t.Errorf("ParseSeverity(%q): %v", name, err)
			continue
		}
		if got != want {
			t.Errorf("ParseSeverity(%q) = %s, want %s", name, got, want)
		}
	}
	if _, err := ParseSeverity("fatal"); err == nil {
		t.Error("expected an error for an unknown severity")
	}
}

func TestSeverityRankOrdersInfoLast(t *testing.T) {
	if SeverityError.Rank() >= SeverityWarning.Rank() {
		t.Error("an error must sort before a warning")
	}
	if SeverityWarning.Rank() >= SeverityInfo.Rank() {
		t.Error("a warning must sort before an informational finding")
	}
	if SeverityInfo.Rank() >= Severity("nonsense").Rank() {
		t.Error("a known severity must sort before an unknown one")
	}
}
