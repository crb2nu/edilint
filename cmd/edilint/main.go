// Command edilint checks healthcare interchange files for the defects that
// break downstream parsers: invisible and lookalike characters, inconsistent
// terminators, broken X12 envelopes, and record counts that disagree with the
// records actually present.
//
// It is a pre-send gate. Exit status 0 means the files are clean, 1 means at
// least one finding, and 2 means edilint could not do its job.
package main

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/crb2nu/edilint"
)

// version is overridden at build time with -ldflags "-X main.version=...".
var version = "dev"

// Exit statuses. These are part of the tool's contract with calling scripts.
const (
	exitClean    = 0
	exitFindings = 1
	exitUsage    = 2
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// diagf writes a diagnostic or usage message. A failure to write the message
// itself is not actionable and must not change the exit status, so it is
// discarded deliberately.
func diagf(w io.Writer, format string, args ...any) {
	_, _ = fmt.Fprintf(w, format, args...)
}

// config is the parsed command line.
type config struct {
	files         []string
	opts          edilint.Options
	layoutPath    string
	jsonOut       bool
	verbose       bool
	allowWarnings bool
	listRules     bool
	showVersion   bool
	showHelp      bool
}

func run(args []string, stdout, stderr io.Writer) int {
	cfg, err := parseArgs(args)
	if err != nil {
		diagf(stderr, "edilint: %v\n", err)
		diagf(stderr, "Try 'edilint --help' for usage.\n")
		return exitUsage
	}

	switch {
	case cfg.showHelp:
		printUsage(stdout)
		return exitClean
	case cfg.showVersion:
		diagf(stdout, "edilint %s\n", version)
		return exitClean
	case cfg.listRules:
		if err := edilint.WriteRules(stdout); err != nil {
			diagf(stderr, "edilint: %v\n", err)
			return exitUsage
		}
		return exitClean
	}

	if len(cfg.files) == 0 {
		diagf(stderr, "edilint: no input files\n")
		diagf(stderr, "Try 'edilint --help' for usage.\n")
		return exitUsage
	}

	if cfg.layoutPath != "" {
		layout, err := edilint.LoadLayout(cfg.layoutPath)
		if err != nil {
			diagf(stderr, "edilint: %v\n", err)
			return exitUsage
		}
		cfg.opts.Layout = layout
	}
	if cfg.opts.Format == edilint.FormatFixed && cfg.opts.Layout == nil {
		diagf(stderr, "edilint: --format fixed requires --layout\n")
		return exitUsage
	}

	// Sharing this map across files enables duplicate ISA13 detection for a batch.
	cfg.opts.SeenISA13 = map[string]string{}

	rr := edilint.NewRunReport()
	for _, path := range dedupe(cfg.files) {
		rep, err := edilint.LintFile(path, cfg.opts)
		if err != nil {
			diagf(stderr, "edilint: %v\n", err)
			return exitUsage
		}
		rr.Add(rep)
	}

	if cfg.jsonOut {
		if err := rr.WriteJSON(stdout); err != nil {
			diagf(stderr, "edilint: %v\n", err)
			return exitUsage
		}
	} else if err := rr.WriteText(stdout, cfg.verbose); err != nil {
		diagf(stderr, "edilint: %v\n", err)
		return exitUsage
	}

	failOn := edilint.SeverityWarning
	if cfg.allowWarnings {
		failOn = edilint.SeverityError
	}
	if !rr.OK(failOn) {
		return exitFindings
	}
	return exitClean
}

// dedupe drops repeated paths while preserving order. Overlapping shell globs
// routinely name the same file twice, and linting it twice would both inflate
// the summary and misreport its own interchange control number as a duplicate
// of itself.
func dedupe(paths []string) []string {
	seen := make(map[string]bool, len(paths))
	out := paths[:0:0]
	for _, p := range paths {
		if seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return out
}

// valueFlags lists the flags that consume a value, so that "--flag value" and
// "--flag=value" can both be accepted without guessing.
var valueFlags = map[string]bool{
	"-f": true, "--format": true,
	"-d": true, "--delimiter": true,
	"--layout": true, "--charset": true, "--type-field": true,
	"--count-rule": true, "--disable": true, "--max-findings": true,
}

// parseArgs accepts flags in any position, in both "--flag value" and
// "--flag=value" form, and stops flag processing at "--".
func parseArgs(args []string) (config, error) {
	var cfg config
	cfg.opts.Format = edilint.FormatAuto

	for i := 0; i < len(args); i++ {
		arg := args[i]

		if arg == "--" {
			cfg.files = append(cfg.files, args[i+1:]...)
			break
		}
		if arg == "-" || !strings.HasPrefix(arg, "-") {
			cfg.files = append(cfg.files, arg)
			continue
		}

		name, val, hasInline := strings.Cut(arg, "=")
		switch {
		case valueFlags[name]:
			if !hasInline {
				if i+1 >= len(args) {
					return cfg, fmt.Errorf("%s requires a value", name)
				}
				i++
				val = args[i]
			}
		case hasInline:
			return cfg, fmt.Errorf("%s does not take a value", name)
		}

		var err error
		switch name {
		case "-h", "--help":
			cfg.showHelp = true
		case "--version":
			cfg.showVersion = true
		case "--list-rules":
			cfg.listRules = true
		case "--json":
			cfg.jsonOut = true
		case "-v", "--verbose":
			cfg.verbose = true
		case "--allow-warnings":
			cfg.allowWarnings = true

		case "-f", "--format":
			cfg.opts.Format, err = edilint.ParseFormat(val)

		case "-d", "--delimiter":
			if _, err = edilint.ParseDelimiter(val); err == nil {
				cfg.opts.Delimiter = val
			}

		case "--layout":
			cfg.layoutPath = val

		case "--charset":
			cfg.opts.X12Charset, err = edilint.ParseCharsetProfile(val)

		case "--type-field":
			n, convErr := strconv.Atoi(val)
			if convErr != nil || n < 1 {
				err = fmt.Errorf("--type-field must be a positive integer (1-based), got %q", val)
			} else {
				cfg.opts.TypeField = n
			}

		case "--count-rule":
			var rule edilint.CountRule
			if rule, err = edilint.ParseCountRule(val); err == nil {
				cfg.opts.CountRules = append(cfg.opts.CountRules, rule)
			}

		case "--disable":
			for _, part := range strings.Split(val, ",") {
				if part = strings.TrimSpace(part); part != "" {
					cfg.opts.Disabled = append(cfg.opts.Disabled, part)
				}
			}

		case "--max-findings":
			n, convErr := strconv.Atoi(val)
			switch {
			case convErr != nil || n < 0:
				err = fmt.Errorf("--max-findings must be a non-negative integer, got %q", val)
			case n == 0:
				cfg.opts.MaxFindings = -1 // unlimited
			default:
				cfg.opts.MaxFindings = n
			}

		default:
			err = fmt.Errorf("unknown flag: %s", name)
		}
		if err != nil {
			return cfg, err
		}
	}

	if cfg.opts.Format == edilint.FormatFixed && cfg.layoutPath == "" {
		return cfg, fmt.Errorf("--format fixed requires --layout")
	}
	return cfg, nil
}

func printUsage(w io.Writer) {
	diagf(w, `edilint - pre-send checks for healthcare interchange files

Usage:
  edilint [flags] <file>...

Reads X12 EDI, HL7v2, delimited and fixed-width files and reports the defects
that break downstream parsers. Use "-" to read standard input.

Exit status:
  0  no findings
  1  at least one finding
  2  usage error or a file could not be read

Flags:
  -f, --format <name>     auto (default), x12, hl7v2, delimited, fixed, text
  -d, --delimiter <char>  field delimiter for delimited files; accepts \t, \0, \xNN
      --layout <file>     fixed-width layout JSON; required for --format fixed
      --charset <name>    X12 character set: basic (default), extended, off
      --type-field <n>    1-based field used as the record-type discriminator
                          for the field-count check (default 1)
      --count-rule <rule> repeatable; recordPrefix:fieldIndex:countedPrefix,
                          e.g. TRL:2:DTL means "field 2 of TRL records declares
                          how many DTL records exist". Field 1 is the record type.
      --disable <rules>   comma-separated rule names or classes, e.g.
                          --disable charset.nonascii,layout
      --max-findings <n>  cap findings printed per file (default 200, 0 = all)
      --allow-warnings    exit 0 when only warnings were found
      --json              emit a JSON document instead of diagnostic lines
  -v, --verbose           print a line for clean files too
      --list-rules        print the rule catalogue and exit
      --version           print the version and exit
  -h, --help              print this help and exit

Examples:
  # Gate a send script on a clean interchange.
  edilint outbound/claims.x12 || exit 1

  # Check a pipe-delimited extract whose trailer declares the detail count.
  edilint --count-rule TRL:2:DTL eligibility.psv

  # Check fixed-width records against a layout.
  edilint --format fixed --layout layouts/remit.json remit.txt

  # Machine-readable output for a CI annotation step.
  edilint --json outbound/*.x12 | jq '.files[].findings[]'
`)
}
