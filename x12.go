package edilint

import (
	"fmt"
	"strconv"
	"strings"
)

// envelopeLevel tracks one open ISA, GS or ST while walking the interchange.
type envelopeLevel struct {
	open    bool
	rec     record
	control string
	// children counts the enclosed envelopes (GS in ISA, ST in GS) or, for a
	// transaction set, the segments from ST through SE inclusive.
	children int
	seen     map[string]int
}

// checkX12Envelope verifies ISA/IEA, GS/GE and ST/SE pairing, recounts the
// declared totals in every trailer, matches header and trailer control numbers,
// reports duplicate control numbers, and validates envelope dates and times.
func checkX12Envelope(s *source, opts Options, rep *Report) {
	if !s.Delims.Declared {
		return
	}

	var isa, gs, st envelopeLevel
	// Each control number lives in its own scope, and the scopes are tracked
	// separately: an ISA13 of "1" and a GS06 of "1" are not duplicates of each
	// other, and generators routinely emit exactly that pair.
	seenISA13 := map[string]int{} // file scope
	seenGS06 := map[string]int{}  // reset per interchange
	trailingReported := false

	for i := range s.Records {
		r := s.Records[i]
		if strings.TrimSpace(r.Text) == "" {
			continue
		}
		if st.open {
			st.children++
		}
		f := s.Fields(r)

		switch r.ID {
		case "ISA":
			if isa.open {
				reportUnclosed(rep, isa.rec, "ISA", "IEA")
				if gs.open {
					reportUnclosed(rep, gs.rec, "GS", "GE")
					gs = envelopeLevel{}
				}
			}
			isa = envelopeLevel{open: true, rec: r, control: elem(f, 13)}
			seenGS06 = map[string]int{}
			checkISA(s, opts, rep, r, f, isa.control, seenISA13)
			trailingReported = false

		case "IEA":
			if !isa.open {
				reportUnopened(rep, r, "IEA", "ISA")
				continue
			}
			if gs.open {
				reportUnclosed(rep, gs.rec, "GS", "GE")
				gs = envelopeLevel{}
			}
			compareControl(rep, r, "ISA13", isa.control, "IEA02", elem(f, 2))
			compareCount(rep, r, "IEA01", elem(f, 1), isa.children, "functional group(s) (GS)")
			isa.open = false

		case "GS":
			if !isa.open {
				rep.add(Finding{
					Rule:     RuleEnvelopeNesting,
					Severity: SeverityError,
					Message:  "GS appears outside an ISA interchange",
					Line:     r.Line, RecordNumber: r.Ordinal, Record: r.ID,
				})
			}
			if gs.open {
				reportUnclosed(rep, gs.rec, "GS", "GE")
			}
			isa.children++
			gs = envelopeLevel{open: true, rec: r, control: elem(f, 6), seen: map[string]int{}}
			checkDuplicate(rep, r, "GS06", gs.control, seenGS06, "interchange")
			checkDate(rep, r, "GS04", elem(f, 4), 8)
			checkTime(rep, r, "GS05", elem(f, 5))

		case "GE":
			if !gs.open {
				reportUnopened(rep, r, "GE", "GS")
				continue
			}
			if st.open {
				reportUnclosed(rep, st.rec, "ST", "SE")
				st = envelopeLevel{}
			}
			compareControl(rep, r, "GS06", gs.control, "GE02", elem(f, 2))
			compareCount(rep, r, "GE01", elem(f, 1), gs.children, "transaction set(s) (ST)")
			gs.open = false

		case "ST":
			if !gs.open {
				rep.add(Finding{
					Rule:     RuleEnvelopeNesting,
					Severity: SeverityError,
					Message:  "ST appears outside a GS functional group",
					Line:     r.Line, RecordNumber: r.Ordinal, Record: r.ID,
				})
			}
			if st.open {
				reportUnclosed(rep, st.rec, "ST", "SE")
			}
			gs.children++
			st = envelopeLevel{open: true, rec: r, control: elem(f, 2), children: 1}
			if gs.seen != nil {
				checkDuplicate(rep, r, "ST02", st.control, gs.seen, "functional group")
			}

		case "SE":
			if !st.open {
				reportUnopened(rep, r, "SE", "ST")
				continue
			}
			compareControl(rep, r, "ST02", st.control, "SE02", elem(f, 2))
			compareCount(rep, r, "SE01", elem(f, 1), st.children,
				"segment(s) from ST through SE inclusive")
			st.open = false

		default:
			if !isa.open && !trailingReported {
				rep.add(Finding{
					Rule:     RuleEnvelopeTrailing,
					Severity: SeverityError,
					Message: fmt.Sprintf("segment %s appears outside any interchange; "+
						"data after IEA is not part of a valid X12 file", r.ID),
					Line: r.Line, RecordNumber: r.Ordinal, Record: r.ID,
				})
				trailingReported = true
			}
		}
	}

	if st.open {
		reportUnclosed(rep, st.rec, "ST", "SE")
	}
	if gs.open {
		reportUnclosed(rep, gs.rec, "GS", "GE")
	}
	if isa.open {
		reportUnclosed(rep, isa.rec, "ISA", "IEA")
	}
}

