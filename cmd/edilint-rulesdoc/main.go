// Command edilint-rulesdoc regenerates docs/rules from the rule catalog.
//
// It writes one page per rule plus an index, and removes pages left behind by
// rules that no longer exist, so the tree it produces is exactly the tree the
// drift guard in internal/rulesdoc expects.
//
// Usage:
//
//	edilint-rulesdoc [dir]
//
// dir defaults to docs/rules, so the command is normally run from the module
// root as `make rulesdoc`.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/crb2nu/edilint"
	"github.com/crb2nu/edilint/internal/rulesdoc"
)

func main() {
	dir := "docs/rules"
	switch len(os.Args) {
	case 1:
	case 2:
		dir = os.Args[1]
	default:
		fail(fmt.Errorf("usage: edilint-rulesdoc [dir]"))
	}
	if err := generate(dir); err != nil {
		fail(err)
	}
}

func generate(dir string) error {
	files := rulesdoc.Files(edilint.Rules())
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for name, contents := range files {
		if err := os.WriteFile(filepath.Join(dir, name), contents, 0o644); err != nil {
			return err
		}
	}
	// A withdrawn rule leaves its page behind, which the drift guard reports as
	// an extra file. Remove it here so regenerating always reconciles the tree.
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if _, ok := files[entry.Name()]; ok {
			continue
		}
		if err := os.Remove(filepath.Join(dir, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "edilint-rulesdoc:", err)
	os.Exit(1)
}
