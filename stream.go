package edilint

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode"
)

// StreamLimits bounds individual records and tracking indexes in the reader
// and file lint/statistics APIs. Zero fields select the defaults; negative values
// are invalid. Limits do not apply to the byte-slice APIs Lint and Stats.
type StreamLimits struct {
	MaxRecordBytes  int // Default: 1 MiB, including a segment's trailing padding.
	MaxStateEntries int // Default: 100,000 distinct keys per tracking index.
	MaxStateBytes   int // Default: 16 MiB per index, retained findings, or stats ranges.
}

// ResourceLimitError means an input cannot be analyzed within its stream limits.
// It is an operational error, not a clean or partially checked report.
type ResourceLimitError struct {
	Resource string
	Limit    int
}

func (e *ResourceLimitError) Error() string {
	return fmt.Sprintf("stream %s limit exceeded (%d); raise the configured limit if this input is expected", e.Resource, e.Limit)
}

func (l StreamLimits) defaults() (StreamLimits, error) {
	if l.MaxRecordBytes < 0 || l.MaxStateEntries < 0 || l.MaxStateBytes < 0 {
		return l, fmt.Errorf("stream limits must not be negative")
	}
	if l.MaxRecordBytes == 0 {
		l.MaxRecordBytes = 1 << 20
	}
	if l.MaxStateEntries == 0 {
		l.MaxStateEntries = 100000
	}
	if l.MaxStateBytes == 0 {
		l.MaxStateBytes = 16 << 20
	}
	return l, nil
}

// LintReader analyzes a reader with bounded record and index storage. Seekable
// ReaderAt inputs are replayed directly from their current position. Other
// readers are copied to a private temporary file, removed before returning,
// because file-wide statistics require replay. The caller retains ownership of
// r. A read or resource-limit error returns no report. Like Lint, this may
// consume Baseline entries and update SeenISA13; discard those on an error.
func LintReader(name string, r io.Reader, opts Options) (*Report, error) {
	limits, err := opts.StreamLimits.defaults()
	if err != nil {
		return nil, err
	}
	return withStreamInput(name, r, func(input io.ReaderAt, size int64) (*Report, error) {
		return lintStream(name, input, size, opts, limits)
	})
}

// withStreamInput preserves seekable inputs and owns the spool for other readers.
func withStreamInput[T any](name string, r io.Reader, analyze func(io.ReaderAt, int64) (*T, error)) (result *T, err error) {
	if ra, ok := r.(interface {
		io.ReaderAt
		io.Seeker
	}); ok {
		start, seekErr := ra.Seek(0, io.SeekCurrent)
		if seekErr == nil {
			end, endErr := ra.Seek(0, io.SeekEnd)
			if _, restoreErr := ra.Seek(start, io.SeekStart); restoreErr != nil {
				return nil, restoreErr
			}
			if endErr == nil {
				return analyze(io.NewSectionReader(ra, start, end-start), end-start)
			}
		}
	}
	f, err := os.CreateTemp("", "edilint-stream-*")
	if err != nil {
		return nil, fmt.Errorf("create stream spool: %w", err)
	}
	defer func() {
		if cleanupErr := errors.Join(f.Close(), os.Remove(f.Name())); cleanupErr != nil {
			result = nil
			err = errors.Join(err, fmt.Errorf("clean up stream spool: %w", cleanupErr))
		}
	}()
	n, err := io.Copy(f, r)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", name, err)
	}
	return analyze(f, n)
}

type streamSource struct {
	input         io.ReaderAt
	size          int64
	limits        StreamLimits
	err           error
	start         int
	startLine     int
	isaLine       int
	unaBytes      int
	typed         bool
	locate        func(int) *record
	externalBytes int
	binary        bool
}

func (st *streamSource) reader() *bufio.Reader {
	return bufio.NewReaderSize(io.NewSectionReader(st.input, 0, st.size), 64<<10)
}

func (st *streamSource) fail(err error) {
	if st.err == nil {
		st.err = err
	}
}

