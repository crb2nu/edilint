# edilint Roadmap — swarm spec (2026-08-01)

> Last Updated: 2026-08-15
> Tier: 1 (see workspace AGENTS.md "Portfolio Tiers")
> Tracking Issue: none open — backlog is the
> [issues list](https://github.com/crb2nu/edilint/issues)

Each workstream section below is a self-contained agent brief: goal, owned paths,
dependencies, acceptance criteria. Standing constraints apply to every workstream
and are non-negotiable.

## Current Status

edilint is a public single-binary Go linter for healthcare interchange files —
X12 EDI, HL7v2 messages and batches, EDIFACT, delimited and fixed-width. Unlike
the rest of the portfolio it lives on GitHub (`crb2nu/edilint`), not the internal
GitLab, so its CI and backlog conventions differ from the workspace default.

The repository is 30 commits old and every one of them landed in the last 90
days. The v0.1 gate plus four workstreams have merged via PRs #1–#4: B (rule
system foundation), A (HL7v2 batch + EDIFACT envelope coverage), C (SARIF,
JUnit and GitHub annotation outputs), and D (release engineering), the last on
2026-08-07. Release automation — goreleaser, tag-driven releases, a Docker
image, and a pre-commit hook — is wired but **has never fired**: the repository
carries no tags, so there is no published release and no install path for a
stranger. That unfired release, not missing checks, is what currently separates
this from the "install to first useful finding in under two minutes" exit
criterion below.

Evidence: `git log` window 2026-08-02 → 2026-08-14 on `main`; `git tag` empty;
`.github/workflows/{ci,release}.yml`; GitHub issues enabled with zero open
issues. Inspected 2026-08-15.

- **Plan store**: none — this file is the plan
- **Deployed**: not deployed (CLI; distribution is GitHub releases, once tagged)
- **CI**: GitHub Actions (`ci.yml`, `release.yml`) — not the shared
  `platform/gitops` GitLab templates

## Vision

A single-binary linter for EDI and healthcare flat files that lives in CI pipelines.
Quiet when clean, precise when not, exit codes that gate sends. The measure of success
is not stars — it's the first stranger whose pipeline fails because edilint caught a
real defect before their trading partner did.

## Scope guards (what edilint is not)

- Not an implementation-guide validator. SNIP Types 2–7 belong to pyx12 and commercial
  validators; the README says so and links them. Scope is SNIP Type 1 subset plus the
  hygiene layer SNIP doesn't name.
- No licensed spec content. Envelope structure, control segments, and character sets
  are public knowledge; IG loop/segment tables are licensed X12 content and stay out.
  Same boundary for NCPDP (licensed) — flat-file *structure* checks only, if ever.
- No network calls, no telemetry, no accounts. A linter that phones home is dead on
  arrival in healthcare shops.

## Standing constraints (every agent, every workstream)

- No proprietary material of any kind. All fixtures are synthetic and fictional.
- Go, stdlib-first; new dependencies need a written justification in the MR/PR.
- Plain factual prose in all docs — no marketing adjectives, no AI-tooling jargon.
- American English spelling everywhere: code, comments, docs, and commit messages
  (behavior, catalog, normalize, analyze).
- Every check has: a stable rule ID, table-driven tests (pass + fail fixtures),
  one-line rationale in the rule reference, and a documented false-positive story.
- Repo conventions (layout, CI, commit format) are whatever v0.1 establishes; read
  before writing.

## Phases

### v0.1 — the gate (in flight, single agent)
Engine + CLI as specced: X12 envelope integrity (ISA/IEA, GS/GE, ST/SE, recounts),
charset profiles, homoglyph/character hygiene, terminator consistency, trailer
recounts (`--count-rule`), field-count consistency, fixed-width layouts, duplicate
ISA13/ST02, envelope date-times. `--json`, exit codes 0/1/2, README with install +
CI snippet + related-tools section. Ships when CI is green and the maintainer (Cody)
has reviewed the private repo.

### v0.2 — trustworthy in anger (workstreams B, A, C, D — B lands first)
The release that makes brownfield adoption possible: rule IDs you can suppress,
a baseline for legacy files, outputs CI systems understand natively, binaries
people can actually install.

