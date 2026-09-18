#!/bin/sh
# Mirror only the ref validated by this pipeline. Never move GitHub main from a tag job.
set -eu
: "${GITHUB_MIRROR_TOKEN:?GITHUB_MIRROR_TOKEN is required for verified mirroring}"
: "${CI_COMMIT_SHA:?CI_COMMIT_SHA is required}"
: "${CI_DEFAULT_BRANCH:?CI_DEFAULT_BRANCH is required}"

git remote remove github 2>/dev/null || true
git remote add github https://github.com/crb2nu/edilint.git
# Keep the credential out of the remote URL and Git's diagnostic output.
github_git() {
  git -c credential.helper= -c 'credential.helper=!f() { printf "%s\n" "username=x-access-token" "password=$GITHUB_MIRROR_TOKEN"; }; f' "$@"
}

if [ -n "${CI_COMMIT_TAG:-}" ]; then
  if [ "$(git rev-parse "refs/tags/${CI_COMMIT_TAG}^{commit}")" != "$CI_COMMIT_SHA" ]; then
    echo "Release tag changed after validation; refusing to mirror it." >&2
    exit 1
  fi
  github_git push github "refs/tags/${CI_COMMIT_TAG}:refs/tags/${CI_COMMIT_TAG}"
elif [ "${CI_COMMIT_BRANCH:-}" = "$CI_DEFAULT_BRANCH" ]; then
  github_git fetch --no-tags github "+refs/heads/${CI_DEFAULT_BRANCH}:refs/remotes/github/${CI_DEFAULT_BRANCH}"
  if git merge-base --is-ancestor "$CI_COMMIT_SHA" "refs/remotes/github/${CI_DEFAULT_BRANCH}"; then
    echo "GitHub already contains this commit; main will not be rewound."
    exit 0
  fi
  github_git push github "${CI_COMMIT_SHA}:refs/heads/${CI_DEFAULT_BRANCH}"
else
  echo "Only default-branch and validated release-tag pipelines may mirror." >&2
  exit 1
fi
