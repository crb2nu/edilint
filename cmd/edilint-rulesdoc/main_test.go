package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/crb2nu/edilint"
)

func TestGenerateWritesOnePagePerRulePlusIndex(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "rules")
	if err := generate(dir); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(entries), len(edilint.Rules())+1; got != want {
		t.Fatalf("generated %d files, want %d", got, want)
	}
	for _, name := range []string{"README.md", edilint.Rules()[0].ID + ".md"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("%s was not written: %v", name, err)
		}
	}
}

// A withdrawn rule must not leave its page behind, or the drift guard reports
// an extra file that regenerating never clears.
func TestGenerateRemovesPagesNoRuleClaims(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "rules")
	if err := generate(dir); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(dir, "EL9999.md")
	if err := os.WriteFile(stale, []byte("# EL9999\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := generate(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("stale page survived regeneration: %v", err)
	}
}

func TestGenerateIsIdempotent(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "rules")
	if err := generate(dir); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(filepath.Join(dir, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	if err = generate(dir); err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(filepath.Join(dir, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Error("regenerating changed the index")
	}
}
