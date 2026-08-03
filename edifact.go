package edilint

import (
	"fmt"
	"strconv"
	"strings"
)

// EDIFACT envelope checks: UNB/UNZ, UNG/UNE and UNH/UNT pairing, trailer
// recounts, control-reference matching, and the UNA service string advice.
// Envelope level only — message content is out of scope, exactly as the X12
// checks stop at the ST/SE boundary.

// edifactDelims holds the service characters in force for an EDIFACT
// interchange: the defaults of the standard's level A repertoire, or the ones
// a UNA service string advice declares.
type edifactDelims struct {
	Component byte // ":" unless UNA declares otherwise
	Element   byte // "+"
	Decimal   byte // "."
	Release   byte // "?"
	// Repetition is UNA position 7, a separator only under syntax version 4.
	// A space declares none, which is also how version 3 reserves the position.
	Repetition byte
	Segment    byte // "'"
	// Declared is false when the file could not be read as EDIFACT at all.
	Declared bool
	// UNA is true when the file carried a service string advice.
	UNA bool
}

func defaultEdifactDelims() edifactDelims {
	return edifactDelims{
		Component: ':', Element: '+', Decimal: '.', Release: '?', Segment: '\'',
		Declared: true,
	}
}

// unaLength is the fixed size of a service string advice: the three-letter tag
// and six service characters. UNA has no terminator; its ninth character is the
// terminator declaration itself.
const unaLength = 9

// buildEdifact derives the service characters and tokenizes the body into
// segments. Like buildX12, structural problems found here are reported rather
// than returned, so a malformed file still produces a report.
func (s *source) buildEdifact(rep *Report) {
	start := leadingSpace(s.Body)
	head := s.Body[start:]

	switch {
	case len(head) >= 3 && string(head[:3]) == "UNA":
		s.Edifact = defaultEdifactDelims()
		s.Edifact.UNA = true
		if len(head) < unaLength {
			rep.add(Finding{
				Rule:     RuleEdifactServiceString,
				Severity: SeverityError,
				Message: fmt.Sprintf("UNA service string advice is %d character(s); it is fixed at nine "+
					"(the tag and six service characters), so the declared separators are unusable and "+
					"the defaults were assumed", len(head)),
				Line: s.LineAt(start), Record: "UNA",
				Expected: "9", Actual: strconv.Itoa(len(head)),
			})
			start += len(head)
			break
		}
		s.Edifact.Component = head[3]
		s.Edifact.Element = head[4]
		s.Edifact.Decimal = head[5]
		s.Edifact.Release = head[6]
		if head[7] != ' ' {
			s.Edifact.Repetition = head[7]
		}
		s.Edifact.Segment = head[8]
		s.checkUNA(rep, s.LineAt(start))
		start += unaLength
	case len(head) >= 3 && string(head[:3]) == "UNB":
		s.Edifact = defaultEdifactDelims()
	default:
		// Format was forced to edifact but there is no envelope to read. Degrade
		// to lines so the character and terminator checks still run.
		rep.add(Finding{
			Rule:     RuleEdifactNesting,
			Severity: SeverityError,
			Message:  "no UNB segment found; file cannot be read as an EDIFACT interchange",
			Line:     1,
		})
		s.buildLines()
		s.assignIDs()
		return
	}

	s.FieldSep = s.Edifact.Element
	// A line break routinely follows the UNA; the first real segment starts on
	// content, exactly as the padding after every terminator is consumed below.
	for start < len(s.Body) && isPadByte(s.Body[start]) {
		start++
	}
	s.tokenizeEdifact(start)
	s.assignIDs()
}

