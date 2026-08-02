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

// config is the parsed command line. Flag values live in opts and in the fields
// beside it; set records which of them the command line actually named, so that
// a configuration file can supply the rest without overwriting anything.
type config struct {
	files         []string
	opts          edilint.Options
	layoutPath    string
	configPath    string
	noConfig      bool
	jsonOut       bool
	verbose       bool
	allowWarnings bool
	listRules     bool
	showVersion   bool
	showHelp      bool
	set           map[string]bool
}

// settings are the options for one run, after the configuration file and the
// command line have been layered.
type settings struct {
	opts          edilint.Options
	layoutPath    string
	allowWarnings bool
	configPath    string
}

func run(args []string, stdout, stderr io.Writer) int {
	cfg, parseErr := parseArgs(args)
	if parseErr != nil {
		diagf(stderr, "edilint: %v\n", parseErr)
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

	set, err := resolve(cfg)
	if err != nil {
		diagf(stderr, "edilint: %v\n", err)
		return exitUsage
	}
	if set.configPath != "" && cfg.verbose {
		diagf(stderr, "edilint: using config %s\n", set.configPath)
	}

	if len(cfg.files) == 0 {
		diagf(stderr, "edilint: no input files\n")
		diagf(stderr, "Try 'edilint --help' for usage.\n")
		return exitUsage
	}

	if set.layoutPath != "" {
		layout, err := edilint.LoadLayout(set.layoutPath)
		if err != nil {
			diagf(stderr, "edilint: %v\n", err)
			return exitUsage
		}
		set.opts.Layout = layout
	}
	if set.opts.Format == edilint.FormatFixed && set.opts.Layout == nil {
		diagf(stderr, "edilint: --format fixed requires --layout\n")
		return exitUsage
	}

	// Sharing this map across files enables duplicate ISA13 detection for a batch.
	set.opts.SeenISA13 = map[string]string{}

	// An unreadable file does not discard the work already done. Every readable
	// file is still reported, and the run exits 2 at the end, so a glob that
	// races with a file being moved still tells the operator what it found.
	rr := edilint.NewRunReport()
	paths := dedupe(cfg.files)
	unreadable := 0
	for _, path := range paths {
		rep, err := edilint.LintFile(path, set.opts)
		if err != nil {
			diagf(stderr, "edilint: %v\n", err)
			unreadable++
			continue
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

	if unreadable > 0 {
		diagf(stderr, "edilint: %d of %d input(s) could not be read\n", unreadable, len(paths))
		return exitUsage
	}

	failOn := edilint.SeverityWarning
	if set.allowWarnings {
		failOn = edilint.SeverityError
	}
	if !rr.OK(failOn) {
		return exitFindings
	}
	return exitClean
}

// resolve layers the configuration file under the command line.
func resolve(cfg config) (settings, error) {
	set := settings{
		opts:          edilint.Options{Format: edilint.FormatAuto},
		layoutPath:    cfg.layoutPath,
		allowWarnings: cfg.allowWarnings,
	}

	conf, err := loadConfig(cfg)
	if err != nil {
		return set, err
	}
	if conf != nil {
		set.configPath = conf.Path
		conf.Apply(&set.opts)
		if conf.Has("allow-warnings") && conf.AllowWarnings {
			set.allowWarnings = true
		}
		if conf.Layout != "" && cfg.layoutPath == "" {
			set.layoutPath = conf.Layout
		}
	}

	// The command line now overwrites whatever the file said, except for the two
	// cumulative settings, which the file has already contributed to.
	if cfg.set["format"] {
		set.opts.Format = cfg.opts.Format
	}
	if cfg.set["delimiter"] {
		set.opts.Delimiter = cfg.opts.Delimiter
	}
	if cfg.set["charset"] {
		set.opts.X12Charset = cfg.opts.X12Charset
	}
	if cfg.set["type-field"] {
		set.opts.TypeField = cfg.opts.TypeField
	}
	if cfg.set["max-findings"] {
		set.opts.MaxFindings = cfg.opts.MaxFindings
	}
	set.opts.Disabled = append(set.opts.Disabled, cfg.opts.Disabled...)
	set.opts.CountRules = append(set.opts.CountRules, cfg.opts.CountRules...)

	// A misspelled rule that silently suppresses nothing is the worst outcome a
	// suppression can have, so the command line rejects one.
	if err := edilint.ValidateSelectors(cfg.opts.Disabled); err != nil {
		return set, fmt.Errorf("--disable: %w", err)
	}
	return set, nil
}

// loadConfig returns the configuration file in force, or nil when there is none.
func loadConfig(cfg config) (*edilint.Config, error) {
	if cfg.noConfig {
		return nil, nil
	}
	path := cfg.configPath
	if path == "" {
		// An explicit --config must exist; a discovered one is optional.
		if path = edilint.FindConfig("."); path == "" {
			return nil, nil
		}
	}
	return edilint.LoadConfig(path)
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
	"--config": true,
}

// boolFlags lists the flags that take no value.
var boolFlags = map[string]bool{
	"-h": true, "--help": true, "--version": true, "--list-rules": true,
	"--json": true, "-v": true, "--verbose": true, "--allow-warnings": true,
	"--no-config": true,
}

// knownFlags is every recognized flag name.
var knownFlags = func() map[string]bool {
	all := make(map[string]bool, len(valueFlags)+len(boolFlags))
	for name := range valueFlags {
		all[name] = true
	}
	for name := range boolFlags {
		all[name] = true
	}
	return all
}()

// parseArgs accepts flags in any position, in both "--flag value" and
// "--flag=value" form, and stops flag processing at "--".
func parseArgs(args []string) (config, error) {
	cfg := config{set: map[string]bool{}}
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

		// Reject an unknown name before complaining about its syntax, so that
		// "--bogus=1" reports the unknown flag rather than sending the reader
		// looking for the right value syntax for a flag that does not exist.
		if !knownFlags[name] {
			return cfg, fmt.Errorf("unknown flag: %s", name)
		}
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
		case "--no-config":
			cfg.noConfig = true

		case "-f", "--format":
			cfg.opts.Format, err = edilint.ParseFormat(val)
			cfg.set["format"] = true

		case "-d", "--delimiter":
			if _, err = edilint.ParseDelimiter(val); err == nil {
				cfg.opts.Delimiter = val
				cfg.set["delimiter"] = true
			}

		case "--layout":
			cfg.layoutPath = val

		case "--config":
			cfg.configPath = val

		case "--charset":
			cfg.opts.X12Charset, err = edilint.ParseCharsetProfile(val)
			cfg.set["charset"] = true

		case "--type-field":
			n, convErr := strconv.Atoi(val)
			if convErr != nil || n < 1 {
				err = fmt.Errorf("--type-field must be a positive integer (1-based), got %q", val)
			} else {
				cfg.opts.TypeField = n
				cfg.set["type-field"] = true
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
			// Zero and the unset default both mean unlimited.
			n, convErr := strconv.Atoi(val)
			if convErr != nil || n < 0 {
				err = fmt.Errorf("--max-findings must be a non-negative integer, got %q", val)
			} else {
				cfg.opts.MaxFindings = n
				cfg.set["max-findings"] = true
			}

		default:
			err = fmt.Errorf("unknown flag: %s", name)
		}
		if err != nil {
			return cfg, err
		}
	}

	if cfg.noConfig && cfg.configPath != "" {
		return cfg, fmt.Errorf("--config and --no-config cannot be used together")
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
      --charset <name>    X12 character set: extended (default), basic, off
      --type-field <n>    1-based field used as the record-type discriminator
                          for the field-count check (default 1)
      --count-rule <rule> repeatable; recordPrefix:fieldIndex:countedPrefix,
                          e.g. TRL:2:DTL means "field 2 of TRL records declares
                          how many DTL records exist". Field 1 is the record type.
      --disable <rules>   comma-separated rule identifiers, names or classes,
                          e.g. --disable EL1006,layout
      --config <file>     configuration file (default .edilint.yml here, if any)
      --no-config         ignore any .edilint.yml in the working directory
      --max-findings <n>  print at most n findings per file (default unlimited).
                          The exit status always reflects every finding.
      --allow-warnings    exit 0 when only warnings were found
      --json              emit a JSON document instead of diagnostic lines
  -v, --verbose           print a line for clean files too
      --list-rules        print the rule catalog and exit
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
