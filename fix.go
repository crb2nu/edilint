package edilint

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Mechanical repairs, as `edilint fix` applies them.
//
// Every repair is tied to the one rule it clears, and each is the smallest
// byte edit that clears it: bytes the repair does not name are never touched.
// The safe tier repairs defects whose correct form is derivable from the file
// itself — a terminator style the file already prefers, a count the records
// can be recounted into, a time one leading zero short of valid. The unsafe
// tier substitutes ASCII for its Unicode lookalikes, which changes content
// bytes on a visual judgment, so it runs only when asked for and its changes
// are always shown as a diff.

// FixOptions configures a Fix run.
type FixOptions struct {
	// Format forces an input format. The zero value (FormatAuto) detects it.
	Format Format

	// Unsafe enables the unsafe repair tier: homoglyph-to-ASCII substitution
	// (rule EL1005). The safe tier always runs.
	Unsafe bool
}

// Repair describes one edit Fix made, or would make, tied to the rule whose
// findings the edit clears.
type Repair struct {
	// ID is the stable identifier of the rule the repair clears, e.g. "EL3006".
	ID string
	// Rule is the rule's dotted name, e.g. "envelope.segment-count".
	Rule string
	// Line is the 1-based physical line the edit starts on.
	Line int
	// Message says what the edit does, e.g. `rewrite SE01 from "9" to "3"`.
	Message string
	// Unsafe is true for repairs in the unsafe tier.
	Unsafe bool
}

// edit is one byte-range replacement within the post-BOM body.
type edit struct {
	start, end int // half-open byte range; start == end inserts
	text       string
	repair     Repair
}

// Fix computes the repaired form of data and the repairs that produce it. When
// nothing applies it returns data unchanged and no repairs.
//
// Fix never repairs what it cannot read: input that is not text, and input
// behind a UTF-16 byte order mark, come back untouched. Applying Fix to its
// own output yields no further repairs.
func Fix(data []byte, opts FixOptions) ([]byte, []Repair) {
	body, bom := splitBOM(data)
	if bom == "UTF-16LE" || bom == "UTF-16BE" {
		// The bytes behind a UTF-16 byte order mark are UTF-16, and stripping
		// the mark alone would only disguise that. Transcoding is not a repair.
		return data, nil
	}
	if _, binary := looksBinary(body); binary {
		return data, nil
	}

	format := opts.Format
	if format == "" || format == FormatAuto {
		format = Detect(body, Options{})
	}
	s := newSource("", body, format, Options{}, &Report{})

	var edits []edit
	edits = append(edits, terminatorFixes(s)...)
	if s.Format == FormatX12 {
		edits = append(edits, x12EnvelopeFixes(s)...)
	}
	if s.Format == FormatHL7v2 {
		edits = append(edits, hl7BatchFixes(s)...)
	}
	if opts.Unsafe {
		edits = append(edits, homoglyphFixes(s)...)
	}
	edits = acceptEdits(edits)

	stripBOM := bom == "UTF-8"
	if len(edits) == 0 && !stripBOM {
		return data, nil
	}

	repairs := make([]Repair, 0, len(edits)+1)
	if stripBOM {
		repairs = append(repairs, Repair{
			ID:      RuleID(RuleBOM),
			Rule:    RuleBOM,
			Line:    1,
			Message: "strip the UTF-8 byte order mark",
		})
	}
	out := make([]byte, 0, len(body))
	if !stripBOM {
		out = append(out, data[:len(data)-len(body)]...)
	}
	pos := 0
	for _, e := range edits {
		out = append(out, body[pos:e.start]...)
		out = append(out, e.text...)
		pos = e.end
		repairs = append(repairs, e.repair)
	}
	out = append(out, body[pos:]...)
	return out, repairs
}

// acceptEdits orders edits by position and drops any that overlap an already
// accepted one. Overlaps only arise between tiers — an unsafe substitution
// inside an element a safe repair rewrites whole — and the earlier, safe edit
// wins because it subsumes the other.
func acceptEdits(edits []edit) []edit {
	sort.SliceStable(edits, func(i, j int) bool {
		if edits[i].start != edits[j].start {
			return edits[i].start < edits[j].start
		}
		if edits[i].repair.Unsafe != edits[j].repair.Unsafe {
			return !edits[i].repair.Unsafe
		}
		return edits[i].end < edits[j].end
	})
	out := edits[:0:0]
	end := -1
	for _, e := range edits {
		if e.start < end {
			continue
		}
		out = append(out, e)
		end = e.end
	}
	return out
}

