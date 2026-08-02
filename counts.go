package edilint

import (
	"fmt"
	"strconv"
	"strings"
)

// CountRule asserts that a field of a trailer or header record declares how many
// records of another type the file contains.
//
// The textual form is "recordPrefix:fieldIndex:countedPrefix", for example
// "TRL:2:DTL" meaning "field 2 of records starting with TRL declares how many
// records starting with DTL exist". Field indexes are 1-based and field 1 is the
// record type itself.
type CountRule struct {
	// Declaring is the literal prefix of the record that carries the count.
	Declaring string
	// Field is the 1-based field index holding the declared count.
	Field int
	// Counted is the literal prefix of the records being counted.
	Counted string
}

// ParseCountRule parses the "prefix:field:prefix" form used by --count-rule.
func ParseCountRule(s string) (CountRule, error) {
	parts := strings.Split(s, ":")
	if len(parts) != 3 {
		return CountRule{}, fmt.Errorf(
			"count rule %q must have the form recordPrefix:fieldIndex:countedPrefix, e.g. TRL:2:DTL", s)
	}
	declaring := strings.TrimSpace(parts[0])
	counted := strings.TrimSpace(parts[2])
	if declaring == "" || counted == "" {
		return CountRule{}, fmt.Errorf("count rule %q has an empty record prefix", s)
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

// checkCountRules recounts every declared record total and reports mismatches.
func checkCountRules(s *source, opts Options, rep *Report) {
	for _, rule := range opts.CountRules {
		applyCountRule(s, rule, rep)
	}
}

func applyCountRule(s *source, rule CountRule, rep *Report) {
	actual := 0
	for _, r := range s.Records {
		if strings.HasPrefix(r.Text, rule.Counted) {
			actual++
		}
	}

	declarers := 0
	for _, r := range s.Records {
		if !strings.HasPrefix(r.Text, rule.Declaring) {
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
				Line: r.Line, Record: r.Ordinal, Segment: r.ID,
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
				Line: r.Line, Record: r.Ordinal, Segment: r.ID,
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
				Line: r.Line, Record: r.Ordinal, Segment: r.ID,
				Expected: strconv.Itoa(declared), Actual: strconv.Itoa(actual),
			})
		}
	}

	if declarers == 0 {
		rep.add(Finding{
			Rule:     RuleCountNoDeclarer,
			Severity: SeverityWarning,
			Message: fmt.Sprintf("count rule %s: no record starting with %q was found, so the declared "+
				"count of %q records (%d present) was never verified",
				rule, rule.Declaring, rule.Counted, actual),
			Expected: fmt.Sprintf("at least one %q record", rule.Declaring),
			Actual:   "none",
		})
	}
}
