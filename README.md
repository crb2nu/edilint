# edilint

A pre-send linter for healthcare interchange files. It reads X12 EDI, HL7v2,
delimited and fixed-width files and reports the defects that break a downstream
parser or draw a trading-partner rejection: invisible and lookalike characters,
inconsistent terminators, broken X12 envelopes, duplicate control numbers, and
declared record counts that disagree with the records actually present.

Single static binary, exit codes, and JSON output, so it works as a gate in a
send script or a CI job.

## Install

```sh
go install github.com/crb2nu/edilint/cmd/edilint@latest
```

## 30-second example

```sh
$ edilint examples/remittance.x12
$ echo $?
0
```

A clean file prints nothing and exits 0. Now a pipe-delimited extract whose
trailer disagrees with its contents:

```sh
$ edilint --count-rule TRL:2:DTL examples/eligibility.psv
examples/eligibility.psv:3: error: [fields.count-outlier] record type "DTL" has 8 field(s) here but 9 in 2 of 3 record(s) of this type; a shifted field count moves every value after the break (record 3, type DTL)
examples/eligibility.psv:5: error: [counts.mismatch] count rule TRL:2:DTL: field 2 declares 4 "DTL" record(s) but the file contains 3 (record 5, type TRL)

1 file checked, 2 findings (2 error, 0 warning)

$ echo $?
1
```

And a fixed-width file checked against a layout:

```sh
$ edilint --format fixed --layout examples/remit-layout.json examples/remit.txt
examples/remit.txt:2: warning: [layout.padding] field "last_name" (offset 15, width 16): value is right-aligned but the layout declares padding on the right (left-aligned) (record 2, type DTL)
examples/remit.txt:3: warning: [layout.padding] field "paid_amount" (offset 31, width 10): value is padded with spaces but the layout declares "0" (record 3, type DTL)
examples/remit.txt:4: error: [layout.length] record is 48 character(s) long but layout "remittance-detail" declares 49; field "paid_date" (offset 41, width 8) is truncated (record 4, type DTL)

1 file checked, 3 findings (1 error, 2 warning)
```

All files under `examples/` are synthetic. The payers, providers, member
identifiers and amounts are invented.

## Exit status

| Code | Meaning |
|---|---|
| 0 | No findings. |
| 1 | At least one finding. |
| 2 | Usage error, or a file could not be read. |

Warnings fail the run by default. Pass `--allow-warnings` to exit 0 unless there
is an error.

## Use in CI

Gate a send script:

```sh
edilint outbound/claims.x12 || exit 1
```

GitHub Actions:

```yaml
name: edi
on: [push, pull_request]

jobs:
  lint:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v5
      - uses: actions/setup-go@v6
        with:
          go-version: stable
      - run: go install github.com/crb2nu/edilint/cmd/edilint@latest
      - run: edilint outbound/*.x12
```

GitLab CI:

```yaml
edilint:
  image: golang:1.26
  script:
    - go install github.com/crb2nu/edilint/cmd/edilint@latest
    - edilint outbound/*.x12
```

For machine consumption, `--json` writes one document describing every file:

```sh
edilint --json outbound/*.x12 | jq -r '.files[].findings[] | "\(.file):\(.line) \(.rule)"'
```

Each finding carries:

| Field | Meaning |
|---|---|
| `rule`, `class`, `severity`, `message` | What was found. |
| `file`, `line`, `column` | Where, in the usual editor coordinates. |
| `record` | The segment identifier for X12 and HL7v2, the record type otherwise. Absent when the leading field holds data rather than a record type. |
| `record_number` | The 1-based record or segment ordinal. |
| `code_point` | The offending character, for the character rules. |
| `expected`, `actual` | The two sides of a count, length or padding mismatch. |

The JSON document carries a `version` field, currently `2`. It is incremented
only when an existing field changes meaning or is removed. Version 2 renamed the
`segment` field to `record`, and the former `record` ordinal to `record_number`.

