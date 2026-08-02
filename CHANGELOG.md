# Changelog

All notable changes to this project are documented here.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and
this project will follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html)
from its first tag.

## [Unreleased]

Nothing has been released yet. The entries below are the changes that will make
up the first tag.

### Added

- X12 EDI, HL7v2, delimited and fixed-width linting across six check classes:
  character hygiene, terminator consistency, X12 envelope integrity, declared
  record counts, field-count consistency and fixed-width layout conformance.
- `--json` output, documented and versioned, currently schema version 3.
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
  file, except that `--disable` and `--count-rule` add to it.
- `--write-baseline <file>` records the findings a set of files produces now,
  and `--baseline <file>` reports only what is not in that recording. Entries
  hold no line or column and ignore the numbers inside a message, so they
  survive edits above them and statistics that shift as a file grows; identical
  findings collapse into one entry with a count, so one more of the same defect
  is still reported. The document is sorted and carries no timestamp, so
  re-recording an unchanged set produces the same bytes.

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
