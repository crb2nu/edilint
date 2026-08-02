package edilint

import (
	"fmt"
	"strconv"
	"strings"
)

// HL7v2 batch structure checks: FHS/FTS and BHS/BTS pairing, the declared
// batch and message counts, and separator declarations that agree across every
// header in the file. Envelope level only — message content is out of scope,
// exactly as the X12 checks stop at the ST/SE boundary.
//
// A file of bare MSH messages is valid without any envelope, so the pairing
// checks run only when the file uses batch segments at all. The separator
// checks always run: a single message with malformed encoding characters is
// broken on its own.

// checkHL7Batch verifies the batch envelope of an HL7v2 input.
func checkHL7Batch(s *source, rep *Report) {
	checkHL7HeaderSeparators(s, rep)

	hasBatch := false
	for _, r := range s.Records {
		switch r.ID {
		case "FHS", "BHS", "BTS", "FTS":
			hasBatch = true
		}
	}
	if !hasBatch {
		return
	}

	var fhs, bhs envelopeLevel
	batches := 0
	strayReported := false

	for i := range s.Records {
		r := s.Records[i]
		if strings.TrimSpace(r.Text) == "" {
			continue
		}
		f := s.Fields(r)

		switch r.ID {
		case "FHS":
			if fhs.open {
				reportBatchUnclosed(rep, fhs.rec, "FHS", "FTS")
			}
			fhs = envelopeLevel{open: true, rec: r}
			batches = 0

		case "FTS":
			if !fhs.open {
				reportBatchUnopened(rep, r, "FTS", "FHS")
				continue
			}
			if bhs.open {
				reportBatchUnclosed(rep, bhs.rec, "BHS", "BTS")
				bhs = envelopeLevel{}
			}
			compareBatchCount(rep, r, RuleBatchFileCount, "FTS-1", elem(f, 1), batches,
				"batch(es) (BHS)", "the file")
			fhs.open = false

		case "BHS":
			if bhs.open {
				reportBatchUnclosed(rep, bhs.rec, "BHS", "BTS")
			}
			batches++
			bhs = envelopeLevel{open: true, rec: r}

		case "BTS":
			if !bhs.open {
				reportBatchUnopened(rep, r, "BTS", "BHS")
				continue
			}
			compareBatchCount(rep, r, RuleBatchMessageCount, "BTS-1", elem(f, 1), bhs.children,
				"message(s) (MSH)", "the batch")
			bhs.open = false

		case "MSH":
			if bhs.open {
				bhs.children++
			} else if !strayReported {
				rep.add(Finding{
					Rule:     RuleBatchStrayMessage,
					Severity: SeverityWarning,
					Message: "MSH appears outside any batch in a file that uses batch envelopes; " +
						"batch-aware readers process only the messages between BHS and BTS",
					Line: r.Line, RecordNumber: r.Ordinal, Record: r.ID,
				})
				strayReported = true
			}
		}
	}

	if bhs.open {
		reportBatchUnclosed(rep, bhs.rec, "BHS", "BTS")
	}
	if fhs.open {
		reportBatchUnclosed(rep, fhs.rec, "FHS", "FTS")
	}
}

// hl7Header is one FHS, BHS or MSH with its declared separators: the field
// separator is the byte after the tag, and the first field after that holds
// the encoding characters.
type hl7Header struct {
	rec      record
	sep      byte
	encoding string
}