// checkUNA validates the six declared service characters: mutually distinct,
// the separators not alphanumeric, and the decimal mark one of the two the
// standard allows. A colliding or alphanumeric set cannot be tokenized
// unambiguously, which is the same defect EL2005 reports for X12.
func (s *source) checkUNA(rep *Report, line int) {
	bad := func(msg string, actual byte) {
		rep.add(Finding{
			Rule:     RuleEdifactServiceString,
			Severity: SeverityError,
			Message:  msg,
			Line:     line, Record: "UNA",
			CodePoint: fmt.Sprintf("U+%04X", actual),
			Actual:    string(rune(actual)),
		})
	}

	named := []struct {
		name string
		b    byte
	}{
		{"component separator", s.Edifact.Component},
		{"element separator", s.Edifact.Element},
		{"release character", s.Edifact.Release},
		{"segment terminator", s.Edifact.Segment},
	}
	if s.Edifact.Repetition != 0 {
		named = append(named, struct {
			name string
			b    byte
		}{"repetition separator", s.Edifact.Repetition})
	}

	for _, n := range named {
		if isAlphanumericByte(n.b) {
			bad(fmt.Sprintf("UNA declares %q as the %s, an alphanumeric character; it cannot be "+
				"distinguished from data", string(rune(n.b)), n.name), n.b)
		}
	}
	if d := s.Edifact.Decimal; d != '.' && d != ',' {
		bad(fmt.Sprintf("UNA declares %q as the decimal mark; EDIFACT allows only a period or a comma",
			string(rune(d))), d)
	}

	withDecimal := append(named, struct {
		name string
		b    byte
	}{"decimal mark", s.Edifact.Decimal})
	for i := 0; i < len(withDecimal); i++ {
		for j := i + 1; j < len(withDecimal); j++ {
			if withDecimal[i].b != withDecimal[j].b {
				continue
			}
			bad(fmt.Sprintf("UNA declares the %s and the %s as the same character %q; the interchange "+
				"cannot be tokenized unambiguously", withDecimal[i].name, withDecimal[j].name,
				string(rune(withDecimal[i].b))), withDecimal[i].b)
		}
	}
}

// tokenizeEdifact splits the body on the segment terminator, honoring the
// release character, and records the inter-segment padding after each
// terminator the way the X12 tokenizer does.
func (s *source) tokenizeEdifact(start int) {
	body := s.Body
	term, release := s.Edifact.Segment, s.Edifact.Release
	pos, ordinal := start, 0
	for pos < len(body) {
		end := -1
		for i := pos; i < len(body); i++ {
			c := body[i]
			if c == release && i+1 < len(body) {
				i++
				continue
			}
			if c == term {
				end = i
				break
			}
		}
		if end < 0 {
			text := string(body[pos:])
			if strings.TrimSpace(text) != "" {
				ordinal++
				s.Records = append(s.Records, record{
					Text:    strings.TrimRight(text, "\r\n \t"),
					Offset:  pos,
					Line:    s.LineAt(pos),
					Ordinal: ordinal,
				})
			}
			return
		}
		ordinal++
		rec := record{
			Text:    string(body[pos:end]),
			Offset:  pos,
			Line:    s.LineAt(pos),
			Ordinal: ordinal,
			Term:    string(term),
		}
		p := end + 1
		for p < len(body) && body[p] != term && isPadByte(body[p]) {
			p++
		}
		rec.Pad = string(body[end+1 : p])
		s.Records = append(s.Records, rec)
		pos = p
	}
}

// edifactSplit splits a segment into its elements, honoring the release
// character. Released characters are kept as written — "?+" stays "?+" — so
// two references compare equal exactly when their raw spellings do.
func edifactSplit(text string, element, release byte) []string {
	var out []string
	var cur strings.Builder
	for i := 0; i < len(text); i++ {
		c := text[i]
		if c == release && i+1 < len(text) {
			cur.WriteByte(c)
			i++
			cur.WriteByte(text[i])
			continue
		}
		if c == element {
			out = append(out, cur.String())
			cur.Reset()
			continue
		}
		cur.WriteByte(c)
	}
	return append(out, cur.String())
}

// checkEdifactSegmentTerms reports segments that are not closed by the segment
// terminator in force, which in practice means a truncated file. Only the
// final segment can be unterminated, because tokenization splits on the
// terminator everywhere else.
func checkEdifactSegmentTerms(s *source, rep *Report) {
	for _, r := range s.Records {
		if r.Term != "" || strings.TrimSpace(r.Text) == "" {
			continue
		}
		rep.add(Finding{
			Rule:     RuleEdifactSegment,
			Severity: SeverityError,
			Message: fmt.Sprintf("segment %s is not closed by the segment terminator %q; "+
				"the interchange appears truncated", r.ID, string(rune(s.Edifact.Segment))),
			Line:         r.Line,
			RecordNumber: r.Ordinal,
			Record:       r.ID,
			Expected:     string(rune(s.Edifact.Segment)),
			Actual:       "end of file",
		})
	}
}

