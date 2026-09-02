package edilint

import (
	"bytes"
	"fmt"
	"strings"
)

// Canonical layout, as `edilint fmt` writes it.
//
// The canonical form of an X12 interchange is one segment per line: each
// segment's content, byte for byte, closed by the terminator the ISA declared
// and followed by a single LF. The canonical form of an HL7v2 message or batch
// file is one segment per line terminated by LF. In both forms, whitespace
// between records is normalized away, whitespace-only records are dropped, and
// a final LF is always present.
//
// Canonicalization is layout only. It never edits the bytes inside a record,
// so a wrong count, a homoglyph or a byte order mark passes through unchanged
// — those are defects, and repairing defects is what Fix is for. The one
// asymmetry is deliberate: an X12 file whose last segment is not closed by the
// declared terminator keeps that defect, because the missing terminator is how
// a truncated interchange announces itself, and a formatter that silently
// closed it would hide the truncation.

// Canonical returns the canonical form of an X12 interchange or an HL7v2
// message or batch file. A format of FormatAuto detects the format from the
// content; any other input format is an error, because only X12 and HL7v2
// have a canonical layout defined.
//
// Canonical is idempotent: Canonical(Canonical(x)) == Canonical(x).
func Canonical(data []byte, format Format) ([]byte, error) {
	body, _ := splitBOM(data)
	prefix := data[:len(data)-len(body)]

	if format == "" || format == FormatAuto {
		format = Detect(body, Options{})
	}

	var out []byte
	switch format {
	case FormatX12:
		s := newSource("", body, FormatX12, Options{}, &Report{})
		if !s.Delims.Declared {
			return nil, fmt.Errorf("no usable ISA segment, so the segment terminator cannot be derived")
		}
		out = canonicalRecords(s)
	case FormatHL7v2:
		s := newSource("", body, FormatHL7v2, Options{}, &Report{})
		out = canonicalRecords(s)
	default:
		return nil, fmt.Errorf("fmt supports x12 and hl7v2 input; this input is %s", format)
	}

	if len(prefix) == 0 {
		return out, nil
	}
	return append(append([]byte{}, prefix...), out...), nil
}

// canonicalRecords renders each non-blank record as content, terminator (for
// X12, whatever the record already carried: the declared terminator, or
// nothing for an unterminated final segment) and a single LF.
func canonicalRecords(s *source) []byte {
	var b bytes.Buffer
	for _, r := range s.Records {
		if strings.TrimSpace(r.Text) == "" {
			continue
		}
		b.WriteString(r.Text)
		if s.Format == FormatX12 {
			b.WriteString(r.Term)
		}
		// When the declared segment terminator is itself a line feed, the
		// terminator already ends the line.
		if r.Term != "\n" || s.Format != FormatX12 {
			b.WriteByte('\n')
		}
	}
	return b.Bytes()
}
