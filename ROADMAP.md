# edilint Roadmap — swarm spec (2026-08-01)

> Last Updated: 2026-11-04
> Tier: 1 (see workspace AGENTS.md "Portfolio Tiers")
> Tracking: [open issues](https://github.com/crb2nu/edilint/issues) — none on 2026-10-31; the Now list below is the backlog. Mills reads only GitLab issues on libs/edilint labeled `mills-eligible`, never GitHub.

Each workstream section below is a self-contained agent brief: goal, owned paths,
dependencies, acceptance criteria. Standing constraints apply to every workstream
and are non-negotiable.

## Current Status

The latest published release is `v0.6.0` (2026-11-04, America/New_York).
Install with `go install github.com/crb2nu/edilint/cmd/edilint@v0.6.0`, or use
its [release archives](https://github.com/crb2nu/edilint/releases/tag/v0.6.0).

On 2026-11-03 `.gitlab-ci.yml` gained its first `workflow:` block (GitLab
!38): push pipelines on Mills' `mills-mq/` train refs are refused, so a
merge-train row here is proven by the one pipeline the queue mints through
the API; !38 itself was the first edilint row to merge through the train
(Mills posture below). The same day three `bl-edilint-*` rows were posted
for the Now list and the first was in review within the hour.

Version `v0.4.0` shipped the streaming APIs, browser build, rule references,
acknowledgment guidance, and release gates described below. Version `v0.4.1`
added the browser release archive. Version `v0.5.0` ships synthetic fixture
generation for 837P, HL7v2, 835 and EDIFACT with targeted envelope defects
(#16–#19) and EDIFACT trailer recounts in `fix` (#20), merged 2026-10-03 and
2026-10-04; the Go 1.26.10 toolchain move (GitLab !31, 2026-10-27); the three
samples the flexinfer.ai [field guide to invisible EDI
defects](https://flexinfer.ai/blog/invisible-edi-defects-field-guide) runs
(`examples/homoglyph.x12`, `shortcount.x12`, `mixed-endings.psv`) with
`TestExamplesMatchTheirTranscripts`, which pins the full output and exit
status of every field-guide and README example command (GitLab !32,
`2f7f8c8`, 2026-10-31); and the idempotent byte order mark strip in `fix`,
which takes every consecutive leading UTF-8 mark in one EL1001 repair
(GitLab !34, `35a47ab6`, 2026-11-01). The `chore(release): prepare v0.5.0`
merge request dated the CHANGELOG section that `internal/releasecheck`
requires and removed the stale `changelog.d/` fragment. Publication follows
the exact-commit checks in [Releasing](docs/releasing.md).

Version `v0.6.0` ships the composite GitHub Action as a root `action.yml`
(`uses: crb2nu/edilint@v0.6.0`, with `files`, `format`, `baseline` and
`version` inputs and an `exit-code` output) and its `action-smoke` workflow
(GitLab !42, `8e40d965`, 2026-11-04); the README transcript test
`TestREADMETranscriptsMatch` (GitLab !39, `591293c7`, 2026-11-03); the
README's Homebrew, release-archive and container install channels with a
pinned pre-commit example (GitLab !41, `d0a6defc`, 2026-11-03); the
GoReleaser v2.19.0 move with the cask's declarative `postflight_steps`
quarantine hook (GitLab !37, `e11e9bfb`, 2026-11-01); and the GitLab CI
never-rule for push pipelines on `mills-mq/` merge-train refs (GitLab !38,
`e9cb20cc`, 2026-11-03). The `chore(release): prepare v0.6.0` merge request
dated the CHANGELOG section, moved every README version pin and the Action's
default `version` input to `v0.6.0`, and updated this status; the tag and the
publication checks follow [Releasing](docs/releasing.md).

`git log` records merged workstreams B (rule system foundation), A (HL7v2 batch
and EDIFACT envelope coverage), C (SARIF, JUnit, and GitHub annotation outputs),
and D (release engineering), completing v0.2, and E (`fmt` + `fix`, !2), F
(`diff` + `stats`, !1) and I (MCP server, !8; acknowledgment cross-reference,
!9), all merged 2026-09-03, completing v0.3 and the first piece of v1.0. These tools shipped in `v0.3.0`. Workstream G now provides generated pages for all 48 rules, examples and repair
guidance, diagnostic URLs, a drift check, and a GitHub Pages workflow
([tracking issue #10](https://github.com/crb2nu/edilint/issues/10)). GitLab !22
adds matching Markdown pages with executable examples and a drift guard for
repository readers. Workstream H
now has seeded parser fuzzing, bounded CI fuzz passes, and lint benchmarks with
allocation regression budgets, bounded-memory reader/file linting, and
reader parity fuzzing. The 2 GiB acceptance test passed under a 128 MiB sampled
Go heap ceiling ([tracking issue #12](https://github.com/crb2nu/edilint/issues/12)).

Statistics also stream through `StatsReader` and `StatsFile`; the 2 GiB
acceptance test passed under the same heap ceiling ([#13](https://github.com/crb2nu/edilint/issues/13)).
Acknowledgment references now include EDIFACT CONTRL reporting levels and
qualified HL7v2 ACK guidance, with public sources and explicit limits for
batch/file errors ([#14](https://github.com/crb2nu/edilint/issues/14)).

The canonical repository is `gitlab.flexinfer.ai/libs/edilint`; README
`Repository` documents GitHub as its push mirror. GitLab CI runs on merge
requests, while the GitHub Actions workflows continue to run on the mirror.

The MCP server (workstream I) is wired into the workspace's loom MCP registry
as `edilint` (`lint_file`, `lint_text`, `list_rules`, `explain_rule`, all
auto-allowed, run from the installed binary), so coding agents in this
workspace lint interchange files without shelling out. fi-fhir's CI lints its
X12 fixture corpus with edilint on every merge request (`lint:edi` pinned to
v0.3.0, `lint:edi-fixtures` to v0.1.0). The Homebrew cask publishes to
`crb2nu/homebrew-tap` from the release workflow, and the tap's own CI installs
the cask on macOS 15 (arm64 and Intel) on every cask push. The 0.5.0 cask push
(tap commit `21e3f804`) failed that CI's arm64 `brew style` step: Homebrew's
`Cask/InstallSteps` cop (brew PR 23366) rejects the legacy `postflight` block
GoReleaser v2.18.2 emitted; the Intel runner's older Homebrew passed, the
checksums match and `brew install` never runs the style check. GitLab !37
moved GoReleaser to v2.19.0 and the hook to `hooks.post.install_steps`,
which renders `postflight_steps`, and the tap's 0.5.0 cask was hand-migrated
to the same stanza (tap PR #2), so the next cask push renders the stanza the
tap already carries. The README documents every install channel since
GitLab !41.

The release pipeline now rehearses six platform archives and a multi-platform
container after merges, scans reachable vulnerabilities with a patched Go
compiler, and requires successful CI for the exact release commit before
publishing a stable version tag. See [Releasing](docs/releasing.md).

Evidence inspected 2026-10-31: canonical and mirror `main` both at `2f7f8c8`
(GitLab pipeline 36450 and the GitHub `ci` run for the same commit green,
`release ready` included), tags identical on both remotes through `v0.4.1`,
the cask and GHCR at 0.4.1, no open merge requests, pull requests or issues
on either forge, govulncheck clean under Go 1.26.10 (database of
2026-10-27), and a local run of the whole gate (build, vet, race tests at
93.1 % root coverage, lint, actionlint, bench, bounded fuzz) green. One
defect surfaced: the GitHub `ci` run for the 2026-10-27 toolchain commit
failed its bounded fuzz pass because `Fix` strips only one of two leading
UTF-8 byte order marks and so is not idempotent (`fix.go`, present since
v0.3.0, visible only since #20 added the idempotence assertion to
`FuzzEdifact`); the failing input was never committed, so later runs passed
by chance. GitLab !34 (2026-11-01) fixed it and committed that input as a
`FuzzEdifact` seed. A build of `2f7f8c8` lints fi-fhir's seven-file EDI
corpus clean with both of its CI command lines, so the pin bump below should
change no finding.

- **Plan store**: this file
- **Distribution**: `go install`, GitHub release archives, GHCR containers, and
  the `crb2nu/homebrew-tap` cask (the README documents only the first)
- **CI**: GitLab on the canonical repository and GitHub Actions on the mirror

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

### v1.0 — ecosystem (workstreams G, H, I)
Rule reference site, published Action, pre-commit hook, streaming performance,
fuzzing, and the MCP server that puts the checks in front of coding agents. The
boring maturity work that separates a weekend project from a tool.

### v0.5 — synthetic fixtures

[#16](https://github.com/crb2nu/edilint/issues/16) introduces `edilint gen 837p
--claims 100` and `edilint gen hl7v2 --messages 20`, backed by the streaming
`Generate` Go API. It supplies deterministic fictional examples, test usage
indicators, configurable date/file controls, correct envelope counts, and
bounded-memory output. These are structural test fixtures, not an
implementation-guide validator or production claim generator.

Acceptance: both formats lint clean with the in-memory and reader APIs; the
census matches the requested claims/messages; options are validated before
writing; repeated generation is byte-identical; output/flush failures are
reported; tests cover distinct file controls and count/date boundaries.

The riskiest assumption is that structural examples are sufficient for users'
test pipelines. Keep the initial templates narrow and gather real use cases
before adding message types or guide-specific behavior.

The follow-up [#17](https://github.com/crb2nu/edilint/issues/17) adds intentional
envelope defects through repeatable `--defect ID` and `GenerateOptions.Defects`:
X12 EL3005–EL3008 and HL7v2 EL6003–EL6004. Each selection produces exactly one
matching finding, including when supported defects are combined; selection
order does not affect the bytes. Invalid, duplicate, or cross-format IDs fail
before output. The clean defaults remain byte-identical, and `fix` restores
count defects to the clean fixture while leaving control mismatches unresolved.
Tests cover every supported combination through both lint APIs and the CLI's
generation/lint exit statuses. This provides deterministic failure fixtures for
pipeline tests without changing the linter's rules.

[#18](https://github.com/crb2nu/edilint/issues/18) extends generation to `835
--claims N`, with one HP group and 835 transaction containing fictional remittance
claims and service lines. Example charge/payment/adjustment amounts balance, and
the BPR total equals the sum of claim payments using integer arithmetic. The
format shares X12 envelope counting and defects EL3005–EL3008. Acceptance covers
lint and census results, claim ID uniqueness, amounts and date/control options,
all defect combinations, repairs, write failures, and unchanged earlier defaults.
This tests remittance-processing pipelines without adding payment validation or
implementation-guide claims to the linter.

[#19](https://github.com/crb2nu/edilint/issues/19) adds `edifact --messages N`:
one test interchange with UNA and narrow ORDERS D.96A examples based on the
repository fixture. Message/order references are unique; UNT and UNZ counts
match the generated records. EL7003 and EL7006 affect only the first UNT, while
EL7005 affects UNZ, so each requested defect produces exactly one finding.
Acceptance covers both lint APIs, census, raw envelope fields and counts,
date/control options, every defect subset, writer failures, bounded generation,
and unchanged earlier output. Count repairs follow in #20 below.
The riskiest assumption remains whether narrow structural examples serve users'
pipelines; functional groups and partner-specific content are not modeled.

[#20](https://github.com/crb2nu/edilint/issues/20) adds safe EDIFACT trailer
recounts to `fix`: UNT-1, UNE-1, and UNZ-1. Complete, consistently nested files
can be repaired with default or custom service characters while preserving
references, payload, escapes, and whitespace. Ambiguous structure prevents all
count edits for that file. Tests cover grouped/ungrouped and multiple interchanges,
empty and malformed count values, idempotence, generator repair round-trips,
and CLI dry-run/write parity. The riskiest assumption is that an incorrect
declaration, rather than lost content, caused the mismatch; the docs retain
the recount caution and repairs never synthesize missing segments.

### Exploration (not scheduled)
- Additional synthetic message types and defect families. A Mills Spinning
  Room draft plan for this line has existed since 2026-10-30 (an X12 834
  generator with the shared envelope defects, two slices, intent `draft`,
  spun from an operator-written brief; see Mills posture for why it should
  not be approved as spun). It stays unscheduled until the v0.5 section's
  riskiest assumption has evidence from a real consumer, which is the fi-fhir
  fixtures item in Now.
- Community rule packs (versioned YAML rules for shop-specific conventions).
- NCPDP batch structure checks (licensing review first).

## Now

Next slices as of 2026-11-03, ordered. Each fits one merge request and names
the paths it owns in its first line, so a Mills run or a person can take it
without widening scope and the roadmap carder, if it is ever turned on for
this repository, cards the action and its paths. Open a GitHub issue when
picking one up by hand so the work is visible outside this file; to hand one
to Mills, post a `bl-edilint-*` row with the files declared (Mills posture
below). Items already in the lane name their row id.

### Mills push (2026-11-03)

The push through the Mills lane, in order: `bl-edilint-readme-transcripts-from-readme-202611`
(the README transcript test; dispatched within a minute of posting, in
review the same hour), `bl-edilint-install-channels-readme-202611` (every
install channel documented, pre-commit pinned), and
`bl-edilint-composite-action-root-action-yml-202611` (the Action as a root
`action.yml` plus a smoke workflow; its `.github/**` touches are declared, so
that merge request waits for the maintainer). The budget overlay (2 USD per
run, 3 runs per day) paces them over two days. Gated behind other evidence,
not posted: the X12 834 generator (Exploration) waits for the fi-fhir fixtures
row to report whether structural fixtures served a real consumer; the
GoReleaser v2.20.0 move waits for that release. Stays with the maintainer:
v1.0 exit evidence, and cutting `v0.6.0` once the three rows have landed
(Releasing, with the 2026-11-01 recipe; cut on 2026-11-04, see the Now
item). Kill-test for the lane itself: the first row merges through the train
on an `api`-source proof; if it escalates on the devbox or the
protected-path hold instead, the other two are held until the cause is
known.

- [ ] **Move `.github/workflows/ci.yml`, `.github/workflows/release.yml` and
  `docs/releasing.md` to GoReleaser v2.20.0 once it ships, for `zap
  :not_required`** — Homebrew master (7.0.11
  on 2026-11-01) adds `Cask/ZapNotRequired`, which rejects the `# No zap
  stanza required` comment GoReleaser v2.19.0 renders when no zap is
  configured; GoReleaser main already renders `zap :not_required`
  (`internal/pipe/cask/template.go`, `zapString`) but the newest stable
  release is v2.19.0 and v2.20.0 exists only as nightlies. The tap's arm64
  `brew style` will fail on the next cask push once the runner's Homebrew
  carries that cop (its 2026-11-01 run did not yet). When v2.20.0 ships, move
  the three `goreleaser-action` pins (`.github/workflows/ci.yml` x2,
  `.github/workflows/release.yml`) and `docs/releasing.md` to it, re-run the
  snapshot rehearsal and `brew style` the rendered cask against Homebrew
  master (`brew update --force` inside a `homebrew/brew` container reaches
  it; the Docker Hub image itself is stuck at 4.6.20). Owns only those four
  files.
- [x] **Make `fix`'s BOM strip idempotent** — `Fix` removes one leading UTF-8
  byte order mark per pass (`stripBOM` in `fix.go`, `splitBOM` in
  `edilint.go`), so a file that starts with two marks needs two passes. That
  contradicts the doc comment's "applying Fix to its own output yields no
  further repairs" and failed the bounded fuzz pass of GitHub `ci` run
  40544409969 on 2026-10-27; because `release.yml` re-runs that pass on the
  tag, it can fail a publish at random. Owns `fix.go`, `fix_test.go`, a
  committed `testdata/fuzz/FuzzEdifact/` seed and a Fixed line under
  Unreleased. Accept: one repair removes every consecutive leading mark in
  every format, a second pass reports none, and the seed passes under plain
  `go test`. Landed as GitLab !34, 2026-11-01.
- [x] **Cut `v0.5.0`** — rename Unreleased to `## [0.5.0] - <date>`, add an
  empty Unreleased, delete the stale
  `changelog.d/feat-bl-edilint-rules-docs-20260901-rulesdoc.added.md` (its
  content shipped in 0.4.0 and nothing folds that directory), merge, wait for
  the GitLab main pipeline and the GitHub `ci` run (with `release ready`) on
  that exact merge commit, then push the annotated tag to `origin` only, per
  [Releasing](docs/releasing.md). Afterwards confirm the release page (six
  CLI archives, the wasm archive, checksums), `ghcr.io/crb2nu/edilint:0.5.0`,
  and the tap's cask commit with its install run; then update Current Status
  above. The `chore(release): prepare v0.5.0` merge request did the CHANGELOG,
  `changelog.d/` and Current Status steps on 2026-11-01; the tag and the
  publication checks follow Releasing.
- [x] **Bump fi-fhir's pins** — closed 2026-11-01 by fi-fhir
  [MR !289](https://gitlab.flexinfer.ai/libs/fi-fhir/-/merge_requests/289):
  `lint:edi` (`ci/lint-edi.yml`, from v0.3.0) and `lint:edi-fixtures`
  (`ci/lint-edi-fixtures.yml`, from v0.1.0) pin v0.5.0, with the Makefile
  mirrors. Both command lines, run with the v0.5.0 binary, report the
  seven-file corpus clean, so the bump changed no finding; growing the
  corpus stays with the fixtures item below.
- [ ] **Generate fi-fhir's EDI fixtures with `edilint gen 837p` and `gen 835
  --defect`** — in the fi-fhir lane as
  `bl-fifhir-edi-fixtures-from-edilint-gen-202611` (posted 2026-11-03), whose
  merge request reports back whether the fixtures sufficed. fi-fhir's
  `testdata/edi/*.edi` corpus is seven hand-written files; `edilint gen 837p` and `gen 835` with
  `--defect` can supply deterministic pass and fail fixtures for its
  `internal/parser/edi` tests (fi-fhir issue #15). The first real consumer of
  the generator, and the evidence the v0.5 section's riskiest assumption asks
  for; it gates any further message type (see Exploration).
- [x] **Document every install channel in `README.md` (pinned `go install`,
  Homebrew, release archives, GHCR) and pin the pre-commit example** — in the
  lane as `bl-edilint-install-channels-readme-202611` (posted 2026-11-03).
  The tap's own CI already installs the
  cask on macOS 15 arm64 and Intel on every cask push, so the workstream D
  acceptance line is proven by machine; what is missing is the README
  (`README.md` Install): `brew install crb2nu/tap/edilint`, the release
  archives, `docker run --rm -v "$PWD:/work" ghcr.io/crb2nu/edilint:<version>
  /work/<file>`, and the pre-commit paragraph's "once a tagged release exists
  to pin", stale since v0.1.0. Owns `README.md` and a CHANGELOG line. Merged as
  GitLab !41 (`d0a6defc`, 2026-11-03).
- [x] **Add `TestREADMETranscriptsMatch` to `cmd/edilint/main_test.go` and delete
  `TestREADMEExamplesRun`** — in the lane as
  `bl-edilint-readme-transcripts-from-readme-202611` (dispatched 2026-11-03).
  `TestExamplesMatchTheirTranscripts` (`cmd/edilint/main_test.go`) pins
  output against literals in the test, so the README can drift from the test
  without failing anything, and a message change must be copied to three
  places (test, README, the field guide in flexinfer-site). Add a test that
  extracts every `$ edilint …` line from the README's `sh` blocks whose
  inputs exist under `examples/`, runs it from the repository root and
  compares the transcript lines that follow; skip the `diff ours.x12
  theirs.x12` example through an explicit allowlist; delete the now-redundant
  `TestREADMEExamplesRun`. Owns `cmd/edilint/main_test.go` only. Accept:
  changing one character of a README transcript fails the test. Merged as
  GitLab !39 (`591293c7`, 2026-11-03).
- [x] **Composite GitHub Action as a root `action.yml` with a smoke workflow
  `.github/workflows/action-smoke.yml`** (workstream D) — decided 2026-11-03:
  the Action lives in this repository, referenced as
  `uses: crb2nu/edilint@v0.5.0` (the mirror already carries the tags; a
  separate `crb2nu/edilint-action` would need its own release train), with
  `files`, `format`, `baseline` and `version` inputs and `go install` inside
  for v1. In the lane as `bl-edilint-composite-action-root-action-yml-202611`
  with the `.github/**` touches declared, so its merge request is held for
  the maintainer's review. Accept when the smoke workflow on the mirror goes
  red on `examples/shortcount.x12` and green on `examples/remittance.x12`;
  the README's Actions snippet switches to `uses:` in the same change.
  Merged as GitLab !42 (`8e40d965`, 2026-11-04); the mirror's `action-smoke`
  run 41767284590 on that commit was green, its `defect` job seeing exit 1
  (EL3006) on `examples/shortcount.x12`.
- [x] **Cut `v0.6.0`** — rename Unreleased to `## [0.6.0] - <date>`, add an
  empty Unreleased, move the README's `go install`, archive, container,
  Actions and pre-commit pins and the Action's default `version` input to
  `v0.6.0`, merge through the Mills queue, wait for the GitLab main pipeline
  and the GitHub `ci` run (with `release ready`) on that exact merge commit,
  then push the annotated tag to `origin` only, per
  [Releasing](docs/releasing.md). Afterwards confirm the release page (six
  CLI archives, the wasm archive, checksums), `ghcr.io/crb2nu/edilint:0.6.0`,
  the tap's cask commit with its install run, and the mirror's `action-smoke`
  run on the tag; then update Current Status above. The `chore(release):
  prepare v0.6.0` merge request did the CHANGELOG, README, `action.yml` and
  Current Status steps on 2026-11-04; the tag and the publication checks
  follow Releasing.
- [ ] **Derive the Action's default `version` from its own ref in
  `action.yml`** — `action.yml` pins `default: v0.6.0`, so every release must
  bump it in the release-prep merge request, and the mirror's `action-smoke`
  run on that prep merge commit is red until the tag exists (`go install` of
  a tag that is not there yet; only `ci.yml` gates publication, so this is
  noise, not a block). Use `github.action_ref` as the fallback when `version`
  is empty, so `uses: crb2nu/edilint@v0.7.0` installs v0.7.0 with no input,
  and give the smoke workflow (`uses: ./`, where that ref is empty) an
  explicit `version`. Owns `action.yml`, `.github/workflows/action-smoke.yml`
  and the README's Actions paragraph. Accept: a tag push's smoke run installs
  the tagged version with no `version` input, and the prep merge request for
  the following release touches no `action.yml` line.
- [ ] **v1.0 exit evidence** — a listing on awesome-edi, one factual
  announcement (the field guide is a long-form post on flexinfer.ai; whether
  it counts is the maintainer's call), and one documented pre-send catch.
  Maintainer work, not agent work.
- [ ] **Merge-train main dedup** — decided against on 2026-10-31: a full main
  pipeline takes 6 to 12 minutes and the Mills lane has landed no row here
  yet, so skipping a tree the train proof already validated (loom-core's
  `scripts/ci/validated_tree_skip.sh` pattern) buys nothing. Revisit only if
  main's duration changes.

## Mills posture (2026-10-31)

**2026-11-03 update.** GitLab !38 added the `workflow:` never-rule for push
pipelines on `mills-mq/` refs and was itself the first edilint row through
the merge train: enqueued at 16:50 UTC, train ref
`mills-mq/train-libs-edilint-38-4ecef0a8`, no push pipeline, one
`api`-source proof (pipeline 37257, nine jobs, all green), merged at 16:54 as
`e9cb20cc`. Protected paths are now seven: platform/gitops !1049 added
`scripts/mirror-github.sh` and `internal/releasecheck/**`. Three
`bl-edilint-*` rows were posted the same day (Now, "Mills push"); the first
was dispatched within a minute and in review within the hour. One trap for
desktop-app sessions: the GitLab MCP merge tool there has no queue URL in its
environment and merges directly, so a row is enqueued with
`POST $MILLS_MERGE_QUEUE_URL/api/mills/merge-queue/enqueue` instead. The
rest of this section is the 2026-10-31 reading and stays as the record.

edilint has been in the Mills cross-repo lane since 2026-09-01 (platform/gitops
`k3s/mills/configmap-policy.yaml`; loom-core's HUD `SPAWN_PROJECTS`). Intake
is an open GitLab issue on libs/edilint labeled `mills-eligible` (polled
every five minutes; item `gl-74-<iid>`, title and description copied once as
the spec, priority from a `priority:P0`–`P3` label, default P2, no file list,
so declare the files before the first run), a direct `bl-edilint-*` post with
slices, or an approved plan routed to Mills. GitHub issues on the mirror are
read by nothing, which is why the Now list above asks for a GitHub issue only
for visibility. Protected paths are `.gitlab-ci.yml`, `.github/**`,
`.goreleaser.yml`, `LICENSE` and `SECURITY.md`; `scripts/mirror-github.sh`,
which runs with the GitHub mirror token on main, and the
`internal/releasecheck` tag gate are not, and should be. The budget overlay is
2 USD per run and 3 runs per day. Four items merged through the lane, all in
September (the 837P fixture family, the roadmap status refresh, the generated
rule pages, fi-fhir's dogfood job); nothing was posted between then and
2026-11-03, and no `mills-eligible` issue has ever existed here.

Since [platform/gitops MR !1005](https://gitlab.flexinfer.ai/platform/gitops/-/merge_requests/1005)
(2026-10-27) the lane proves merge requests on the merged-result train
(`proof_mode: merge_ref`, `train_window: 1`). Until 2026-11-03 no edilint
row had used it:
[MR !31](https://gitlab.flexinfer.ai/libs/edilint/-/merge_requests/31) and
[MR !32](https://gitlab.flexinfer.ai/libs/edilint/-/merge_requests/32) were
merged directly; !38 was the first (update above). Every job here runs on any ref, so a train ref's push
pipeline would run the full gating set, and the queue's habit of adopting a
green push pipeline would have been a real proof in this repository, unlike
in fi-fhir, whose four train merges were proven by a one-job pipeline
(fi-fhir ROADMAP, Mills posture). Since 2026-11-03 `.gitlab-ci.yml` carries
loom-core's `workflow:` never-rule for push pipelines on `mills-mq/` refs,
so a train here has exactly one proof, the pipeline the queue mints through
the API with `MILLS_MERGE_QUEUE=1`, ahead of the queue requiring its own
`api`-source proof (a loom-core item). The CI image matches
`.go-version` (1.26.10) and main pipelines take 6 to 12 minutes, so
main-pipeline dedup is not worth doing. Window 1 only: a wider window needs
an operator checkout of this repository, which exists for loom-core alone.
The hub's `MILLS_MERGE_QUEUE_PROJECTS` and `LOOM_MRWATCH_PROJECTS` still name
loom-core only; a workstation `merge_merge_request` already routes this
project through the queue.

No Mills component reads this file today. The 2026-10-30 X12 834 draft
(`plan-add-synthetic-x12-834-generator-with-targeted-envelope-defec-010e7f`)
was spun from an operator-written brief, has no backlog row, and places the
generator in `cmd/edilint/gen834.go` rather than beside the root-level
`generate.go`; respin it with the right files or drop it, and not before the
fi-fhir fixtures item has run. If `intake.carding.target_projects` is set to
include edilint, the carder reads `ROADMAP.md` on main every 15 minutes and
cards only unchecked `- [ ]` bullets under an H2 named exactly `Now`, `Next`,
`Later` or `Tier N` (other H2s card at priority 8), keeping only each
bullet's first line; the heading above is exactly `Now` for that reason, and
the first line of each item should carry the action and its paths before
carding is turned on. Two Now items are shaped for the lane and avoid every
protected path: the idempotent BOM strip (`fix.go`, `fix_test.go`, a fuzz
seed, CHANGELOG), which landed as !34 by hand, and the README transcript
guard (`cmd/edilint/main_test.go` only), posted on 2026-11-03 together with
the install-channels README item and the Action (the latter with its
`.github/**` touches declared, so it is held for review rather than refused).

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

**Delivered slice:** [#11](https://github.com/crb2nu/edilint/issues/11) adds
root-package Go fuzz targets for every parser and auto detection, report and
formatting invariants, and clean/malformed/scaling benchmarks. Both CI systems
retain benchmark output and gate allocation counts; minimized fuzz failures are
retained for reproduction. Fixed-width integer overflow and ambiguous X12
formatting found during the work are covered by regressions.

**Delivered streaming slice:** [#12](https://github.com/crb2nu/edilint/issues/12)
adds `LintReader`, moves `LintFile` and the lint CLI to bounded record iterators,
and replays input for whole-file statistics. Nonseekable inputs spool to a private
temporary file. Record and state limits fail explicitly without a partial report.
Fixture, baseline, error-path, and fuzz tests compare reader diagnostics with
`Lint`; both APIs have tracked benchmarks. `make stream-check` processed a
2 GiB synthetic X12 file (523,012 content segments) with about 4 MiB sampled Go
heap, below its 128 MiB ceiling. The ordinary suite runs an 8 MiB version.
`fmt`, `fix`, `diff`, and browser/MCP text inputs remain in-memory.

**Streaming statistics follow-up:** [#13](https://github.com/crb2nu/edilint/issues/13)
adds `StatsReader`, `StatsFile`, and bounded-memory `edilint stats`, preserving
version 1 output. Record histograms, envelope counts, control/date ranges, and
character profiles use bounded iterators and retained state instead of a full
X12 tree. The CLI exposes the same record and state limits as linting. Fixture
and fuzz parity guard malformed-input behavior; `make stats-stream-check`
verifies counts and sampled heap on a synthetic 2 GiB file.

**Goal:** streaming check architecture for multi-GB files (bounded memory,
single pass where the check allows), benchmarks in CI with regression thresholds,
go-fuzz harnesses for every parser (X12 tokenizer, HL7 batch, EDIFACT, fixed-width),
crash-free guarantee on arbitrary bytes.
**Owns:** parser internals refactor coordination (SOLO while touching parsers),
`fuzz/`, benchmark suite.
**Depends:** A, E, F merged (parsers stable).
**Accept:** 2 GB synthetic file lints in bounded memory; fuzzers run clean for a
sustained CI budget; benchmarks tracked.

## Workstream I — MCP server

**Goal:** `edilint mcp` serves the checks over the Model Context Protocol on
standard input and output, so a coding agent can lint the files it generates
and look up rule identifiers without shelling out. Tools: `lint_file`,
`lint_text`, `list_rules`, `explain_rule`, all read-only. Both protocol
generations are spoken (the `initialize` handshake through revision
2025-11-25 and the per-request metadata of 2026-07-28), with the standard
library only, so the zero-dependency, zero-network promise still holds.
Landscape research (2026-09-02) found no X12, HL7v2 or EDIFACT validation
exposed as an agent tool anywhere; FHIR `$validate` wrappers are the only prior
art.
**Owns:** `mcp/`, `cmd/edilint/mcp.go`, the README section, their tests.
**Depends:** B, C (rule IDs and the JSON document are the tool's vocabulary).
**Accept:** a client that speaks either generation lists four tools and gets
findings back with `exit_status`; every stdout line is one JSON message;
unknown arguments are tool errors, not silent no-ops; configuration file and
per-call options layer exactly as the flags do.

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
- Since 2026-09-01, merge requests land on the GitLab canonical repository and
  then mirror to GitHub (README `Repository`).
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

GitLab is canonical for merge requests; GitHub issues track the public backlog.
The workstream sections above define the remaining acceptance criteria. Open a
GitHub issue when picking up a new slice so work is visible outside this file.
