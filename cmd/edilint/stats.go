package main

import (
	"io"
	"strings"

	"github.com/crb2nu/edilint"
)

// runStats implements "edilint stats": a census of one or more interchange
// files. It is a report, not a gate, so it exits 0 whatever the files contain
// and 2 only when it could not do its job.
func runStats(args []string, stdout, stderr io.Writer) int {
	var jsonOut, help bool
	var files []string

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
		name, _, hasInline := strings.Cut(arg, "=")
		if hasInline {
			diagf(stderr, "edilint: stats: %s does not take a value\n", name)
			return exitUsage
		}
		switch name {
		case "-h", "--help":
			help = true
		case "--json":
			jsonOut = true
		default:
			diagf(stderr, "edilint: stats: unknown flag: %s\n", name)
			diagf(stderr, "Try 'edilint stats --help' for usage.\n")
			return exitUsage
		}
	}

	if help {
		printStatsUsage(stdout)
		return exitClean
	}
	if len(files) == 0 {
		diagf(stderr, "edilint: stats: no input files\n")
		diagf(stderr, "Try 'edilint stats --help' for usage.\n")
		return exitUsage
	}

	// An unusable file does not discard the work already done, mirroring the
	// linting path: every readable file is reported and the run exits 2.
	sr := edilint.NewStatsReport()
	paths := dedupe(files)
	unusable := 0
	for _, path := range paths {
		data, err := readInput(path)
		if err != nil {
			diagf(stderr, "edilint: %v\n", err)
			unusable++
			continue
		}
		fs, err := edilint.Stats(path, data)
		if err != nil {
			diagf(stderr, "edilint: %v\n", err)
			unusable++
			continue
		}
		sr.Add(fs)
	}

	var err error
	if jsonOut {
		err = sr.WriteJSON(stdout)
	} else {
		err = sr.WriteText(stdout)
	}
	if err != nil {
		diagf(stderr, "edilint: %v\n", err)
		return exitUsage
	}

	if unusable > 0 {
		diagf(stderr, "edilint: %d of %d input(s) could not be read\n", unusable, len(paths))
		return exitUsage
	}
	return exitClean
}

func printStatsUsage(w io.Writer) {
	diagf(w, `edilint stats - census of interchange files

Usage:
  edilint stats [--json] <file>...

Reports what each file contains: record counts and a record histogram for any
format, and for X12 the envelope census — interchange, functional group and
transaction set counts by type, control-number ranges (ISA13, GS06, ST02),
envelope date ranges (ISA09, GS04), the declared separators, and the narrowest
X12 character-set profile that admits every character observed. Use "-" to
read standard input.

Exit status:
  0  the census was produced
  2  usage error, or a file could not be read

Flags:
      --json    write the versioned JSON document instead of text
  -h, --help    print this help and exit

Examples:
  # What is in this batch before it goes out?
  edilint stats outbound/*.x12

  # Machine-readable census of one file.
  edilint stats --json claims.x12
`)
}