func (s *source) acceptState(entries, keyBytes int) bool {
	if s.stream == nil {
		return true
	}
	st := s.stream
	if entries > st.limits.MaxStateEntries {
		st.fail(&ResourceLimitError{"state entries", st.limits.MaxStateEntries})
	}
	if entries > st.limits.MaxStateBytes/64 || keyBytes > st.limits.MaxStateBytes-entries*64 {
		st.fail(&ResourceLimitError{"state bytes", st.limits.MaxStateBytes})
	}
	return st.err == nil
}

func lintStream(name string, input io.ReaderAt, size int64, opts Options, limits StreamLimits) (*Report, error) {
	if size < 0 || uint64(size) > uint64(^uint(0)>>1) {
		return nil, fmt.Errorf("input size is not representable on this platform")
	}
	input = checkedReaderAt{input}
	prefix := make([]byte, min(size, binarySampleBytes+4))
	if _, err := io.ReadFull(io.NewSectionReader(input, 0, size), prefix); err != nil {
		return nil, err
	}
	body, bom := splitBOM(prefix)
	share, binary := looksBinary(body)
	skip := len(prefix) - len(body)
	st := &streamSource{input: io.NewSectionReader(input, int64(skip), size-int64(skip)), size: size - int64(skip), limits: limits, typed: true}
	st.binary = binary
	rep := newReport(name, opts)
	rep.findingLimit, rep.limitError = limits.MaxStateBytes, st.fail
	parseReport := &Report{}
	s, err := st.prepare(name, opts, parseReport)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", name, err)
	}
	rep.Format = s.Format
	if bom != "" {
		checkBOM(rep, s.Format, bom)
	}
	if binary {
		rep.add(Finding{Rule: RuleInvalidUTF8, Severity: SeverityError,
			Message: fmt.Sprintf("file does not look like text: %.0f%% of the first %s is invalid UTF-8 or NUL. It was probably picked up by a glob; no interchange checks were run.", share*100, describeSample(body)), Line: 1})
		if st.err != nil {
			return nil, st.err
		}
		rep.finalize(opts.maxFindings())
		return rep, nil
	}
	validateLayout(&opts, rep)
	s.Layout = opts.Layout
	for _, f := range parseReport.Findings {
		rep.add(f)
	}
	s.assignStreamIDs()
	checkStreamCharset(s, rep)
	checkSource(s, opts, rep)
	if st.err != nil {
		return nil, fmt.Errorf("read %s: %w", name, st.err)
	}
	rep.finalize(opts.maxFindings())
	return rep, nil
}

// assignStreamIDs applies the same whole-file record-type heuristic as assignIDs.
func (s *source) assignStreamIDs() {
	st := s.stream
	if s.Format == FormatDelimited || s.Format == FormatFixed {
		st.typed = false
		ids := map[string]bool{}
		n, keyBytes := 0, 0
		for r := range s.records() {
			n++
			id := s.recordID(r.Text)
			if !ids[id] {
				keyBytes += len(id)
				if !s.acceptState(len(ids)+1, keyBytes) {
					break
				}
				ids[strings.Clone(id)] = true
			}
		}
		st.typed = len(ids) <= max(3, n/3)
	}
}

