package edilint

import (
	"bytes"
	"fmt"
	"sort"
	"strings"
)

// delims holds the separator characters an X12 interchange declares in its ISA.
type delims struct {
	Element    byte
	SubElement byte
	Segment    byte
	Repetition byte // ISA11 when the interchange version is 00501 or later
	// Declared is false when the ISA was too malformed to derive separators.
	Declared bool
	// ISALen is the observed ISA segment length including the terminator.
	ISALen int
	// Version is ISA12, the interchange control version number.
	Version string
}

// record is one logical unit of an input: a line for line-oriented formats,
// a segment for X12.
type record struct {
	ID      string // record type / segment identifier
	Text    string // content, terminator excluded
	Offset  int    // byte offset of Text[0] within the body
	Line    int    // 1-based physical line the record starts on
	Ordinal int    // 1-based record ordinal
	Term    string // the terminator that followed, "" at end of file
	Pad     string // X12 only: whitespace between Term and the next segment
}

// source is the parsed view of one input file shared by all checks.
type source struct {
	Name    string
	Body    []byte
	Format  Format
	Records []record

	// FieldSep is the byte that separates fields within a record.
	// Zero when the format has no delimiter (fixed-width, plain text).
	FieldSep byte
	// Delims is populated for FormatX12.
	Delims delims
	// ISAOffset is the byte offset of the leading ISA, -1 when absent.
	ISAOffset int
	// Layout is the fixed-width layout in force, if any.
	Layout *Layout
	// Charset is the X12 character-set profile to enforce.
	Charset CharsetProfile

	lineStarts []int
}

// newSource builds the shared parsed view. Structural problems discovered while
// parsing are appended to rep rather than returned as errors, so a malformed
// file still produces a report.
func newSource(name string, body []byte, format Format, opts Options, rep *Report) *source {
	s := &source{
		Name:       name,
		Body:       body,
		Format:     format,
		ISAOffset:  -1,
		Layout:     opts.Layout,
		Charset:    opts.charset(),
		lineStarts: indexLines(body),
	}

	switch format {
	case FormatX12:
		s.buildX12(opts, rep)
	case FormatHL7v2:
		s.buildLines()
		s.FieldSep = hl7FieldSep(s.Records)
		s.assignIDs()
	case FormatDelimited:
		s.buildLines()
		s.FieldSep = resolveDelimiter(body, opts, rep)
		s.assignIDs()
	case FormatFixed, FormatText:
		s.buildLines()
		s.assignIDs()
	}

	return s
}

// buildX12 derives the ISA-declared separators and tokenizes the body into segments.
func (s *source) buildX12(opts Options, rep *Report) {
	isa := bytes.Index(s.Body, []byte("ISA"))
	if isa < 0 || isa > leadingSpace(s.Body) {
		// Format was forced to x12 but there is no leading ISA. Degrade to lines
		// so the character and terminator checks still run.
		rep.add(Finding{
			Rule:     RuleISALength,
			Severity: SeverityError,
			Message:  "no ISA segment found; file cannot be read as an X12 interchange",
			Line:     1,
		})
		s.buildLines()
		s.assignIDs()
		return
	}
	s.ISAOffset = isa

	d, ok := deriveDelims(s.Body, isa)
	if !ok {
		rep.add(Finding{
			Rule:         RuleISALength,
			Severity:     SeverityError,
			Message:      "ISA segment is truncated; unable to derive element, sub-element and segment separators",
			Line:         s.LineAt(isa),
			Record:       "ISA",
			RecordNumber: 1,
		})
		s.buildLines()
		s.assignIDs()
		return
	}
	if opts.Delimiter != "" {
		b, err := ParseDelimiter(opts.Delimiter)
		if err != nil {
			reportBadDelimiter(rep, opts.Delimiter, err, "the element separator the ISA declares")
		} else {
			d.Element = b
		}
	}
	s.Delims = d
	s.FieldSep = d.Element
	s.tokenizeSegments(d.Segment)
	s.assignIDs()
}

// buildLines splits the body on CR, LF or CRLF.
func (s *source) buildLines() {
	body := s.Body
	pos, ordinal := 0, 0
	for pos <= len(body) {
		idx := bytes.IndexAny(body[pos:], "\r\n")
		if idx < 0 {
			if pos < len(body) {
				ordinal++
				s.Records = append(s.Records, record{
					Text:    string(body[pos:]),
					Offset:  pos,
					Line:    s.LineAt(pos),
					Ordinal: ordinal,
				})
			}
			return
		}
		end := pos + idx
		term := string(body[end : end+1])
		next := end + 1
		if body[end] == '\r' && next < len(body) && body[next] == '\n' {
			term = "\r\n"
			next++
		}
		ordinal++
		s.Records = append(s.Records, record{
			Text:    string(body[pos:end]),
			Offset:  pos,
			Line:    s.LineAt(pos),
			Ordinal: ordinal,
			Term:    term,
		})
		pos = next
	}
}