// terminatorFixes repairs record terminators, branching exactly as the
// terminator checks do: X12 with declared separators gets the segment fixes,
// EDIFACT gets nothing (its repairs are out of scope), and every other input
// gets the line-ending fixes.
func terminatorFixes(s *source) []edit {
	if s.Format == FormatX12 && s.Delims.Declared {
		return append(x12TerminatorFixes(s), x12PaddingFixes(s)...)
	}
	if s.Format == FormatEdifact && s.Edifact.Declared {
		return nil
	}
	return lineEndingFixes(s)
}

// lineEndingFixes rewrites minority line terminators to the dominant style
// (EL2001) and appends the missing final terminator (EL2002).
func lineEndingFixes(s *source) []edit {
	if len(s.Records) == 0 {
		return nil
	}

	counts := map[string]int{}
	for _, r := range s.Records {
		if r.Term != "" {
			counts[r.Term]++
		}
	}

	var edits []edit
	if len(counts) > 1 {
		dominant := modalKey(counts)
		for _, r := range s.Records {
			if r.Term == "" || r.Term == dominant {
				continue
			}
			start := r.Offset + len(r.Text)
			edits = append(edits, edit{
				start: start,
				end:   start + len(r.Term),
				text:  dominant,
				repair: Repair{
					ID:   RuleID(RuleMixedTerminator),
					Rule: RuleMixedTerminator,
					Line: r.Line,
					Message: fmt.Sprintf("rewrite the %s line terminator to the file's dominant %s",
						renderWS(r.Term), renderWS(dominant)),
				},
			})
		}
	}

	last := s.Records[len(s.Records)-1]
	if last.Term == "" && strings.TrimSpace(last.Text) != "" {
		term := "\n"
		if len(counts) > 0 {
			term = modalKey(counts)
		}
		at := last.Offset + len(last.Text)
		edits = append(edits, edit{
			start: at,
			end:   at,
			text:  term,
			repair: Repair{
				ID:      RuleID(RuleMissingFinal),
				Rule:    RuleMissingFinal,
				Line:    last.Line,
				Message: fmt.Sprintf("append the missing %s terminator to the last record", renderWS(term)),
			},
		})
	}
	return edits
}

// x12TerminatorFixes closes a trailing segment the declared terminator does
// not close (EL2003), which in practice is a final segment the transport cut
// the terminator from. The segment's content is not touched, so a segment cut
// mid-element stays visibly wrong.
func x12TerminatorFixes(s *source) []edit {
	var edits []edit
	for _, r := range s.Records {
		if r.Term != "" || strings.TrimSpace(r.Text) == "" {
			continue
		}
		at := r.Offset + len(r.Text)
		edits = append(edits, edit{
			start: at,
			end:   at,
			text:  string(s.Delims.Segment),
			repair: Repair{
				ID:   RuleID(RuleX12Segment),
				Rule: RuleX12Segment,
				Line: r.Line,
				Message: fmt.Sprintf("append the declared segment terminator %s",
					renderWS(string(s.Delims.Segment))),
			},
		})
	}
	return edits
}

// x12PaddingFixes rewrites minority inter-segment whitespace to the dominant
// style (EL2004), over the same records the padding check judges: everything
// but the final segment, whose trailing whitespace is not a separator.
func x12PaddingFixes(s *source) []edit {
	if len(s.Records) < 3 {
		return nil
	}
	body := s.Records[:len(s.Records)-1]

	counts := map[string]int{}
	for _, r := range body {
		counts[r.Pad]++
	}
	if len(counts) < 2 {
		return nil
	}
	dominant := modalKey(counts)

	var edits []edit
	for _, r := range body {
		if r.Pad == dominant {
			continue
		}
		start := r.Offset + len(r.Text) + len(r.Term)
		edits = append(edits, edit{
			start: start,
			end:   start + len(r.Pad),
			text:  dominant,
			repair: Repair{
				ID:   RuleID(RuleX12Padding),
				Rule: RuleX12Padding,
				Line: r.Line,
				Message: fmt.Sprintf("rewrite the %s after the segment terminator to the file's dominant %s",
					renderWS(r.Pad), renderWS(dominant)),
			},
		})
	}
	return edits
}