// checkHL7HeaderSeparators validates each header's encoding characters and
// reports headers that disagree with the file's first one. Batch tooling reads
// every message with the separators of the header it saw first, so a message
// that declares different ones is misparsed, not honored.
func checkHL7HeaderSeparators(s *source, rep *Report) {
	var headers []hl7Header
	for _, r := range s.Records {
		switch r.ID {
		case "FHS", "BHS", "MSH":
		default:
			continue
		}
		if len(r.Text) < 4 {
			continue
		}
		sep := r.Text[3]
		rest := r.Text[4:]
		if i := strings.IndexByte(rest, sep); i >= 0 {
			rest = rest[:i]
		}
		headers = append(headers, hl7Header{rec: r, sep: sep, encoding: rest})
	}
	if len(headers) == 0 {
		return
	}

	for _, h := range headers {
		checkHL7Encoding(rep, h)
	}

	first := headers[0]
	for _, h := range headers[1:] {
		if h.sep != first.sep {
			rep.add(Finding{
				Rule:     RuleBatchSeparator,
				Severity: SeverityError,
				Message: fmt.Sprintf("%s declares field separator %q but the file's first header (%s on line %d) "+
					"declares %q; every header in a file must use the same separators",
					h.rec.ID, string(rune(h.sep)), first.rec.ID, first.rec.Line, string(rune(first.sep))),
				Line: h.rec.Line, RecordNumber: h.rec.Ordinal, Record: h.rec.ID,
				Expected: string(rune(first.sep)), Actual: string(rune(h.sep)),
			})
		}
		if h.encoding != first.encoding {
			rep.add(Finding{
				Rule:     RuleBatchSeparator,
				Severity: SeverityError,
				Message: fmt.Sprintf("%s-2 is %q but the file's first header (%s on line %d) declares %q; "+
					"every header in a file must use the same encoding characters",
					h.rec.ID, h.encoding, first.rec.ID, first.rec.Line, first.encoding),
				Line: h.rec.Line, RecordNumber: h.rec.Ordinal, Record: h.rec.ID,
				Expected: first.encoding, Actual: h.encoding,
			})
		}
	}
}

// checkHL7Encoding validates one header's encoding characters: four of them
// (component, repetition, escape, subcomponent), or five when the truncation
// character of v2.7 is declared, all distinct and none equal to the field
// separator.
func checkHL7Encoding(rep *Report, h hl7Header) {
	element := h.rec.ID + "-2"
	bad := func(reason string) {
		rep.add(Finding{
			Rule:     RuleBatchSeparator,
			Severity: SeverityError,
			Message: fmt.Sprintf("%s is %q: %s (expected four or five distinct encoding characters, "+
				"conventionally %q)", element, h.encoding, reason, `^~\&`),
			Line: h.rec.Line, RecordNumber: h.rec.Ordinal, Record: h.rec.ID,
			Expected: `^~\&`, Actual: h.encoding,
		})
	}

	switch len(h.encoding) {
	case 4, 5:
	case 0:
		bad("empty encoding characters")
		return
	default:
		bad(fmt.Sprintf("expected 4 or 5 characters, got %d", len(h.encoding)))
		return
	}

	seen := map[byte]bool{}
	for i := 0; i < len(h.encoding); i++ {
		c := h.encoding[i]
		if c == h.sep {
			bad(fmt.Sprintf("character %d repeats the field separator %q", i+1, string(rune(h.sep))))
			return
		}
		if seen[c] {
			bad(fmt.Sprintf("character %q appears twice", string(rune(c))))
			return
		}
		seen[c] = true
	}
}

// compareBatchCount recounts a declared batch or message total. The HL7v2
// count fields are optional, so an empty value verifies nothing rather than
// failing; that is the difference from the mandatory X12 and EDIFACT counts.
func compareBatchCount(rep *Report, r record, rule, element, declared string, actual int, unit, scope string) {
	d := strings.TrimSpace(declared)
	if d == "" {
		return
	}
	n, err := strconv.Atoi(d)
	if err != nil {
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

func reportBatchUnclosed(rep *Report, r record, header, trailer string) {
	rep.add(Finding{
		Rule:     RuleBatchUnclosed,
		Severity: SeverityError,
		Message:  fmt.Sprintf("%s is never closed by a matching %s", header, trailer),
		Line:     r.Line, RecordNumber: r.Ordinal, Record: r.ID,
		Expected: trailer, Actual: "none",
	})
}

func reportBatchUnopened(rep *Report, r record, trailer, header string) {
	rep.add(Finding{
		Rule:     RuleBatchUnopened,
		Severity: SeverityError,
		Message:  fmt.Sprintf("%s appears without a preceding %s", trailer, header),
		Line:     r.Line, RecordNumber: r.Ordinal, Record: r.ID,
		Expected: header, Actual: "none",
	})
}
