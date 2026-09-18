package edilint

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Run the 2 GiB acceptance explicitly with make stream-check. The ordinary
// suite uses 8 MiB, exercising the same path on every supported CI platform.
func TestStreamMemoryBound(t *testing.T) {
	testStreamMemoryBound(t, false)
}

func TestStatsStreamMemoryBound(t *testing.T) {
	testStreamMemoryBound(t, true)
}

func testStreamMemoryBound(t *testing.T, stats bool) {
	t.Helper()
	const heapBudget = 128 << 20
	target := int64(8 << 20)
	if os.Getenv("EDILINT_STREAM_ACCEPTANCE") == "1" {
		target = 2 << 30
	}
	segment := []byte("NTE*ADD*" + strings.Repeat("A", 4096) + "~\n")
	count := (target + int64(len(segment)) - 1) / int64(len(segment))
	f, err := os.Create(filepath.Join(t.TempDir(), "large.x12"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err = fmt.Fprint(f, interchangeHeader()); err != nil {
		t.Fatal(err)
	}
	chunk := bytes.Repeat(segment, 1024)
	for left := count; left > 0; {
		n := min(left, 1024)
		if _, err = f.Write(chunk[:int(n)*len(segment)]); err != nil {
			t.Fatal(err)
		}
		left -= n
	}
	if _, err = fmt.Fprintf(f, "SE*%d*0001~\nGE*1*1~\nIEA*1*000000001~\n", count+2); err != nil {
		t.Fatal(err)
	}
	if _, err = f.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	chunk = nil
	runtime.GC()
	r := &heapGuardReader{File: f, limit: heapBudget}
	if stats {
		fs, statsErr := StatsReader("large.x12", r, StreamLimits{})
		if statsErr != nil {
			t.Fatal(statsErr)
		}
		if fs.Records != int(count)+6 || fs.RecordsByID["NTE"] != int(count) ||
			fs.Envelope.Interchanges != 1 || fs.Envelope.Groups != 1 || fs.Envelope.Transactions != 1 {
			t.Fatalf("incorrect census: %+v", fs)
		}
	} else {
		rep, lintErr := LintReader("large.x12", r, Options{})
		if lintErr != nil {
			t.Fatal(lintErr)
		}
		requireClean(t, rep)
	}
	info, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("stats=%t: analyzed %d bytes, %d content segments; peak sampled heap %d bytes (budget %d)", stats, info.Size(), count, r.peak, heapBudget)
}

func interchangeHeader() string {
	return isa("000000001", "260101", "1200") + "\nGS*HP*SENDER*RECEIVER*20260101*1200*1*X*005010X221A1~\nST*835*0001~\n"
}

// Check both read interfaces: a regression to ReadAll must fail before it can
// grow to the input's full size. Sampling is synchronous, so no monitor leaks.
type heapGuardReader struct {
	*os.File
	limit, peak uint64
}

func (r *heapGuardReader) checkHeap() error {
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	r.peak = max(r.peak, stats.HeapAlloc)
	if r.peak > r.limit {
		return fmt.Errorf("stream heap %d exceeds budget %d", r.peak, r.limit)
	}
	return nil
}

func (r *heapGuardReader) Read(p []byte) (int, error) {
	if err := r.checkHeap(); err != nil {
		return 0, err
	}
	return r.File.Read(p)
}

func (r *heapGuardReader) ReadAt(p []byte, off int64) (int, error) {
	if err := r.checkHeap(); err != nil {
		return 0, err
	}
	return r.File.ReadAt(p, off)
}
