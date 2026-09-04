// The fmt and fix subcommands. fmt rewrites X12 and HL7v2 files into the
// canonical one-segment-per-line layout; fix applies mechanical repairs, each
// tied to the lint rule it clears. Both share the lint command's exit
// vocabulary: 0 means nothing to do (or done), 1 means the gate failed —
// a file fmt --check found non-canonical, a repair a dry run found pending —
// and 2 means the command could not do its job.
package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/crb2nu/edilint"
)

// fmtConfig is the parsed command line of the fmt subcommand.
type fmtConfig struct {
	files    []string
	format   edilint.Format
	check    bool
	write    bool
	showHelp bool
}

func runFmt(args []string, stdout, stderr io.Writer) int {
	cfg, err := parseFmtArgs(args)
	if err != nil {
		diagf(stderr, "edilint fmt: %v\n", err)
		diagf(stderr, "Try 'edilint fmt --help' for usage.\n")
		return exitUsage
	}
	if cfg.showHelp {
		printFmtUsage(stdout)
		return exitClean
	}
	if len(cfg.files) == 0 {
		diagf(stderr, "edilint fmt: no input files\n")
		diagf(stderr, "Try 'edilint fmt --help' for usage.\n")
		return exitUsage
	}

	failed := 0
	dirty := 0
	for _, path := range dedupe(cfg.files) {
		data, err := readInput(path)
		if err != nil {
			diagf(stderr, "edilint fmt: %v\n", err)
			failed++
			continue
		}
		out, err := edilint.Canonical(data, cfg.format)
		if err != nil {
			diagf(stderr, "edilint fmt: %s: %v\n", path, err)
			failed++
			continue
		}

		switch {
		case cfg.check:
			if !bytes.Equal(data, out) {
				diagf(stdout, "%s\n", path)
				dirty++
			}
		case cfg.write:
			if !bytes.Equal(data, out) {
				if err := os.WriteFile(path, out, 0o600); err != nil {
					diagf(stderr, "edilint fmt: write %s: %v\n", path, err)
					failed++
				}
			}
		default:
			if _, err := stdout.Write(out); err != nil {
				diagf(stderr, "edilint fmt: %v\n", err)
				return exitUsage
			}
		}
	}

	switch {
	case failed > 0:
		return exitUsage
	case cfg.check && dirty > 0:
		return exitFindings
	default:
		return exitClean
	}
}

// fixConfig is the parsed command line of the fix subcommand.
type fixConfig struct {
	files    []string
	format   edilint.Format
	write    bool
	dryRun   bool
	unsafe   bool
	showHelp bool
}

func runFix(args []string, stdout, stderr io.Writer) int {
	cfg, err := parseFixArgs(args)
	if err != nil {
		diagf(stderr, "edilint fix: %v\n", err)
		diagf(stderr, "Try 'edilint fix --help' for usage.\n")
		return exitUsage
	}
	if cfg.showHelp {
		printFixUsage(stdout)
		return exitClean
	}
	if len(cfg.files) == 0 {
		diagf(stderr, "edilint fix: no input files\n")
		diagf(stderr, "Try 'edilint fix --help' for usage.\n")
		return exitUsage
	}

	failed := 0
	pending := 0
	applied := 0
	touched := 0
	for _, path := range dedupe(cfg.files) {
		data, err := readInput(path)
		if err != nil {
			diagf(stderr, "edilint fix: %v\n", err)
			failed++
			continue
		}
		out, repairs := edilint.Fix(data, edilint.FixOptions{Format: cfg.format, Unsafe: cfg.unsafe})
		if len(repairs) == 0 {
			continue
		}
		touched++

		unsafeApplied := false
		for _, r := range repairs {
			diagf(stderr, "%s:%d: [%s %s] %s\n", path, r.Line, r.ID, r.Rule, r.Message)
			if r.Unsafe {
				unsafeApplied = true
			}
		}

		if !cfg.write {
			pending += len(repairs)
			if _, err := io.WriteString(stdout, edilint.UnifiedDiff(path, data, out)); err != nil {
				diagf(stderr, "edilint fix: %v\n", err)
				return exitUsage
			}
			continue
		}

		if err := os.WriteFile(path, out, 0o600); err != nil {
			diagf(stderr, "edilint fix: write %s: %v\n", path, err)
			failed++
			continue
		}
		applied += len(repairs)
		// An unsafe repair rewrites content bytes on a visual judgment, so its
		// run always shows the operator the resulting diff, applied or not.
		if unsafeApplied {
			if _, err := io.WriteString(stdout, edilint.UnifiedDiff(path, data, out)); err != nil {
				diagf(stderr, "edilint fix: %v\n", err)
				return exitUsage
			}
		}
	}

	switch {
	case applied > 0:
		diagf(stderr, "edilint fix: applied %d repair(s) in %d file(s)\n", applied, touched)
	case pending > 0:
		diagf(stderr, "edilint fix: %d repair(s) available in %d file(s); re-run with --write to apply\n",
			pending, touched)
	}

	switch {
	case failed > 0:
		return exitUsage
	case pending > 0:
		return exitFindings
	default:
		return exitClean
	}
}