// checkEdifactEnvelope verifies UNB/UNZ, UNG/UNE and UNH/UNT pairing, recounts
// the declared totals in every trailer, and matches header and trailer control
// references.
func checkEdifactEnvelope(s *source, rep *Report) {
	if !s.Edifact.Declared {
		return
	}

	var unb, ung, unh envelopeLevel
	// UNZ-1 counts functional groups when the interchange uses them and
	// messages when it does not, so both are recounted.
	messages, groups := 0, 0
	trailingReported := false

	for i := range s.Records {
		r := s.Records[i]
		if strings.TrimSpace(r.Text) == "" {
			continue
		}
		if unh.open {
			unh.children++
		}
		f := s.Fields(r)

		switch r.ID {
		case "UNB":
			if unb.open {
				reportEdifactUnclosed(rep, unb.rec, "UNB", "UNZ")
				if ung.open {
					reportEdifactUnclosed(rep, ung.rec, "UNG", "UNE")
					ung = envelopeLevel{}
				}
				if unh.open {
					reportEdifactUnclosed(rep, unh.rec, "UNH", "UNT")
					unh = envelopeLevel{}
				}
			}
			unb = envelopeLevel{open: true, rec: r, control: elem(f, 5)}
			messages, groups = 0, 0
			trailingReported = false
			if strings.TrimSpace(unb.control) == "" {
				rep.add(Finding{
					Rule:     RuleEdifactControlRef,
					Severity: SeverityError,
					Message:  "UNB-5 interchange control reference is empty",
					Line:     r.Line, RecordNumber: r.Ordinal, Record: r.ID,
				})
			}

		case "UNZ":
			if !unb.open {
				reportEdifactUnopened(rep, r, "UNZ", "UNB")
				continue
			}
			if unh.open {
				reportEdifactUnclosed(rep, unh.rec, "UNH", "UNT")
				unh = envelopeLevel{}
			}
			if ung.open {
				reportEdifactUnclosed(rep, ung.rec, "UNG", "UNE")
				ung = envelopeLevel{}
			}
			compareEdifactRef(rep, r, "UNB-5", unb.control, "UNZ-2", elem(f, 2))
			actual, unit := messages, "message(s) (UNH)"
			if groups > 0 {
				actual, unit = groups, "functional group(s) (UNG)"
			}
			compareEdifactCount(rep, r, RuleEdifactInterchangeCount, "UNZ-1", elem(f, 1), actual,
				unit, "the interchange")
			unb.open = false

		case "UNG":
			if !unb.open {
				rep.add(Finding{
					Rule:     RuleEdifactNesting,
					Severity: SeverityError,
					Message:  "UNG appears outside a UNB interchange",
					Line:     r.Line, RecordNumber: r.Ordinal, Record: r.ID,
				})
			}
			if ung.open {
				reportEdifactUnclosed(rep, ung.rec, "UNG", "UNE")
			}
			groups++
			ung = envelopeLevel{open: true, rec: r, control: elem(f, 5)}

		case "UNE":
			if !ung.open {
				reportEdifactUnopened(rep, r, "UNE", "UNG")
				continue
			}
			if unh.open {
				reportEdifactUnclosed(rep, unh.rec, "UNH", "UNT")
				unh = envelopeLevel{}
			}
			compareEdifactRef(rep, r, "UNG-5", ung.control, "UNE-2", elem(f, 2))
			compareEdifactCount(rep, r, RuleEdifactGroupCount, "UNE-1", elem(f, 1), ung.children,
				"message(s) (UNH)", "the group")
			ung.open = false

		case "UNH":
			if !unb.open {
				rep.add(Finding{
					Rule:     RuleEdifactNesting,
					Severity: SeverityError,
					Message:  "UNH appears outside a UNB interchange",
					Line:     r.Line, RecordNumber: r.Ordinal, Record: r.ID,
				})
			}
			if unh.open {
				reportEdifactUnclosed(rep, unh.rec, "UNH", "UNT")
			}
			messages++
			if ung.open {
				ung.children++
			}
			unh = envelopeLevel{open: true, rec: r, control: elem(f, 1), children: 1}

		case "UNT":
			if !unh.open {
				reportEdifactUnopened(rep, r, "UNT", "UNH")
				continue
			}
			compareEdifactRef(rep, r, "UNH-1", unh.control, "UNT-2", elem(f, 2))
			compareEdifactCount(rep, r, RuleEdifactSegmentCount, "UNT-1", elem(f, 1), unh.children,
				"segment(s) from UNH through UNT inclusive", "the message")
			unh.open = false

		case "UNA":
			rep.add(Finding{
				Rule:     RuleEdifactServiceString,
				Severity: SeverityError,
				Message: "UNA service string advice appears after the interchange has started; " +
					"it is only honored as the very first thing in the file",
				Line: r.Line, RecordNumber: r.Ordinal, Record: r.ID,
			})

		default:
			if !unb.open && !trailingReported {
				rep.add(Finding{
					Rule:     RuleEdifactTrailing,
					Severity: SeverityError,
					Message: fmt.Sprintf("segment %s appears outside any interchange; "+
						"data after UNZ is not part of a valid EDIFACT file", r.ID),
					Line: r.Line, RecordNumber: r.Ordinal, Record: r.ID,
				})
				trailingReported = true
			}
		}
	}

	if unh.open {
		reportEdifactUnclosed(rep, unh.rec, "UNH", "UNT")
	}
	if ung.open {
		reportEdifactUnclosed(rep, ung.rec, "UNG", "UNE")
	}
	if unb.open {
		reportEdifactUnclosed(rep, unb.rec, "UNB", "UNZ")
	}
}

