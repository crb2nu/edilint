package edilint

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// minGroupForOutlier is the smallest number of records sharing a discriminator
// before a differing field count is treated as an outlier rather than as two
// equally plausible shapes.
const minGroupForOutlier = 3

// checkFieldCounts reports records that carry a different number of fields from
// the other records of the same type.
//
// It applies to delimited and HL7v2 inputs. X12 is excluded because omitting
// trailing elements is legal and routine there, so a varying element count
// carries no signal.
func checkFieldCounts(s *source, opts Options, rep *Report) {
	switch s.Format {
	case FormatDelimited, FormatHL7v2:
	default:
		return
	}
	if s.FieldSep == 0 {
		return
	}

	typeField := opts.typeField()
	severity := SeverityError
	if s.Format == FormatHL7v2 {
		// HL7v2 permits omitting trailing fields, so a varying count is a smell
		// rather than a defect.
		severity = SeverityWarning
	}

	type group struct {
		counts map[int]int
		total  int
	}
	groups := map[string]*group{}
	entries, keyBytes := 0, 0

	for r := range s.records() {
		if strings.TrimSpace(r.Text) == "" {
			continue
		}
		fields := s.Fields(r)
		key := ""
		if typeField <= len(fields) {
			key = strings.TrimSpace(fields[typeField-1])
		}
		g, ok := groups[key]
		if !ok {
			entries++
			keyBytes += len(key)
			if !s.acceptState(entries, keyBytes) {
				return
			}
			g = &group{counts: map[int]int{}}
			groups[strings.Clone(key)] = g
		}
		if _, exists := g.counts[len(fields)]; !exists {
			entries++
			if !s.acceptState(entries, keyBytes) {
				return
			}
		}
		g.counts[len(fields)]++
		g.total++
	}

	modals := map[string]int{}
	for key, g := range groups {
		modals[key] = modalInt(g.counts)
	}
	for r := range s.records() {
		if strings.TrimSpace(r.Text) == "" {
			continue
		}
		fields := s.Fields(r)
		key := ""
		if typeField <= len(fields) {
			key = strings.TrimSpace(fields[typeField-1])
		}
		g := groups[key]
		if g == nil || g.total < minGroupForOutlier || len(g.counts) < 2 {
			continue
		}
		modal := modals[key]
		if len(fields) == modal {
			continue
		}
		label := key
		if label == "" {
			label = "(untyped)"
		}
		rep.add(Finding{
			Rule: RuleFieldOutlier, Severity: severity,
			Message: fmt.Sprintf("record type %q has %d field(s) here but %d in %d of %d record(s) "+
				"of this type; a shifted field count moves every value after the break",
				label, len(fields), modal, g.counts[modal], g.total),
			Line: r.Line, RecordNumber: r.Ordinal, Record: r.ID,
			Expected: strconv.Itoa(modal), Actual: strconv.Itoa(len(fields)),
		})
	}
}

// modalInt returns the most frequent key, breaking ties toward the larger value.
func modalInt(counts map[int]int) int {
	keys := make([]int, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	best, bestN := 0, -1
	for _, k := range keys {
		if counts[k] >= bestN {
			best, bestN = k, counts[k]
		}
	}
	return best
}