// checkISA validates the interchange header's control number, date and time.
func checkISA(s *source, opts Options, rep *Report, r record, f []string, control string, seenISA13 map[string]int) {
	ctl := strings.TrimSpace(control)
	if ctl == "" {
		rep.add(Finding{
			Rule:     RuleEnvelopeMissingID,
			Severity: SeverityError,
			Message:  "ISA13 interchange control number is empty",
			Line:     r.Line, RecordNumber: r.Ordinal, Record: r.ID,
		})
	} else {
		checkDuplicate(rep, r, "ISA13", ctl, seenISA13, "file")
		if opts.SeenISA13 != nil {
			if prev, ok := opts.SeenISA13[ctl]; ok && prev != s.Name {
				rep.add(Finding{
					Rule:     RuleDupControl,
					Severity: SeverityError,
					Message: fmt.Sprintf("ISA13 %q was already used in %s; a repeated interchange control "+
						"number is rejected as a duplicate transmission (TA1 025)", ctl, prev),
					Line: r.Line, RecordNumber: r.Ordinal, Record: r.ID,
					Expected: "unique interchange control number",
					Actual:   ctl,
				})
			} else if !ok {
				opts.SeenISA13[ctl] = s.Name
			}
		}
	}
	checkDate(rep, r, "ISA09", elem(f, 9), 6)
	checkTime(rep, r, "ISA10", elem(f, 10))
}

// checkDuplicate records a control number and reports a repeat within scope.
func checkDuplicate(rep *Report, r record, element, value string, seen map[string]int, scope string) {
	value = strings.TrimSpace(value)
	if value == "" || seen == nil {
		return
	}
	if prev, ok := seen[value]; ok {
		rep.add(Finding{
			Rule:     RuleDupControl,
			Severity: SeverityError,
			Message: fmt.Sprintf("%s control number %q is already used by record %d in this %s; "+
				"control numbers must be unique within their scope", element, value, prev, scope),
			Line: r.Line, RecordNumber: r.Ordinal, Record: r.ID,
			Expected: fmt.Sprintf("unique %s within the %s", element, scope),
			Actual:   value,
		})
		return
	}
	seen[value] = r.Ordinal
}

// compareControl matches a trailer control number against its header.
func compareControl(rep *Report, r record, headerName, headerVal, trailerName, trailerVal string) {
	h, t := strings.TrimSpace(headerVal), strings.TrimSpace(trailerVal)
	if h == t {
		return
	}
	hn, herr := strconv.Atoi(h)
	tn, terr := strconv.Atoi(t)
	if herr == nil && terr == nil && hn == tn {
		rep.add(Finding{
			Rule:     RuleControlNumber,
			Severity: SeverityWarning,
			Message: fmt.Sprintf("%s is %q but %s is %q; the values are numerically equal but not identical, "+
				"and strict partners compare them as strings", headerName, h, trailerName, t),
			Line: r.Line, RecordNumber: r.Ordinal, Record: r.ID,
			Expected: h, Actual: t,
		})
		return
	}
	rep.add(Finding{
		Rule:     RuleControlNumber,
		Severity: SeverityError,
		Message: fmt.Sprintf("%s is %q but the matching %s is %q; header and trailer control numbers must match",
			headerName, h, trailerName, t),
		Line: r.Line, RecordNumber: r.Ordinal, Record: r.ID,
		Expected: h, Actual: t,
	})
}

// compareCount recounts a declared trailer total against what the file contains.
func compareCount(rep *Report, r record, element, declared string, actual int, unit string) {
	rule := RuleSegmentCount
	switch element {
	case "GE01":
		rule = RuleGroupCount
	case "IEA01":
		rule = RuleInterchangeCount
	}

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
		Message: fmt.Sprintf("%s declares %d %s but the file contains %d",
			element, n, unit, actual),
		Line: r.Line, RecordNumber: r.Ordinal, Record: r.ID,
		Expected: strconv.Itoa(n), Actual: strconv.Itoa(actual),
	})
}

