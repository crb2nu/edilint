package main

import (
	"io"
	"strconv"
	"strings"

	"github.com/crb2nu/edilint"
)

// runStats implements "edilint stats": a census of one or more interchange
// files. Text with lint defects still receives a census; operational failures
// (including binary input and resource limits) exit 2.
func runStats(args []string, stdout, stderr io.Writer) int {
	var jsonOut, help bool
	var files []string
	var limits edilint.StreamLimits

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
		if name == "--max-record-bytes" || name == "--max-state-entries" || name == "--max-state-bytes" {
			if !hasInline && i+1 < len(args) {
				i++
				val = args[i]
			}
			n, err := strconv.Atoi(val)
			if err != nil || n < 0 {
				diagf(stderr, "edilint: stats: %s must be a non-negative integer, got %q\n", name, val)
				return exitUsage
			}
			switch name {
			case "--max-record-bytes":
				limits.MaxRecordBytes = n
			case "--max-state-entries":
				limits.MaxStateEntries = n
			case "--max-state-bytes":
				limits.MaxStateBytes = n
			}
			continue
		}
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
		fs, err := edilint.StatsFile(path, limits)
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
  edilint stats [options] <file>...

Reports what each file contains: record counts and a record histogram for any
format, and for X12 the envelope census — interchange, functional group and
transaction set counts by type, control-number ranges (ISA13, GS06, ST02),
envelope date ranges (ISA09, GS04), the declared separators, and the narrowest
X12 character-set profile that admits every character observed. Use "-" to
read standard input. Files use bounded record and index storage; pipes are
spooled to a private temporary file, removed when analysis finishes.

Exit status:
  0  the census was produced
  2  usage error, a file could not be analyzed, or a resource limit was exceeded

Flags:
      --json                   write the versioned JSON document instead of text
      --max-record-bytes <n>    record/padding limit (default 1048576)
      --max-state-entries <n>   distinct keys per index (default 100000)
      --max-state-bytes <n>     bytes per index/ranges (default 16777216)
  -h, --help                   print this help and exit

Limit values must be non-negative; zero selects the default. A failed file has
no partial census; other readable files are still reported.

Examples:
  # What is in this batch before it goes out?
  edilint stats outbound/*.x12

  # Machine-readable census of one file.
  edilint stats --json claims.x12
`)
}
