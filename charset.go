package edilint

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"
)

// CharsetProfile selects which X12 character set is enforced for X12 content.
type CharsetProfile string

const (
	// CharsetBasic enforces the X12 basic character set: A-Z, 0-9, space and
	// ! " & ' ( ) * + , - . / : ; ? =
	CharsetBasic CharsetProfile = "basic"
	// CharsetExtended additionally allows a-z and % ~ @ [ ] _ { } \ | < > # $
	CharsetExtended CharsetProfile = "extended"
	// CharsetOff disables the X12 character-set rules entirely.
	CharsetOff CharsetProfile = "off"
)

// ParseCharsetProfile converts a user-supplied --charset value.
func ParseCharsetProfile(s string) (CharsetProfile, error) {
	switch CharsetProfile(s) {
	case CharsetBasic, CharsetExtended, CharsetOff:
		return CharsetProfile(s), nil
	case "":
		return CharsetBasic, nil
	default:
		return "", fmt.Errorf("unknown charset %q (want basic, extended or off)", s)
	}
}

// x12BasicSpecials are the non-alphanumeric characters in the X12 basic set.
const x12BasicSpecials = ` !"&'()*+,-./:;?=`

// x12ExtendedSpecials are the characters the extended set adds on top of the
// basic set, alongside the lowercase letters.
const x12ExtendedSpecials = "%~@[]_{}\\|<>#$"

// inX12Basic reports whether r belongs to the X12 basic character set.
func inX12Basic(r rune) bool {
	switch {
	case r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		return true
	}
	return strings.ContainsRune(x12BasicSpecials, r)
}

// inX12Extended reports whether r belongs to the X12 extended character set,
// which is a superset of the basic set.
func inX12Extended(r rune) bool {
	if inX12Basic(r) {
		return true
	}
	if r >= 'a' && r <= 'z' {
		return true
	}
	return strings.ContainsRune(x12ExtendedSpecials, r)
}

// checkCharset walks every rune in the body looking for content that will not
// survive a downstream parser: invalid UTF-8, stray control characters,
// invisible formatting marks, ASCII lookalikes, and characters outside the
// declared X12 character set.
func checkCharset(s *source, rep *Report) {
	body := s.Body
	allowed := allowedControls(s)
	structural := structuralCharacters(s)
	agg := newCharAggregator()

	line, col := 1, 1
	for off := 0; off < len(body); {
		r, size := utf8.DecodeRune(body[off:])

		if r == utf8.RuneError && size == 1 {
			rep.add(s.locate(off, Finding{
				Rule:     RuleInvalidUTF8,
				Severity: SeverityError,
				Message: fmt.Sprintf("invalid UTF-8 byte 0x%02X; the file is not valid UTF-8 and byte-oriented "+
					"parsers will disagree with text-oriented ones about field boundaries", body[off]),
				Line:      line,
				Column:    col,
				CodePoint: fmt.Sprintf("0x%02X", body[off]),
			}))
			off, col = off+1, col+1
			continue
		}

		switch {
		case r < 0x20 || r == 0x7F:
			checkControl(s, rep, allowed, r, line, col, off)

		case r < 0x80:
			if s.Format == FormatX12 && !structural[r] {
				checkX12Charset(s, rep, agg, r, line, col, off)
			}

		default:
			checkNonASCII(s, rep, agg, r, line, col, off)
		}

		// Advance the line/column cursor. A CR that begins a CRLF pair does not
		// open a new line by itself; the LF that follows does.
		switch r {
		case '\n':
			line, col = line+1, 1
		case '\r':
			if off+1 < len(body) && body[off+1] == '\n' {
				col++
			} else {
				line, col = line+1, 1
			}
		default:
			col++
		}
		off += size
	}

	agg.emit(rep)
}

// charBucket accumulates one rule's hits within one record so that a file whose
// every name field is mixed case produces one finding per record rather than one
// per character.
type charBucket struct {
	order    int
	rule     string
	severity Severity
	line     int
	column   int
	record   int
	segment  string
	chars    []rune
	seen     map[rune]bool
	count    int
}

// charAggregator collects the high-volume character rules.
type charAggregator struct {
	buckets map[string]*charBucket
	next    int
}

