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
- `--json` output, documented and versioned, currently schema version 2.
- Exit statuses suitable for gating a send script: 0 clean, 1 findings, 2 the
  tool could not do its job.
- `--count-rule`, `--layout`, `--charset`, `--disable`, `--max-findings` and
  `--type-field` for tuning a run to a partner's conventions.

### Changed

- The X12 character-set default is `extended`; `--charset basic` is the opt-in
  strict profile.
- `--max-findings` bounds printed output only. Findings are counted in full, so
  the exit status and the summary never depend on it.
- A file that cannot be read no longer discards the findings already computed
  for the files that could. The run still exits 2.

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