// readInput reads one input path, with "-" meaning standard input, mirroring
// the lint command.
func readInput(path string) ([]byte, error) {
	if path == "-" {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return nil, fmt.Errorf("read stdin: %w", err)
		}
		return data, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return data, nil
}

// parseFmtArgs accepts the fmt subcommand's flags in any position, in both
// "--flag value" and "--flag=value" form, stopping flag processing at "--".
func parseFmtArgs(args []string) (fmtConfig, error) {
	cfg := fmtConfig{format: edilint.FormatAuto}

	files, flags, err := splitSubArgs(args,
		map[string]bool{"-f": true, "--format": true},
		map[string]bool{"-h": true, "--help": true, "--check": true, "--write": true})
	if err != nil {
		return cfg, err
	}
	cfg.files = files

	for _, f := range flags {
		switch f.name {
		case "-h", "--help":
			cfg.showHelp = true
		case "--check":
			cfg.check = true
		case "--write":
			cfg.write = true
		case "-f", "--format":
			format, err := edilint.ParseFormat(f.value)
			if err != nil {
				return cfg, err
			}
			switch format {
			case edilint.FormatAuto, edilint.FormatX12, edilint.FormatHL7v2:
				cfg.format = format
			default:
				return cfg, fmt.Errorf("fmt supports x12 and hl7v2 input, not --format %s", format)
			}
		}
	}

	if cfg.check && cfg.write {
		return cfg, fmt.Errorf("--check and --write cannot be used together: " +
			"--check reports, --write rewrites")
	}
	if cfg.write && containsStdin(cfg.files) {
		return cfg, fmt.Errorf("--write cannot rewrite standard input; " +
			"omit --write to print the canonical form")
	}
	return cfg, nil
}

// parseFixArgs accepts the fix subcommand's flags, in the same forms.
func parseFixArgs(args []string) (fixConfig, error) {
	cfg := fixConfig{format: edilint.FormatAuto}

	files, flags, err := splitSubArgs(args,
		map[string]bool{"-f": true, "--format": true},
		map[string]bool{"-h": true, "--help": true, "--write": true, "--dry-run": true, "--unsafe": true})
	if err != nil {
		return cfg, err
	}
	cfg.files = files

	for _, f := range flags {
		switch f.name {
		case "-h", "--help":
			cfg.showHelp = true
		case "--write":
			cfg.write = true
		case "--dry-run":
			cfg.dryRun = true
		case "--unsafe":
			cfg.unsafe = true
		case "-f", "--format":
			format, err := edilint.ParseFormat(f.value)
			if err != nil {
				return cfg, err
			}
			cfg.format = format
		}
	}

	if cfg.write && cfg.dryRun {
		return cfg, fmt.Errorf("--dry-run and --write cannot be used together: " +
			"a dry run is the default, --write applies it")
	}
	if cfg.write && containsStdin(cfg.files) {
		return cfg, fmt.Errorf("--write cannot rewrite standard input; " +
			"omit --write to print the diff")
	}
	return cfg, nil
}

// subFlag is one parsed subcommand flag.
type subFlag struct {
	name  string
	value string
}