// tokenizeSegments splits an X12 body on the declared segment terminator,
// recording the inter-segment padding that follows each terminator.
func (s *source) tokenizeSegments(term byte) {
	body := s.Body
	pos, ordinal := s.ISAOffset, 0
	for pos < len(body) {
		idx := bytes.IndexByte(body[pos:], term)
		if idx < 0 {
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
		end := pos + idx
		ordinal++
		rec := record{
			Text:    string(body[pos:end]),
			Offset:  pos,
			Line:    s.LineAt(pos),
			Ordinal: ordinal,
			Term:    string(term),
		}
		// Consume inter-segment whitespace so the next segment starts on content.
		p := end + 1
		for p < len(body) && body[p] != term && isPadByte(body[p]) {
			p++
		}
		rec.Pad = string(body[end+1 : p])
		s.Records = append(s.Records, rec)
		pos = p
	}
}

func isPadByte(b byte) bool {
	return b == '\r' || b == '\n' || b == ' ' || b == '\t'
}

// assignIDs fills record.ID according to the source format.
//
// For delimited and fixed-width inputs the leading field is only treated as a
// record type when it actually behaves like one, that is when it repeats across
// records. Otherwise it is data — a member id in a headerless CSV, say — and
// labeling findings with it would be misleading.
func (s *source) assignIDs() {
	ids := make([]string, len(s.Records))
	for i := range s.Records {
		ids[i] = s.recordID(s.Records[i].Text)
	}

	if s.Format == FormatDelimited || s.Format == FormatFixed {
		distinct := map[string]bool{}
		for _, id := range ids {
			distinct[id] = true
		}
		limit := len(s.Records) / 3
		if limit < 3 {
			limit = 3
		}
		if len(distinct) > limit {
			return
		}
	}

	for i := range s.Records {
		s.Records[i].ID = ids[i]
	}
}

func (s *source) recordID(text string) string {
	switch s.Format {
	case FormatHL7v2:
		if len(text) >= 3 {
			return text[:3]
		}
		return text
	case FormatX12, FormatDelimited:
		if s.FieldSep != 0 {
			if i := strings.IndexByte(text, s.FieldSep); i >= 0 {
				return text[:i]
			}
		}
		return text
	case FormatFixed:
		if s.Layout != nil && len(s.Layout.Fields) > 0 {
			w := s.Layout.Fields[0].Width
			if len(text) >= w {
				return strings.TrimSpace(text[:w])
			}
		}
		if len(text) >= 3 {
			return strings.TrimSpace(text[:3])
		}
		return strings.TrimSpace(text)
	default:
		return ""
	}
}

// Fields splits a record into its fields. Field 1 is the record type.
// Fixed-width records are split according to the layout when one is present.
func (s *source) Fields(r record) []string {
	if s.Format == FormatFixed && s.Layout != nil {
		return s.Layout.split(r.Text)
	}
	if s.FieldSep == 0 {
		return []string{r.Text}
	}
	return strings.Split(r.Text, string(s.FieldSep))
}

// LineAt returns the 1-based physical line containing the given byte offset.
func (s *source) LineAt(offset int) int {
	if offset < 0 {
		return 0
	}
	i := sort.SearchInts(s.lineStarts, offset+1)
	if i < 1 {
		return 1
	}
	return i
}

// RecordAt returns the record containing offset, or nil.
func (s *source) RecordAt(offset int) *record {
	i := sort.Search(len(s.Records), func(i int) bool {
		return s.Records[i].Offset > offset
	})
	if i == 0 {
		return nil
	}
	r := &s.Records[i-1]
	if offset >= r.Offset && offset < r.Offset+len(r.Text) {
		return r
	}
	return nil
}

// indexLines returns the byte offset at which each physical line starts.
func indexLines(body []byte) []int {
	starts := []int{0}
	for i := 0; i < len(body); i++ {
		switch body[i] {
		case '\n':
			starts = append(starts, i+1)
		case '\r':
			if i+1 < len(body) && body[i+1] == '\n' {
				continue
			}
			starts = append(starts, i+1)
		}
	}
	return starts
}

// isaScanLimit bounds how far deriveDelims looks for the sixteenth element
// separator. A conformant ISA is 106 bytes; this leaves generous room for a
// malformed one while keeping the scan proportional to a segment, not a file.
const isaScanLimit = 512

// deriveDelims reads the separator characters out of an ISA segment. It walks to
// the sixteenth element separator rather than trusting the fixed 106-character
// width, so that a mis-sized ISA still yields usable separators.
func deriveDelims(body []byte, isa int) (delims, bool) {
	if isa+4 > len(body) {
		return delims{}, false
	}
	d := delims{Element: body[isa+3]}

	// A well-formed ISA is 106 bytes, so the sixteenth separator cannot be far
	// away. Bounding the scan here rather than inside the loop keeps a wrong
	// element-separator guess from walking the whole file.
	end := min(len(body), isa+isaScanLimit)

	seen := 0
	for i := isa + 3; i < end; i++ {
		if body[i] != d.Element {
			continue
		}
		seen++
		if seen == 16 {
			if i+2 >= len(body) {
				return delims{}, false
			}
			d.SubElement = body[i+1]
			d.Segment = body[i+2]
			d.ISALen = i + 2 - isa + 1
			d.Declared = true
			break
		}
	}
	if !d.Declared {
		return delims{}, false
	}

	// ISA11 sits at fixed offset 82 and ISA12 at 84..88 in a well-formed ISA.
	//
	// ISA11 changed meaning between releases: from 005010 it is the repetition
	// separator, but in 004010 it carries the interchange control standards
	// identifier, conventionally the letter "U". Rather than trusting ISA12 —
	// which is itself a field that can be wrong — treat ISA11 as a declared
	// repetition separator only when it is a single non-alphanumeric character.
	// An alphanumeric ISA11 declares no separator, so that character keeps its
	// ordinary meaning everywhere else in the interchange.
	if isa+89 <= len(body) {
		if c := body[isa+82]; !isAlphanumericByte(c) {
			d.Repetition = c
		}
		d.Version = string(body[isa+84 : isa+89])
	}
	return d, true
}

// resolveDelimiter honors an explicit delimiter option, otherwise detects one.
func resolveDelimiter(body []byte, opts Options, rep *Report) byte {
	if opts.Delimiter != "" {
		b, err := ParseDelimiter(opts.Delimiter)
		if err != nil {
			reportBadDelimiter(rep, opts.Delimiter, err, "delimiter detection")
		} else {
			return b
		}
	}
	d := detectDelimiter(body)
	if d == 0 && rep != nil {
		rep.add(Finding{
			Rule:     RuleFieldOutlier,
			Severity: SeverityWarning,
			Message:  "unable to detect a field delimiter; pass --delimiter to enable field checks",
			Line:     1,
		})
	}
	return d
}

// ParseDelimiter converts a user-supplied delimiter string into a byte.
// It accepts a single character or one of the escapes \t, \r, \n, \0 and \xNN.
func ParseDelimiter(s string) (byte, error) {
	switch s {
	case "\\t", "tab":
		return '\t', nil
	case "\\r":
		return '\r', nil
	case "\\n":
		return '\n', nil
	case "\\0":
		return 0x00, nil
	}
	if strings.HasPrefix(s, "\\x") && len(s) == 4 {
		var v int
		if _, err := fmt.Sscanf(s[2:], "%x", &v); err == nil && v >= 0 && v <= 0xff {
			return byte(v), nil
		}
	}
	if len(s) != 1 {
		return 0, fmt.Errorf("delimiter must be a single character or an escape such as \\t, got %q", s)
	}
	return s[0], nil
}

// hl7FieldSep returns the field separator declared in MSH-1, defaulting to '|'.
func hl7FieldSep(records []record) byte {
	for _, r := range records {
		if len(r.Text) >= 4 && (strings.HasPrefix(r.Text, "MSH") ||
			strings.HasPrefix(r.Text, "FHS") || strings.HasPrefix(r.Text, "BHS")) {
			return r.Text[3]
		}
	}
	return '|'
}

// reportBadDelimiter records an unusable Options.Delimiter rather than silently
// analyzing the file under options the caller did not ask for. The CLI rejects
// the value up front, so this is reachable only from the library.
func reportBadDelimiter(rep *Report, value string, err error, fallback string) {
	if rep == nil {
		return
	}
	rep.add(Finding{
		Rule:     RuleFieldOutlier,
		Severity: SeverityError,
		Message: fmt.Sprintf("delimiter option %q is unusable (%v); analysis fell back to %s",
			value, err, fallback),
		Line: 1,
	})
}