// x12EnvelopeFixes walks the interchange the way the envelope check does and
// repairs what a recount derives: SE01, GE01 and IEA01 rewritten to the
// counted totals (EL3006, EL3007, EL3008), and ISA10/GS05 times zero-padded
// when one leading zero restores a valid value (EL3010).
func x12EnvelopeFixes(s *source) []edit {
	if !s.Delims.Declared {
		return nil
	}

	var edits []edit
	var isa, gs, st envelopeLevel

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
			if gs.open {
				gs = envelopeLevel{}
			}
			isa = envelopeLevel{open: true}
			edits = append(edits, timePadFix(s, r, "ISA10", 10)...)

		case "IEA":
			if !isa.open {
				continue
			}
			gs = envelopeLevel{}
			edits = append(edits, recountFix(s, r, RuleInterchangeCount, "IEA01", 1,
				elem(f, 1), isa.children, true)...)
			isa.open = false

		case "GS":
			isa.children++
			gs = envelopeLevel{open: true}
			edits = append(edits, timePadFix(s, r, "GS05", 5)...)

		case "GE":
			if !gs.open {
				continue
			}
			if st.open {
				st = envelopeLevel{}
			}
			edits = append(edits, recountFix(s, r, RuleGroupCount, "GE01", 1,
				elem(f, 1), gs.children, true)...)
			gs.open = false

		case "ST":
			gs.children++
			st = envelopeLevel{open: true, children: 1}

		case "SE":
			if !st.open {
				continue
			}
			edits = append(edits, recountFix(s, r, RuleSegmentCount, "SE01", 1,
				elem(f, 1), st.children, true)...)
			st.open = false
		}
	}
	return edits
}

// hl7BatchFixes recounts the optional batch totals the way the batch check
// does: BTS-1 rewritten to the counted MSH messages in its batch (EL6003) and
// FTS-1 to the counted BHS batches in its file (EL6004). An empty count field
// declares nothing and is left empty.
func hl7BatchFixes(s *source) []edit {
	var edits []edit
	var fhs, bhs envelopeLevel
	batches := 0

	for i := range s.Records {
		r := s.Records[i]
		if strings.TrimSpace(r.Text) == "" {
			continue
		}
		f := s.Fields(r)

		switch r.ID {
		case "FHS":
			fhs = envelopeLevel{open: true}
			batches = 0

		case "FTS":
			if !fhs.open {
				continue
			}
			bhs = envelopeLevel{}
			edits = append(edits, recountFix(s, r, RuleBatchFileCount, "FTS-1", 1,
				elem(f, 1), batches, false)...)
			fhs.open = false

		case "BHS":
			batches++
			bhs = envelopeLevel{open: true}

		case "BTS":
			if !bhs.open {
				continue
			}
			edits = append(edits, recountFix(s, r, RuleBatchMessageCount, "BTS-1", 1,
				elem(f, 1), bhs.children, false)...)
			bhs.open = false

		case "MSH":
			if bhs.open {
				bhs.children++
			}
		}
	}
	return edits
}

// recountFix rewrites a declared trailer count to the recounted total. An
// unparsable declaration is rewritten like a wrong one. fixEmpty says what an
// empty declaration means: the X12 counts are mandatory, so an empty one is a
// defect and is filled in; the HL7v2 counts are optional, so an empty one
// declares nothing and stays empty.
func recountFix(s *source, r record, rule, element string, field int, declared string, actual int, fixEmpty bool) []edit {
	d := strings.TrimSpace(declared)
	if d == "" && !fixEmpty {
		return nil
	}
	if n, err := strconv.Atoi(d); err == nil && n == actual {
		return nil
	}
	start, end, ok := fieldRange(s, r, field)
	if !ok {
		return nil
	}
	return []edit{{
		start: start,
		end:   end,
		text:  strconv.Itoa(actual),
		repair: Repair{
			ID:   RuleID(rule),
			Rule: rule,
			Line: r.Line,
			Message: fmt.Sprintf("rewrite %s from %q to %q, the recounted total",
				element, declared, strconv.Itoa(actual)),
		},
	}}
}