func newCharAggregator() *charAggregator {
	return &charAggregator{buckets: map[string]*charBucket{}}
}

// add records one hit. Only the high-volume rules (charset.x12-basic and
// charset.nonascii) are routed here; the sharp, rare rules such as
// charset.homoglyph stay per-occurrence so the exact position is available.
func (a *charAggregator) add(s *source, rule string, sev Severity, r rune, line, col, off int) {
	f := s.locate(off, Finding{Line: line, Column: col})
	key := fmt.Sprintf("%s|%d|%d", rule, f.Record, line)
	b, ok := a.buckets[key]
	if !ok {
		b = &charBucket{
			order: a.next, rule: rule, severity: sev,
			line: line, column: col, record: f.Record, segment: f.Segment,
			seen: map[rune]bool{},
		}
		a.next++
		a.buckets[key] = b
	}
	b.count++
	if !b.seen[r] {
		b.seen[r] = true
		b.chars = append(b.chars, r)
	}
}

// maxListedChars caps how many distinct characters an aggregated message names.
const maxListedChars = 8

func (a *charAggregator) emit(rep *Report) {
	buckets := make([]*charBucket, 0, len(a.buckets))
	for _, b := range a.buckets {
		buckets = append(buckets, b)
	}
	sort.Slice(buckets, func(i, j int) bool { return buckets[i].order < buckets[j].order })

	for _, b := range buckets {
		var msg string
		switch b.rule {
		case RuleX12Basic:
			msg = fmt.Sprintf("record contains %d character(s) outside the X12 basic character set "+
				"but within the extended set (%s); pass --charset extended if the partner accepts them",
				b.count, listChars(b.chars))
		default:
			msg = fmt.Sprintf("record contains %d non-ASCII character(s) (%s) in content expected to be ASCII",
				b.count, listChars(b.chars))
		}
		rep.add(Finding{
			Rule:      b.rule,
			Severity:  b.severity,
			Message:   msg,
			Line:      b.line,
			Column:    b.column,
			Record:    b.record,
			Segment:   b.segment,
			CodePoint: codePoint(b.chars[0]),
			Actual:    string(b.chars),
		})
	}
}

// listChars renders the distinct offending characters, truncating long lists.
func listChars(chars []rune) string {
	shown := chars
	suffix := ""
	if len(shown) > maxListedChars {
		shown = shown[:maxListedChars]
		suffix = fmt.Sprintf(", and %d more", len(chars)-maxListedChars)
	}
	parts := make([]string, 0, len(shown))
	for _, r := range shown {
		parts = append(parts, fmt.Sprintf("%q", string(r)))
	}
	return strings.Join(parts, ", ") + suffix
}

func checkControl(s *source, rep *Report, allowed map[rune]bool, r rune, line, col, off int) {
	if allowed[r] {
		return
	}
	if r == '\t' {
		rep.add(s.locate(off, Finding{
			Rule:     RuleNonPrint,
			Severity: SeverityWarning,
			Message: "tab character in a file that is not tab-delimited; it is invisible in review " +
				"and is silently normalised by some transports",
			Line:      line,
			Column:    col,
			CodePoint: "U+0009",
		}))
		return
	}
	rep.add(s.locate(off, Finding{
		Rule:     RuleNonPrint,
		Severity: SeverityError,
		Message: fmt.Sprintf("non-printable control character %s in record content; "+
			"it is invisible in review and breaks field boundaries downstream", controlName(r)),
		Line:      line,
		Column:    col,
		CodePoint: codePoint(r),
	}))
}

func checkX12Charset(s *source, rep *Report, agg *charAggregator, r rune, line, col, off int) {
	switch s.Charset {
	case CharsetOff:
		return
	case CharsetExtended:
		if inX12Extended(r) {
			return
		}
	default: // CharsetBasic
		if inX12Basic(r) {
			return
		}
		if inX12Extended(r) {
			// Lowercase text is the overwhelmingly common case here, so these are
			// collapsed to one finding per record.
			agg.add(s, RuleX12Basic, SeverityWarning, r, line, col, off)
			return
		}
	}
	// Reaching here means the character is outside the widest set in play.
	sets := "basic and extended character sets"
	if s.Charset == CharsetExtended {
		sets = "extended character set"
	}
	rep.add(s.locate(off, Finding{
		Rule:     RuleX12Extended,
		Severity: SeverityError,
		Message: fmt.Sprintf("character %q is outside the X12 %s; "+
			"trading partners may reject the interchange", string(r), sets),
		Line:      line,
		Column:    col,
		CodePoint: codePoint(r),
		Expected:  "X12 " + sets,
		Actual:    string(r),
	}))
}

