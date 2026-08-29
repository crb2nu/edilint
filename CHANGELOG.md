# Changelog

All notable changes to this project are documented here.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and
this project will follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html)
from its first tag.

## [Unreleased]

### Added

- `edilint fmt`: canonical layout for X12 interchanges and HL7v2 messages and
  batches — one segment per line, closed by its terminator and a single LF,
  inter-record whitespace normalized away. The default prints to standard
  output, `--write` rewrites in place, and `--check` prints the name of each
  file that is not already canonical and exits 1 if there were any.
  Formatting is layout only and idempotent: the bytes inside a record are
  never touched, so defects — a byte order mark, a wrong count, an
  unterminated final X12 segment — pass through and still lint as findings.
- `edilint fix`: mechanical repairs, each tied to the one rule it clears and
  each the smallest byte edit that clears it. The safe tier strips a UTF-8
  byte order mark (`EL1001`), normalizes line terminators to the dominant
  style and appends a missing final one (`EL2001`, `EL2002`), closes an
  unterminated trailing X12 segment and normalizes inter-segment whitespace
  (`EL2003`, `EL2004`), rewrites SE01, GE01, IEA01, BTS-1 and FTS-1 to their
  recounted totals (`EL3006`, `EL3007`, `EL3008`, `EL6003`, `EL6004`), and
  zero-pads an ISA10 or GS05 time one leading zero short of valid (`EL3010`).
  `--unsafe` adds homoglyph-to-ASCII substitution (`EL1005`), skipping any
  lookalike whose ASCII form is a structural character of the file. The
  default is a dry run that describes each pending repair and prints a
  unified diff; `--write` applies exactly what the dry run showed, and unsafe
  repairs print their diff even then. Input the tool cannot read — binary, or
  behind a UTF-16 byte order mark — comes back untouched.
- `Canonical` and `Fix` in the library, carrying both subcommands.

## [0.1.0] - 2026-08-15

### Added

- X12 EDI, HL7v2, delimited and fixed-width linting across six check classes:
  character hygiene, terminator consistency, X12 envelope integrity, declared
  record counts, field-count consistency and fixed-width layout conformance.
- HL7v2 batch structure checks (`EL6xxx`): FHS/FTS and BHS/BTS pairing, BTS-1
  and FTS-1 recounts, and field-separator and encoding-character consistency
  across every FHS, BHS and MSH header in a file. A stream of bare MSH messages
  is valid without an envelope and produces no batch findings.
- EDIFACT envelope checks (`EL7xxx`) behind a new detected `edifact` format:
  UNB/UNZ, UNG/UNE and UNH/UNT pairing, UNT-1/UNE-1/UNZ-1 recounts,
  control-reference matching (UNB-5/UNZ-2, UNG-5/UNE-2, UNH-1/UNT-2), and
  validation of the UNA service string advice, whose declared separators —
  including a moved segment terminator and the release character — govern how
  the file is read. An unterminated final segment is reported as truncation
  (`EL2006`). Detection requires `UNA`/`UNB` to be followed by a separator, so
  a delimited file whose first field starts with those letters is not misread.
- `--json` output, documented and versioned, currently schema version 3. The
  shape is committed as a JSON schema in `schema/report.v3.schema.json`, and
  the test suite holds the schema and the structs to each other.
- `--output <name>` selects the output format: `text` (the default diagnostic
  lines), `json` (`--json` remains as shorthand), `sarif` (SARIF 2.1.0 for
  GitHub code scanning, rule catalog embedded), `junit` (JUnit XML for CI test
  panels: a testsuite per file, a testcase per finding, clean files as passing
  cases) and `github` (one GitHub Actions workflow-command annotation per
  finding). Every format goes to standard output, and none of them changes
  what a run finds or how it exits. Severities keep their names where the
  target vocabulary has them; `info` becomes SARIF `note`, JUnit `skipped` and
  Actions `notice`, rendered but never gating, the same contract it has
  everywhere else. When `--max-findings` drops findings, `junit` adds a
  failing testcase and `github` a `::notice` saying how many, so truncated
  output cannot read as complete.
- Exit statuses suitable for gating a send script: 0 clean, 1 findings, 2 the
  tool could not do its job.
- `--count-rule`, `--layout`, `--charset`, `--disable`, `--max-findings` and
  `--type-field` for tuning a run to a partner's conventions.
