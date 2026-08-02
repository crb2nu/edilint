package edilint

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Pad sides for LayoutField.Pad.
const (
	// PadRight means the value is left-aligned and padding is appended.
	PadRight = "right"
	// PadLeft means the value is right-aligned and padding is prepended.
	PadLeft = "left"
)

// LayoutField is one field in a fixed-width record layout.
type LayoutField struct {
	Name  string `json:"name"`
	Width int    `json:"width"`
	// Pad is "left" or "right", naming the side the padding is added to.
	// Empty disables the padding check for this field.
	Pad string `json:"pad,omitempty"`
	// PadChar is the single padding character; it defaults to a space.
	PadChar string `json:"padChar,omitempty"`
}

// padByte returns the field's padding character, defaulting to a space.
func (f LayoutField) padByte() byte {
	if f.PadChar == "" {
		return ' '
	}
	return f.PadChar[0]
}

// Layout describes the ordered fields of a fixed-width record.
//
// The JSON form is:
//
//	{
//	  "name": "remittance-detail",
//	  "fields": [
//	    {"name": "record_type", "width": 3},
//	    {"name": "member_id",   "width": 12, "pad": "right"},
//	    {"name": "paid_amount", "width": 10, "pad": "left", "padChar": "0"}
//	  ]
//	}
type Layout struct {
	Name   string        `json:"name,omitempty"`
	Fields []LayoutField `json:"fields"`
}

// LoadLayout reads and validates a layout JSON file.
func LoadLayout(path string) (*Layout, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read layout %s: %w", path, err)
	}
	var l Layout
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&l); err != nil {
		return nil, fmt.Errorf("parse layout %s: %w", path, err)
	}
	if err := l.Validate(); err != nil {
		return nil, fmt.Errorf("layout %s: %w", path, err)
	}
	return &l, nil
}

// Validate checks that the layout is usable.
func (l *Layout) Validate() error {
	if len(l.Fields) == 0 {
		return fmt.Errorf("layout has no fields")
	}
	for i, f := range l.Fields {
		if f.Name == "" {
			return fmt.Errorf("field %d has no name", i+1)
		}
		if f.Width < 1 {
			return fmt.Errorf("field %q has width %d; widths must be at least 1", f.Name, f.Width)
		}
		switch f.Pad {
		case "", PadLeft, PadRight:
		default:
			return fmt.Errorf("field %q has pad %q; want %q or %q", f.Name, f.Pad, PadLeft, PadRight)
		}
		if len(f.PadChar) > 1 {
			return fmt.Errorf("field %q has padChar %q; it must be a single ASCII character, since "+
				"fixed-width field widths are byte offsets", f.Name, f.PadChar)
		}
	}
	return nil
}

// RecordLength is the sum of all field widths.
func (l *Layout) RecordLength() int {
	n := 0
	for _, f := range l.Fields {
		n += f.Width
	}
	return n
}

// split carves a record into field values, tolerating short records.
func (l *Layout) split(text string) []string {
	out := make([]string, 0, len(l.Fields))
	pos := 0
	for _, f := range l.Fields {
		if pos >= len(text) {
			out = append(out, "")
			continue
		}
		end := pos + f.Width
		if end > len(text) {
			end = len(text)
		}
		out = append(out, text[pos:end])
		pos += f.Width
	}
	return out
}

// fieldAt returns the 0-based offset of the nth field.
func (l *Layout) fieldAt(n int) int {
	pos := 0
	for i := 0; i < n && i < len(l.Fields); i++ {
		pos += l.Fields[i].Width
	}
	return pos
}

// checkLayout verifies record lengths and padding against the declared layout.
func checkLayout(s *source, opts Options, rep *Report) {
	l := opts.Layout
	if l == nil {
		return
	}
	want := l.RecordLength()

	for _, r := range s.Records {
		if strings.TrimSpace(r.Text) == "" {
			continue
		}
		if got := len(r.Text); got != want {
			rep.add(Finding{
				Rule:     RuleLayoutLength,
				Severity: SeverityError,
				Message: fmt.Sprintf("record is %d character(s) long but layout %s declares %d; %s",
					got, layoutName(l), want, describeDrift(l, got, want)),
				Line: r.Line, RecordNumber: r.Ordinal, Record: r.ID,
				Expected: strconv.Itoa(want), Actual: strconv.Itoa(got),
			})
			// Field offsets are meaningless once the length is wrong.
			continue
		}
		checkPadding(l, r, rep)
	}
}

// describeDrift names the field at which a length mismatch starts to matter.
func describeDrift(l *Layout, got, want int) string {
	// Lint rejects an empty layout before reaching here, but this function
	// indexes the last field, so it enforces the invariant itself as well.
	if len(l.Fields) == 0 {
		return "the layout declares no fields"
	}
	if got > want {
		return fmt.Sprintf("%d character(s) trail the last field %q", got-want, l.Fields[len(l.Fields)-1].Name)
	}
	// Find the first field that the record does not fully cover.
	pos := 0
	for _, f := range l.Fields {
		if pos+f.Width > got {
			return fmt.Sprintf("field %q (offset %d, width %d) is truncated", f.Name, pos, f.Width)
		}
		pos += f.Width
	}
	return "the record is short"
}

// checkPadding reports fields whose padding is unambiguously on the wrong side.
//
// Ambiguous cases are deliberately not reported: a right-aligned field padded
// with "0" cannot be distinguished from a value that genuinely ends in zeros, so
// only the wrong-character and space-padding cases are flagged there.
func checkPadding(l *Layout, r record, rep *Report) {
	for i, f := range l.Fields {
		if f.Pad == "" {
			continue
		}
		start := l.fieldAt(i)
		end := start + f.Width
		if end > len(r.Text) {
			return
		}
		value := r.Text[start:end]

		pad := f.padByte()
		lead := runLen(value, pad, true)
		trail := runLen(value, pad, false)
		if lead == len(value) || strings.TrimSpace(value) == "" {
			continue // entirely padding, or an empty field
		}

		flag := func(msg string) {
			rep.add(Finding{
				Rule:     RuleLayoutPadding,
				Severity: SeverityWarning,
				Message: fmt.Sprintf("field %q (offset %d, width %d): %s",
					f.Name, start, f.Width, msg),
				Line: r.Line, RecordNumber: r.Ordinal, Record: r.ID,
				Expected: fmt.Sprintf("padded on the %s with %q", f.Pad, string(rune(pad))),
				Actual:   value,
			})
		}

		if pad == ' ' {
			switch {
			case f.Pad == PadRight && lead > 0 && trail == 0:
				flag("value is right-aligned but the layout declares padding on the right (left-aligned)")
			case f.Pad == PadLeft && trail > 0 && lead == 0:
				flag("value is left-aligned but the layout declares padding on the left (right-aligned)")
			}
			continue
		}

		// A non-space pad character was declared, so stray space padding is a
		// reliable signal that the value was written by a different formatter.
		spaceLead := runLen(value, ' ', true)
		spaceTrail := runLen(value, ' ', false)
		if spaceLead > 0 || spaceTrail > 0 {
			flag(fmt.Sprintf("value is padded with spaces but the layout declares %q", string(rune(pad))))
		}
	}
}

// runLen counts the leading or trailing run of c in s.
func runLen(s string, c byte, leading bool) int {
	n := 0
	if leading {
		for n < len(s) && s[n] == c {
			n++
		}
		return n
	}
	for n < len(s) && s[len(s)-1-n] == c {
		n++
	}
	return n
}

func layoutName(l *Layout) string {
	if l.Name == "" {
		return "(unnamed)"
	}
	return strconv.Quote(l.Name)
}