func checkNonASCII(s *source, rep *Report, agg *charAggregator, r rune, line, col, off int) {
	if name, ok := isInvisible(r); ok {
		rep.add(s.locate(off, Finding{
			Rule:     RuleZeroWidth,
			Severity: SeverityError,
			Message: fmt.Sprintf("invisible character %s (%s); it renders as nothing but occupies "+
				"bytes and shifts fixed-width offsets", name, codePoint(r)),
			Line:      line,
			Column:    col,
			CodePoint: codePoint(r),
		}))
		return
	}
	if ascii, ok := confusableASCII(r); ok {
		rep.add(s.locate(off, Finding{
			Rule:     RuleHomoglyph,
			Severity: SeverityError,
			Message: fmt.Sprintf("%s looks like ASCII %q but is not; ASCII-expecting parsers will "+
				"not match this value", codePoint(r), string(ascii)),
			Line:      line,
			Column:    col,
			CodePoint: codePoint(r),
			Expected:  string(ascii),
			Actual:    string(r),
		}))
		return
	}
	agg.add(s, RuleNonASCII, SeverityWarning, r, line, col, off)
}

// locate fills in the record ordinal and segment identifier for a byte offset.
func (s *source) locate(off int, f Finding) Finding {
	if r := s.RecordAt(off); r != nil {
		f.Record = r.Ordinal
		f.Segment = r.ID
	}
	return f
}

// allowedControls returns the control characters that are legitimate for this
// format. The sets are keyed by rune so that callers can consult them with a
// decoded rune and never need a narrowing conversion.
func allowedControls(s *source) map[rune]bool {
	a := map[rune]bool{'\r': true, '\n': true}
	if s.FieldSep != 0 {
		a[rune(s.FieldSep)] = true
	}
	addDelims(a, s)
	return a
}

// structuralCharacters returns the characters that carry structure rather than
// content, so the X12 character-set rules do not flag a partner's chosen
// delimiters.
func structuralCharacters(s *source) map[rune]bool {
	b := map[rune]bool{'\r': true, '\n': true}
	addDelims(b, s)
	return b
}

// addDelims records the separators an X12 interchange declared.
func addDelims(set map[rune]bool, s *source) {
	if s.Format != FormatX12 || !s.Delims.Declared {
		return
	}
	set[rune(s.Delims.Element)] = true
	set[rune(s.Delims.SubElement)] = true
	set[rune(s.Delims.Segment)] = true
	if s.Delims.Repetition != 0 {
		set[rune(s.Delims.Repetition)] = true
	}
}

func codePoint(r rune) string {
	return fmt.Sprintf("U+%04X", r)
}

// controlName renders a control character as its conventional abbreviation.
func controlName(r rune) string {
	names := map[rune]string{
		0x00: "NUL", 0x01: "SOH", 0x02: "STX", 0x03: "ETX", 0x04: "EOT",
		0x05: "ENQ", 0x06: "ACK", 0x07: "BEL", 0x08: "BS", 0x09: "TAB",
		0x0A: "LF", 0x0B: "VT", 0x0C: "FF", 0x0D: "CR", 0x0E: "SO",
		0x0F: "SI", 0x10: "DLE", 0x11: "DC1", 0x12: "DC2", 0x13: "DC3",
		0x14: "DC4", 0x15: "NAK", 0x16: "SYN", 0x17: "ETB", 0x18: "CAN",
		0x19: "EM", 0x1A: "SUB", 0x1B: "ESC", 0x1C: "FS", 0x1D: "GS",
		0x1E: "RS", 0x1F: "US", 0x7F: "DEL",
	}
	if n, ok := names[r]; ok {
		return fmt.Sprintf("%s (%s)", n, codePoint(r))
	}
	return codePoint(r)
}
