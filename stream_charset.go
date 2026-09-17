package edilint

import (
	"errors"
	"fmt"
	"io"
	"iter"
	"unicode/utf8"
)

// Sharp character findings precede aggregated findings in the existing engine.
// Preserve that admission order even when the report's retention limit is hit,
// replaying only when the first pass actually encounters aggregate candidates.
func checkStreamCharset(s *source, rep *Report) {
	if s.stream.err != nil {
		return
	}
	agg := newCharAggregator()
	agg.outsideOnly = true
	allowed, structural := allowedControls(s), structuralCharacters(s)
	walkStreamRunes(s, func(r rune, size int, raw byte, line, col, off int) {
		switch {
		case r == utf8.RuneError && size == 1:
			rep.add(s.locate(off, Finding{Rule: RuleInvalidUTF8, Severity: SeverityError,
				Message: fmt.Sprintf("invalid UTF-8 byte 0x%02X; the file is not valid UTF-8 and byte-oriented parsers will disagree with text-oriented ones about field boundaries", raw),
				Line:    line, Column: col, CodePoint: fmt.Sprintf("0x%02X", raw)}))
		case r < 0x20 || r == 0x7f:
			checkControl(s, rep, allowed, r, line, col, off)
		case r < 0x80:
			if s.Format == FormatX12 && !structural[r] {
				checkX12Charset(s, rep, agg, r, line, col, off)
			}
		default:
			checkNonASCII(s, rep, agg, r, line, col, off)
		}
	})
	if !agg.hits || s.stream.err != nil {
		return
	}
	// Separator bytes can form valid Unicode with the next record. Keep their
	// bounded, record-zero aggregates from the first pass, then emit each at its
	// first occurrence so report retention matches the in-memory engine.
	outside := agg
	agg = newCharAggregator()
	lastRecord, lastLine := 0, 0
	walkStreamRunes(s, func(r rune, size int, _ byte, line, col, off int) {
		if line != lastLine {
			agg.emit(rep)
			agg = newCharAggregator()
			lastLine, lastRecord = line, 0
		}
		if r == utf8.RuneError && size == 1 {
			return
		}
		rule, severity := "", SeverityWarning
		if r >= 0x80 {
			if _, invisible := isInvisible(r); invisible {
				return
			}
			if _, confusable := confusableASCII(r); confusable {
				return
			}
			rule = RuleNonASCII
		} else if s.Format == FormatX12 && s.Charset != CharsetOff && s.Charset != CharsetExtended &&
			r >= 0x20 && r != 0x7f && !structural[r] && !inX12Basic(r) && inX12Extended(r) {
			rule = RuleX12Basic
		}
		if rule == "" {
			return
		}
		location := s.locate(off, Finding{})
		if location.RecordNumber == 0 {
			agg.emit(rep)
			agg = newCharAggregator()
			lastRecord = 0
			key := fmt.Sprintf("%s|0|%d", rule, line)
			if b, ok := outside.buckets[key]; ok {
				(&charAggregator{buckets: map[string]*charBucket{key: b}}).emit(rep)
				delete(outside.buckets, key)
			}
			return
		}
		if location.RecordNumber != lastRecord {
			agg.emit(rep)
			agg = newCharAggregator()
			lastRecord = location.RecordNumber
		}
		agg.add(s, rule, severity, r, line, col, off)
	})
	if s.stream.err == nil {
		agg.emit(rep)
	}
}

// The rune reader is independent of the record reader, so even an invalid
// separator that bisects a UTF-8 sequence cannot change character diagnostics.
func walkStreamRunes(s *source, visit func(rune, int, byte, int, int, int)) {
	st := s.stream
	if st.err != nil {
		return
	}
	next, stop := iter.Pull(s.records())
	defer stop()
	current, exists := next()
	st.locate = func(off int) *record {
		for exists && off >= current.Offset+len(current.Text) {
			current, exists = next()
		}
		if exists && off >= current.Offset {
			return &current
		}
		return nil
	}
	defer func() { st.locate = nil }()
	br := st.reader()
	line, col, off := 1, 1, 0
	for st.err == nil {
		p, err := br.Peek(1)
		if errors.Is(err, io.EOF) {
			return
		}
		if err != nil {
			st.fail(err)
			return
		}
		raw := p[0]
		r, size, err := br.ReadRune()
		if err != nil {
			st.fail(err)
			return
		}
		visit(r, size, raw, line, col, off)
		switch r {
		case '\n':
			line, col = line+1, 1
		case '\r':
			if p, peekErr := br.Peek(1); peekErr == nil && p[0] == '\n' {
				col++
			} else {
				line, col = line+1, 1
			}
		default:
			col++
		}
		off += size
	}
}