// prepare inspects only the header and bounded line samples. Diagnostic checks
// still see the entire body, including leading whitespace and UNA bytes.
func (st *streamSource) prepare(name string, opts Options, rep *Report) (*source, error) {
	br := st.reader()
	off, line := 0, 1
	prevCR := false
	for {
		p, err := br.Peek(1)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		if !bytes.ContainsRune([]byte(" \t\r\n\v\f"), rune(p[0])) {
			break
		}
		c, _ := br.ReadByte()
		if c == '\r' {
			line++
		} else if c == '\n' && !prevCR {
			line++
		}
		prevCR = c == '\r'
		off++
	}
	head := make([]byte, isaScanLimit+2)
	n, err := io.ReadFull(br, head)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return nil, err
	}
	head = head[:n]
	format := opts.Format
	if format == "" || format == FormatAuto {
		switch {
		case bytes.HasPrefix(head, []byte("ISA")):
			format = FormatX12
		case bytes.HasPrefix(head, []byte("MSH")), bytes.HasPrefix(head, []byte("FHS")), bytes.HasPrefix(head, []byte("BHS")):
			format = FormatHL7v2
		case looksEdifact(head):
			format = FormatEdifact
		case opts.Layout != nil:
			format = FormatFixed
		case opts.Delimiter != "":
			format = FormatDelimited
		default:
			format = FormatText
		}
	}
	s := &source{Name: name, Format: format, Layout: opts.Layout, Charset: opts.charset(), ISAOffset: -1, stream: st}
	if format == FormatX12 || format == FormatEdifact {
		metaReport := &Report{}
		meta := newSource(name, head, format, opts, metaReport)
		s.Delims, s.Edifact, s.FieldSep = meta.Delims, meta.Edifact, meta.FieldSep
		for _, f := range metaReport.Findings {
			if f.Line > 0 && f.Record != "" {
				f.Line += line - 1
			}
			rep.add(f)
		}
		if s.Delims.Declared || s.Edifact.Declared {
			st.start, st.startLine, st.isaLine = off, line, line
			if format == FormatX12 {
				s.ISAOffset = off
			}
			if s.Edifact.UNA {
				st.unaBytes = min(len(head), unaLength)
				st.start += st.unaBytes
			}
		}
	}
	if format == FormatDelimited || (format == FormatText && (opts.Format == "" || opts.Format == FormatAuto)) {
		samples := st.delimiterSamples()
		s.FieldSep = delimiterFromCounts(samples)
		if format == FormatText && s.FieldSep != 0 {
			s.Format = FormatDelimited
		}
		if opts.Delimiter != "" {
			if d, parseErr := ParseDelimiter(opts.Delimiter); parseErr == nil {
				s.FieldSep = d
			} else {
				reportBadDelimiter(rep, opts.Delimiter, parseErr, "delimiter detection")
			}
		}
		if s.Format == FormatDelimited && s.FieldSep == 0 && opts.Delimiter == "" {
			rep.add(Finding{Rule: RuleFieldOutlier, Severity: SeverityWarning, Message: "unable to detect a field delimiter; pass --delimiter to enable field checks", Line: 1})
		}
	}
	if format == FormatHL7v2 && !st.binary {
		s.FieldSep = '|'
		for r := range s.records() {
			if len(r.Text) >= 4 && (r.ID == "FHS" || r.ID == "BHS" || r.ID == "MSH") {
				s.FieldSep = r.Text[3]
				break
			}
		}
	}
	return s, st.err
}

// Detection needs counts, not sampled record contents. This also preserves
// format inference on binary files without retaining very long sample lines.
func (st *streamSource) delimiterSamples() []delimiterCounts {
	br := st.reader()
	var samples []delimiterCounts
	var counts delimiterCounts
	nonempty, width := false, 0
	for len(samples) < 200 && st.err == nil {
		r, size, err := br.ReadRune()
		if err != nil && !errors.Is(err, io.EOF) {
			st.fail(err)
			break
		}
		if err != nil || r == '\r' || r == '\n' {
			if nonempty {
				samples = append(samples, counts)
			}
			counts = delimiterCounts{}
			nonempty, width = false, 0
			if err != nil {
				break
			}
			continue
		}
		width += size
		if !st.binary && width > st.limits.MaxRecordBytes {
			st.fail(&ResourceLimitError{"record bytes", st.limits.MaxRecordBytes})
			break
		}
		if !unicode.IsSpace(r) {
			nonempty = true
		}
		for i, d := range candidateDelimiters {
			if r == rune(d) {
				counts[i]++
			}
		}
	}
	return samples
}

