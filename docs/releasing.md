# Releasing edilint

Feature merges validate and rehearse a release. A maintainer chooses when to
publish by pushing a stable `vMAJOR.MINOR.PATCH` tag to the canonical GitLab
repository. Merging alone does not publish a version.

## What must pass

GitLab runs formatting, vet, lint, workflow syntax, rule-reference drift, race
tests, bounded fuzzing, allocation budgets, vulnerability scanning, Go 1.23
compatibility, and a CLI build. Its security jobs also scan source and secrets.
Only successful main pipelines mirror their commit to GitHub.

GitHub adds race tests on Linux, macOS, and Windows; a GoReleaser rehearsal of
all six OS/architecture archives; checksum and archive-content verification;
native packaged CLI tests for version, JSON output, and exit statuses 0/1/2;
and Linux amd64/arm64 container builds with a native container smoke test.
The `release ready` job requires every check to succeed.

Release tags must resolve to the checked-out commit, belong to main, have a
unique dated CHANGELOG section with authored notes, and not be older than an
existing stable version. Major versions 2 and above require a matching Go
module path. Prerelease and build-metadata tags are not supported yet.
GitHub requires the latest main push run of `ci.yml` for that **exact commit**
to have succeeded; another commit's green run does not qualify. Tag pipelines
then repeat the quality and packaging checks with current vulnerability data.
The final publish job rechecks eligibility immediately before publication.

Both CI systems use patched Go 1.26.8 for these checks and release builds.
Update `.go-version` and the `.go-base` image in `.gitlab-ci.yml` together when
updating the compiler. The separate Go 1.23 job checks compatibility only.
GoReleaser, golangci-lint, govulncheck, and actionlint versions are pinned in
the workflows or Makefile. A new reachable vulnerability blocks publication
until the compiler or dependency is fixed and all checks pass again.

## Rehearse without publishing

Run the GitHub `ci` workflow manually on main (Actions → ci → Run workflow),
or inspect the automatic run after a merge. Download its `release-rehearsal`
artifact to inspect the six archives, `checksums.txt`, and `metadata.json`.
Artifacts expire after seven days. This workflow cannot publish a release.

For a local rehearsal, use the Go version from `.go-version` and GoReleaser
v2.18.2:

```sh
export GOTOOLCHAIN="go$(cat .go-version)"
make ci release-checks
TAP_GITHUB_TOKEN='' goreleaser release --snapshot --clean --skip=publish,docker --parallelism=2
go run ./internal/releasecheck artifacts --dir dist
```

The last command checks every archive and executes the binary for the local
platform. The container and other native OS checks run in GitHub CI.

## Publish a version

1. Choose the next stable version. Move the intended entries from Unreleased
   into a section such as `## [0.4.0] - YYYY-MM-DD`, using the actual release
   date. Keep an Unreleased section for future changes. Merge this change
   through the normal feature/MR checks.
2. Wait for the resulting main commit's GitLab pipeline and GitHub `ci`
   workflow, including `release ready`, to succeed. Record that commit SHA.
3. From a clean checkout, fetch main and tags, create an annotated version tag
   at that exact SHA, and push **only that tag** to the canonical remote:

   ```sh
   git fetch origin main --tags
   git tag -a v0.4.0 <verified-main-commit-sha> -m 'Release v0.4.0'
   git push origin refs/tags/v0.4.0
   ```

   Replace the example version and SHA. Do not push tags directly to GitHub.
4. Wait for the GitLab tag pipeline, its tag-only mirror job, and the GitHub
   `release` workflow. Verify the GitHub release notes and six archives plus
   checksums, and the versioned image at `ghcr.io/crb2nu/edilint:0.4.0`.

Publication is serialized across versions. GoReleaser rebuilds from the same
tag/configuration after rehearsal and publishes the GitHub release, GHCR
version and `latest` image tags, and the optional Homebrew cask. Rehearsal
artifacts themselves are not promoted. The linter remains an offline binary;
the GitHub API check is a separate CI-only command.

## Credentials and repository settings

- GitLab protects `v*` tags so only maintainers can create release tags.
  `GITHUB_MIRROR_TOKEN` must be masked and protected, available to protected
  main and tag pipelines, and authorized to push contents and workflow files
  to `crb2nu/edilint`. A missing credential fails the mirror job.
- GitHub validation jobs have read-only repository access. Only the final
  publish job has `contents: write` and `packages: write`; it uses the built-in
  `GITHUB_TOKEN` for releases and GHCR. No publish token is inherited by the
  reusable quality workflow.
- `TAP_GITHUB_TOKEN` is optional. Configure a token able to update
  `crb2nu/homebrew-tap` to publish the cask; without it, that upload is skipped.

Main mirroring uses a fast-forward push and skips obsolete pipelines. Release
tag mirroring pushes only the validated tag and never changes main. Neither
path force-pushes published history or bulk-pushes unrelated tags.

## Failed or interrupted releases

If validation fails, fix the cause through a new main merge and rerun checks.
Do not move a published tag. If the commit itself must change after a tag was
pushed, use a new version with its own notes and green CI.

For a transient pre-publication failure, rerun the failed workflow/jobs after
the cause is resolved; guards run again. For a partial publication, first
inspect the GitHub release assets, GHCR tags, and optional tap commit. These
destinations are not one transaction, so a retry may encounter existing
assets. Prefer a new patch version if published artifacts or source need
correction. Do not delete/reuse a version or force `latest` backwards to make
a retry pass. Failed or unavailable CI evidence always blocks publication.
