package edilint

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// checkTerminators verifies that record terminators are used consistently.
//
// For line-oriented formats that means one line-ending style throughout. For
// X12 it means every segment ends with the terminator the ISA declared, the
// declared separators are usable and mutually distinct, and the optional
// whitespace between segments is applied uniformly.
func checkTerminators(s *source, rep *Report) {
	if s.Format == FormatX12 && s.Delims.Declared {
		checkX12Separators(s, rep)
		checkX12SegmentTerms(s, rep)
		checkX12Padding(s, rep)
		return
	}
	checkLineEndings(s, rep)
}

// checkLineEndings reports mixed CR/LF/CRLF usage and a missing final terminator.
func checkLineEndings(s *source, rep *Report) {
	if len(s.Records) == 0 {
		return
	}

	counts := map[string]int{}
	for _, r := range s.Records {
		if r.Term != "" {
			counts[r.Term]++
		}
	}
	if len(counts) > 1 {
		dominant := modalKey(counts)
		for _, r := range s.Records {
			if r.Term == "" || r.Term == dominant {
				continue
			}
			rep.add(Finding{
				Rule:     RuleMixedTerminator,
				Severity: SeverityError,
				Message: fmt.Sprintf("line ends with %s but the file predominantly uses %s (%s); "+
					"mixed line endings split records inconsistently across platforms",
					renderWS(r.Term), renderWS(dominant), describeCounts(counts)),
				Line:         r.Line,
				RecordNumber: r.Ordinal,
				Record:       r.ID,
				Expected:     renderWS(dominant),
				Actual:       renderWS(r.Term),
			})
		}
	}

	last := s.Records[len(s.Records)-1]
	if last.Term == "" && strings.TrimSpace(last.Text) != "" {
		rep.add(Finding{
			Rule:     RuleMissingFinal,
			Severity: SeverityWarning,
			Message: "last record has no terminator; readers that require a trailing newline will " +
				"drop or truncate it",
			Line:         last.Line,
			RecordNumber: last.Ordinal,
			Record:       last.ID,
		})
	}
}

// checkX12Separators validates the separator characters declared in the ISA.
func checkX12Separators(s *source, rep *Report) {
	d := s.Delims
	isaLine := s.LineAt(s.ISAOffset)

	if d.ISALen != 106 {
		rep.add(Finding{
			Rule:     RuleISALength,
			Severity: SeverityError,
			Message: fmt.Sprintf("ISA segment is %d characters including the terminator; X12 fixes it at 106, "+
				"so every downstream fixed-offset read of the envelope is shifted", d.ISALen),
			Line:         isaLine,
			RecordNumber: 1,
			Record:       "ISA",
			Expected:     "106",
			Actual:       strconv.Itoa(d.ISALen),
		})
	}

	named := []struct {
		name string
		b    byte
	}{
		{"element separator", d.Element},
		{"sub-element separator", d.SubElement},
		{"segment terminator", d.Segment},
	}
	// Repetition is only set when ISA11 actually declared a separator; see
	// deriveDelims. A 004010 interchange whose ISA11 is "U" declares none, and
	// must not be reported for having an alphanumeric separator.
	if d.Repetition != 0 {
		named = append(named, struct {
			name string
			b    byte
		}{"repetition separator", d.Repetition})
	}

	for _, n := range named {
		if isAlphanumericByte(n.b) {
			rep.add(Finding{
				Rule:     RuleX12Separator,
				Severity: SeverityError,
				Message: fmt.Sprintf("%s is %q, an alphanumeric character; it cannot be distinguished "+
					"from data", n.name, string(rune(n.b))),
				Line:         isaLine,
				RecordNumber: 1,
				Record:       "ISA",
				CodePoint:    fmt.Sprintf("U+%04X", n.b),
				Actual:       string(rune(n.b)),
			})
		}
	}
	for i := 0; i < len(named); i++ {
		for j := i + 1; j < len(named); j++ {
			if named[i].b != named[j].b {
				continue
			}
			rep.add(Finding{
				Rule:     RuleX12Separator,
				Severity: SeverityError,
				Message: fmt.Sprintf("%s and %s are both %q; the interchange cannot be tokenised unambiguously",
					named[i].name, named[j].name, string(rune(named[i].b))),
				Line:         isaLine,
				RecordNumber: 1,
				Record:       "ISA",
				CodePoint:    fmt.Sprintf("U+%04X", named[i].b),
				Actual:       string(rune(named[i].b)),
			})
		}
	}
}

