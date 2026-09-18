package edilint

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func equalStatsStream(t testing.TB, data []byte) {
	t.Helper()
	want, wantErr := Stats("stream", data)
	got, err := StatsReader("stream", bytes.NewReader(data), StreamLimits{})
	if wantErr != nil {
		if got != nil || err == nil || err.Error() != wantErr.Error() {
			t.Fatalf("binary rejection differs: got=%+v err=%v want=%v", got, err, wantErr)
		}
		return
	}
	if err != nil || !reflect.DeepEqual(got, want) {
		a, _ := json.Marshal(got)
		b, _ := json.Marshal(want)
		t.Fatalf("stats differ: err=%v\ngot %s\nwant %s\ninput %q", err, a, b, data[:min(len(data), 512)])
	}
}

func TestStatsStreamFixtures(t *testing.T) {
	paths, err := filepath.Glob("testdata/*")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
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
			equalStatsStream(t, data)
		})
	}
}

func TestStatsStreamMalformedAndBoundaries(t *testing.T) {
	for _, data := range []string{
		"", "\r\n", "\xef\xbb\xbf", "ISA*00", "\x00\x00\x00",
		"HDR|1\nDTL|2\nTRL|1\n", "a|1\nb|2\nc|3\nd|4\n",
		"a|1\n \n\t\nv|2\n", "MSH|^~\\&|S|R\rPID|é\r",
		"\xef\xbb\xbf\r\n" + interchangeHeader() + "NTE*é\xff~",
		interchangeHeader() + "ST**~GE*1*1~ST*LOOSE*9~IEA*1*1~GS*LOOSE~ST*LOOSE~",
		interchangeHeader() + "ST*837*2~ST*835*10~ST*835*02~ST*835*1A~" + interchangeHeader(),
		isa("1", "BAD", "1200") + "GS**S*R*BAD*T**X~ST**~",
		strings.TrimSuffix(isa("000000001", "260101", "1200"), "~") + "\xc3\xa9*ADD*lowercase\xc3",
	} {
		equalStatsStream(t, []byte(data))
	}
}

func TestStatsStreamLimits(t *testing.T) {
	for _, tc := range []struct {
		name, data, resource string
		limits               StreamLimits
	}{
		{"record", strings.Repeat("X", 33), "record bytes", StreamLimits{MaxRecordBytes: 32}},
		{"padding", isa("000000001", "260101", "1200") + strings.Repeat(" ", 256), "record bytes", StreamLimits{MaxRecordBytes: 128}},
		{"histogram", interchangeHeader(), "state entries", StreamLimits{MaxStateEntries: 2}},
		{"types", "A|1\nB|2\nC|3\nD|4\n", "state entries", StreamLimits{MaxStateEntries: 3}},
		{"histogram bytes", "MSH|^~\\&|S|R\r", "state bytes", StreamLimits{MaxStateBytes: 66}},
		{"ranges", isa(strings.Repeat("1", 100), "260101", "1200"), "state bytes", StreamLimits{MaxStateBytes: 300}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := StatsReader("stream", strings.NewReader(tc.data), tc.limits)
			var limit *ResourceLimitError
			if got != nil || !errors.As(err, &limit) || limit.Resource != tc.resource {
				t.Fatalf("got=%+v error=%v", got, err)
			}
			if _, err := StatsReader("stream", strings.NewReader(tc.data), StreamLimits{}); err != nil {
				t.Fatalf("raising limits should succeed: %v", err)
			}
		})
	}
	for _, limits := range []StreamLimits{{MaxRecordBytes: -1}, {MaxStateEntries: -1}, {MaxStateBytes: -1}} {
		if got, err := StatsReader("stream", strings.NewReader(""), limits); got != nil || err == nil {
			t.Fatalf("negative limits accepted: %+v %v", got, err)
		}
	}
}

func TestStatsStreamReadersAndCleanup(t *testing.T) {
	tmp := t.TempDir()
	for _, key := range []string{"TMPDIR", "TMP", "TEMP"} {
		t.Setenv(key, tmp)
	}
	data := readFixture(t, "stats_multi.x12")
	want, _ := Stats("stream", data)
	got, err := StatsReader("stream", &oneByteReader{data}, StreamLimits{})
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("one-byte reader: %+v %v", got, err)
	}
	r := bytes.NewReader(append([]byte("ignored"), data...))
	if _, err = r.Seek(7, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	got, err = StatsReader("stream", r, StreamLimits{})
	pos, seekErr := r.Seek(0, io.SeekCurrent)
	if err != nil || seekErr != nil || pos != 7 || !reflect.DeepEqual(got, want) {
		t.Fatalf("seekable reader: %+v %v position=%d err=%v", got, err, pos, seekErr)
	}
	boom := errors.New("read failed")
	for _, reader := range []io.Reader{
		io.MultiReader(bytes.NewReader(data), failingReader{boom}),
		&replayFailureReader{Reader: bytes.NewReader(data), failAfter: 2, err: boom},
		&replayFailureReader{Reader: bytes.NewReader(data), failAfter: 3, err: boom},
	} {
		got, err = StatsReader("stream", reader, StreamLimits{})
		if got != nil || !errors.Is(err, boom) {
			t.Fatalf("read failure: %+v %v", got, err)
		}
	}
	got, err = StatsReader("stream", &oneByteReader{[]byte("too long")}, StreamLimits{MaxRecordBytes: 2})
	if got != nil || err == nil {
		t.Fatalf("spooled limit failure: %+v %v", got, err)
	}
	entries, err := os.ReadDir(tmp)
	if err != nil || len(entries) != 0 {
		t.Fatalf("spool leaked: %v %v", entries, err)
	}
	path := filepath.Join(t.TempDir(), "input.x12")
	if err = os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	got, err = StatsFile(path, StreamLimits{})
	want.File = path
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("StatsFile: %+v %v", got, err)
	}
	if got, err := StatsFile(path+".missing", StreamLimits{}); got != nil || err == nil {
		t.Fatalf("missing file: %+v %v", got, err)
	}
}

func FuzzStatsStreamParity(f *testing.F) {
	for _, name := range []string{"stats_multi.x12", "835_envelope_broken.x12", "hl7v2_batch_broken.hl7", "edifact_clean.edi", "eligibility_broken.psv", "remit_clean.txt"} {
		f.Add(readFixture(f, name))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > maxFuzzInput {
			t.Skip()
		}
		equalStatsStream(t, data)
	})
}