- Stable rule identifiers of the form `EL####`, grouped by check class:
  `EL1xxx` character hygiene, `EL2xxx` terminators, `EL3xxx` X12 envelope,
  `EL4xxx` counts, `EL5xxx` fixed-width layouts. `EL6xxx` and `EL7xxx` are
  reserved for HL7v2 batch and EDIFACT envelope structure. Every finding carries
  its identifier in both the text and the JSON output, and `--list-rules` prints
  it as the first column.
- `--disable` accepts an identifier as well as a rule name or a class.
- A third severity, `info`, for findings that are printed but never fail a run.
  No rule ships with it; it is what a configuration file downgrades a rule to.
- `.edilint.yml` configuration file, read from the working directory, holding
  `format`, `delimiter`, `charset`, `type-field`, `max-findings`,
  `allow-warnings`, `layout`, `disable`, `severity` and `count-rules`.
  `--config` names another file and `--no-config` ignores it. Flags overrule the
  file, except that `--disable` and `--count-rule` add to it. The file must be
  UTF-8 with LF or CRLF line endings (a leading UTF-8 byte order mark is
  tolerated, UTF-16 is rejected), and a duplicate key at either level, an
  unknown setting and an unparsable value are all errors naming the line. A
  fuzz target holds the reader to that contract, and CI runs a bounded pass of
  it.
- `--write-baseline <file>` records the findings a set of files produces now,
  and `--baseline <file>` reports only what is not in that recording. Entries
  hold no line or column and ignore the unquoted numbers inside a message, so
  they survive edits above them and statistics that shift as a file grows.
  Quoted values and the code point of a character finding are part of the
  match, so swapping one defect for another — a different bad date, control
  number or homoglyph — is still reported. Identical findings collapse into
  one entry with a count, so one more of the same defect is reported too. The
  document is sorted and carries no timestamp, so re-recording an unchanged
  set produces the same bytes.
- Release engineering: pushing a `v*` tag builds and publishes archives for
  darwin, linux and windows on amd64 and arm64 via goreleaser, a Homebrew
  cask in `crb2nu/homebrew-tap` (skipped cleanly until the tap and its
  token exist), and a from-scratch Docker image at `ghcr.io/crb2nu/edilint` —
  a static binary and nothing else, because a linter that makes no network
  calls needs no CA bundle, shell or libc. `goreleaser check` runs in CI so
  the release configuration cannot drift between tags. A pre-commit hook
  definition lets `pre-commit` shops pin a released tag and lint interchange
  files before they are committed.
- `edilint mcp` serves the checks over the Model Context Protocol on standard
  input and output, so a coding agent can lint the files it generates without
  shelling out. Four read-only tools: `lint_file`, `lint_text`, `list_rules`
  and `explain_rule`. The lint tools take the same tuning as the flags and
  return the command line's text diagnostics plus the `--json` document and an
  `exit_status`; findings are capped at 200 per file unless asked otherwise,
  with exact summary counts either way. Both protocol generations are spoken,
  the `initialize` handshake of revisions 2025-11-25 and earlier and the
  per-request metadata of 2026-07-28, using the standard library only. The
  server makes no network connection and reads only the files a call names.

### Changed

- The X12 character-set default is `extended`; `--charset basic` is the opt-in
  strict profile.
- `--max-findings` bounds printed output only. Findings are counted in full, so
  the exit status and the summary never depend on it.
- A file that cannot be read no longer discards the findings already computed
  for the files that could. The run still exits 2.
- Diagnostic lines now name the rule identifier alongside the rule name:
  `error: [EL3006 envelope.segment-count] ...`. Both are greppable.
- The JSON schema is version 3. Findings gained `id`, summaries gained `infos`,
  and `severity` widened to include `info`. The documented policy for the
  version field now covers additions as well as removals and changes of
  meaning, since a consumer pinned to a version can meet any of them.
- A `--disable` entry that names no rule, identifier or class is a usage error
  rather than a silent no-op.

### Fixed

- Bounded finding retention. A defect-dense input, such as a binary file caught
  by a shell glob, used to allocate one finding per offending byte; 8 MB of
  binary input peaked above 3 GB of resident memory.
- Input that is not text is now reported once and skipped, rather than producing
  one finding per byte.
- A clean file emits `"findings": []` rather than `null`, so the documented `jq`
  pipeline works on clean input.
- `Lint` no longer panics on a caller-built `Layout` or `CountRule` that the CLI
  would have rejected. Both are validated and reported as findings.
- An unusable `Options.Delimiter` is reported instead of being silently ignored.
