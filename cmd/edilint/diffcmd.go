package main

import (
	"io"
	"strings"

	"github.com/crb2nu/edilint"
)

// runDiff implements "edilint diff": a structural, element-level comparison of
// two X12 files. It mirrors the linter's exit-code contract: 0 when the files
// are structurally identical, 1 when they differ, 2 when the comparison could
// not be made.
func runDiff(args []string, stdout, stderr io.Writer) int {
	var opts edilint.DiffOptions
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
			diagf(stderr, "edilint: diff: %s does not take a value\n", name)
			return exitUsage
		}
		switch name {
		case "-h", "--help":
			help = true
		case "--strict":
			opts.Strict = true
		case "--json":
			jsonOut = true
		default:
			diagf(stderr, "edilint: diff: unknown flag: %s\n", name)
			diagf(stderr, "Try 'edilint diff --help' for usage.\n")
			return exitUsage
		}
	}

	if help {
		printDiffUsage(stdout)
		return exitClean
	}
	if len(files) != 2 {
		diagf(stderr, "edilint: diff requires exactly two files, got %d\n", len(files))
		diagf(stderr, "Try 'edilint diff --help' for usage.\n")
		return exitUsage
	}
	if files[0] == "-" && files[1] == "-" {
		diagf(stderr, "edilint: diff can read standard input for at most one of the two files\n")
		return exitUsage
	}

	aData, err := readInput(files[0])
	if err != nil {
		diagf(stderr, "edilint: %v\n", err)
		return exitUsage
	}
	bData, err := readInput(files[1])
	if err != nil {
		diagf(stderr, "edilint: %v\n", err)
		return exitUsage
	}

	rep, err := edilint.DiffX12(files[0], aData, files[1], bData, opts)
	if err != nil {
		diagf(stderr, "edilint: %v\n", err)
		return exitUsage
	}

	if jsonOut {
		err = rep.WriteJSON(stdout)
	} else {
		err = rep.WriteText(stdout)
	}
	if err != nil {
		diagf(stderr, "edilint: %v\n", err)
		return exitUsage
	}

	if !rep.Identical {
		return exitFindings
	}
	return exitClean
}

func printDiffUsage(w io.Writer) {
	diagf(w, `edilint diff - structural comparison of two X12 files

Usage:
  edilint diff [--strict] [--json] <a> <b>

Compares two X12 interchanges element by element, aligning segments by their
position within the envelope hierarchy — interchange, functional group,
transaction set — never by byte offset. Cosmetic differences in terminator
style, whitespace after segment terminators and trailing whitespace inside
elements are ignored unless --strict. Use "-" to read one of the two inputs
from standard input.

Exit status:
  0  the files are structurally identical
  1  at least one difference
  2  usage error, a file could not be read, or an input is not X12

Flags:
      --strict  also report cosmetic differences
      --json    write the versioned JSON document instead of text
  -h, --help    print this help and exit

Examples:
  # Compare what two systems generated for the same remittance.
  edilint diff ours.x12 theirs.x12

  # Byte-pedantic comparison, machine-readable.
  edilint diff --strict --json ours.x12 theirs.x12
`)
}
