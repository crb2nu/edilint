package edilint

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"
)

// StatsReader builds the same census as Stats using bounded record and index
// storage. Zero limits select the StreamLimits defaults. Seekable ReaderAt
// inputs are read from their current position, which is preserved; other
// readers are spooled to a private temporary file removed before return.
// The caller retains ownership of r. Read, spool, and limit failures return
// an error and no partial census. Malformed text still receives a census.
func StatsReader(name string, r io.Reader, limits StreamLimits) (*FileStats, error) {
	limits, err := limits.defaults()
	if err != nil {
		return nil, err
	}
	return withStreamInput(name, r, func(input io.ReaderAt, size int64) (*FileStats, error) {
		return statsStream(name, input, size, limits)
	})
}

// StatsFile builds a bounded-memory census of path. A path of "-" reads stdin.
func StatsFile(path string, limits StreamLimits) (*FileStats, error) {
	if path == "-" {
		return StatsReader(path, os.Stdin, limits)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	return StatsReader(path, f, limits)
}

func statsStream(name string, input io.ReaderAt, size int64, limits StreamLimits) (*FileStats, error) {
	if size < 0 || uint64(size) > uint64(^uint(0)>>1) {
		return nil, fmt.Errorf("input size is not representable on this platform")
	}
	input = checkedReaderAt{input}
	prefix := make([]byte, min(size, binarySampleBytes+4))
	if _, err := io.ReadFull(io.NewSectionReader(input, 0, size), prefix); err != nil {
		return nil, fmt.Errorf("read %s: %w", name, err)
	}
	body, _ := splitBOM(prefix)
	if share, binary := looksBinary(body); binary {
		return nil, fmt.Errorf("%s does not look like text: %.0f%% of the sample is invalid UTF-8 or NUL", name, share*100)
	}
	skip := int64(len(prefix) - len(body))
	st := &streamSource{input: io.NewSectionReader(input, skip, size-skip), size: size - skip, limits: limits, typed: true}
	s, err := st.prepare(name, Options{}, &Report{})
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", name, err)
	}
	s.assignStreamIDs()
	fs := &FileStats{File: name, Format: s.Format, Bytes: int(size)}
	histogram := map[string]int{}
	keyBytes := 0
	var envelope *streamEnvelopeStats
	if s.Format == FormatX12 && s.Delims.Declared {
		fs.Separators = separatorStats(s.Delims)
		envelope = &streamEnvelopeStats{stats: &EnvelopeStats{}}
		fs.Envelope = envelope.stats
	}
	for r := range s.records() {
		if strings.TrimSpace(r.Text) == "" {
			continue
		}
		fs.Records++
		addStatsKey(s, histogram, r.ID, &keyBytes)
		if envelope != nil {
			envelope.add(s, r)
		}
		if st.err != nil {
			break
		}
	}
	if len(histogram) > 0 {
		fs.RecordsByID = histogram
	}
	if envelope != nil && st.err == nil {
		fs.Charset = streamCharsetStats(s)
		if len(fs.Envelope.GroupsByCode) == 0 {
			fs.Envelope.GroupsByCode = nil
		}
		if len(fs.Envelope.TransactionsByType) == 0 {
			fs.Envelope.TransactionsByType = nil
		}
	}
	if st.err != nil {
		return nil, fmt.Errorf("read %s: %w", name, st.err)
	}
	return fs, nil
}

func addStatsKey(s *source, counts map[string]int, key string, keyBytes *int) {
	if key == "" {
		return
	}
	if _, ok := counts[key]; !ok {
		*keyBytes += len(key)
		if !s.acceptState(len(counts)+1, *keyBytes) {
			return
		}
	}
	// Map assignments may replace an equal string key; never retain a record.
	counts[strings.Clone(key)]++
}

// Count headers only when their parents are open, matching parseX12Doc's
// tolerant hierarchy without retaining segments or control-number samples.
type streamEnvelopeStats struct {
	stats               *EnvelopeStats
	isa, gs             bool
	groupBytes, txBytes int
}

func (e *streamEnvelopeStats) add(s *source, r record) {
	es := e.stats
	switch r.ID {
	case "ISA":
		e.isa, e.gs = true, false
		es.Interchanges++
		fields := s.Fields(r)
		addStatsRange(&es.ISA13, elem(fields, 13), 0)
		addStatsRange(&es.ISADates, elem(fields, 9), 6)
	case "IEA":
		e.isa, e.gs = false, false
	case "GS":
		if e.isa {
			e.gs = true
			es.Groups++
			fields := s.Fields(r)
			if es.GroupsByCode == nil {
				es.GroupsByCode = map[string]int{}
			}
			addStatsKey(s, es.GroupsByCode, strings.TrimSpace(elem(fields, 1)), &e.groupBytes)
			addStatsRange(&es.GS06, elem(fields, 6), 0)
			addStatsRange(&es.GSDates, elem(fields, 4), 8)
		}
	case "GE":
		e.gs = false
	case "ST":
		if e.gs {
			es.Transactions++
			fields := s.Fields(r)
			if es.TransactionsByType == nil {
				es.TransactionsByType = map[string]int{}
			}
			addStatsKey(s, es.TransactionsByType, strings.TrimSpace(elem(fields, 1)), &e.txBytes)
			addStatsRange(&es.ST02, elem(fields, 2), 0)
		}
	default:
		return
	}
	entries, size := 0, 0
	for _, v := range []*ValueRange{es.ISA13, es.GS06, es.ST02, es.ISADates, es.GSDates} {
		if v != nil {
			entries += 2
			size += len(v.Min) + len(v.Max)
		}
	}
	s.acceptState(0, size+entries*64)
}

func addStatsRange(dst **ValueRange, value string, dateWidth int) {
	value = strings.TrimSpace(value)
	if value == "" || (dateWidth > 0 && (len(value) != dateWidth || !allDigits(value))) {
		return
	}
	if *dst == nil {
		value = strings.Clone(value)
		*dst = &ValueRange{Min: value, Max: value}
		return
	}
	if valueLess(value, (*dst).Min) {
		(*dst).Min = strings.Clone(value)
	}
	if valueLess((*dst).Max, value) {
		(*dst).Max = strings.Clone(value)
	}
}

// Read runes independently of segments: malformed separators can bisect UTF-8.
func streamCharsetStats(s *source) *CharsetStats {
	cs := &CharsetStats{Profile: string(CharsetBasic)}
	structural := structuralCharacters(s)
	br := s.stream.reader()
	for {
		r, size, err := br.ReadRune()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			s.stream.fail(err)
			break
		}
		switch {
		case r == utf8.RuneError && size == 1:
			cs.BeyondExtended++
		case r < 0x20 || r == 0x7f || structural[r] || inX12Basic(r):
		case inX12Extended(r):
			cs.ExtendedOnly++
		default:
			cs.BeyondExtended++
		}
	}
	if cs.BeyondExtended > 0 {
		cs.Profile = "beyond-extended"
	} else if cs.ExtendedOnly > 0 {
		cs.Profile = string(CharsetExtended)
	}
	return cs
}