### v0.3 — the tools people share (workstreams E, F)
`fmt`, `fix`, `diff`, `stats`. Linters get installed; formatters and diffs get
recommended. This phase is why anyone tells a coworker.

### v1.0 — ecosystem (workstreams G, H)
Rule reference site, published Action, pre-commit hook, streaming performance,
fuzzing. The boring maturity work that separates a weekend project from a tool.

### Exploration (not scheduled)
- `edilint gen` — synthetic X12/HL7v2 test-file generator (`edilint gen 837p
  --claims 100`). Test data is a real, unserved pain; also feeds our own corpus.
  Big enough to be its own project; decide after v1.0.
- Community rule packs (versioned YAML rules for shop-specific conventions).
- NCPDP batch structure checks (licensing review first).

---

## Workstream B — rule system foundation (BLOCKS A, C, G)

**Goal:** every finding carries a stable rule ID (`EL####`, grouped by class:
EL1xxx charset/hygiene, EL2xxx terminators, EL3xxx X12 envelope, EL4xxx counts,
EL5xxx fixed-width, EL6xxx HL7v2, EL7xxx EDIFACT); severity levels (error/warning/
info); per-rule disable via flag and inline config file (`.edilint.yml`: enabled
rules, severities, charset profile, count-rules, layout paths); `--baseline
baseline.json` (record current findings, report only new ones — the brownfield
adoption path).
**Owns:** `internal/rules/`, config loading, finding struct, CLI flag surface.
Touches every check package mechanically (ID assignment) — run SOLO, nothing else
in flight.
**Accept:** all existing checks emit IDs; `edilint --list-rules` prints the table;
baseline round-trips (create → re-run → zero findings → new defect → one finding);
config file documented in README.

## Workstream A — format coverage: HL7v2 batch + EDIFACT envelope

**Goal:** HL7v2 batch structure (FHS/BHS/BTS/FTS pairing, BTS-01/FTS-01 recounts,
MSH field-separator/encoding-character consistency across messages, segment
terminator checks) and EDIFACT envelope (UNB/UNZ, UNH/UNT pairing, UNT-01 recounts,
UNA service-string consistency, control-reference matching). Envelope level only —
no message-content validation.
**Owns:** `internal/checks/hl7batch/`, `internal/checks/edifact/`, their fixtures,
format-detection additions.
**Depends:** B (rule IDs EL6xxx/EL7xxx).
**Accept:** synthetic batch fixtures pass/fail per rule; detection distinguishes
HL7v2 single vs batch, X12 vs EDIFACT, correctly on all fixtures.

## Workstream C — CI-native outputs

**Goal:** `--format sarif` (GitHub code scanning ingestible — validate against the
SARIF 2.1.0 schema), `--format junit` (CI test panels), `--format github` (Actions
workflow annotations). JSON schema for the default `--json` output, versioned and
committed.
**Owns:** `internal/output/`, output-format docs.
**Depends:** B (stable IDs/severities are the SARIF vocabulary).
**Accept:** SARIF file accepted by GitHub code-scanning upload in a test repo;
JUnit renders in GitLab CI; schema validated in CI.

## Workstream D — release engineering (independent; start anytime after v0.1)

**Goal:** tagged releases with goreleaser (darwin/linux/windows, arm64+amd64),
Homebrew tap (`crb2nu/homebrew-tap`), minimal Docker image (scratch/distroless),
a composite GitHub Action (`crb2nu/edilint-action`) with `files`, `format`,
`baseline` inputs, and a `pre-commit` hook definition.
**Owns:** `.goreleaser.yml`, release workflow, tap repo, action repo. No engine code.
**Accept:** `brew install crb2nu/tap/edilint` works on a clean machine; the Action
runs green in a demo repo; `docker run edilint` lints a mounted file.

## Workstream E — fmt + fix

