![Banner](assets/banner.png)

# edilint

A pre-send linter for healthcare interchange files. It reads X12 EDI, HL7v2
messages and batches, EDIFACT, delimited and fixed-width files and reports the
defects that break a downstream parser or draw a trading-partner rejection:
invisible and lookalike characters, inconsistent terminators, broken X12,
HL7v2 batch and EDIFACT envelopes, duplicate control numbers, and declared
record counts that disagree with the records actually present.

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
examples/eligibility.psv:3: error: [EL4101 fields.count-outlier] record type "DTL" has 8 field(s) here but 9 in 2 of 3 record(s) of this type; a shifted field count moves every value after the break (record 3, type DTL)
examples/eligibility.psv:5: error: [EL4001 counts.mismatch] count rule TRL:2:DTL: field 2 declares 4 "DTL" record(s) but the file contains 3 (record 5, type TRL)

1 file checked, 2 findings (2 error, 0 warning)

$ echo $?
1
```

Each finding carries a stable rule identifier and the rule's name, so a line can
be grepped, suppressed or baselined by either.

And a fixed-width file checked against a layout:

```sh
$ edilint --format fixed --layout examples/remit-layout.json examples/remit.txt
examples/remit.txt:2: warning: [EL5002 layout.padding] field "last_name" (offset 15, width 16): value is right-aligned but the layout declares padding on the right (left-aligned) (record 2, type DTL)
examples/remit.txt:3: warning: [EL5002 layout.padding] field "paid_amount" (offset 31, width 10): value is padded with spaces but the layout declares "0" (record 3, type DTL)
examples/remit.txt:4: error: [EL5001 layout.length] record is 48 character(s) long but layout "remittance-detail" declares 49; field "paid_date" (offset 41, width 8) is truncated (record 4, type DTL)

