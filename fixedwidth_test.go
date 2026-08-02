package edilint

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// remitLayout mirrors testdata/remit_layout.json: 3+12+16+10+8 = 49 characters.
func remitLayout() *Layout {
	return &Layout{
		Name: "remittance-detail",
		Fields: []LayoutField{
			{Name: "record_type", Width: 3},
			{Name: "member_id", Width: 12, Pad: PadRight},
			{Name: "last_name", Width: 16, Pad: PadRight},
			{Name: "paid_amount", Width: 10, Pad: PadLeft, PadChar: "0"},
			{Name: "paid_date", Width: 8},
		},
	}
}

func TestLayoutValidate(t *testing.T) {
	tests := []struct {
		name    string
		layout  Layout
		wantErr string
	}{
		{
			name:   "valid",
			layout: *remitLayout(),
		},
		{
			name:    "no fields",
			layout:  Layout{},
			wantErr: "no fields",
		},
		{
			name:    "unnamed field",
			layout:  Layout{Fields: []LayoutField{{Width: 3}}},
			wantErr: "has no name",
		},
		{
			name:    "zero width",
			layout:  Layout{Fields: []LayoutField{{Name: "a", Width: 0}}},
			wantErr: "widths must be at least 1",
		},
		{
			name:    "unknown pad side",
			layout:  Layout{Fields: []LayoutField{{Name: "a", Width: 3, Pad: "middle"}}},
			wantErr: `want "left" or "right"`,
		},
		{
			name:    "multi-character pad",
			layout:  Layout{Fields: []LayoutField{{Name: "a", Width: 3, PadChar: "ab"}}},
			wantErr: "single character",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.layout.Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected an error containing %q", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error = %q, want it to contain %q", err, tc.wantErr)
			}
		})
	}
}

func TestLoadLayout(t *testing.T) {
	t.Run("reads the committed fixture", func(t *testing.T) {
		l, err := LoadLayout(filepath.Join("testdata", "remit_layout.json"))
		if err != nil {
			t.Fatalf("LoadLayout: %v", err)
		}
		if got := l.RecordLength(); got != 49 {
			t.Errorf("record length = %d, want 49", got)
		}
		if len(l.Fields) != 5 {
			t.Errorf("fields = %d, want 5", len(l.Fields))
		}
	})

	t.Run("rejects unknown keys", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "layout.json")
		body := `{"fields":[{"name":"a","width":3,"padding":"right"}]}`
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatalf("write layout: %v", err)
		}
		if _, err := LoadLayout(path); err == nil {
			t.Fatal("expected an error for the misspelled key")
		}
	})

	t.Run("reports a missing file", func(t *testing.T) {
		if _, err := LoadLayout(filepath.Join(t.TempDir(), "absent.json")); err == nil {
			t.Fatal("expected an error for a missing layout")
		}
	})
}

func TestFixedWidthChecks(t *testing.T) {
	pad := func(s string, n int) string { return s + strings.Repeat(" ", n-len(s)) }
	good := "DTL" + pad("NGH900000001", 12) + pad("RIVERA", 16) + "0000144000" + "20260116"

	tests := []struct {
		name        string
		record      string
		wantLength  int
		wantPadding int
	}{
		{
			name:   "well-formed record",
			record: good,
		},
		{
			name:       "one character short",
			record:     good[:len(good)-1],
			wantLength: 1,
		},
		{
			name:       "one character long",
			record:     good + "X",
			wantLength: 1,
		},
		{
			name:        "surname right-aligned instead of left",
			record:      "DTL" + pad("NGH900000001", 12) + strings.Repeat(" ", 10) + "RIVERA" + "0000144000" + "20260116",
			wantPadding: 1,
		},
		{
			name:        "amount space-padded instead of zero-padded",
			record:      "DTL" + pad("NGH900000001", 12) + pad("RIVERA", 16) + "    144000" + "20260116",
			wantPadding: 1,
		},
		{
			name:   "empty optional field is not padding drift",
			record: "DTL" + pad("NGH900000001", 12) + strings.Repeat(" ", 16) + "0000144000" + "20260116",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rep := Lint("test", []byte(tc.record+"\n"), Options{
				Format:   FormatFixed,
				Layout:   remitLayout(),
				Disabled: []string{ClassCharset, ClassTerminator},
			})
			requireRule(t, rep, RuleLayoutLength, tc.wantLength)
			requireRule(t, rep, RuleLayoutPadding, tc.wantPadding)
		})
	}
}

func TestFixedWidthLengthFindingNamesTheField(t *testing.T) {
	rep := Lint("test", []byte("DTLNGH900000001RIVERA\n"), Options{
		Format:   FormatFixed,
		Layout:   remitLayout(),
		Disabled: []string{ClassCharset, ClassTerminator},
	})
	f := firstOf(rep, RuleLayoutLength)
	if f == nil {
		t.Fatalf("expected a length finding, got %v", ruleNames(rep))
	}
	if !strings.Contains(f.Message, `field "last_name"`) {
		t.Errorf("message should name the field where the record runs out, got %q", f.Message)
	}
	if f.Expected != "49" {
		t.Errorf("expected = %q, want 49", f.Expected)
	}
}

func TestFixedWidthFixtures(t *testing.T) {
	layout, err := LoadLayout(filepath.Join("testdata", "remit_layout.json"))
	if err != nil {
		t.Fatalf("LoadLayout: %v", err)
	}
	opts := Options{Format: FormatFixed, Layout: layout}

	t.Run("clean", func(t *testing.T) {
		requireClean(t, lintFixture(t, "remit_clean.txt", opts))
	})

	t.Run("drift", func(t *testing.T) {
		rep := lintFixture(t, "remit_drift.txt", opts)
		requireRule(t, rep, RuleLayoutPadding, 2)
		requireRule(t, rep, RuleLayoutLength, 1)
	})
}

func TestLayoutSplitToleratesShortRecords(t *testing.T) {
	l := remitLayout()
	got := l.split("DTLNGH9000")
	if len(got) != 5 {
		t.Fatalf("split returned %d fields, want 5", len(got))
	}
	if got[0] != "DTL" {
		t.Errorf("field 1 = %q, want DTL", got[0])
	}
	if got[1] != "NGH9000" {
		t.Errorf("field 2 = %q, want the truncated remainder", got[1])
	}
	if got[4] != "" {
		t.Errorf("field 5 = %q, want empty", got[4])
	}
}