// splitSubArgs separates a subcommand's arguments into files and flags, with
// the same conventions as the lint command line: flags anywhere, both value
// syntaxes, "-" as a file, and "--" ending flag processing.
func splitSubArgs(args []string, valueFlags, boolFlags map[string]bool) (files []string, flags []subFlag, err error) {
	for i := 0; i < len(args); i++ {
		arg := args[i]

		if arg == "--" {
			files = append(files, args[i+1:]...)
			break
		}
		if arg == "-" || !strings.HasPrefix(arg, "-") {
			files = append(files, arg)
			continue
		}

		name, val, hasInline := strings.Cut(arg, "=")
		if !valueFlags[name] && !boolFlags[name] {
			return nil, nil, fmt.Errorf("unknown flag: %s", name)
		}
		switch {
		case valueFlags[name]:
			if !hasInline {
				if i+1 >= len(args) {
					return nil, nil, fmt.Errorf("%s requires a value", name)
				}
				i++
				val = args[i]
			}
		case hasInline:
			return nil, nil, fmt.Errorf("%s does not take a value", name)
		}
		flags = append(flags, subFlag{name: name, value: val})
	}
	return files, flags, nil
}

func containsStdin(files []string) bool {
	for _, f := range files {
		if f == "-" {
			return true
		}
	}
	return false
}

func printFmtUsage(w io.Writer) {
	diagf(w, `edilint fmt - canonical layout for X12 and HL7v2 files

Usage:
  edilint fmt [flags] <file>...

Rewrites an X12 interchange or an HL7v2 message or batch file into the
canonical layout: one segment per line, each closed by its terminator and a
single LF, whitespace between records normalized away. The bytes inside a
record are never changed, so formatting a file cannot alter what it says —
and cannot repair it either; that is 'edilint fix'. Formatting is idempotent:
a canonical file passes through unchanged.

By default the canonical form is printed to standard output. Use "-" to read
standard input.

Exit status:
  0  done; with --check, every file was already canonical
  1  --check found a file that is not canonical
  2  usage error, unsupported input, or a file could not be read or written

Flags:
      --check           print the name of each file that is not canonical and
                        exit 1 if there were any; write nothing
      --write           rewrite each file in place instead of printing
  -f, --format <name>   auto (default), x12, hl7v2
  -h, --help            print this help and exit

Examples:
  # Print the canonical form of an interchange.
  edilint fmt claims.x12

  # Gate a CI job on canonical layout.
  edilint fmt --check outbound/*.x12

  # Rewrite in place.
  edilint fmt --write outbound/*.x12
`)
}

func printFixUsage(w io.Writer) {
	diagf(w, `edilint fix - mechanical repairs tied to lint rules

Usage:
  edilint fix [flags] <file>...

Applies the repairs whose correct form the file itself determines, each tied
to the rule it clears:

  EL1001  strip a UTF-8 byte order mark
  EL2001  rewrite minority line terminators to the file's dominant style
  EL2002  append the missing final record terminator
  EL2003  append the declared X12 segment terminator a trailing segment lost
  EL2004  rewrite minority inter-segment whitespace to the dominant style
  EL3006  rewrite SE01 to the recounted segment total
  EL3007  rewrite GE01 to the recounted transaction set total
  EL3008  rewrite IEA01 to the recounted functional group total
  EL3010  zero-pad an ISA10 or GS05 time one leading zero short of valid
  EL6003  rewrite BTS-1 to the recounted message total
  EL6004  rewrite FTS-1 to the recounted batch total

--unsafe adds one more:

  EL1005  replace Unicode lookalike characters with the ASCII they imitate

The default is a dry run: each pending repair is described on standard error
and the resulting change is printed as a unified diff. --write applies the
same changes to the files. Unsafe repairs print their diff even with --write,
so a content substitution is never invisible.

Exit status:
  0  nothing to repair; with --write, all repairs were applied
  1  a dry run found repairs pending
  2  usage error, or a file could not be read or written

Flags:
      --write           apply the repairs in place
      --dry-run         print what would change without applying it (default)
      --unsafe          add the homoglyph substitution tier
  -f, --format <name>   auto (default), x12, hl7v2, edifact, delimited, fixed, text
  -h, --help            print this help and exit

Examples:
  # See what would change.
  edilint fix claims.x12

  # Apply the safe repairs.
  edilint fix --write claims.x12

  # Substitute homoglyphs too, reviewing the diff it prints.
  edilint fix --write --unsafe claims.x12
`)
}