// checkDate validates an X12 date element of the given width: 6 for YYMMDD,
// 8 for CCYYMMDD.
func checkDate(rep *Report, r record, element, value string, width int) {
	v := strings.TrimSpace(value)
	shape := "YYMMDD"
	if width == 8 {
		shape = "CCYYMMDD"
	}
	bad := func(reason string) {
		rep.add(Finding{
			Rule:     RuleDateTime,
			Severity: SeverityError,
			Message:  fmt.Sprintf("%s is %q: %s (expected %s)", element, value, reason, shape),
			Line:     r.Line, RecordNumber: r.Ordinal, Record: r.ID,
			Expected: shape, Actual: value,
		})
	}

	if v == "" {
		bad("empty date")
		return
	}
	if len(v) != width || !allDigits(v) {
		bad(fmt.Sprintf("expected %d digits, got %d character(s)", width, len(v)))
		return
	}

	var year int
	if width == 8 {
		year, _ = strconv.Atoi(v[0:4])
		v = v[4:]
	} else {
		yy, _ := strconv.Atoi(v[0:2])
		// X12 has no century in YYMMDD; the conventional pivot puts 00-69 in the
		// 2000s. The century only matters here for the February leap-day check.
		if yy <= 69 {
			year = 2000 + yy
		} else {
			year = 1900 + yy
		}
		v = v[2:]
	}
	month, _ := strconv.Atoi(v[0:2])
	day, _ := strconv.Atoi(v[2:4])

	if month < 1 || month > 12 {
		bad(fmt.Sprintf("month %02d is out of range", month))
		return
	}
	maxDay := daysInMonth(year, month)
	if day < 1 || day > maxDay {
		bad(fmt.Sprintf("day %02d is out of range for month %02d (max %d)", day, month, maxDay))
	}
}

// checkTime validates an X12 time element: HHMM, optionally with seconds and
// hundredths (HHMMSS or HHMMSSDD).
func checkTime(rep *Report, r record, element, value string) {
	v := strings.TrimSpace(value)
	bad := func(reason string) {
		rep.add(Finding{
			Rule:     RuleDateTime,
			Severity: SeverityError,
			Message:  fmt.Sprintf("%s is %q: %s (expected HHMM, HHMMSS or HHMMSSDD)", element, value, reason),
			Line:     r.Line, RecordNumber: r.Ordinal, Record: r.ID,
			Expected: "HHMM", Actual: value,
		})
	}

	if v == "" {
		bad("empty time")
		return
	}
	if !allDigits(v) {
		bad("contains non-digit characters")
		return
	}
	switch len(v) {
	case 4, 6, 8:
	default:
		bad(fmt.Sprintf("expected 4, 6 or 8 digits, got %d", len(v)))
		return
	}

	hour, _ := strconv.Atoi(v[0:2])
	minute, _ := strconv.Atoi(v[2:4])
	if hour > 23 {
		bad(fmt.Sprintf("hour %02d is out of range", hour))
		return
	}
	if minute > 59 {
		bad(fmt.Sprintf("minute %02d is out of range", minute))
		return
	}
	if len(v) >= 6 {
		if sec, _ := strconv.Atoi(v[4:6]); sec > 59 {
			bad(fmt.Sprintf("second %02d is out of range", sec))
		}
	}
}

func reportUnclosed(rep *Report, r record, header, trailer string) {
	rep.add(Finding{
		Rule:     RuleUnclosed,
		Severity: SeverityError,
		Message:  fmt.Sprintf("%s is never closed by a matching %s", header, trailer),
		Line:     r.Line, RecordNumber: r.Ordinal, Record: r.ID,
		Expected: trailer, Actual: "none",
	})
}

func reportUnopened(rep *Report, r record, trailer, header string) {
	rep.add(Finding{
		Rule:     RuleUnopened,
		Severity: SeverityError,
		Message:  fmt.Sprintf("%s appears without a preceding %s", trailer, header),
		Line:     r.Line, RecordNumber: r.Ordinal, Record: r.ID,
		Expected: header, Actual: "none",
	})
}

// elem returns the nth X12 element of a segment. Element 0 is the segment ID.
func elem(fields []string, n int) string {
	if n < 0 || n >= len(fields) {
		return ""
	}
	return fields[n]
}

func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func daysInMonth(year, month int) int {
	switch month {
	case 1, 3, 5, 7, 8, 10, 12:
		return 31
	case 4, 6, 9, 11:
		return 30
	case 2:
		if year%4 == 0 && (year%100 != 0 || year%400 == 0) {
			return 29
		}
		return 28
	}
	return 0
}