// timePadFix zero-pads an envelope time that lost one leading zero (EL3010):
// an all-digit value one digit short of HHMM, HHMMSS or HHMMSSDD is padded to
// that width, and only when the padded value is a valid time. Anything else —
// an empty value, a non-digit value, an out-of-range value, a value more than
// one digit short — is a defect the file cannot answer for itself, and is
// left for a person.
func timePadFix(s *source, r record, element string, field int) []edit {
	start, end, ok := fieldRange(s, r, field)
	if !ok {
		return nil
	}
	raw := s.Body[start:end]
	v := strings.TrimSpace(string(raw))
	switch len(v) {
	case 3, 5, 7:
	default:
		return nil
	}
	if !allDigits(v) {
		return nil
	}
	padded := "0" + v
	if !validX12Time(padded) {
		return nil
	}
	return []edit{{
		start: start,
		end:   end,
		text:  padded,
		repair: Repair{
			ID:      RuleID(RuleDateTime),
			Rule:    RuleDateTime,
			Line:    r.Line,
			Message: fmt.Sprintf("zero-pad %s from %q to %q", element, string(raw), padded),
		},
	}}
}

// validX12Time reports whether an all-digit value passes the same range checks
// the envelope datetime rule applies to HHMM, HHMMSS and HHMMSSDD times.
func validX12Time(v string) bool {
	switch len(v) {
	case 4, 6, 8:
	default:
		return false
	}
	hour, _ := strconv.Atoi(v[0:2])
	minute, _ := strconv.Atoi(v[2:4])
	if hour > 23 || minute > 59 {
		return false
	}
	if len(v) >= 6 {
		if sec, _ := strconv.Atoi(v[4:6]); sec > 59 {
			return false
		}
	}
	return true
}

// fieldRange locates the byte range of a record's nth field within the source
// body, where field 0 is the record type. X12 records split on the element
// separator, HL7v2 records on the field separator declared in the first
// header.
func fieldRange(s *source, r record, n int) (start, end int, ok bool) {
	sep := s.FieldSep
	if sep == 0 {
		return 0, 0, false
	}
	from := 0
	for seen := 0; seen < n; seen++ {
		i := strings.IndexByte(r.Text[from:], sep)
		if i < 0 {
			return 0, 0, false
		}
		from += i + 1
	}
	to := len(r.Text)
	if i := strings.IndexByte(r.Text[from:], sep); i >= 0 {
		to = from + i
	}
	return r.Offset + from, r.Offset + to, true
}

// homoglyphFixes substitutes ASCII for the Unicode characters that imitate it
// (EL1005). A lookalike whose ASCII form is one of the file's structural
// characters — a declared X12 separator, the HL7v2 field separator or an
// encoding character — is left alone, because substituting it would change
// the record structure rather than the content.
func homoglyphFixes(s *source) []edit {
	structural := fixStructural(s)

	var edits []edit
	for off := 0; off < len(s.Body); {
		r, size := utf8.DecodeRune(s.Body[off:])
		if r == utf8.RuneError && size == 1 {
			off++
			continue
		}
		if ascii, ok := confusableASCII(r); ok && !structural[ascii] {
			edits = append(edits, edit{
				start: off,
				end:   off + size,
				text:  string(ascii),
				repair: Repair{
					ID:   RuleID(RuleHomoglyph),
					Rule: RuleHomoglyph,
					Line: s.LineAt(off),
					Message: fmt.Sprintf("replace %s with ASCII %q",
						codePoint(r), string(ascii)),
					Unsafe: true,
				},
			})
		}
		off += size
	}
	return edits
}

// fixStructural returns the characters that carry record structure in this
// input, which a homoglyph substitution must never produce.
func fixStructural(s *source) map[rune]bool {
	set := structuralCharacters(s)
	if s.FieldSep != 0 {
		set[rune(s.FieldSep)] = true
	}
	if s.Format == FormatHL7v2 {
		for _, r := range s.Records {
			switch r.ID {
			case "FHS", "BHS", "MSH":
			default:
				continue
			}
			if len(r.Text) < 4 {
				continue
			}
			rest := r.Text[4:]
			if i := strings.IndexByte(rest, r.Text[3]); i >= 0 {
				rest = rest[:i]
			}
			for _, enc := range rest {
				set[enc] = true
			}
			break
		}
	}
	return set
}