When `--max-findings` truncates the output, the `findings` array is shortened
but every `summary` still reports the true totals and carries `"truncated":
true`. The exit status is unaffected by truncation.

## Usage

```
edilint [flags] <file>...
```

Use `-` to read standard input. Passing several files enables duplicate
interchange control number detection across the whole batch.

| Flag | Purpose |
|---|---|
| `-f`, `--format <name>` | `auto` (default), `x12`, `hl7v2`, `delimited`, `fixed`, `text`. |
| `-d`, `--delimiter <char>` | Field delimiter for delimited files. Accepts `\t`, `\0`, `\xNN`. |
| `--layout <file>` | Fixed-width layout JSON. Required for `--format fixed`. |
| `--charset <name>` | X12 character set: `extended` (default), `basic`, `off`. |
| `--type-field <n>` | 1-based field used as the record-type discriminator for the field-count check. Default 1. |
| `--count-rule <rule>` | Repeatable. `recordType:fieldIndex:countedType`. |
| `--disable <rules>` | Comma-separated rule names or classes, e.g. `--disable charset.nonascii,layout`. |
| `--max-findings <n>` | Print at most n findings per file. Default unlimited. The exit status always reflects every finding. |
| `--allow-warnings` | Exit 0 when only warnings were found. |
| `--json` | Emit a JSON document instead of diagnostic lines. |
| `-v`, `--verbose` | Print a line for clean files too. |
| `--list-rules` | Print the rule catalogue and exit. |

### Format detection

`ISA` in the leading bytes selects X12; `MSH`, `FHS` or `BHS` selects HL7v2; a
`--layout` selects fixed-width; otherwise a field delimiter is inferred from how
consistently a candidate character appears across records. Anything else is
treated as plain text, which limits the run to the character and terminator
checks. `--format` overrides detection.

### Count rules

`--count-rule TRL:2:DTL` means "field 2 of `TRL` records declares how many `DTL`
records exist". Field indexes are 1-based and field 1 is the record type, so for
X12 the first element of a segment is field 2. The flag is repeatable, and it
works on X12 segments and fixed-width records as well as delimited ones.

How a record is matched depends on the format:

| Format | Matching |
|---|---|
| delimited | The first field must **equal** the given value. `TRL` does not match `TRLR`. |
| X12, fixed-width, HL7v2 | The record must **start with** the given value, since there is no first field to compare. |

The difference matters when record types share a prefix. In a delimited file
with both `TRL` and `TRLR` records, `--count-rule TRL:2:DTL` reads only the
`TRL` record. In a fixed-width file the same rule would match both, so give the
full record type as it appears at the start of the record.

### X12 character sets

`--charset extended` is the default: A-Z, a-z, 0-9, space, and
`! " & ' ( ) * + , - . / : ; ? = % ~ @ [ ] _ { } \ | < > # $`. In printable
ASCII only the caret and the backtick fall outside it, and a caret is exempt
when ISA11 declares it as the repetition separator.

`--charset basic` is the stricter, opt-in profile. It drops lowercase letters
and the extended punctuation, reporting anything legal only in the extended set
as a warning aggregated per record. Use it when a partner has told you they
require the basic set.

`--charset off` disables both character-set rules. The homoglyph, control
character and zero-width rules are unaffected by this flag.

ISA11 is read as the repetition separator only when it holds a single
non-alphanumeric character. A 004010 interchange whose ISA11 is the letter `U`
declares no repetition separator, so `U` keeps its ordinary meaning and a stray
caret elsewhere in that file is reported rather than silently ignored.

### Fixed-width layouts

```json
{
  "name": "remittance-detail",
  "fields": [
    {"name": "record_type", "width": 3},
    {"name": "member_id",   "width": 12, "pad": "right"},
    {"name": "last_name",   "width": 16, "pad": "right"},
    {"name": "paid_amount", "width": 10, "pad": "left", "padChar": "0"},
    {"name": "paid_date",   "width": 8}
  ]
}
```

`pad` names the side the padding is added to, so `"right"` describes a
left-aligned value and `"left"` describes a right-aligned one. `padChar`
defaults to a space. Omitting `pad` disables the padding check for that field.