**Goal:** `edilint fmt` — canonical pretty-print for X12 (segment-per-line, stable
round-trip: fmt(fmt(x)) == fmt(x), `--check` mode for CI) and HL7v2 batch.
`edilint fix` — safe-by-default repairs, each tied to the rule it clears: normalize
terminators, strip BOM, recount trailers (declare-what-you-counted, the modern
x12norm), zero-pad envelope times; `--unsafe` tier for homoglyph→Latin substitution
(always prints a diff, never in-place without `--write`).
**Owns:** `internal/format/`, `internal/fix/`, `cmd` wiring for both subcommands.
**Depends:** B; benefits from A landing first (fmt for HL7 batch).
**Accept:** round-trip property tests; every fix has a before/after fixture pair and
clears exactly its rule; `fix --dry-run` output matches what `--write` then does.

## Workstream F — diff + stats

**Goal:** `edilint diff a.edi b.edi` — structural, element-level diff (aligned by
segment position within transaction sets, not byte offset; ignores cosmetic
terminator/whitespace differences unless `--strict`) for vendor spec disputes.
`edilint stats` — file census: interchange/group/transaction counts by type,
control-number ranges, date ranges, charset profile observed, segment histogram.
**Owns:** `internal/diff/`, `internal/stats/`, cmd wiring.
**Depends:** B. Independent of E — can run in parallel with it.
**Accept:** diff of a fixture pair with one changed element reports exactly that
element with segment path; stats output on the 837 fixture matches hand-counted
values; both support `--json`.

## Workstream G — rule reference + docs site

**Goal:** shellcheck-style per-rule pages generated from rule metadata (what it
catches, why it matters, failing example, fix, false-positive notes) — a `docs/rules/`
tree rendered to GitHub Pages, plus a landing page. Every finding prints its rule URL.
**Owns:** `docs/`, pages workflow, rule-metadata extraction tooling.
**Depends:** B; content grows as A/E/F land.
**Accept:** every shipped rule has a page; CI fails if a rule lacks one; finding
output links resolve.

## Workstream H — performance + robustness

**Goal:** streaming check architecture for multi-GB files (bounded memory,
single pass where the check allows), benchmarks in CI with regression thresholds,
go-fuzz harnesses for every parser (X12 tokenizer, HL7 batch, EDIFACT, fixed-width),
crash-free guarantee on arbitrary bytes.
**Owns:** parser internals refactor coordination (SOLO while touching parsers),
`fuzz/`, benchmark suite.
**Depends:** A, E, F merged (parsers stable).
**Accept:** 2 GB synthetic file lints in bounded memory; fuzzers run clean for a
sustained CI budget; benchmarks tracked.

---

## Sequencing & swarm rules

```
v0.1 (in flight)
  └── B (solo — foundation refactor)
        ├── A ──┐
        ├── C ──┼── parallel wave 1
        ├── D ──┘   (D can even start before B)
        ├── E ──┐
        ├── F ──┼── parallel wave 2
        └── G ──┘   (G trails, consumes everything)
                └── H (solo — parser refactor, after wave 2)
```

- One agent per workstream, repo-local worktree, branch `feat/<workstream>`,
  conventional commits, MR/PR per the repo's flow. Solo workstreams (B, H) get the
  repo to themselves — nothing else merges while they're open.
- Every agent reads `ROADMAP.md` + its own section only; scope outside the owned
  paths is a review rejection, not a judgment call.
- Maintainer (Cody) reviews every MR before merge in v0.2; can delegate to
  auto-merge-on-green later if the review burden says so.

## Definition of "legitimately useful" (v1.0 exit criteria)

- Install to first useful finding in under two minutes on a stranger's machine
- Passes its own dogfood: fi-fhir CI runs edilint on its X12 fixtures
- Listed on awesome-edi; announced once on r/edi and the HL7/interop communities —
  factual post, no launch theater
- One documented case (even a friend's shop) of a defect caught pre-send
- Zero telemetry, zero network, zero accounts — verifiable by reading main()

## Backlog

Full backlog: [open issues](https://github.com/crb2nu/edilint/issues) ·
[pull requests](https://github.com/crb2nu/edilint/pulls)

This repo is on GitHub, so the workspace `P1`/`P2`/`P3` GitLab label convention
does not apply here. Zero issues were open as of 2026-08-15 — the workstream
sections above are the working backlog. Open a GitHub issue for a workstream
when an agent picks it up, so in-flight work is visible outside this file.
```
