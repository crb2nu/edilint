package edilint

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// StatsSchemaVersion is the version of the `edilint stats --json` document
// shape. It follows the same discipline as SchemaVersion: it is incremented,
// and a new schema file committed, whenever a consumer written against the
// previous version could meet something it has not seen before.
const StatsSchemaVersion = 1

// StatsReport is the top-level result of one `edilint stats` invocation.
type StatsReport struct {
	Version int          `json:"version"`
	Files   []*FileStats `json:"files"`
}

// NewStatsReport returns an empty stats report.
func NewStatsReport() *StatsReport {
	return &StatsReport{Version: StatsSchemaVersion}
}

// Add appends one file's census.
func (sr *StatsReport) Add(fs *FileStats) {
	sr.Files = append(sr.Files, fs)
}

// FileStats is the census of one input file. The envelope, separator and
// character-set sections apply only to X12 input and are absent otherwise.
type FileStats struct {
	File   string `json:"file"`
	Format Format `json:"format"`
	// Bytes is the input size as read, byte order mark included.
	Bytes int `json:"bytes"`
	// Records is the number of records with content: segments for X12 and
	// EDIFACT, non-blank lines otherwise.
	Records int `json:"records"`
	// RecordsByID is the record histogram, keyed by segment identifier or
	// record type. Absent when the format assigns no record types, as in a
	// delimited file whose leading field holds data.
	RecordsByID map[string]int  `json:"records_by_id,omitempty"`
	Separators  *SeparatorStats `json:"separators,omitempty"`
	Charset     *CharsetStats   `json:"charset,omitempty"`
	Envelope    *EnvelopeStats  `json:"envelope,omitempty"`
}

// SeparatorStats records the separators an X12 interchange declared in its ISA.
type SeparatorStats struct {
	Element    string `json:"element"`
	SubElement string `json:"sub_element"`
	Segment    string `json:"segment"`
	// Repetition is absent when ISA11 declares no repetition separator, as in
	// a 004010 interchange.
	Repetition string `json:"repetition,omitempty"`
}

// CharsetStats reports the narrowest X12 character-set profile that admits
// every content character observed. Structural characters — the declared
// separators, carriage returns and line feeds — and control characters are not
// classified; control characters are the linter's business.
type CharsetStats struct {
	// Profile is "basic", "extended", or "beyond-extended" when characters
	// outside even the extended set occur.
	Profile string `json:"profile"`
	// ExtendedOnly counts characters legal in the extended set but not the
	// basic set, lowercase letters being the usual population.
	ExtendedOnly int `json:"extended_only"`
	// BeyondExtended counts characters outside even the extended set,
	// non-ASCII characters included.
	BeyondExtended int `json:"beyond_extended"`
}

// EnvelopeStats is the X12 envelope census.
type EnvelopeStats struct {
	Interchanges int `json:"interchanges"`
	Groups       int `json:"groups"`
	Transactions int `json:"transactions"`
	// GroupsByCode counts functional groups by GS01 functional identifier
	// code, and TransactionsByType counts transaction sets by ST01.
	GroupsByCode       map[string]int `json:"groups_by_code,omitempty"`
	TransactionsByType map[string]int `json:"transactions_by_type,omitempty"`
	// The control-number ranges cover ISA13, GS06 and ST02. Empty values are
	// not sampled, so a range is absent when no envelope declared the value.
	ISA13 *ValueRange `json:"isa13,omitempty"`
	GS06  *ValueRange `json:"gs06,omitempty"`
	ST02  *ValueRange `json:"st02,omitempty"`
	// ISADates ranges over ISA09 (YYMMDD) and GSDates over GS04 (CCYYMMDD).
	// Only well-formed all-digit values of the right width are sampled;
	// malformed dates are the linter's business.
	ISADates *ValueRange `json:"isa_dates,omitempty"`
	GSDates  *ValueRange `json:"gs_dates,omitempty"`
}

// ValueRange is the least and greatest of a sampled set of values. Values that
// parse as integers are ordered numerically, so control number 2 is below 10;
// anything else is ordered lexically.
type ValueRange struct {
	Min string `json:"min"`
	Max string `json:"max"`
}