Padding drift is only reported where it is unambiguous. A right-aligned field
padded with `0` cannot be distinguished from a value that genuinely ends in
zeros, so for non-space pad characters only stray space padding is flagged.

### Suppressing rules

`--disable` accepts a full rule name or any dot-delimited prefix, so
`--disable charset` suppresses every `charset.*` rule while
`--disable charset.nonascii` suppresses only that one.

## Rules

Rule names are stable and appear in both the text and JSON output.
`edilint --list-rules` prints this catalogue at runtime.

### charset

| Rule | Severity | Applies to | Detects |
|---|---|---|---|
| `charset.bom` | error | all (warning for delimited) | File starts with a byte order mark. An error for X12, HL7v2 and fixed-width, where a BOM before ISA or MSH shifts every fixed position in the file; a warning for delimited, because spreadsheet exports emit one routinely and most CSV readers cope. |
| `charset.invalid-utf8` | error | all | Byte sequence is not valid UTF-8. |
| `charset.nonprintable` | error | all | Control character in record content that is not a declared separator. Tabs are reported as warnings. |
| `charset.zero-width` | error | all | Zero-width or bidirectional formatting character that renders as nothing but occupies bytes. |
| `charset.homoglyph` | error | all | Unicode character that is visually identical to an ASCII one, such as Cyrillic А for A. |
| `charset.nonascii` | warning | all | Non-ASCII character that is not a known lookalike. |
| `charset.x12-basic` | warning | x12 (requires `--charset basic`) | Character outside the X12 basic character set but inside the extended set. Off by default; the default profile is extended. |
| `charset.x12-extended` | error | x12 | Character outside the X12 extended character set, which in printable ASCII means the caret or the backtick. A caret is exempt when ISA11 declares it as the repetition separator. |

### terminator

| Rule | Severity | Applies to | Detects |
|---|---|---|---|
| `terminator.mixed` | error | hl7v2, delimited, fixed, text | File mixes CRLF, LF and CR line endings. |
| `terminator.missing-final` | warning | hl7v2, delimited, fixed, text | Last record has no terminator. |
| `terminator.x12-segment` | error | x12 | Segment is not closed by the segment terminator the ISA declared. |
| `terminator.x12-padding` | warning | x12 | Whitespace between segment terminators is applied inconsistently. |
| `terminator.x12-separator` | error | x12 | Declared separators collide with each other or are alphanumeric. |

### envelope

| Rule | Severity | Applies to | Detects |
|---|---|---|---|
| `envelope.isa-length` | error | x12 | ISA segment is not the fixed 106 characters, or is absent. |
| `envelope.nesting` | error | x12 | GS appears outside an ISA, or ST outside a GS. |
| `envelope.unclosed` | error | x12 | ISA, GS or ST has no matching IEA, GE or SE. |
| `envelope.unopened` | error | x12 | IEA, GE or SE appears without its header. |
| `envelope.control-number` | error | x12 | Header and trailer control numbers differ (ISA13/IEA02, GS06/GE02, ST02/SE02). A leading-zero-only difference is reported as a warning. |
| `envelope.segment-count` | error | x12 | SE01 does not match the recounted segments from ST through SE inclusive. |
| `envelope.group-count` | error | x12 | GE01 does not match the recounted transaction sets in the group. |
| `envelope.interchange-count` | error | x12 | IEA01 does not match the recounted functional groups in the interchange. |
| `envelope.duplicate-control-number` | error | x12 | Duplicate ISA13 within the file or across the files in one run, duplicate GS06 within an interchange, or duplicate ST02 within a functional group. |
| `envelope.datetime` | error | x12 | ISA09/GS04 dates or ISA10/GS05 times are not valid YYMMDD, CCYYMMDD or HHMM[SS[DD]] values. |
| `envelope.missing-control-id` | error | x12 | ISA13 interchange control number is empty. |
| `envelope.trailing-data` | error | x12 | Segments appear outside any interchange. |