1 file checked, 3 findings (1 error, 2 warning)
```

All files under `examples/` are synthetic. The payers, providers, member
identifiers and amounts are invented.

The synthetic `testdata/837p_claims_*.x12` corpus covers single- and multi-transaction professional claim batches plus isolated envelope, character, and terminator faults.

## Exit status

| Code | Meaning |
|---|---|
| 0 | No findings. |
| 1 | At least one finding. |
| 2 | Usage error, or a file could not be read. Files that *were* readable are still reported before the run exits 2. |

Warnings fail the run by default. Pass `--allow-warnings` to exit 0 unless there
is an error. Findings re-graded to `info` in a configuration file are printed
but never fail a run.

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

### CI-native output formats

`--output` selects how findings are rendered. Every format goes to standard
output, and none of them changes what the run found or how it exits.

| Format | For |
|---|---|
| `text` (default) | Humans and grep: one diagnostic line per finding. |
| `json` | Scripts. The versioned document described below; `--json` is shorthand. |
| `sarif` | GitHub code scanning. SARIF 2.1.0 with the rule catalog embedded. |
| `junit` | CI test panels (GitLab, Jenkins). One testsuite per file, one testcase per finding. |
| `github` | GitHub Actions annotations, one workflow command per finding. |

SARIF into GitHub code scanning, so findings appear on the pull request's
Security tab and as review annotations:

```yaml
      - run: edilint --output sarif outbound/*.x12 > edilint.sarif
        continue-on-error: true
      - uses: github/codeql-action/upload-sarif@v3
        with:
          sarif_file: edilint.sarif
```

`continue-on-error` keeps the upload step reachable when there are findings;
drop it (and the upload) to use the exit status as a plain gate instead.

JUnit into GitLab's test panel:

```yaml
edilint:
  image: golang:1.26
  script:
    - go install github.com/crb2nu/edilint/cmd/edilint@latest
    - edilint --output junit outbound/*.x12 > edilint-junit.xml
  artifacts:
    when: always
    reports:
      junit: edilint-junit.xml
```

A clean file renders as one passing testcase, so a green pipeline shows what
was checked. Errors and warnings render as failures; findings re-graded to
`info` render as skipped, because info never fails a run.

`--output github` needs no upload step: the runner turns each printed
`::error` or `::warning` line into an annotation on the file and line.

Severities map onto each format's own vocabulary the same way: `error` and
`warning` keep their names, and `info` becomes SARIF `note`, JUnit `skipped`,
and Actions `notice` — rendered, never gating.

As a [pre-commit](https://pre-commit.com) hook, once a tagged release exists
to pin:

```yaml
repos:
  - repo: https://github.com/crb2nu/edilint
    rev: <a released tag>
    hooks:
      - id: edilint
```

The default hook runs on `.x12`, `.edi`, `.edifact` and `.hl7` files; a shop
whose files are named by transaction set (`*.837`, `*.835`) overrides `files`
in its own configuration.

For machine consumption, `--json` writes one document describing every file:

```sh
edilint --json outbound/*.x12 | jq -r '.files[].findings[] | "\(.file):\(.line) \(.rule)"'
```

Each finding carries:

| Field | Meaning |
|---|---|
| `id` | The stable rule identifier, e.g. `EL3006`. |
| `rule`, `class`, `severity`, `message` | What was found. `severity` is `error`, `warning` or `info`. |
| `file`, `line`, `column` | Where, in the usual editor coordinates. |
| `record` | The segment identifier for X12 and HL7v2, the record type otherwise. Absent when the leading field holds data rather than a record type. |
| `record_number` | The 1-based record or segment ordinal. |
| `code_point` | The offending character, for the character rules. |
| `expected`, `actual` | The two sides of a count, length or padding mismatch. |

Each `summary` object carries `total`, `errors`, `warnings` and `infos`.

The JSON document carries a `version` field, currently `3`. It is incremented
whenever a consumer written against the previous version could meet something it
has not seen before: a field added, removed, renamed, given a new meaning, or
given a value outside its documented set. The shape is committed as a JSON
schema in [`schema/report.v3.schema.json`](schema/report.v3.schema.json), and
the test suite holds the schema and the code to each other, so the two cannot
drift apart silently.

| Version | Change |
|---|---|
| 3 | Findings gained `id`. Summaries gained `infos`. `severity` widened to include `info`. |
| 2 | The finding field `segment` was renamed to `record`, and the former `record` ordinal to `record_number`. |

`findings` is always an array. A clean file emits `[]`, never `null`, so
iterating it is safe without a guard.

When output is truncated the `findings` array is shortened, but every
`summary` still reports the true totals and carries `"truncated": true`. The
exit status is never affected by truncation.

## Usage

```
edilint [flags] <file>...
```

Use `-` to read standard input. Passing several files enables duplicate
interchange control number detection across the whole batch.

| Flag | Purpose |
|---|---|
| `-f`, `--format <name>` | `auto` (default), `x12`, `hl7v2`, `edifact`, `delimited`, `fixed`, `text`. |
| `-d`, `--delimiter <char>` | Field delimiter for delimited files. Accepts `\t`, `\0`, `\xNN`. |
| `--layout <file>` | Fixed-width layout JSON. Required for `--format fixed`. |
| `--charset <name>` | X12 character set: `extended` (default), `basic`, `off`. |
| `--type-field <n>` | 1-based field used as the record-type discriminator for the field-count check. Default 1. |
| `--count-rule <rule>` | Repeatable. `recordType:fieldIndex:countedType`. |
| `--disable <rules>` | Comma-separated rule identifiers, names or classes, e.g. `--disable EL1006,layout`. |
| `--config <file>` | Configuration file. Defaults to `.edilint.yml` in the working directory, if there is one. |
| `--no-config` | Ignore any `.edilint.yml` in the working directory. |
| `--baseline <file>` | Report only findings absent from this baseline. |
| `--write-baseline <file>` | Record this run's findings as a baseline and exit 0. |
| `--max-findings <n>` | Print at most n findings per file. The exit status and the summary always reflect every finding, whatever this is set to. |
| `--allow-warnings` | Exit 0 when only warnings were found. |
| `--output <name>` | Output format: `text` (default), `json`, `sarif`, `junit`, `github`. |
| `--json` | Shorthand for `--output json`. |
| `-v`, `--verbose` | Print a line for clean files too, name the configuration file in use, and report stale baseline entries. |
| `--list-rules` | Print the rule catalog and exit. |

### Behavior on very defective files

edilint is meant to be pointed at a directory with a glob, which means it will
eventually be pointed at something that is not an interchange file at all. Two
limits keep that from being expensive:

- **Input that is not text is reported once.** If the first 64 KB is more than
  30% invalid UTF-8 or NUL bytes, edilint says so in a single finding and runs
  no interchange checks. A `.gz` or `.zip` swept up by a glob costs one line of
  output, not one line per byte.
- **At most 10,000 findings per file are kept.** Everything is counted — the
  summary totals, the exit status and the "and N more" notice are all exact —
  but only the first 10,000 are retained for display. Raise the ceiling by
  passing a larger `--max-findings`.

Neither limit changes an exit status. A file with a million defects and a file
with one both exit 1.

### Format detection

`ISA` in the leading bytes selects X12; `MSH`, `FHS` or `BHS` selects HL7v2;
`UNA` or `UNB` followed by a separator selects EDIFACT; a
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

`--disable` accepts a rule identifier, a full rule name, or any dot-delimited
prefix of a name. These three all suppress the same rule:

```sh
edilint --disable EL1006 claims.x12
edilint --disable charset.nonascii claims.x12
edilint --disable charset claims.x12       # and the rest of the class with it
```

Identifiers are matched whatever their case, and only in full: `EL1` suppresses
nothing. Use the class name to suppress a class.

An entry that names no rule, no identifier and no class is a usage error rather
than a silent no-op, because a misspelled suppression that quietly suppresses
nothing is the worst outcome a suppression can have.

## Subcommands

Beside linting, the binary carries subcommands. Each has its own `--help`.

### fmt

```
edilint fmt [--check | --write] [-f <format>] <file>...
```

`edilint fmt` rewrites an X12 interchange or an HL7v2 message or batch file
into a canonical layout: one segment per line, each closed by its terminator
and a single LF, whitespace between records normalized away, whitespace-only
records dropped, a final LF always present. By default the canonical form is
printed to standard output; `--write` rewrites the files in place, and
`--check` writes nothing, prints the name of each file that is not already
canonical, and exits 1 if there were any — the CI mode:

```sh
edilint fmt --check outbound/*.x12
```

Formatting is layout only, and it is idempotent: a canonical file passes
through unchanged, and `fmt(fmt(x))` is `fmt(x)`. The bytes inside a record
are never touched, so formatting cannot alter what a file says — and cannot
repair it either. A byte order mark, a wrong count and a homoglyph all pass
through and still lint as findings; so does an X12 file whose last segment is
missing the declared terminator, because that missing terminator is how a
truncated interchange announces itself. Repairs are `edilint fix`.

Formats other than X12 and HL7v2 have no canonical layout defined, and are a
usage error rather than a guess.

### fix

```
edilint fix [--write] [--unsafe] [-f <format>] <file>...
```

`edilint fix` applies mechanical repairs, each tied to the one rule whose
findings it clears, and each the smallest byte edit that does so — bytes a
repair does not name are never touched. The default is a dry run: every
pending repair is described on standard error, the resulting change is
printed as a unified diff, and the exit status is 1 so a pipeline can gate on
"repairs pending". `--write` applies exactly what the dry run showed.

The safe tier repairs defects whose correct form the file itself determines:

| Fixes | Repair | When not to use |
|---|---|---|
| `EL1001` | Strip the UTF-8 byte order mark nothing downstream wants. | A UTF-16 mark is never stripped: the bytes behind it are UTF-16 and need transcoding, not a strip, so the file is left whole. |
| `EL2001` | Rewrite minority line terminators to the file's dominant style. | When the strays are the intended style — a file that should be CRLF but is mostly LF — the majority wins anyway, because it is the only signal the file offers. |
| `EL2002` | Append the file's terminator to the last record. | A last record the transport truncated mid-field gets terminated as it stands; confirm it is complete first. |
| `EL2003` | Append the declared X12 segment terminator to a trailing unterminated segment. | The missing terminator is often the only visible sign of a cut-short transfer. The segment's content is left as-is, so one cut mid-element still lints wrong — but read the tail before repairing. |
| `EL2004` | Rewrite minority inter-segment whitespace to the dominant style. | Same majority rule as `EL2001`: if the minority style was the intended one, this normalizes the wrong way. |
| `EL3006` `EL3007` `EL3008` | Rewrite SE01, GE01 and IEA01 to the recounted totals — declare what was counted. | If records were *lost* rather than miscounted, recounting endorses the loss. A declared count far from the actual one deserves reading before repairing. |
| `EL3010` | Zero-pad an ISA10 or GS05 time that is one digit short of HHMM, HHMMSS or HHMMSSDD, when the padded value is valid. | Only a dropped leading zero is derivable. A time out of range, non-numeric, or two digits short is left for a person; so are the envelope dates, whose lost zeros sit mid-value where padding cannot restore them. |
| `EL6003` `EL6004` | Rewrite BTS-1 and FTS-1 to the recounted totals. An empty count field is optional and stays empty. | The same caution as the X12 recounts. |

`--unsafe` adds one more tier:

| Fixes | Repair | When not to use |
|---|---|---|
| `EL1005` | Replace each Unicode lookalike with the ASCII character it imitates — Cyrillic А becomes A, a no-break space becomes a space. | This edits content bytes on a visual judgment, which is why it is opt-in and why its diff is always printed, `--write` or not. A lookalike whose ASCII form is one of the file's structural characters — a declared X12 separator, the HL7v2 field separator or an encoding character — is skipped, because substituting it would change the record structure. |

`fix` exits 0 when there was nothing to repair or `--write` applied
everything, 1 when a dry run found repairs pending, and 2 when it could not
do its job. Repairs never reach beyond their catalog: findings with no listed
fix — a duplicate control number, a character outside the X12 set, a layout
mismatch — are untouched, and EDIFACT repairs are not implemented, so a
defective EDIFACT file comes back byte-identical rather than half-repaired.

## Configuration file

Settings that belong to a directory of files rather than to one command line go
in `.edilint.yml`. edilint reads that file, or `.edilint.yaml`, from the working
directory. It does not search parent directories: pass `--config` to name a file
anywhere else, and `--no-config` to ignore the one that is there.

```yaml
# .edilint.yml
version: 1

# Analysis settings. Each is optional and each has a flag that overrules it.
format: auto              # auto, x12, hl7v2, edifact, delimited, fixed, text
delimiter: "|"            # accepts \t, \0, \xNN as the flag does
charset: extended         # extended, basic, off
type-field: 1
max-findings: 0
allow-warnings: false
layout: layouts/remit.json

# Rules to turn off, by identifier, name or class.
disable:
  - EL1006                # charset.nonascii
  - layout

# Rules to re-grade. A rule set to info is printed but never fails a run.
severity:
  EL2004: info            # terminator.x12-padding
  envelope.segment-count: warning

# Declared-count assertions, in the --count-rule form.
count-rules:
  - TRL:2:DTL
```

A working example is in [`examples/edilint.yml`](examples/edilint.yml):

```sh
edilint --config examples/edilint.yml examples/eligibility.psv
```

Notes on the schema:

- Every setting is optional, and an empty file is valid.
- An unknown setting, an unknown rule and an unparsable value are all errors
  naming the line, so a typo is reported rather than ignored.
- A flag overrules the file, with two exceptions: `--disable` and `--count-rule`
  add to what the file asked for rather than replacing it. Two sources both
  asking for quiet should both be heard.
- `layout` is resolved relative to the configuration file's own directory, so a
  committed `.edilint.yml` works from any working directory.
- There is no way to switch a rule back on. Rules are on by default and the file
  lists what to turn off; an allowlist as well would need precedence rules
  nobody remembers.
- The file is read with a restricted YAML reader: a mapping whose values are
  scalars, lists of scalars, or one further mapping of scalars. Anchors,
  multi-line scalars and lists of mappings are errors, not silent
  misinterpretations. So are duplicate keys at either level, a UTF-16 file,
  invalid UTF-8 and a carriage return without a line feed; a UTF-8 byte order
  mark at the start is tolerated, and LF and CRLF line endings both work. This
  is what keeps the tool free of dependencies.

## Baselines

A baseline is how edilint gets adopted on files that already have defects.
Record what a set of files reports today, and every later run gates on what is
new:

```sh
edilint --write-baseline .edilint-baseline.json outbound/*.x12
edilint --baseline .edilint-baseline.json outbound/*.x12
```

The first command writes the file and exits 0 whatever it found: recording is
bookkeeping, not a gate. The second reports nothing, exits 0, and keeps doing so
until a new defect appears.

Commit the baseline. It is sorted and holds no timestamp, so recording the same
findings twice produces the same bytes and a diff shows only real movement.

**What a baseline matches on.** An entry records the file, the rule identifier,
the record type, the code point when the rule reports one, and the message —
and deliberately no line, column or record ordinal. Those move whenever a
segment is inserted above them, and a baseline that expired on the next edit
would be useless.

Unquoted numbers in the message are ignored when matching, for the same reason:
several messages carry a statistic over the whole file, such as "9 field(s)
here but 10 in 2 of 3 record(s) of this type", and that changes as soon as an
unrelated record is added. Quoted values are kept exactly, digits included: a
quoted bad date or control number is the defect itself, and swapping it for a
different wrong value is reported as new. Identical findings collapse into one
entry with a `count`, so thirty non-printable characters record as one entry of
thirty and a thirty-first is still reported.

Three consequences worth knowing:

- Paths are recorded as they were given, cleaned but still relative, so run
  edilint from the same directory each time.
- Two findings of the same rule, on the same record type, in the same file,
  whose messages differ only in their unquoted numbers, count as the same
  finding. Swapping one for the other is not reported; adding one on top of it
  is. What the message quotes, and the code point of a character finding, do
  distinguish: a different bad date, control number or homoglyph is new.
- Rewording a rule's message in a later release invalidates the entries that
  quoted it, and those findings resurface. Re-record with `--write-baseline`
  after an upgrade whose changelog says messages changed.

`--baseline` and `--write-baseline` cannot be combined, and `--write-baseline`
refuses to write anything if an input could not be read, so a baseline never
bakes in a gap. Run with `-v` to be told when recorded findings no longer occur,
which is the signal to re-record.

## Use with a coding agent

`edilint mcp` serves the checks over the
[Model Context Protocol](https://modelcontextprotocol.io/) on standard input and
output, so an agent that generates or edits interchange files can lint them in
the loop instead of shelling out and parsing text. It is the same binary and the
same rules; nothing is downloaded and no network connection is made.

Register it with the client as the command `edilint` and the argument `mcp`.
For Claude Code:

```sh
claude mcp add edilint -- edilint mcp
```

Other clients take the equivalent command and arguments in their configuration
file:

```json
{ "mcpServers": { "edilint": { "command": "edilint", "args": ["mcp"] } } }
```

Four tools are exposed, all read-only:

| Tool | Purpose |
|---|---|
| `lint_file` | Lint one or more files by path. Duplicate control numbers are detected across the files of one call. |
| `lint_text` | Lint content passed in the call, for a file that exists only in the conversation. |
| `list_rules` | The rule catalog, optionally filtered to one class. |
| `explain_rule` | What a rule checks, its default severity and formats, and how to suppress or baseline it. |

The lint tools accept the same tuning as the flags (`format`, `delimiter`,
`charset`, `type_field`, `count_rules`, `disable`, `max_findings`, `layout`,
`allow_warnings`). They return the text diagnostics the command line prints,
followed by the exit status it would return, plus structured content holding
`exit_status` (0, 1 or 2, with the meanings above) and the same document
`--json` writes. Findings are the normal result of a call, not an error; a call
is an error only when its arguments are wrong or no input could be read.
Findings are capped at 200 per file unless the call or the configuration file
says otherwise, and the summary counts are exact regardless.

A `.edilint.yml` in the server's working directory, or the file named by
`edilint mcp --config`, is the base every call starts from, exactly as on the
command line. Both generations of the protocol are spoken: the `initialize`
handshake of revisions 2025-11-25 and earlier, and the per-request metadata of
revision 2026-07-28. The transport is implemented with the standard library, so
`go install` still pulls in nothing.

## Rules

Rule identifiers and names are both stable and both appear in the text and JSON
output. `edilint --list-rules` prints this catalog at runtime.

An identifier's leading digit is its check class, so it says which part of the
tool produced a finding before you look anything up:

| Block | Class |
|---|---|
| `EL1xxx` | Character set and character hygiene |
| `EL2xxx` | Record and segment terminators |
| `EL3xxx` | X12 envelope structure |
| `EL4xxx` | Declared record counts and field-count consistency |
| `EL5xxx` | Fixed-width layouts |
| `EL6xxx` | HL7v2 batch structure |
| `EL7xxx` | EDIFACT envelope structure |

Identifiers are permanent. A withdrawn rule keeps its number rather than passing
it to something else, so a suppression written today cannot silently come to
mean something different later.

The severity column is the severity a check normally assigns. A few rules grade
themselves by format, and a configuration file can override any of them.

### charset

| ID | Rule | Severity | Applies to | Detects |
|---|---|---|---|---|
| `EL1001` | `charset.bom` | error | all (warning for delimited) | File starts with a byte order mark. An error for X12, HL7v2 and fixed-width, where a BOM before ISA or MSH shifts every fixed position in the file; a warning for delimited, because spreadsheet exports emit one routinely and most CSV readers cope. |
| `EL1002` | `charset.invalid-utf8` | error | all | Byte sequence is not valid UTF-8. Reported once, without running the interchange checks, when the input is dense enough in invalid UTF-8 and NUL bytes not to be text at all. |
| `EL1003` | `charset.nonprintable` | error | all | Control character in record content that is not a declared separator. Tabs are reported as warnings. |
| `EL1004` | `charset.zero-width` | error | all | Zero-width or bidirectional formatting character that renders as nothing but occupies bytes. |
| `EL1005` | `charset.homoglyph` | error | all | Unicode character that is visually identical to an ASCII one, such as Cyrillic А for A. |
| `EL1006` | `charset.nonascii` | warning | all | Non-ASCII character that is not a known lookalike. |
| `EL1007` | `charset.x12-basic` | warning | x12 (requires `--charset basic`) | Character outside the X12 basic character set but inside the extended set. Off by default; the default profile is extended. |
| `EL1008` | `charset.x12-extended` | error | x12 | Character outside the X12 extended character set, which in printable ASCII means the caret or the backtick. A caret is exempt when ISA11 declares it as the repetition separator. |

### terminator

| ID | Rule | Severity | Applies to | Detects |
|---|---|---|---|---|
| `EL2001` | `terminator.mixed` | error | hl7v2, delimited, fixed, text | File mixes CRLF, LF and CR line endings. |
| `EL2002` | `terminator.missing-final` | warning | hl7v2, delimited, fixed, text | Last record has no terminator. |
| `EL2003` | `terminator.x12-segment` | error | x12 | Segment is not closed by the segment terminator the ISA declared. |
| `EL2004` | `terminator.x12-padding` | warning | x12 | Whitespace between segment terminators is applied inconsistently. |
| `EL2005` | `terminator.x12-separator` | error | x12 | Declared separators collide with each other or are alphanumeric. |
| `EL2006` | `terminator.edifact-segment` | error | edifact | Segment is not closed by the segment terminator in force, which in practice means the file was truncated mid-segment. |

### envelope

| ID | Rule | Severity | Applies to | Detects |
|---|---|---|---|---|
| `EL3001` | `envelope.isa-length` | error | x12 | ISA segment is not the fixed 106 characters, or is absent. |
| `EL3002` | `envelope.nesting` | error | x12 | GS appears outside an ISA, or ST outside a GS. |
| `EL3003` | `envelope.unclosed` | error | x12 | ISA, GS or ST has no matching IEA, GE or SE. |
| `EL3004` | `envelope.unopened` | error | x12 | IEA, GE or SE appears without its header. |
| `EL3005` | `envelope.control-number` | error | x12 | Header and trailer control numbers differ (ISA13/IEA02, GS06/GE02, ST02/SE02). A leading-zero-only difference is reported as a warning. |
| `EL3006` | `envelope.segment-count` | error | x12 | SE01 does not match the recounted segments from ST through SE inclusive. |
| `EL3007` | `envelope.group-count` | error | x12 | GE01 does not match the recounted transaction sets in the group. |
| `EL3008` | `envelope.interchange-count` | error | x12 | IEA01 does not match the recounted functional groups in the interchange. |
| `EL3009` | `envelope.duplicate-control-number` | error | x12 | Duplicate ISA13 within the file or across the files in one run, duplicate GS06 within an interchange, or duplicate ST02 within a functional group. |
| `EL3010` | `envelope.datetime` | error | x12 | ISA09/GS04 dates or ISA10/GS05 times are not valid YYMMDD, CCYYMMDD or HHMM[SS[DD]] values. |
| `EL3011` | `envelope.missing-control-id` | error | x12 | ISA13 interchange control number is empty. |
| `EL3012` | `envelope.trailing-data` | error | x12 | Segments appear outside any interchange. |

### What the trading partner would say

Every X12 rule maps to the acknowledgment a receiver's front end returns for
the same defect: a TA1 note code (TA105) for interchange-level problems, and
999 codes at group (AK905), transaction set (IK502), segment (IK304) or data
element (IK403) level. A 997 carries the same codes in AK905, AK502, AK304 and
AK403. The mapping is what `explain_rule` reports in the MCP server and what
the `help` text of every rule carries in SARIF output, so a finding can be
read as the rejection it prevents.

| Rule | Acknowledgment |
|---|---|
| `EL1007`, `EL1008` | 999 IK403 6, invalid character in a data element |
| `EL2003` | TA1 004, segment terminator invalid; TA1 023, premature end of file when the unterminated segment swallows the trailer |
| `EL2004` | 999 IK304 1, unrecognized segment identifier, because the padding is read as part of the next segment's identifier |
| `EL2005` | TA1 026, 027 and 032, invalid data element, component or repetition separator; TA1 004, segment terminator invalid |
| `EL3001` | TA1 022, invalid control structure. A receiver that cannot read the ISA often cannot return a TA1 at all. |
| `EL3002` | TA1 024, invalid interchange content, for a GS outside any ISA; 999 IK502 18, transaction set not in a functional group |
| `EL3003` | TA1 023, premature end of file; 999 AK905 3, group trailer missing; 999 IK502 2, transaction set trailer missing |
| `EL3004` | TA1 022, invalid control structure; 999 IK304 2, unexpected segment |
| `EL3005` | TA1 001; 999 AK905 4; 999 IK502 3: header and trailer control numbers do not match, at each level |
| `EL3006` | 999 IK502 4, number of included segments does not match the actual count |
| `EL3007` | 999 AK905 5, number of included transaction sets does not match the actual count |
| `EL3008` | TA1 021, invalid number of included groups value |
| `EL3009` | TA1 025, duplicate interchange control number; 999 AK905 19 and IK502 23, control number not unique within its parent |
| `EL3010` | TA1 014 and 015, invalid interchange date or time; 999 AK905 30 and 31, invalid group date or time |
| `EL3011` | TA1 018, invalid interchange control number value |
| `EL3012` | TA1 022, invalid control structure, for segments after the IEA |

Rules outside X12 have no entry. HL7v2 receivers answer with ACK messages and
EDIFACT receivers with CONTRL messages, whose codes are not mapped yet.

### counts

| ID | Rule | Severity | Applies to | Detects |
|---|---|---|---|---|
| `EL4001` | `counts.mismatch` | error | all (requires --count-rule) | A declared record count does not match the recounted total. |
| `EL4002` | `counts.unparsable` | error | all (requires --count-rule) | The field a count rule points at is not an integer. |
| `EL4003` | `counts.missing-field` | error | all (requires --count-rule) | The declaring record has fewer fields than the count rule reads. |
| `EL4004` | `counts.no-declaring-record` | warning | all (requires --count-rule) | No record matched the count rule's declaring record type, so nothing was verified. |

### fields

| ID | Rule | Severity | Applies to | Detects |
|---|---|---|---|---|
| `EL4101` | `fields.count-outlier` | error | delimited, hl7v2 (warning) | A record carries a different number of fields from others of the same record type. |

### layout

| ID | Rule | Severity | Applies to | Detects |
|---|---|---|---|---|
| `EL5001` | `layout.length` | error | fixed (requires --layout) | Record length does not match the sum of the layout's field widths. |
| `EL5002` | `layout.padding` | warning | fixed (requires --layout) | A field's padding is unambiguously on the side opposite the one the layout declares. |

### hl7batch

A file of bare MSH messages is valid without any envelope, so the pairing and
count checks run only when the file uses FHS, BHS, BTS or FTS at all. The
separator checks always run: a single message with malformed encoding
characters is broken on its own.

| ID | Rule | Severity | Applies to | Detects |
|---|---|---|---|---|
| `EL6001` | `hl7batch.unclosed` | error | hl7v2 | FHS or BHS is never closed by a matching FTS or BTS, so the batch envelope is incomplete. |
| `EL6002` | `hl7batch.unopened` | error | hl7v2 | FTS or BTS appears without its matching FHS or BHS header. |
| `EL6003` | `hl7batch.message-count` | error | hl7v2 | BTS-1 does not match the recounted MSH messages in the batch. An empty BTS-1 is not checked; the field is optional. |
| `EL6004` | `hl7batch.batch-count` | error | hl7v2 | FTS-1 does not match the recounted BHS batches in the file. An empty FTS-1 is not checked; the field is optional. |
| `EL6005` | `hl7batch.separator` | error | hl7v2 | FHS, BHS and MSH headers disagree on the field separator or the encoding characters, or a header's encoding characters are malformed. Split-and-merge tooling reads every message with the first header's separators, so a disagreeing message is misparsed. |
| `EL6006` | `hl7batch.stray-message` | warning | hl7v2 | MSH appears outside any open batch in a file that uses batch envelopes, so batch-aware readers will not process it. |

### edifact

Envelope level only, the same boundary as the X12 checks: UNB/UNZ, UNG/UNE and
UNH/UNT pairing, trailer recounts, control-reference matching and the UNA
service string advice. Message content is out of scope.

| ID | Rule | Severity | Applies to | Detects |
|---|---|---|---|---|
| `EL7001` | `edifact.unclosed` | error | edifact | UNB, UNG or UNH is never closed by a matching UNZ, UNE or UNT. |
| `EL7002` | `edifact.unopened` | error | edifact | UNZ, UNE or UNT appears without its matching header. |
| `EL7003` | `edifact.segment-count` | error | edifact | UNT-1 does not match the recounted segments from UNH through UNT inclusive. |
| `EL7004` | `edifact.group-count` | error | edifact | UNE-1 does not match the recounted messages in the functional group. |
| `EL7005` | `edifact.interchange-count` | error | edifact | UNZ-1 does not match the recounted messages in the interchange, or the recounted functional groups when UNG groups are used. |
| `EL7006` | `edifact.control-reference` | error | edifact | Header and trailer control references differ (UNB-5/UNZ-2, UNG-5/UNE-2, UNH-1/UNT-2), or a header's reference is empty. A numerically-equal-but-not-identical pair is reported as a warning. |
| `EL7007` | `edifact.service-string` | error | edifact | UNA is not the fixed nine characters, its service characters collide or are alphanumeric, or its decimal mark is not a period or a comma. |
| `EL7008` | `edifact.nesting` | error | edifact | UNG or UNH appears outside the envelope that must enclose it, or no UNB is present in a file forced to the edifact format. |
| `EL7009` | `edifact.trailing-data` | error | edifact | Segments appear outside any interchange; data after UNZ is not part of a valid EDIFACT file. |

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

A nil error from `LintFile` means the input was read and analyzed, not that it
was clean. Check `Report.OK` or inspect `Report.Findings`.

## Repository

The canonical repository is `gitlab.flexinfer.ai/libs/edilint`, where merge requests
run the GitLab CI in `.gitlab-ci.yml`; `github.com/crb2nu/edilint` is a push mirror
of `main` and tags. The GitHub Actions workflow still runs on the mirror and adds the
macOS and Windows test matrix. Issues and pull requests opened on GitHub are read;
changes land through GitLab and arrive on GitHub with the next mirror push.

## License

Apache-2.0.
