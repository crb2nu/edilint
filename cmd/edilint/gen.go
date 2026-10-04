package main

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/crb2nu/edilint"
)

func runGen(args []string, stdout, stderr io.Writer) int {
	opts, help, err := parseGenArgs(args)
	if err != nil {
		diagf(stderr, "edilint: gen: %v\nTry 'edilint gen --help' for usage.\n", err)
		return exitUsage
	}
	if help {
		printGenUsage(stdout)
		return exitClean
	}
	if err := edilint.Generate(stdout, opts); err != nil {
		diagf(stderr, "edilint: %v\n", err)
		return exitUsage
	}
	return exitClean
}

func parseGenArgs(args []string) (edilint.GenerateOptions, bool, error) {
	var opts edilint.GenerateOptions
	var help, endFlags bool
	var claims, messages bool
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" && !endFlags {
			endFlags = true
			continue
		}
		if endFlags || !strings.HasPrefix(arg, "-") {
			if opts.Kind != "" {
				return opts, false, fmt.Errorf("expected one fixture kind, got extra argument %q", arg)
			}
			opts.Kind = arg
			continue
		}
		name, val, inline := strings.Cut(arg, "=")
		switch name {
		case "-h", "--help":
			if inline {
				return opts, false, fmt.Errorf("%s does not take a value", name)
			}
			help = true
			continue
		case "--claims", "--messages", "--control", "--date", "--defect":
		default:
			return opts, false, fmt.Errorf("unknown flag: %s", name)
		}
		if !inline {
			i++
			if i == len(args) {
				return opts, false, fmt.Errorf("%s requires a value", name)
			}
			val = args[i]
		}
		if name == "--defect" {
			opts.Defects = append(opts.Defects, val)
			continue
		}
		if name == "--date" {
			if val == "" {
				return opts, false, fmt.Errorf("--date requires a value")
			}
			opts.Date = val
			continue
		}
		n, err := strconv.Atoi(val)
		if err != nil || n < 1 {
			return opts, false, fmt.Errorf("%s requires a positive integer", name)
		}
		switch name {
		case "--claims":
			claims = true
			opts.Count = n
		case "--messages":
			messages = true
			opts.Count = n
		case "--control":
			opts.ControlNumber = n
		}
	}
	if help {
		return opts, true, nil
	}
	if opts.Kind == "" {
		return opts, false, fmt.Errorf("requires a fixture kind: 837p, 835, hl7v2, or edifact")
	}
	if claims && opts.Kind != "837p" && opts.Kind != "835" {
		return opts, false, fmt.Errorf("--claims is only supported for 837p or 835")
	}
	if messages && opts.Kind != "hl7v2" && opts.Kind != "edifact" {
		return opts, false, fmt.Errorf("--messages is only supported for hl7v2 or edifact")
	}
	return opts, false, nil
}

func printGenUsage(w io.Writer) {
	diagf(w, `edilint gen - generate fictional test fixtures

Usage:
  edilint gen 837p [--claims <n>] [--control <n>] [--date YYYY-MM-DD] [--defect <ID>]...
  edilint gen 835 [--claims <n>] [--control <n>] [--date YYYY-MM-DD] [--defect <ID>]...
  edilint gen hl7v2 [--messages <n>] [--control <n>] [--date YYYY-MM-DD] [--defect <ID>]...
  edilint gen edifact [--messages <n>] [--control <n>] [--date YYYY-MM-DD] [--defect <ID>]...

Writes a synthetic X12 837P/835 transaction, HL7v2 ADT A08 batch, or EDIFACT
interchange with ORDERS examples to standard output. Identities are fictional
and envelopes declare test usage. These structural examples do not establish
implementation-guide compliance.

Output is deterministic and streamed with bounded memory. X12 segments end
in ~ plus LF; EDIFACT in apostrophe plus LF; HL7 in CR.
No configuration files are loaded.

Flags:
      --claims <n>    claims in the 837P/835 transaction (default 1; max 1000000)
      --messages <n>  HL7/EDIFACT messages (default 1; max 1000000)
      --control <n>   file control number (default 1; max 999999999)
      --date <date>   envelope/service date (default 2026-01-01; years 2000-2099)
      --defect <ID>   inject one supported envelope error; repeat for multiple IDs
  -h, --help          print this help and exit

Use distinct control numbers when generating separate files for the same
test run. Times are fixed at noon. A write failure may leave partial output.

Supported defects (case-insensitive IDs, no duplicates):
  837p/835: EL3005 SE02 control mismatch; EL3006 SE01 segment count;
            EL3007 GE01 transaction count; EL3008 IEA01 group count
  hl7v2:   EL6003 BTS-1 message count; EL6004 FTS-1 batch count
  edifact: EL7003 first UNT-1 segment count; EL7005 UNZ-1 message count;
           EL7006 first UNT-2 reference mismatch
Each selected ID produces exactly one finding. Count defects overstate by one.
Selection order does not change the output. The default fixture is clean.

Exit status:
  0  fixture written, including any requested defects
  2  usage error or output could not be written

Examples:
  edilint gen 837p --claims 100 > claims.x12
  edilint gen 835 --claims 100 --control 2 > remittance.x12
  edilint gen hl7v2 --messages 20 --control 3 > batch.hl7
  edilint gen edifact --messages 20 --control 4 > orders.edi
  edilint gen 837p --claims 10 | edilint --no-config -
  edilint gen 837p --defect EL3006 > bad-count.x12
`)
}
