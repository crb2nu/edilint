package edilint

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

func TestGenerateEdifactEnvelopes(t *testing.T) {
	for _, count := range []int{1, 2, 17, 1000} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			var b bytes.Buffer
			if err := Generate(&b, GenerateOptions{Kind: "edifact", Count: count, Date: "2024-02-29", ControlNumber: 42}); err != nil {
				t.Fatal(err)
			}
			// Inspect raw records independently of the EDIFACT parser so a
			// matching parser/generator error cannot mask a trailer defect.
			lines := strings.Split(strings.TrimSuffix(b.String(), "\n"), "\n")
			if len(lines) != 4*count+3 || lines[0] != "UNA:+.? '" {
				t.Fatalf("bad service string or record count: %q (%d)", lines[0], len(lines))
			}
			unb := strings.Split(strings.TrimSuffix(lines[1], "'"), "+")
			if len(unb) != 12 || unb[1] != "UNOA:3" || unb[4] != "240229:1200" || unb[5] != "000000042" || unb[11] != "1" {
				t.Fatalf("invalid test interchange: %q", lines[1])
			}
			refs, orders := map[string]bool{}, map[string]bool{}
			for i := 0; i < count; i++ {
				start := 2 + 4*i
				unh := strings.Split(strings.TrimSuffix(lines[start], "'"), "+")
				bgm := strings.Split(strings.TrimSuffix(lines[start+1], "'"), "+")
				if len(unh) != 3 || unh[0] != "UNH" || unh[2] != "ORDERS:D:96A:UN" || !strings.HasPrefix(unh[1], "FAKE") || refs[unh[1]] {
					t.Fatalf("invalid or repeated message header: %q", lines[start])
				}
				if len(bgm) != 4 || bgm[0] != "BGM" || !strings.HasPrefix(bgm[2], "FAKEORDER") || orders[bgm[2]] {
					t.Fatalf("invalid or repeated order identifier: %q", lines[start+1])
				}
				refs[unh[1]], orders[bgm[2]] = true, true
				if lines[start+2] != "DTM+137:202402291200:203'" || lines[start+3] != "UNT+4+"+unh[1]+"'" {
					t.Fatalf("bad message date or trailer: %q", lines[start+2:start+4])
				}
			}
			if lines[len(lines)-1] != fmt.Sprintf("UNZ+%d+000000042'", count) || bytes.Contains(b.Bytes(), []byte("\r")) {
				t.Fatal("invalid interchange trailer or line endings")
			}
		})
	}
}
