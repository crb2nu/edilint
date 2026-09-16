// Command edilint-docs generates the static rule reference from the catalog.
package main

import (
	"bytes"
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/crb2nu/edilint"
)

//go:embed *.tmpl guidance.json style.css
var sources embed.FS

type guidance struct {
	Example        string `json:"example"`
	Fix            string `json:"fix"`
	FalsePositives string `json:"false_positives"`
}

type rulePage struct {
	edilint.RuleDoc
	guidance
	Help string
	URL  string
}

func main() {
	check := flag.Bool("check", false, "fail if generated pages are missing or stale")
	out := flag.String("out", "docs", "output directory")
	flag.Parse()
	if err := run(*out, *check); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func render() (map[string][]byte, error) {
	body, err := sources.ReadFile("guidance.json")
	if err != nil {
		return nil, err
	}
	var notes map[string]guidance
	if err := json.Unmarshal(body, &notes); err != nil {
		return nil, err
	}
	return renderRules(edilint.Rules(), notes)
}

func renderRules(rules []edilint.RuleDoc, notes map[string]guidance) (map[string][]byte, error) {
	tmpl, err := template.ParseFS(sources, "*.tmpl")
	if err != nil {
		return nil, err
	}
	files := map[string][]byte{".nojekyll": {}}
	var pages []rulePage
	for _, rule := range rules {
		note, ok := notes[rule.ID]
		if !ok || strings.TrimSpace(note.Example) == "" || strings.TrimSpace(note.Fix) == "" || strings.TrimSpace(note.FalsePositives) == "" {
			return nil, fmt.Errorf("%s lacks complete example, fix or false-positive guidance", rule.ID)
		}
		page := rulePage{rule, note, edilint.RuleHelp(rule), edilint.RuleURL(rule.ID)}
		var b bytes.Buffer
		if err = tmpl.ExecuteTemplate(&b, "rule.tmpl", page); err != nil {
			return nil, err
		}
		files["rules/"+rule.ID+".html"] = b.Bytes()
		pages = append(pages, page)
	}
	if len(notes) != len(rules) {
		return nil, fmt.Errorf("guidance contains entries outside the rule catalog")
	}
	var index bytes.Buffer
	if err = tmpl.ExecuteTemplate(&index, "index.tmpl", pages); err != nil {
		return nil, err
	}
	files["index.html"] = index.Bytes()
	files["style.css"], err = sources.ReadFile("style.css")
	return files, err
}

func run(out string, check bool) error {
	files, err := render()
	if err != nil {
		return err
	}
	// Stable ordering makes both the first drift error and writes repeatable.
	paths := make([]string, 0, len(files))
	for name := range files {
		paths = append(paths, name)
	}
	sort.Strings(paths)
	for _, name := range paths {
		path := filepath.Join(out, filepath.FromSlash(name))
		if check {
			actual, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(actual, files[name]) {
				return fmt.Errorf("%s is missing or stale; run make docs", path)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		// The static web server must be able to read these public pages.
		if err := os.WriteFile(path, files[name], 0o644); err != nil { //nolint:gosec // G306: public static documentation
			return err
		}
	}
	return nil
}
