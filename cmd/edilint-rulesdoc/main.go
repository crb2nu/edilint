package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/crb2nu/edilint"
	"github.com/crb2nu/edilint/internal/rulesdoc"
)

func main() {
	const dir = "docs/rules"
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fail(err)
	}
	for name, contents := range rulesdoc.Files(edilint.Rules()) {
		if err := os.WriteFile(filepath.Join(dir, name), contents, 0o644); err != nil {
			fail(err)
		}
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "edilint-rulesdoc:", err)
	os.Exit(1)
}
