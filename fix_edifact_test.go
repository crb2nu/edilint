package edilint

import (
	"bytes"
	"slices"
	"strings"
	"testing"
)

func TestFixEdifactCounts(t *testing.T) {
	unb := "UNB+UNOC:3+S+R+260101:1200+I'"
	ung := "UNG+ORDERS+S+R+260101:1200+G+UN+D:96A'"
	msg := func(ref, count string) string {
		return "UNH+" + ref + "+ORDERS:D:96A:UN'FTX+AAI+++KEEP?+THIS?'TOO??'UNT+" + count + "+" + ref + "'"
	}
	custom := strings.NewReplacer(":", ";", "+", "*", "?", "!", "'", "~")
	for _, tc := range []struct {
		name, before, after string
		ids, remaining      []string
	}{
		{"ungrouped", unb + msg("M", "9") + "UNZ+7+I'", unb + msg("M", "3") + "UNZ+1+I'", []string{"EL7003", "EL7005"}, nil},
		{"empty records", unb + "'" + strings.ReplaceAll(msg("M", "9"), "'UNT", "''  'UNT") + "UNZ+1+I'", unb + "'" + strings.ReplaceAll(msg("M", "3"), "'UNT", "''  'UNT") + "UNZ+1+I'", []string{"EL7003"}, nil},
		{"multiple messages", unb + msg("A", "") + msg("B", "abc") + "UNZ+0+I'", unb + msg("A", "3") + msg("B", "3") + "UNZ+2+I'", []string{"EL7003", "EL7003", "EL7005"}, nil},
		{"groups", unb + ung + msg("A", "8") + msg("B", "3") + "UNE+9+G'" + ung + "UNE++G'UNZ+abc+I'", unb + ung + msg("A", "3") + msg("B", "3") + "UNE+2+G'" + ung + "UNE+0+G'UNZ+2+I'", []string{"EL7003", "EL7004", "EL7004", "EL7005"}, nil},
		{"multiple interchanges", unb + msg("A", "3") + "UNZ+9+I'" + unb + ung + msg("B", "0") + "UNE+0+G'UNZ+9+I'", unb + msg("A", "3") + "UNZ+1+I'" + unb + ung + msg("B", "3") + "UNE+1+G'UNZ+1+I'", []string{"EL7005", "EL7003", "EL7004", "EL7005"}, nil},
		{"escaped count and reference", unb + msg("M?+1", "9?+9") + "UNZ+1+I'", unb + msg("M?+1", "3") + "UNZ+1+I'", []string{"EL7003"}, nil},
		{"custom service characters", custom.Replace("UNA:+.? '" + unb + msg("M?+1", "bad?+count") + "UNZ+2+I'"), custom.Replace("UNA:+.? '" + unb + msg("M?+1", "3") + "UNZ+1+I'"), []string{"EL7003", "EL7005"}, nil},
		{"padding", " \nUNA:+.? '\r\n" + unb + " \r\n" + msg("M", "9") + "\t\nUNZ+1+I'\r\n", " \nUNA:+.? '\r\n" + unb + " \r\n" + msg("M", "3") + "\t\nUNZ+1+I'\r\n", []string{"EL7003"}, []string{"EL1003"}},
		{"reference mismatch", unb + msg("M", "3") + "UNZ+9+OTHER'", unb + msg("M", "3") + "UNZ+1+OTHER'", []string{"EL7005"}, []string{"EL7006"}},
		{"padded counts unchanged", unb + msg("M", "003") + "UNZ+001+I'", unb + msg("M", "003") + "UNZ+001+I'", nil, nil},
		{"empty interchange", unb + "UNZ+9+I'", unb + "UNZ+0+I'", []string{"EL7005"}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := []byte(tc.before)
			out, repairs := Fix(input, FixOptions{})
			if string(out) != tc.after || string(input) != tc.before {
				t.Fatalf("output=%q, want=%q; input mutated=%v", out, tc.after, string(input) != tc.before)
			}
			var ids []string
			for _, repair := range repairs {
				ids = append(ids, repair.ID)
				if repair.Line < 1 || repair.Rule == "" || repair.Unsafe || repair.Message == "" {
					t.Fatalf("invalid repair metadata: %+v", repair)
				}
			}
			if !slices.Equal(ids, tc.ids) {
				t.Fatalf("repairs=%v, want=%v", ids, tc.ids)
			}
			var remaining []string
			for _, finding := range Lint("fixed", out, Options{}).Findings {
				remaining = append(remaining, finding.ID)
			}
			if !slices.Equal(remaining, tc.remaining) {
				t.Fatalf("remaining findings=%v, want=%v", remaining, tc.remaining)
			}
			again, more := Fix(out, FixOptions{})
			if !bytes.Equal(again, out) || len(more) != 0 {
				t.Fatalf("non-idempotent repair: %+v", more)
			}
		})
	}
}

func TestFixEdifactMissingCountFieldIsNotInvented(t *testing.T) {
	data := []byte("UNB+UNOC:3+S+R+260101:1200+I'UNH+M+ORDERS:D:96A:UN'UNT'UNZ+1+I'")
	out, repairs := Fix(data, FixOptions{})
	if !bytes.Equal(out, data) || len(repairs) != 0 {
		t.Fatalf("missing field was invented: %q; %+v", out, repairs)
	}
}

func TestFixEdifactDeclinesAmbiguousStructure(t *testing.T) {
	unb := "UNB+UNOC:3+S+R+260101:1200+I'"
	ung := "UNG+ORDERS+S+R+260101:1200+G+UN+D:96A'"
	unh := "UNH+M+ORDERS:D:96A:UN'"
	msg := unh + "BGM+220+FAKE+9'UNT+9+M'"
	for name, data := range map[string]string{
		"no header":               msg + "UNZ+9+I'",
		"unclosed message":        unb + unh + "UNZ+9+I'",
		"unclosed group":          unb + ung + msg + "UNZ+9+I'",
		"unclosed interchange":    unb + msg,
		"unopened message":        unb + "UNT+9+M'UNZ+9+I'",
		"unopened group":          unb + msg + "UNE+9+G'UNZ+9+I'",
		"nested message":          unb + unh + msg + "UNZ+9+I'",
		"nested group":            unb + ung + ung + msg + "UNE+9+G'UNZ+9+I'",
		"nested interchange":      unb + unb + msg + "UNZ+9+I'",
		"group in message":        unb + unh + ung + "UNT+9+M'UNE+9+G'UNZ+9+I'",
		"mixed before group":      unb + msg + ung + msg + "UNE+9+G'UNZ+9+I'",
		"mixed after group":       unb + ung + msg + "UNE+9+G'" + msg + "UNZ+9+I'",
		"truncated trailer":       unb + msg + "UNZ+9+I",
		"payload outside message": unb + "BGM+220+FAKE+9'" + msg + "UNZ+9+I'",
		"trailing data":           unb + msg + "UNZ+9+I'FTX+AAI+++TAIL'",
		"late UNA":                unb + "UNA:+.? '" + msg + "UNZ+9+I'",
		"invalid UNA":             "UNA:+/? '" + unb + msg + "UNZ+9+I'",
		"colliding UNA":           "UNA:++? '" + unb + msg + "UNZ+9+I'",
	} {
		t.Run(name, func(t *testing.T) {
			out, repairs := Fix([]byte(data), FixOptions{Format: FormatEdifact})
			if string(out) != data || len(repairs) != 0 {
				t.Fatalf("ambiguous file was changed: %q; %+v", out, repairs)
			}
		})
	}
}
