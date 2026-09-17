package edilint

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func equalStreamReport(t testing.TB, data []byte, opts Options) {
	t.Helper()
	got, err := LintReader("stream", bytes.NewReader(data), opts)
	if err != nil {
		t.Fatalf("LintReader: %v", err)
	}
	compareStreamReport(t, data, opts, got)
}

func compareStreamReport(t testing.TB, data []byte, opts Options, got *Report) {
	t.Helper()
	want := Lint("stream", data, opts)
	if got.Format != want.Format || !reflect.DeepEqual(got.Summary, want.Summary) || len(got.Findings) != len(want.Findings) {
		t.Fatalf("stream differs: format %s/%s; summaries %+v/%+v; findings %d/%d; input prefix %q", got.Format, want.Format, got.Summary, want.Summary, len(got.Findings), len(want.Findings), data[:min(512, len(data))])
	}
	for i := range got.Findings {
		if !reflect.DeepEqual(got.Findings[i], want.Findings[i]) {
			t.Fatalf("finding %d differs; input prefix %q\ngot: %+v\nwant: %+v", i, data[:min(512, len(data))], got.Findings[i], want.Findings[i])
		}
	}
}

func TestStreamMatchesFixtures(t *testing.T) {
	files, err := filepath.Glob("testdata/*")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range files {
		switch filepath.Ext(path) {
		case ".x12", ".hl7", ".edi", ".psv", ".txt", ".csv":
		default:
			continue
		}
		t.Run(filepath.Base(path), func(t *testing.T) {
			data, readErr := os.ReadFile(path)
			if readErr != nil {
				t.Fatal(readErr)
			}
			opts := Options{CountRules: []CountRule{{Declaring: "TRL", Field: 2, Counted: "DTL"}}}
			if strings.HasPrefix(filepath.Base(path), "remit_") {
				opts.Layout = remitLayout()
			}
			equalStreamReport(t, data, opts)
			opts.MaxFindings, opts.X12Charset = 2, CharsetBasic
			opts.Disabled = []string{RuleBOM}
			opts.Severities = map[string]Severity{RuleMixedTerminator: SeverityInfo}
			equalStreamReport(t, data, opts)
		})
	}
}