// Stats builds the census of one input. name is used only for display. The
// returned error is non-nil only when the input is not text at all; a file
// with defects still gets its census.
func Stats(name string, data []byte) (*FileStats, error) {
	body, _ := splitBOM(data)
	if share, binary := looksBinary(body); binary {
		return nil, fmt.Errorf("%s does not look like text: %.0f%% of the sample is invalid UTF-8 or NUL", name, share*100)
	}

	format := Detect(body, Options{})
	scratch := &Report{}
	src := newSource(name, body, format, Options{}, scratch)

	fs := &FileStats{
		File:   name,
		Format: format,
		Bytes:  len(data),
	}
	histogram := map[string]int{}
	for _, r := range src.Records {
		if strings.TrimSpace(r.Text) == "" {
			continue
		}
		fs.Records++
		if r.ID != "" {
			histogram[r.ID]++
		}
	}
	if len(histogram) > 0 {
		fs.RecordsByID = histogram
	}

	if format == FormatX12 && src.Delims.Declared {
		fs.Separators = separatorStats(src.Delims)
		fs.Charset = charsetStats(src)
		fs.Envelope = envelopeStats(parseX12Doc(src))
	}
	return fs, nil
}

func separatorStats(d delims) *SeparatorStats {
	s := &SeparatorStats{
		Element:    string(d.Element),
		SubElement: string(d.SubElement),
		Segment:    string(d.Segment),
	}
	if d.Repetition != 0 {
		s.Repetition = string(d.Repetition)
	}
	return s
}

// charsetStats classifies every content character against the X12 character
// sets, reusing the same membership tests and the same structural-character
// exemptions the charset checks enforce.
func charsetStats(s *source) *CharsetStats {
	structural := structuralCharacters(s)
	cs := &CharsetStats{Profile: string(CharsetBasic)}

	for off := 0; off < len(s.Body); {
		r, size := utf8.DecodeRune(s.Body[off:])
		off += size
		if r == utf8.RuneError && size == 1 {
			cs.BeyondExtended++
			continue
		}
		if r < 0x20 || r == 0x7F || structural[r] {
			continue
		}
		switch {
		case inX12Basic(r):
		case inX12Extended(r):
			cs.ExtendedOnly++
		default:
			cs.BeyondExtended++
		}
	}

	switch {
	case cs.BeyondExtended > 0:
		cs.Profile = "beyond-extended"
	case cs.ExtendedOnly > 0:
		cs.Profile = string(CharsetExtended)
	}
	return cs
}

// envelopeStats walks the envelope tree and counts what it holds.
func envelopeStats(doc *x12Doc) *EnvelopeStats {
	es := &EnvelopeStats{
		GroupsByCode:       map[string]int{},
		TransactionsByType: map[string]int{},
	}
	var isa13, gs06, st02, isaDates, gsDates []string

	for _, i := range doc.Interchanges {
		es.Interchanges++
		isa13 = appendValue(isa13, i.Control)
		if head := headSeg(i.Frame); head.ID == "ISA" {
			isaDates = appendDate(isaDates, elem(head.Elems, 9), 6)
		}
		for _, g := range i.Groups {
			es.Groups++
			if g.Code != "" {
				es.GroupsByCode[g.Code]++
			}
			gs06 = appendValue(gs06, g.Control)
			if head := headSeg(g.Frame); head.ID == "GS" {
				gsDates = appendDate(gsDates, elem(head.Elems, 4), 8)
			}
			for _, t := range g.Txns {
				es.Transactions++
				if t.Type != "" {
					es.TransactionsByType[t.Type]++
				}
				st02 = appendValue(st02, t.Control)
			}
		}
	}

	if len(es.GroupsByCode) == 0 {
		es.GroupsByCode = nil
	}
	if len(es.TransactionsByType) == 0 {
		es.TransactionsByType = nil
	}
	es.ISA13 = rangeOf(isa13)
	es.GS06 = rangeOf(gs06)
	es.ST02 = rangeOf(st02)
	es.ISADates = rangeOf(isaDates)
	es.GSDates = rangeOf(gsDates)
	return es
}

// appendValue samples a control value, skipping empties.
func appendValue(values []string, v string) []string {
	if v == "" {
		return values
	}
	return append(values, v)
}

// appendDate samples a date element only when it has the declared shape.
func appendDate(values []string, v string, width int) []string {
	v = strings.TrimSpace(v)
	if len(v) != width || !allDigits(v) {
		return values
	}
	return append(values, v)
}

// rangeOf reduces sampled values to their least and greatest, nil when nothing
// was sampled.
func rangeOf(values []string) *ValueRange {
	if len(values) == 0 {
		return nil
	}
	r := &ValueRange{Min: values[0], Max: values[0]}
	for _, v := range values[1:] {
		if valueLess(v, r.Min) {
			r.Min = v
		}
		if valueLess(r.Max, v) {
			r.Max = v
		}
	}
	return r
}