// compareEdifactRef matches a trailer control reference against its header,
// with the same string-first, numerically-equal-is-a-warning grading the X12
// control-number check uses.
func compareEdifactRef(rep *Report, r record, headerName, headerVal, trailerName, trailerVal string) {
	h, t := strings.TrimSpace(headerVal), strings.TrimSpace(trailerVal)
	if h == t {
		return
	}
	hn, herr := strconv.Atoi(h)
	tn, terr := strconv.Atoi(t)
	if herr == nil && terr == nil && hn == tn {
		rep.add(Finding{
			Rule:     RuleEdifactControlRef,
			Severity: SeverityWarning,
			Message: fmt.Sprintf("%s is %q but %s is %q; the values are numerically equal but not identical, "+
				"and strict partners compare them as strings", headerName, h, trailerName, t),
			Line: r.Line, RecordNumber: r.Ordinal, Record: r.ID,
			Expected: h, Actual: t,
		})
		return
	}
	rep.add(Finding{
		Rule:     RuleEdifactControlRef,
		Severity: SeverityError,
		Message: fmt.Sprintf("%s is %q but the matching %s is %q; header and trailer control references must match",
			headerName, h, trailerName, t),
		Line: r.Line, RecordNumber: r.Ordinal, Record: r.ID,
		Expected: h, Actual: t,
	})
}

// compareEdifactCount recounts a declared trailer total against what the
// enclosing envelope contains. The count elements are mandatory in EDIFACT, so
// an empty value is an error here, unlike the optional HL7v2 batch counts.
func compareEdifactCount(rep *Report, r record, rule, element, declared string, actual int, unit, scope string) {
	d := strings.TrimSpace(declared)
	n, err := strconv.Atoi(d)
	if d == "" || err != nil {
		rep.add(Finding{
			Rule:     rule,
			Severity: SeverityError,
			Message:  fmt.Sprintf("%s is %q, which is not a number; the declared count cannot be verified", element, declared),
			Line:     r.Line, RecordNumber: r.Ordinal, Record: r.ID,
			Expected: "an integer", Actual: declared,
		})
		return
	}
	if n == actual {
		return
	}
	rep.add(Finding{
		Rule:     rule,
		Severity: SeverityError,
		Message: fmt.Sprintf("%s declares %d %s but %s contains %d",
			element, n, unit, scope, actual),
		Line: r.Line, RecordNumber: r.Ordinal, Record: r.ID,
		Expected: strconv.Itoa(n), Actual: strconv.Itoa(actual),
	})
}

func reportEdifactUnclosed(rep *Report, r record, header, trailer string) {
	rep.add(Finding{
		Rule:     RuleEdifactUnclosed,
		Severity: SeverityError,
		Message:  fmt.Sprintf("%s is never closed by a matching %s", header, trailer),
		Line:     r.Line, RecordNumber: r.Ordinal, Record: r.ID,
		Expected: trailer, Actual: "none",
	})
}

func reportEdifactUnopened(rep *Report, r record, trailer, header string) {
	rep.add(Finding{
		Rule:     RuleEdifactUnopened,
		Severity: SeverityError,
		Message:  fmt.Sprintf("%s appears without a preceding %s", trailer, header),
		Line:     r.Line, RecordNumber: r.Ordinal, Record: r.ID,
		Expected: header, Actual: "none",
	})
}