func TestStreamReaderFailuresAndCleanup(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	t.Setenv("TMP", os.Getenv("TMPDIR"))
	t.Setenv("TEMP", os.Getenv("TMPDIR"))
	data := readFixture(t, "hl7v2_batch_broken.hl7")
	got, err := LintReader("stream", &oneByteReader{data: data}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	want := Lint("stream", data, Options{})
	if !reflect.DeepEqual(got.Findings, want.Findings) {
		t.Fatalf("one-byte reader: %+v", got.Findings)
	}
	boom := errors.New("reader broke")
	got, err = LintReader("stream", io.MultiReader(strings.NewReader("DTL|1\n"), failingReader{boom}), Options{})
	if got != nil || !errors.Is(err, boom) {
		t.Fatalf("got report=%v error=%v", got, err)
	}
	got, err = LintReader("stream", strings.NewReader(strings.Repeat("X", 33)), Options{StreamLimits: StreamLimits{MaxRecordBytes: 32}})
	var limit *ResourceLimitError
	if got != nil || !errors.As(err, &limit) {
		t.Fatalf("got report=%v error=%v", got, err)
	}
	entries, err := os.ReadDir(os.Getenv("TMPDIR"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("spool leaked: %v, %v", entries, err)
	}
}

type oneByteReader struct{ data []byte }

func (r *oneByteReader) Read(p []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, io.EOF
	}
	if len(p) == 0 {
		return 0, nil
	}
	p[0], r.data = r.data[0], r.data[1:]
	return 1, nil
}

type failingReader struct{ err error }

func (r failingReader) Read([]byte) (int, error) { return 0, r.err }

func TestStreamBoundariesAndLimits(t *testing.T) {
	for _, data := range []string{
		"", "\r\n", "\xef\xbb\xbf", "\n\rISA*00", "\n\rfoo",
		"DTL|é\r\nDTL|ø\rTRL|2", "UNA:+.? '\r\nUNB+UNOC:3+S+R+260101:1200+1'FTX+AAI+++?+??escaped?'quote'UNZ+0+1'",
		"\r\nUNA", "\r\nUNB", "\t\t\t\nA|B\nC|D\n", "UNA:+.? ?UNB+A?", "HDR|A\nDTL|B\nDTL|C|D\nDTL|E\nTRL|3\n",
	} {
		for _, format := range []Format{FormatAuto, FormatText, FormatX12, FormatHL7v2, FormatEdifact, FormatDelimited, FormatFixed} {
			t.Run(string(format)+"/"+data, func(t *testing.T) { equalStreamReport(t, []byte(data), Options{Format: format}) })
		}
	}
	for _, opts := range []Options{
		{StreamLimits: StreamLimits{MaxRecordBytes: -1}},
		{Format: FormatDelimited, StreamLimits: StreamLimits{MaxStateEntries: 1}},
		{Format: FormatDelimited, StreamLimits: StreamLimits{MaxStateBytes: 1}},
	} {
		if rep, err := LintReader("stream", strings.NewReader("HDR|1\nDTL|2\nTRL|1\n"), opts); rep != nil || err == nil {
			t.Fatalf("limit ignored: %+v %v", rep, err)
		}
	}
	data := []byte("ignored\nHDR|1\nDTL|2\n")
	r := bytes.NewReader(data)
	if _, err := r.Seek(8, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	rep, err := LintReader("stream", r, Options{})
	if err != nil || !reflect.DeepEqual(rep.Findings, Lint("stream", data[8:], Options{}).Findings) {
		t.Fatalf("reader start offset ignored: %+v %v", rep, err)
	}
}

func TestStreamBaselineAndCrossFileControls(t *testing.T) {
	data := readFixture(t, "835_charset.x12")
	run := NewRunReport()
	run.Add(Lint("stream", data, Options{}))
	baseline := NewBaseline(run)
	opts := Options{Baseline: baseline, SeenISA13: map[string]string{}}
	want := Lint("stream", data, opts)
	baseline.Reset()
	opts.SeenISA13 = map[string]string{}
	got, err := LintReader("stream", bytes.NewReader(data), opts)
	if err != nil || !reflect.DeepEqual(got.Summary, want.Summary) || !reflect.DeepEqual(got.Findings, want.Findings) {
		t.Fatalf("baseline mismatch: %+v %v", got, err)
	}
	got, err = LintReader("second", bytes.NewReader(data), opts)
	if err != nil || countRule(got, RuleDupControl) == 0 {
		t.Fatalf("cross-file control tracking lost: %+v %v", got, err)
	}
}

func TestStreamLimitCoverage(t *testing.T) {
	tests := []struct {
		name, data string
		opts       Options
		resource   string
	}{
		{"characters", "éø\n", Options{Format: FormatText, StreamLimits: StreamLimits{MaxStateEntries: 1}}, "state entries"},
		{"record", strings.Repeat("X", 33), Options{Format: FormatText, StreamLimits: StreamLimits{MaxRecordBytes: 32}}, "record bytes"},
		{"padding", isa("000000001", "260101", "1200") + strings.Repeat(" ", 256), Options{StreamLimits: StreamLimits{MaxRecordBytes: 128}}, "record bytes"},
		{"controls", interchange("ST*835*1~", "SE*2*1~", "ST*835*2~", "SE*2*2~"), Options{StreamLimits: StreamLimits{MaxStateEntries: 1}}, "state entries"},
		{"findings", "A\x01\n", Options{Format: FormatText, StreamLimits: StreamLimits{MaxStateBytes: 128}}, "finding bytes"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rep, err := LintReader("stream", strings.NewReader(tc.data), tc.opts)
			var limit *ResourceLimitError
			if rep != nil || !errors.As(err, &limit) || limit.Resource != tc.resource {
				t.Fatalf("report=%+v error=%v", rep, err)
			}
		})
	}
}

func FuzzStreamParity(f *testing.F) {
	for _, name := range []string{"835_clean.x12", "835_envelope_broken.x12", "hl7v2_batch_broken.hl7", "edifact_clean.edi", "eligibility_broken.psv", "remit_clean.txt"} {
		f.Add(readFixture(f, name), uint8(0))
	}
	f.Add([]byte("\r\n\xef\xbb\xbfDTL|é\rTRL|1"), uint8(1))
	// A malformed record can repeat a large record ID in hundreds of findings.
	f.Add([]byte(strings.Repeat("A", 50000)+strings.Repeat("\x01", 500)), uint8(2))
	f.Fuzz(func(t *testing.T, data []byte, mode uint8) {
		if len(data) > maxFuzzInput {
			t.Skip()
		}
		formats := []Format{FormatAuto, FormatText, FormatX12, FormatHL7v2, FormatEdifact, FormatDelimited, FormatFixed}
		opts := Options{Format: formats[int(mode)%len(formats)], MaxFindings: 32, X12Charset: CharsetBasic}
		if opts.Format == FormatFixed {
			opts.Layout = remitLayout()
		}
		got, err := LintReader("stream", bytes.NewReader(data), opts)
		if err != nil {
			var limit *ResourceLimitError
			// Bounded rejection is valid for pathological inputs; never accept
			// a partial report or an unrelated reader error as parity.
			if got == nil && errors.As(err, &limit) {
				return
			}
			t.Fatalf("LintReader: report=%+v error=%v", got, err)
		}
		compareStreamReport(t, data, opts, got)
	})
}

// Read failures can occur after detection, during a replayed check.
type replayFailureReader struct {
	*bytes.Reader
	reads     int
	failAfter int
	err       error
}

func (r *replayFailureReader) ReadAt(p []byte, off int64) (int, error) {
	r.reads++
	if r.reads > r.failAfter {
		return 0, r.err
	}
	return r.Reader.ReadAt(p, off)
}

func TestStreamReplayErrors(t *testing.T) {
	data := readFixture(t, "835_clean.x12")
	for _, after := range []int{0, 2, 5} {
		boom := errors.New("replay failed")
		r := &replayFailureReader{Reader: bytes.NewReader(data), failAfter: after, err: boom}
		rep, err := LintReader("stream", r, Options{})
		if rep != nil || !errors.Is(err, boom) {
			t.Fatalf("after %d: report=%+v error=%v", after, rep, err)
		}
	}
}

func TestStreamLongDetectionAndDenseFindings(t *testing.T) {
	for _, data := range []string{
		strings.TrimSuffix(isa("000000001", "260101", "1200"), "~") + "\xc3" + strings.Repeat("\xa9*ADD*lowercase\xc3", 12000),
		strings.Repeat("A", 70000) + "|B\nC|D\nE|F\n",
		strings.Repeat("\x00", 70000) + "|B\nC|D\nE|F\n",
		interchange(strings.Repeat("NTE*ADD*éølowercase~", 12000)),
		interchange(strings.Repeat("NTE*ADD*éø\x01lowercase~\n", 12000)),
	} {
		equalStreamReport(t, []byte(data), Options{X12Charset: CharsetBasic})
	}
}

func TestStreamEarlyEOFIsError(t *testing.T) {
	data := readFixture(t, "835_clean.x12")
	r := &replayFailureReader{Reader: bytes.NewReader(data), failAfter: 2, err: io.EOF}
	rep, err := LintReader("stream", r, Options{})
	if rep != nil || !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("report=%+v error=%v", rep, err)
	}
}