// checkX12SegmentTerms reports segments that are not closed by the declared
// terminator, which in practice means a truncated file.
func checkX12SegmentTerms(s *source, rep *Report) {
	for _, r := range s.Records {
		if r.Term != "" || strings.TrimSpace(r.Text) == "" {
			continue
		}
		rep.add(Finding{
			Rule:     RuleX12Segment,
			Severity: SeverityError,
			Message: fmt.Sprintf("segment %s is not closed by the declared segment terminator %s; "+
				"the interchange appears truncated", r.ID, renderWS(string(s.Delims.Segment))),
			Line:         r.Line,
			RecordNumber: r.Ordinal,
			Record:       r.ID,
			Expected:     renderWS(string(s.Delims.Segment)),
			Actual:       "end of file",
		})
	}
}

// checkX12Padding reports inconsistent whitespace between segment terminators.
// A file that puts a line break after some segments but not others is usually
// the result of two generators writing into the same stream.
func checkX12Padding(s *source, rep *Report) {
	if len(s.Records) < 3 {
		return
	}
	// The final segment's trailing whitespace is not an inter-segment separator.
	body := s.Records[:len(s.Records)-1]

	counts := map[string]int{}
	for _, r := range body {
		counts[r.Pad]++
	}
	if len(counts) < 2 {
		return
	}
	dominant := modalKey(counts)
	for _, r := range body {
		if r.Pad == dominant {
			continue
		}
		rep.add(Finding{
			Rule:     RuleX12Padding,
			Severity: SeverityWarning,
			Message: fmt.Sprintf("segment %s is followed by %s where the file predominantly uses %s (%s); "+
				"inconsistent inter-segment whitespace breaks readers that split on the terminator plus newline",
				r.ID, renderWS(r.Pad), renderWS(dominant), describeCounts(counts)),
			Line:         r.Line,
			RecordNumber: r.Ordinal,
			Record:       r.ID,
			Expected:     renderWS(dominant),
			Actual:       renderWS(r.Pad),
		})
	}
}

func isAlphanumericByte(b byte) bool {
	return (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9')
}

// modalKey returns the most frequent key, breaking ties lexically for determinism.
func modalKey(counts map[string]int) string {
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	best, bestN := "", -1
	for _, k := range keys {
		if counts[k] > bestN {
			best, bestN = k, counts[k]
		}
	}
	return best
}

// describeCounts renders a terminator histogram, e.g. "CRLF x12, LF x1".
func describeCounts(counts map[string]int) string {
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if counts[keys[i]] != counts[keys[j]] {
			return counts[keys[i]] > counts[keys[j]]
		}
		return keys[i] < keys[j]
	})
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s x%d", renderWS(k), counts[k]))
	}
	return strings.Join(parts, ", ")
}

// renderWS gives whitespace and separator runs a readable name.
func renderWS(s string) string {
	switch s {
	case "":
		return "nothing"
	case "\r\n":
		return "CRLF"
	case "\n":
		return "LF"
	case "\r":
		return "CR"
	}
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '\r':
			b.WriteString("CR")
		case '\n':
			b.WriteString("LF")
		case '\t':
			b.WriteString("TAB")
		case ' ':
			b.WriteString("SP")
		default:
			b.WriteString(strconv.QuoteRune(r))
		}
	}
	return b.String()
}
