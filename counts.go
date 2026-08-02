package edilint

import (
	"fmt"
	"strconv"
	"strings"
)

// CountRule asserts that a field of a trailer or header record declares how many
// records of another type the file contains.
//
// The textual form is "recordType:fieldIndex:countedType", for example
// "TRL:2:DTL" meaning "field 2 of TRL records declares how many DTL records
// exist". Field indexes are 1-based and field 1 is the record type itself.
//
// How a record is matched depends on the format. Delimited records match when
// their first field equals the given value exactly, so TRL does not match TRLR.
// X12 and fixed-width records, which have no first field to compare, match on a
// literal prefix of the record instead.
type CountRule struct {
	// Declaring identifies the record type that carries the count.
	Declaring string
	// Field is the 1-based field index holding the declared count.
	Field int
	// Counted identifies the record type being counted.
	Counted string
}

// ParseCountRule parses the "type:field:type" form used by --count-rule.
func ParseCountRule(s string) (CountRule, error) {
	parts := strings.Split(s, ":")
	if len(parts) != 3 {
		return CountRule{}, fmt.Errorf(
			"count rule %q must have the form recordType:fieldIndex:countedType, e.g. TRL:2:DTL", s)
	}
	declaring := strings.TrimSpace(parts[0])
	counted := strings.TrimSpace(parts[2])
	if declaring == "" || counted == "" {
		return CountRule{}, fmt.Errorf("count rule %q has an empty record type", s)
	}
	field, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil || field < 1 {
		return CountRule{}, fmt.Errorf("count rule %q: field index must be a positive integer (1-based)", s)
	}
	return CountRule{Declaring: declaring, Field: field, Counted: counted}, nil
}

// String renders the rule in its --count-rule form.
func (c CountRule) String() string {
	return fmt.Sprintf("%s:%d:%s", c.Declaring, c.Field, c.Counted)
}

// Validate checks that a rule is usable. ParseCountRule guarantees this for the
// CLI, but CountRule has exported fields, so a library caller can build one that
// does not.
func (c CountRule) Validate() error {
	if c.Declaring == "" || c.Counted == "" {
		return fmt.Errorf("record types must not be empty")
	}
	if c.Field < 1 {
		return fmt.Errorf("field index %d must be a positive integer (1-based)", c.Field)
	}
	return nil
}

// checkCountRules recounts every declared record total and reports mismatches.
func checkCountRules(s *source, opts Options, rep *Report) {
	for _, rule := range opts.CountRules {
		if err := rule.Validate(); err != nil {
			rep.add(Finding{
				Rule:     RuleCountUnparsable,
				Severity: SeverityError,
				Message: fmt.Sprintf("count rule %q is unusable, so it was not applied: %v",
					rule, err),
				Line: 1,
			})
			continue
		}
		applyCountRule(s, rule, rep)
	}
}

// matchesType reports whether a record is of the given type.
//
// Delimited files compare the first field for equality: their record types are
// genuine fields, so a prefix test would let TRL match TRLR and silently inflate
// a count. X12 and fixed-width records have no delimiter to split on at this
// point, so they match on a literal prefix.
func matchesType(s *source, r record, want string) bool {
	if s.Format != FormatDelimited {
		return strings.HasPrefix(r.Text, want)
	}
	fields := s.Fields(r)
	return len(fields) > 0 && fields[0] == want
}

func applyCountRule(s *source, rule CountRule, rep *Report) {
	actual := 0
	for _, r := range s.Records {
		if matchesType(s, r, rule.Counted) {
			actual++
		}
	}

	declarers := 0
	for _, r := range s.Records {
		if !matchesType(s, r, rule.Declaring) {
			continue
		}
		declarers++

		fields := s.Fields(r)
		if len(fields) < rule.Field {
			rep.add(Finding{
				Rule:     RuleCountShortRec,
				Severity: SeverityError,
				Message: fmt.Sprintf("count rule %s: record has %d field(s) but the rule reads field %d",
					rule, len(fields), rule.Field),
				Line: r.Line, RecordNumber: r.Ordinal, Record: r.ID,
				Expected: fmt.Sprintf("at least %d fields", rule.Field),
				Actual:   strconv.Itoa(len(fields)),
			})
			continue
		}

		raw := strings.TrimSpace(fields[rule.Field-1])
		declared, err := strconv.Atoi(raw)
		if err != nil {
			rep.add(Finding{
				Rule:     RuleCountUnparsable,
				Severity: SeverityError,
				Message: fmt.Sprintf("count rule %s: field %d is %q, which is not an integer",
					rule, rule.Field, raw),
				Line: r.Line, RecordNumber: r.Ordinal, Record: r.ID,
				Expected: "an integer", Actual: raw,
			})
			continue
		}

		if declared != actual {
			rep.add(Finding{
				Rule:     RuleCountMismatch,
				Severity: SeverityError,
				Message: fmt.Sprintf("count rule %s: field %d declares %d %q record(s) but the file contains %d",
					rule, rule.Field, declared, rule.Counted, actual),
				Line: r.Line, RecordNumber: r.Ordinal, Record: r.ID,
				Expected: strconv.Itoa(declared), Actual: strconv.Itoa(actual),
			})
		}
	}

	if declarers == 0 {
		rep.add(Finding{
			Rule:     RuleCountNoDeclarer,
			Severity: SeverityWarning,
			Message: fmt.Sprintf("count rule %s: no %q record was found, so the declared "+
				"count of %q records (%d present) was never verified",
				rule, rule.Declaring, rule.Counted, actual),
			Expected: fmt.Sprintf("at least one %q record", rule.Declaring),
			Actual:   "none",
		})
	}
}