### counts

| Rule | Severity | Applies to | Detects |
|---|---|---|---|
| `counts.mismatch` | error | all (requires --count-rule) | A declared record count does not match the recounted total. |
| `counts.unparsable` | error | all (requires --count-rule) | The field a count rule points at is not an integer. |
| `counts.missing-field` | error | all (requires --count-rule) | The declaring record has fewer fields than the count rule reads. |
| `counts.no-declaring-record` | warning | all (requires --count-rule) | No record matched the count rule's declaring record type, so nothing was verified. |

### fields

| Rule | Severity | Applies to | Detects |
|---|---|---|---|
| `fields.count-outlier` | error | delimited, hl7v2 (warning) | A record carries a different number of fields from others of the same record type. |

### layout

| Rule | Severity | Applies to | Detects |
|---|---|---|---|
| `layout.length` | error | fixed (requires --layout) | Record length does not match the sum of the layout's field widths. |
| `layout.padding` | warning | fixed (requires --layout) | A field's padding is unambiguously on the side opposite the one the layout declares. |

## Scope

edilint covers a subset of WEDI SNIP Type 1, which is X12 syntax integrity:
segment and element structure, envelope pairing, control number matching and
recounted totals. On top of that it adds a hygiene layer that SNIP does not
name — character-set and homoglyph checks, terminator consistency, and
declared-versus-actual record counts for non-X12 flat files — because those are
the defects that most often survive a syntax validator and break the receiver.

edilint is **not** an implementation-guide validator. It does not check SNIP
Types 2 through 7:

- **Type 2**, HIPAA implementation guide requirements (segment and element
  usage, loop repeats, required-versus-situational rules)
- **Type 3**, balancing (claim amounts against line items, remittance totals)
- **Type 4**, inter-segment situational dependencies
- **Type 5**, external code set validation (ICD, CPT, HCPCS, NPI)
- **Type 6**, product- or service-specific requirements
- **Type 7**, trading partner specific requirements

It does not know what an 837 or an 835 means. It checks that the file is
structurally and lexically sound before you spend a compliance validator's time
or a partner's goodwill on it. For guide-level validation use one of the tools
below or a commercial validator.

## Related tools

- **[pyx12](https://github.com/azoner/pyx12)** — the closest prior art. A Python
  library and the `x12valid` / `x12norm` command line tools, with HIPAA
  implementation-guide maps, so it covers guide-level validation that edilint
  does not. Reach for it when you need SNIP Type 2 and above.
- **[moov-io/x12](https://github.com/moov-io/x12)** — a Go X12 parser and
  generator. Complementary: it gives you a typed representation to build
  transactions with, where edilint inspects bytes on the way out.
- **[Stedi EDI Inspector](https://www.stedi.com/edi/inspector)** — a free
  browser tool for reading and validating an interchange interactively. Good for
  investigating one file; not scriptable, so it does not gate a pipeline.
- **[fi-fhir](https://github.com/crb2nu/fi-fhir)** — from the same author. edilint
  checks files at the gate; fi-fhir parses the same formats (HL7v2, X12, CSV) into
  semantic events, maps them in a studio UI, and routes them through configurable
  workflows. Reach for it when linting is not enough and you need the pipeline
  behind the gate.

The difference edilint offers is form factor rather than depth: a single static
binary with meaningful exit codes and JSON output, designed to run
non-interactively on every file you send.

## Library use

The check engine is a plain Go package with no dependencies outside the standard
library:

```go
import "github.com/crb2nu/edilint"

rep := edilint.Lint("claims.x12", data, edilint.Options{
    CountRules: []edilint.CountRule{{Declaring: "TRL", Field: 2, Counted: "DTL"}},
})
for _, f := range rep.Findings {
    log.Printf("%s:%d %s: %s", f.File, f.Line, f.Rule, f.Message)
}
```

A nil error from `LintFile` means the input was read and analysed, not that it
was clean. Check `Report.OK` or inspect `Report.Findings`.

## License

Apache-2.0.