// valueLess orders control values numerically when both sides parse as
// integers, lexically otherwise.
func valueLess(a, b string) bool {
	an, aErr := strconv.Atoi(a)
	bn, bErr := strconv.Atoi(b)
	if aErr == nil && bErr == nil {
		if an != bn {
			return an < bn
		}
		return a < b
	}
	return a < b
}

// WriteJSON writes the stats report as a single indented JSON document.
func (sr *StatsReport) WriteJSON(w io.Writer) error {
	if sr.Files == nil {
		sr.Files = []*FileStats{}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(sr)
}

// WriteText writes one section per file.
func (sr *StatsReport) WriteText(w io.Writer) error {
	for i, fs := range sr.Files {
		if i > 0 {
			if _, err := fmt.Fprintln(w); err != nil {
				return err
			}
		}
		if err := fs.writeText(w); err != nil {
			return err
		}
	}
	return nil
}

func (fs *FileStats) writeText(w io.Writer) error {
	unit := "record"
	if fs.Format == FormatX12 || fs.Format == FormatEdifact {
		unit = "segment"
	}
	if _, err := fmt.Fprintf(w, "%s: %s, %s, %s\n",
		fs.File, fs.Format, plural(fs.Bytes, "byte"), plural(fs.Records, unit)); err != nil {
		return err
	}

	if s := fs.Separators; s != nil {
		line := fmt.Sprintf("  separators: element %q, sub-element %q, segment %q",
			s.Element, s.SubElement, s.Segment)
		if s.Repetition != "" {
			line += fmt.Sprintf(", repetition %q", s.Repetition)
		}
		if _, err := fmt.Fprintln(w, line); err != nil {
			return err
		}
	}
	if c := fs.Charset; c != nil {
		if _, err := fmt.Fprintf(w, "  charset: %s (%d extended-only, %d beyond extended)\n",
			c.Profile, c.ExtendedOnly, c.BeyondExtended); err != nil {
			return err
		}
	}
	if e := fs.Envelope; e != nil {
		if err := e.writeText(w); err != nil {
			return err
		}
	}
	if len(fs.RecordsByID) > 0 {
		if _, err := fmt.Fprintf(w, "  %ss by ID: %s\n", unit, histogramString(fs.RecordsByID)); err != nil {
			return err
		}
	}
	return nil
}

func (e *EnvelopeStats) writeText(w io.Writer) error {
	lines := []string{
		"  interchanges: " + strconv.Itoa(e.Interchanges) +
			rangeString(", ISA13 ", e.ISA13) + rangeString(", ISA09 dates ", e.ISADates),
		"  groups: " + strconv.Itoa(e.Groups) + countsString(e.GroupsByCode) +
			rangeString(", GS06 ", e.GS06) + rangeString(", GS04 dates ", e.GSDates),
		"  transactions: " + strconv.Itoa(e.Transactions) + countsString(e.TransactionsByType) +
			rangeString(", ST02 ", e.ST02),
	}
	for _, line := range lines {
		if _, err := fmt.Fprintln(w, line); err != nil {
			return err
		}
	}
	return nil
}

// rangeString renders a value range as one value, or "min..max".
func rangeString(label string, r *ValueRange) string {
	if r == nil {
		return ""
	}
	if r.Min == r.Max {
		return label + r.Min
	}
	return label + r.Min + ".." + r.Max
}

// countsString renders a by-key count map as " (835: 2, 837: 1)", keys sorted.
func countsString(counts map[string]int) string {
	if len(counts) == 0 {
		return ""
	}
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s: %d", k, counts[k]))
	}
	return " (" + strings.Join(parts, ", ") + ")"
}

// histogramString renders the record histogram ordered by count, most common
// first, ties broken by identifier.
func histogramString(counts map[string]int) string {
	type entry struct {
		id string
		n  int
	}
	entries := make([]entry, 0, len(counts))
	for id, n := range counts {
		entries = append(entries, entry{id, n})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].n != entries[j].n {
			return entries[i].n > entries[j].n
		}
		return entries[i].id < entries[j].id
	})
	parts := make([]string, 0, len(entries))
	for _, e := range entries {
		parts = append(parts, fmt.Sprintf("%s %d", e.id, e.n))
	}
	return strings.Join(parts, ", ")
}