// records holds at most one record and its padding. The scanner's offsets are
// relative to the BOM-stripped body, matching the in-memory parser.
func (st *streamSource) records(s *source, yield func(record) bool) {
	if st.err != nil {
		return
	}
	segmented := s.Delims.Declared || s.Edifact.Declared
	term, release := s.Delims.Segment, byte(0)
	if s.Edifact.Declared {
		term, release = s.Edifact.Segment, s.Edifact.Release
	}
	br := st.reader()
	off, line, ordinal := 0, 1, 0
	prevCR := false
	read := func() (byte, error) {
		c, err := br.ReadByte()
		if err == nil {
			off++
			if c == '\r' {
				line++
			} else if c == '\n' && !prevCR {
				line++
			}
			prevCR = c == '\r'
		}
		return c, err
	}
	if segmented {
		// Seek to the header without walking the skipped prefix on every pass.
		br.Reset(io.NewSectionReader(st.input, int64(st.start), st.size-int64(st.start)))
		off, line = st.start, st.startLine
		if s.Edifact.Declared {
			// UNA can itself contain CR/LF service characters.
			if s.Edifact.UNA {
				p := make([]byte, st.unaBytes)
				if _, err := st.input.ReadAt(p, int64(st.start-len(p))); err != nil {
					st.fail(err)
					return
				}
				for _, c := range p {
					if c == '\r' {
						line++
					} else if c == '\n' && !prevCR {
						line++
					}
					prevCR = c == '\r'
				}
			}
			for {
				p, err := br.Peek(1)
				if err != nil || !isPadByte(p[0]) {
					break
				}
				_, _ = read()
			}
		}
	}
	var text, pad []byte
	for st.err == nil {
		r := record{Offset: off, Line: line, Ordinal: ordinal + 1}
		text, pad = text[:0], pad[:0]
		escaped, eof := false, false
		for {
			c, err := read()
			if errors.Is(err, io.EOF) {
				eof = true
				break
			}
			if err != nil {
				st.fail(err)
				return
			}
			if segmented {
				hasRelease := s.Edifact.Declared && c == release && int64(off) < st.size
				if !escaped && c == term && !hasRelease {
					r.Term = string([]byte{c})
					break
				}
				if s.Edifact.Declared && !escaped && c == release {
					escaped = true
				} else {
					escaped = false
				}
			} else if c == '\r' || c == '\n' {
				r.Term = string([]byte{c})
				if c == '\r' {
					if p, peekErr := br.Peek(1); peekErr == nil && p[0] == '\n' {
						_, _ = read()
						r.Term = "\r\n"
					}
				}
				break
			}
			if len(text) >= st.limits.MaxRecordBytes {
				st.fail(&ResourceLimitError{"record bytes", st.limits.MaxRecordBytes})
				return
			}
			text = append(text, c)
		}
		if segmented {
			if eof {
				if len(bytes.TrimSpace(text)) == 0 {
					return
				}
				text = bytes.TrimRight(text, "\r\n \t")
			} else {
				for {
					p, err := br.Peek(1)
					if err != nil || p[0] == term || !isPadByte(p[0]) {
						break
					}
					if len(text)+len(pad) >= st.limits.MaxRecordBytes {
						st.fail(&ResourceLimitError{"record bytes", st.limits.MaxRecordBytes})
						return
					}
					c, _ := read()
					pad = append(pad, c)
				}
			}
		} else if eof && len(text) == 0 {
			return
		}
		r.Text, r.Pad = string(text), string(pad)
		if st.typed {
			r.ID = s.recordID(r.Text)
		}
		ordinal++
		if !yield(r) || eof {
			return
		}
	}
}

// SectionReader never requests bytes beyond the captured input size. An early
// EOF therefore means the input shrank or the reader failed during a replay.
type checkedReaderAt struct{ io.ReaderAt }

func (r checkedReaderAt) ReadAt(p []byte, off int64) (int, error) {
	n, err := r.ReaderAt.ReadAt(p, off)
	if n < len(p) && (err == nil || errors.Is(err, io.EOF)) {
		err = io.ErrUnexpectedEOF
	}
	return n, err
}
