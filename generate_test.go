package edilint

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
)

func TestGenerateCleanAndDeterministic(t *testing.T) {
	for _, kind := range []string{"837p", "hl7v2"} {
		for _, count := range []int{0, 1, 2, 1000} {
			t.Run(fmt.Sprintf("%s/%d", kind, count), func(t *testing.T) {
				opts := GenerateOptions{Kind: kind, Count: count}
				var a, b bytes.Buffer
				if err := Generate(&a, opts); err != nil {
					t.Fatal(err)
				}
				if err := Generate(&b, opts); err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(a.Bytes(), b.Bytes()) {
					t.Fatal("identical options generated different bytes")
				}
				requireClean(t, Lint("generated", a.Bytes(), Options{X12Charset: CharsetBasic}))
				rep, err := LintReader("generated", bytes.NewReader(a.Bytes()), Options{})
				if err != nil {
					t.Fatal(err)
				}
				requireClean(t, rep)
				fs, err := Stats("generated", a.Bytes())
				if err != nil {
					t.Fatal(err)
				}
				want := count
				if want == 0 {
					want = 1
				}
				if kind == "837p" {
					if fs.Format != FormatX12 || fs.RecordsByID["CLM"] != want || fs.RecordsByID["SV1"] != want {
						t.Fatalf("unexpected claims census: %+v", fs)
					}
					if fs.Envelope.Interchanges != 1 || fs.Envelope.Groups != 1 || fs.Envelope.TransactionsByType["837"] != 1 {
						t.Fatalf("unexpected envelope: %+v", fs.Envelope)
					}
					// Count and inspect the bytes independently of the parser.
					segments := strings.Split(strings.TrimSuffix(a.String(), "~\n"), "~\n")
					isa := strings.Split(segments[0], "*")
					if len(segments[0])+1 != 106 || isa[15] != "T" {
						t.Fatalf("invalid ISA width or test indicator: %q", segments[0])
					}
					if segments[len(segments)-3] != fmt.Sprintf("SE*%d*0001", len(segments)-4) {
						t.Fatalf("incorrect SE count: %q", segments[len(segments)-3])
					}
					if bytes.Contains(a.Bytes(), []byte("\r")) {
						t.Fatal("unexpected CR in X12 fixture")
					}
				} else {
					if fs.Format != FormatHL7v2 || fs.RecordsByID["MSH"] != want || fs.RecordsByID["PID"] != want {
						t.Fatalf("unexpected HL7 census: %+v", fs)
					}
					if !strings.HasSuffix(a.String(), fmt.Sprintf("BTS|%d\rFTS|1\r", want)) {
						t.Fatal("incorrect batch trailers")
					}
					ids := make(map[string]bool)
					for _, segment := range strings.Split(a.String(), "\r") {
						if !strings.HasPrefix(segment, "MSH|") {
							continue
						}
						fields := strings.Split(segment, "|")
						if fields[10] != "T" || ids[fields[9]] {
							t.Fatalf("production or duplicate MSH: %q", segment)
						}
						ids[fields[9]] = true
					}
					if bytes.Contains(a.Bytes(), []byte("\n")) {
						t.Fatal("unexpected LF in HL7 fixture")
					}
				}
			})
		}
	}
}

func TestGenerateOptions(t *testing.T) {
	for _, kind := range []string{"837p", "hl7v2"} {
		for _, date := range []string{"2000-01-01", "2024-02-29", "2099-12-31"} {
			var b bytes.Buffer
			if err := Generate(&b, GenerateOptions{Kind: kind, Date: date, ControlNumber: 999999999}); err != nil {
				t.Fatal(err)
			}
			requireClean(t, Lint("generated", b.Bytes(), Options{}))
			if !strings.Contains(b.String(), strings.ReplaceAll(date, "-", "")) || !strings.Contains(b.String(), "999999999") {
				t.Fatalf("date or control missing: %q", b.String())
			}
		}
	}
	// A shared lint session accepts independently generated files with distinct
	// interchange controls, and still catches the generator's default collision.
	opts := Options{SeenISA13: map[string]string{}}
	for _, control := range []int{1, 2, 1} {
		var b bytes.Buffer
		if err := Generate(&b, GenerateOptions{Kind: "837p", ControlNumber: control}); err != nil {
			t.Fatal(err)
		}
		rep := Lint(fmt.Sprintf("file-%d", len(opts.SeenISA13)), b.Bytes(), opts)
		if control == 1 && len(opts.SeenISA13) == 2 {
			if len(rep.Findings) != 1 || rep.Findings[0].Rule != RuleDupControl {
				t.Fatalf("expected duplicate interchange finding: %+v", rep.Findings)
			}
		} else {
			requireClean(t, rep)
		}
	}
}

func TestGenerateRejectsInvalidOptionsBeforeWriting(t *testing.T) {
	cases := []GenerateOptions{
		{}, {Kind: "835"}, {Kind: "837p", Count: -1},
		{Kind: "hl7v2", Count: MaxGenerateCount + 1},
		{Kind: "837p", ControlNumber: -1}, {Kind: "hl7v2", ControlNumber: 1_000_000_000},
	}
	for _, date := range []string{"2026-02-29", "2026-1-01", "1999-12-31", "2100-01-01", "2026-01-01\n"} {
		cases = append(cases, GenerateOptions{Kind: "837p", Date: date})
	}
	for _, opts := range cases {
		var b bytes.Buffer
		if err := Generate(&b, opts); err == nil {
			t.Errorf("expected error for %+v", opts)
		}
		if b.Len() != 0 {
			t.Errorf("invalid options wrote %d bytes: %+v", b.Len(), opts)
		}
	}
}

type fixtureFailureWriter struct {
	err   error
	calls int
}

func (w *fixtureFailureWriter) Write([]byte) (int, error) {
	w.calls++
	return 0, w.err
}

func TestGenerateWriterErrors(t *testing.T) {
	broken := errors.New("output failed")
	for _, kind := range []string{"837p", "hl7v2"} {
		// One item fails at flush; the maximum stops at the first full buffer.
		// A short write with no error must also fail, never silently truncate.
		for _, count := range []int{1, MaxGenerateCount} {
			for _, cause := range []error{broken, nil} {
				w := &fixtureFailureWriter{err: cause}
				err := Generate(w, GenerateOptions{Kind: kind, Count: count})
				want := cause
				if want == nil {
					want = io.ErrShortWrite
				}
				if !errors.Is(err, want) || w.calls != 1 {
					t.Fatalf("kind=%s count=%d: error=%v calls=%d", kind, count, err, w.calls)
				}
			}
		}
	}
}

func BenchmarkGenerate(b *testing.B) {
	for _, kind := range []string{"837p", "hl7v2"} {
		b.Run(kind, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if err := Generate(io.Discard, GenerateOptions{Kind: kind, Count: 1000}); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
